package render

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileRef is a replacement that inlines content from a repo file (or a section
// of one). Phase 2 implements resolution; phase 1 wires the type and interface.
type FileRef struct {
	// File is the repo-relative path to inline.
	File string
	// Section, when non-empty, selects a heading-bounded section. A single
	// element matches by heading text; a multi-element path disambiguates
	// nested headings (e.g. ["A","B"]).
	Section []string
	// IncludeHeading keeps the matched heading line in the extracted content.
	IncludeHeading bool
	// StripFrontmatter controls leading-YAML-frontmatter stripping. nil means
	// the default (strip); a non-nil false keeps the frontmatter.
	StripFrontmatter *bool
}

// ReplacementValue is a single resolved-or-resolvable replacement: exactly one
// of Literal / FileRef is meaningful (FileRef != nil selects the file-ref form).
type ReplacementValue struct {
	// Literal is an inline string value.
	Literal string
	// FileRef, when non-nil, resolves to inlined repo content via a
	// FileRefResolver.
	FileRef *FileRef
}

// ResolvedRef is the output of resolving a FileRef: the raw inlined content plus
// the digest of exactly those bytes and provenance for the manifest.
type ResolvedRef struct {
	// Content is the raw inlined text (never re-templated).
	Content string
	// ContentHash is sha256 over the canonical inlined bytes.
	ContentHash string
	// MatchedHeading is the heading actually matched (empty for whole-file refs).
	MatchedHeading string
	// Warnings carries non-fatal notices (e.g. an ambiguous heading match).
	Warnings []string
}

// FileRefResolver resolves a FileRef to inlined content, enforcing repo
// containment on the fully symlink-resolved real path. It is a pure interface so
// render stays a leaf; phase 2 supplies the implementation scoped to a repo root.
type FileRefResolver interface {
	Resolve(ref FileRef) (ResolvedRef, error)
}

// RenderInput is the full input to a single skill render.
type RenderInput struct {
	// SkillMD is the SKILL.md template bytes (frontmatter + body). The customize:
	// block lives in the frontmatter; the body is the restricted template.
	SkillMD []byte
	// Values are the replacement values from skills.yaml, keyed by var name.
	Values map[string]ReplacementValue
	// Files are the side files (references/scripts/assets), skill-relative.
	Files []InputFile
	// Resolver resolves file-ref replacement values. May be nil when no Values
	// use the file-ref form.
	Resolver FileRefResolver
	// Provenance, when set, stamps metadata.auto_skill into the emitted SKILL.md
	// after the digest (excluded from it).
	Provenance *Provenance
}

// InputFile is a raw side file supplied to render.
type InputFile struct {
	// Path is the skill-relative slash-separated path.
	Path string
	// Mode is the git file mode ("100644" or "100755"); empty defaults to file.
	Mode string
	// Data is the raw bytes.
	Data []byte
}

// ResolvedFileRefInfo records a resolved file-ref for manifest population.
type ResolvedFileRefInfo struct {
	Var            string
	Path           string
	ContentHash    string
	MatchedHeading string
}

// Tree is the canonical rendered output: the emitted files plus the derived
// state phases 4 needs to populate the manifest.
type Tree struct {
	// Files are the emitted files (SKILL.md + side files), sorted by path. The
	// SKILL.md entry includes the provenance stamp when Provenance was set.
	Files []TreeFile
	// SkillVersion is the full-tree digest, computed BEFORE the stamp is added.
	SkillVersion string
	// TemplateHash is sha256 over the canonicalized SKILL.md template input.
	TemplateHash string
	// Replacements are the resolved string values used for substitution.
	Replacements map[string]string
	// FileRefs records resolved file-ref provenance for the manifest.
	FileRefs []ResolvedFileRefInfo
	// Warnings carries non-fatal notices accumulated during render.
	Warnings []string
}

// SkillMDPath is the canonical path of the templated entry file.
const SkillMDPath = "SKILL.md"

// Render is the pure render function: it parses the SKILL.md template, resolves
// replacement values against the customize schema, executes the restricted
// template with raw substitution, canonicalizes the full tree, computes
// skill_version over every emitted file, and finally (if Provenance is set)
// stamps metadata.auto_skill into the emitted SKILL.md — excluded from the
// digest. It performs no code execution and touches no network or cache.
func Render(in RenderInput) (Tree, error) {
	front, body, err := splitFrontmatter(in.SkillMD)
	if err != nil {
		return Tree{}, fmt.Errorf("render: %w", err)
	}

	schema, err := ParseCustomize(front)
	if err != nil {
		return Tree{}, fmt.Errorf("render: %w", err)
	}

	tmpl, err := ParseTemplate(body)
	if err != nil {
		return Tree{}, err
	}

	var warnings []string
	var fileRefs []ResolvedFileRefInfo

	// Resolve replacement values to flat strings (literals verbatim; file-refs
	// via the resolver).
	supplied := make(map[string]string, len(in.Values))
	// Deterministic iteration for stable warning/manifest order.
	varNames := make([]string, 0, len(in.Values))
	for name := range in.Values {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)
	for _, name := range varNames {
		v := in.Values[name]
		if v.FileRef != nil {
			if in.Resolver == nil {
				return Tree{}, &CustomizeError{
					ErrCode: "missing_resolver",
					Var:     name,
					Message: fmt.Sprintf("missing_resolver: replacement %q is a file-ref but no FileRefResolver was provided", name),
				}
			}
			resolved, err := in.Resolver.Resolve(*v.FileRef)
			if err != nil {
				return Tree{}, fmt.Errorf("render: resolve file-ref %q: %w", name, err)
			}
			supplied[name] = resolved.Content
			warnings = append(warnings, resolved.Warnings...)
			fileRefs = append(fileRefs, ResolvedFileRefInfo{
				Var:            name,
				Path:           v.FileRef.File,
				ContentHash:    resolved.ContentHash,
				MatchedHeading: resolved.MatchedHeading,
			})
		} else {
			supplied[name] = v.Literal
		}
	}

	values, err := ResolveValues(schema, tmpl.Vars(), supplied)
	if err != nil {
		return Tree{}, err
	}

	renderedBody, err := tmpl.Render(values)
	if err != nil {
		return Tree{}, err
	}

	// Assemble the emitted SKILL.md: frontmatter (customize: stripped) + body.
	skillMD, err := assembleSkillMD(front, renderedBody)
	if err != nil {
		return Tree{}, fmt.Errorf("render: %w", err)
	}

	// Build the canonical tree (SKILL.md + side files), de-duplicated by path
	// with SKILL.md authoritative.
	files := []TreeFile{newTreeFile(SkillMDPath, ModeFile, []byte(skillMD))}
	seen := map[string]bool{SkillMDPath: true}
	for _, f := range in.Files {
		path := normalizePath(f.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, newTreeFile(path, f.Mode, f.Data))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	// Compute skill_version over the UNSTAMPED tree.
	skillVersion := ComputeSkillVersion(files)

	// Stamp metadata.auto_skill into SKILL.md AFTER the digest (excluded).
	if in.Provenance != nil {
		stamped, err := stampSkillMD(front, renderedBody, *in.Provenance, skillVersion)
		if err != nil {
			return Tree{}, fmt.Errorf("render: %w", err)
		}
		for i := range files {
			if files[i].Path == SkillMDPath {
				files[i] = newTreeFile(SkillMDPath, ModeFile, []byte(stamped))
				break
			}
		}
	}

	return Tree{
		Files:        files,
		SkillVersion: skillVersion,
		TemplateHash: sha256Hex(canonicalizeText(in.SkillMD)),
		Replacements: values,
		FileRefs:     fileRefs,
		Warnings:     warnings,
	}, nil
}

// normalizePath converts a path to slash form and strips leading "./" segments.
// It trims surrounding slashes first, then loops stripping any leading "./" so
// inputs like "/./x" or "././x" reach a fixed point (normalizePath is
// idempotent). It deliberately does NOT use path.Clean — ".." and "//" keep
// their original meaning; only leading "./" and surrounding slashes are removed.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.Trim(p, "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
		p = strings.Trim(p, "/")
	}
	return p
}

// splitFrontmatter splits SKILL.md content into its raw YAML frontmatter block
// (without the --- fences) and the body. It mirrors skill.parseFrontmatterAndBody
// but returns the raw frontmatter text so render can manipulate it as a YAML
// node. Content with no frontmatter returns an empty front and the whole body.
func splitFrontmatter(content []byte) (front string, body string, err error) {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		// No frontmatter — treat the whole input as body.
		return "", normalized, nil
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	endLen := len("\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			endLen = len("\n---")
		} else {
			return "", normalized, errors.New("frontmatter closing --- not found")
		}
	}
	front = rest[:end]
	if end+endLen < len(rest) {
		body = rest[end+endLen:]
	}
	return front, body, nil
}

// assembleSkillMD reconstructs the emitted SKILL.md from the template
// frontmatter (with the customize: block removed) and the rendered body.
func assembleSkillMD(front, body string) (string, error) {
	cleaned, err := transformFrontmatter(front, nil)
	if err != nil {
		return "", err
	}
	return composeSkillMD(cleaned, body), nil
}

// stampSkillMD reconstructs SKILL.md with the customize: block removed and the
// metadata.auto_skill stamp added.
func stampSkillMD(front, body string, p Provenance, skillVersion string) (string, error) {
	stamp := ProvenanceStamp(p, skillVersion)
	cleaned, err := transformFrontmatter(front, stamp)
	if err != nil {
		return "", err
	}
	return composeSkillMD(cleaned, body), nil
}

// composeSkillMD joins a frontmatter block and body with --- fences. When there
// is no frontmatter, the body is emitted as-is.
func composeSkillMD(front, body string) string {
	if strings.TrimSpace(front) == "" {
		return body
	}
	front = strings.TrimRight(front, "\n")
	return "---\n" + front + "\n---\n" + body
}

// transformFrontmatter parses the YAML frontmatter, removes the customize: key,
// and (when stamp != nil) sets metadata.auto_skill, re-marshalling
// deterministically. An empty frontmatter passes through unchanged.
func transformFrontmatter(front string, stamp map[string]any) (string, error) {
	if strings.TrimSpace(front) == "" {
		return front, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return "", fmt.Errorf("parse frontmatter: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("frontmatter must be a YAML mapping")
	}
	mapping := doc.Content[0]

	deleteMappingKey(mapping, "customize")

	if stamp != nil {
		metaNode := getOrCreateMapping(mapping, "metadata")
		var stampNode yaml.Node
		if err := stampNode.Encode(stamp); err != nil {
			return "", fmt.Errorf("encode provenance stamp: %w", err)
		}
		setMappingKey(metaNode, "auto_skill", &stampNode)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("marshal frontmatter: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// deleteMappingKey removes a key (and its value) from a mapping node.
func deleteMappingKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// getOrCreateMapping returns the mapping node stored at key, creating an empty
// mapping if absent.
func getOrCreateMapping(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, keyNode, valNode)
	return valNode
}

// setMappingKey sets key=value in a mapping node, replacing an existing value.
func setMappingKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping.Content = append(mapping.Content, keyNode, value)
}
