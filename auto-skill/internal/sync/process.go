package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/discovery"
	"github.com/mistakenot/auto-skill/internal/render"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
	"github.com/mistakenot/auto-skill/internal/transport"
	"gopkg.in/yaml.v3"
)

// tokenBudgetWarn is the advisory body-size threshold (token estimate, chars/4).
// `sync` only WARNS at this level and never blocks a render — `lint` is the gate
// (G-token-budget). 4000 tokens mirrors lint's warn threshold.
const tokenBudgetWarn = 4000

// Action values for an Install decision.
const (
	InstallWrite = "write"
	InstallSkip  = "skip"
)

// StagedSkill is one fully rendered skill ready for installation into every
// target. The Files are the canonical rendered tree (no provenance stamp — the
// sync pipeline does not stamp, so the on-disk tree hashes byte-for-byte back to
// SkillVersion). Phase 5 stages these via WriteSkillDir / StageSkillDir.
type StagedSkill struct {
	Name         string                       `json:"name"`
	Source       string                       `json:"source"` // "authored" or canonical repo URL
	Commit       string                       `json:"commit,omitempty"`
	SkillVersion string                       `json:"skill_version"`
	TemplateHash string                       `json:"template_hash"`
	Files        []render.TreeFile            `json:"-"`
	Replacements map[string]string            `json:"replacements,omitempty"`
	FileRefs     []render.ResolvedFileRefInfo `json:"file_refs,omitempty"`
	Warnings     []string                     `json:"warnings,omitempty"`
	ForcedRender bool                         `json:"forced_render,omitempty"` // re-rendered for a render_version bump
}

// Install is the per-(target, skill) decision phase 5 acts on: write the staged
// tree into Dir/Skill, or skip because the on-disk tree already matches.
type Install struct {
	Target string `json:"target"` // target style name
	Dir    string `json:"dir"`    // target skills directory
	Skill  string `json:"skill"`  // skill name
	Action string `json:"action"` // InstallWrite | InstallSkip
	OnDisk string `json:"on_disk,omitempty"`
	Want   string `json:"want"` // expected skill_version
}

// ProcessResult is phase C's output: the rendered skills, the per-target install
// decisions, and the populated (validated) manifest. Phase 5 consumes this to
// run the journaled commit (stage → swap → receipts → manifest → lock → clear);
// it writes nothing itself.
type ProcessResult struct {
	Targets  []Target        `json:"targets"`
	Staged   []*StagedSkill  `json:"staged"`
	Installs []Install       `json:"installs"`
	Manifest *skill.Manifest `json:"-"`
	Warnings []string        `json:"warnings,omitempty"`
	Errors   []error         `json:"-"`
}

// HasErrors reports whether any per-skill processing error was collected.
func (r *ProcessResult) HasErrors() bool { return len(r.Errors) > 0 }

// skillSource identifies where a skill's input bytes come from for rendering.
type skillSource struct {
	name     string
	authored bool
	sourceID string // "authored" or canonical repo URL
	commit   string // upstream commit ("" for authored)
	rootDir  string // directory holding SKILL.md + side files (extracted temp, or ./skills/<name>)
	cleanup  func()
}

// Process runs phase C: it extracts each vendored skill from the cache, discovers
// authored ./skills/** skills, renders the union (authored shadows vendored on a
// name clash), computes each skill_version, decides write-vs-skip per target
// against the on-disk tree digest (with a one-time render_version lazy re-render),
// emits advisory token-budget warnings, and populates the manifest. Render
// parallelism is bounded by --jobs. Output is identical regardless of skill
// order (determinism). It performs NO writes — staging + the journaled commit are
// phase 5's job; Process only renders, reads on-disk digests, and plans.
func Process(env skill.Env, plan *Plan, fetch *FetchResult, opts Options) (*ProcessResult, error) {
	tr := opts.Trace
	done := trace.Spanf(tr, "sync process load config")
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		done("error=%v", err)
		return nil, err
	}
	done("skills=%d", len(syaml.Skills))
	targets := resolveTargets(env, syaml)
	// Hard guard: refuse to render into any target that escapes the project root
	// (absolute or ".."-traversing). ValidateSkillsYAML rejects these too, but the
	// sync engine must never write outside root even if a hand-edited skills.yaml
	// bypasses that check — this is the arbitrary-write defense (H3).
	if terr := guardTargetsWithinRoot(env, targets); terr != nil {
		done("error=%v", terr)
		return nil, terr
	}
	oldManifest := loadManifestBestEffort(env)
	oldManifestSkills := 0
	if oldManifest != nil {
		oldManifestSkills = len(oldManifest.Skills)
	}
	trace.Logf(tr, "sync process targets=%d old_manifest_skills=%d", len(targets), oldManifestSkills)

	result := &ProcessResult{Targets: targets}

	done = trace.Spanf(tr, "sync gather sources")
	sources, srcErrs := gatherSources(env, plan, fetch, tr)
	result.Errors = append(result.Errors, srcErrs...)
	done("sources=%d errors=%d", len(sources), len(srcErrs))
	defer func() {
		for _, s := range sources {
			if s.cleanup != nil {
				s.cleanup()
			}
		}
	}()

	// Render every source, bounded by --jobs. Results are index-aligned so the
	// output is independent of completion order (determinism).
	staged := make([]*StagedSkill, len(sources))
	renderErrs := make([]error, len(sources))
	boundedRun(opts.jobs(), indexes(len(sources)), func(i int) error {
		s := sources[i]
		done := trace.Spanf(tr, "sync render skill %s source=%s", s.name, s.sourceID)
		st, rerr := renderSource(syaml, s)
		if rerr != nil {
			renderErrs[i] = fmt.Errorf("render %s: %w", s.name, rerr)
			done("error=%v", rerr)
			return nil
		}
		// render_version lazy re-render: a manifest entry recorded below the
		// engine's current render_version forces a one-time re-render even when
		// the on-disk digest still matches the old output.
		st.ForcedRender = renderVersionStale(oldManifest, s.name)
		staged[i] = st
		done("files=%d refs=%d warnings=%d forced=%t version=%s",
			len(st.Files), len(st.FileRefs), len(st.Warnings), st.ForcedRender, short(st.SkillVersion))
		return nil
	})

	for _, e := range renderErrs {
		if e != nil {
			result.Errors = append(result.Errors, e)
		}
	}

	for _, st := range staged {
		if st == nil {
			continue
		}
		result.Staged = append(result.Staged, st)
		result.Warnings = append(result.Warnings, st.Warnings...)
	}
	sort.Slice(result.Staged, func(i, j int) bool { return result.Staged[i].Name < result.Staged[j].Name })

	// Per-target install decisions (deterministic order: target, then skill).
	for _, t := range targets {
		trace.Logf(tr, "sync install compare target=%s dir=%s staged=%d", t.Name, t.Dir, len(result.Staged))
		for _, st := range result.Staged {
			inst := Install{
				Target: t.Name,
				Dir:    t.Dir,
				Skill:  st.Name,
				Want:   st.SkillVersion,
				Action: InstallWrite,
			}
			disk, exists, derr := onDiskDigest(filepath.Join(t.Dir, st.Name))
			if derr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("digest %s/%s: %w", t.Name, st.Name, derr))
			}
			inst.OnDisk = disk
			if exists && !st.ForcedRender && disk == st.SkillVersion {
				inst.Action = InstallSkip
			}
			result.Installs = append(result.Installs, inst)
			trace.Logf(tr, "sync install decision target=%s skill=%s action=%s on_disk=%s want=%s",
				inst.Target, inst.Skill, inst.Action, short(inst.OnDisk), short(inst.Want))
		}
	}

	done = trace.Spanf(tr, "sync build manifest")
	manifest, mErrs := buildManifest(result.Staged, targets)
	if len(mErrs) > 0 {
		done("errors=%d", len(mErrs))
		return result, fmt.Errorf("manifest validation failed: %s", joinValidation(mErrs))
	}
	done("skills=%d targets=%d", len(manifest.Skills), len(manifest.Targets))
	// A scoped (--target) run stages only the targeted skills (plus authored), so
	// the freshly built manifest omits every non-targeted vendored skill. Carry
	// their prior ownership forward so the write does not disown them — a dropped
	// skill is later misread as a foreign dir and wedges the next full sync.
	if scope := normalizeNames(opts.Targets); scope != nil {
		manifest = mergeScopedManifest(oldManifest, manifest, scope)
		if vErrs := skill.ValidateManifest(manifest); len(vErrs) > 0 {
			return result, fmt.Errorf("merged manifest validation failed: %s", joinValidation(vErrs))
		}
		trace.Logf(tr, "sync merged scoped manifest preserved_scope=%d skills=%d", len(scope), len(manifest.Skills))
	} else if oldManifest != nil {
		// Full (non-scoped) run: a skill that FAILED to materialize this run
		// (render error, failed fetch, missing commit) but is still declared —
		// present in the lock plan or authored — must keep its prior manifest
		// ownership. Otherwise its already-rendered target dirs drop out of the
		// managed set and the next sync reclassifies them as "foreign", wedging
		// with a conflict error. This is the transactional-manifest fix for the
		// foreign-target cluster (H1, H5, M1–M4): the manifest write carries a
		// failed skill's previous entry forward rather than disowning it. A skill
		// genuinely removed from the lock/authored source is absent from
		// `intended`, so it still drops out and is pruned as designed.
		intended := intendedNames(plan, sources)
		manifest = mergeFailedRenders(oldManifest, manifest, intended)
		if vErrs := skill.ValidateManifest(manifest); len(vErrs) > 0 {
			return result, fmt.Errorf("carried-forward manifest validation failed: %s", joinValidation(vErrs))
		}
		trace.Logf(tr, "sync carried forward failed-render manifest intended=%d skills=%d", len(intended), len(manifest.Skills))
	}
	result.Manifest = manifest
	return result, nil
}

// gatherSources builds the rendered-input source for every skill in the union of
// vendored (from the plan/lock) and authored (./skills/**), with authored
// shadowing vendored on a name clash. Vendored skills are extracted from the
// cache into temp dirs (cleaned up by the caller via cleanup); authored skills
// point at their working-tree directory.
func gatherSources(env skill.Env, plan *Plan, fetch *FetchResult, tr *trace.Logger) ([]*skillSource, []error) {
	var errs []error
	byName := map[string]*skillSource{}

	// Vendored skills first (authored will overwrite on clash).
	if plan != nil {
		failed := failedRepoKeys(fetch)
		c := cache.NewCache(env.UpstreamCacheDir()).WithTrace(tr)
		for i := range plan.Skills {
			sp := plan.Skills[i]
			if !sp.processable() {
				trace.Logf(tr, "sync source skip skill=%s action=%s", sp.Name, sp.Action)
				continue
			}
			if failed[sp.Repo] {
				errs = append(errs, fmt.Errorf("skip %s: repo %s failed to fetch", sp.Name, sp.Repo))
				trace.Logf(tr, "sync source skip skill=%s failed_repo=%s", sp.Name, sp.Repo)
				continue
			}
			src, err := extractVendored(c, sp, tr)
			if err != nil {
				errs = append(errs, err)
				trace.Logf(tr, "sync source vendored error skill=%s error=%v", sp.Name, err)
				continue
			}
			byName[src.name] = src
			trace.Logf(tr, "sync source vendored skill=%s root=%s", src.name, src.rootDir)
		}
	}

	// Authored ./skills/** shadow vendored on a name clash.
	authored, aerrs := discoverAuthored(env)
	errs = append(errs, aerrs...)
	trace.Logf(tr, "sync source authored discovered=%d errors=%d", len(authored), len(aerrs))
	for _, src := range authored {
		if prev, ok := byName[src.name]; ok && prev.cleanup != nil {
			prev.cleanup() // drop the shadowed vendored extract
			trace.Logf(tr, "sync source authored shadows vendored skill=%s", src.name)
		}
		byName[src.name] = src
	}

	out := make([]*skillSource, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, errs
}

// extractVendored materializes a vendored skill's subtree from the cache into a
// temp directory rooted at the skill itself (the file-ref resolver root).
func extractVendored(c *cache.Cache, sp SkillPlan, tr *trace.Logger) (*skillSource, error) {
	done := trace.Spanf(tr, "sync extract vendored skill=%s commit=%s subpath=%s", sp.Name, short(sp.TargetCommit), sp.Subpath)
	canonical, cacheID, err := transport.CanonicalizeURL(sp.URL)
	if err != nil {
		done("error=%v", err)
		return nil, fmt.Errorf("canonicalize %s (%s): %w", sp.Name, sp.URL, err)
	}
	repo, err := c.Open(cacheID, sp.URL)
	if err != nil {
		done("error=%v", err)
		return nil, fmt.Errorf("open cache for %s: %w", sp.Name, err)
	}
	if present, perr := repo.CommitPresent(sp.TargetCommit); perr != nil || !present {
		if perr == nil {
			perr = errors.New("missing objects")
		}
		done("error=%v", perr)
		return nil, fmt.Errorf("skip %s: commit %s not present in cache: %w", sp.Name, short(sp.TargetCommit), perr)
	}
	dest, err := os.MkdirTemp("", "auto-skill-extract-"+sp.Name+"-*")
	if err != nil {
		done("error=%v", err)
		return nil, fmt.Errorf("temp dir for %s: %w", sp.Name, err)
	}
	if err := repo.Extract(sp.TargetCommit, sp.Subpath, dest); err != nil {
		_ = os.RemoveAll(dest)
		// The commit is present (checked above), so a missing subpath means the
		// path was renamed or removed upstream — report it with remediation
		// instead of a raw extract failure.
		if errors.Is(err, cache.ErrSubpathNotFound) {
			done("error=%v", err)
			return nil, &RenamedUpstreamError{Name: sp.Name, Subpath: sp.Subpath, Commit: sp.TargetCommit}
		}
		done("error=%v", err)
		return nil, fmt.Errorf("extract %s (%s:%s): %w", sp.Name, short(sp.TargetCommit), sp.Subpath, err)
	}
	d := dest
	done("root=%s", dest)
	return &skillSource{
		name:     sp.Name,
		authored: false,
		sourceID: canonical,
		commit:   sp.TargetCommit,
		rootDir:  dest,
		cleanup:  func() { _ = os.RemoveAll(d) },
	}, nil
}

// discoverAuthored finds authored skills under ./skills (only — the agent
// output directories are NOT scanned, they are sync's own write targets).
func discoverAuthored(env skill.Env) ([]*skillSource, []error) {
	found, err := discovery.Discover(env.Root, discovery.Options{Paths: []string{"skills"}})
	if err != nil {
		return nil, []error{fmt.Errorf("discover authored skills: %w", err)}
	}
	var errs []error
	out := make([]*skillSource, 0, len(found))
	for _, d := range found {
		if !d.NameValid {
			errs = append(errs, fmt.Errorf("authored skill at %s has invalid name %q", d.Subpath, d.Name))
			continue
		}
		out = append(out, &skillSource{
			name:     d.Name,
			authored: true,
			sourceID: "authored",
			rootDir:  filepath.Join(env.Root, d.Subpath),
		})
	}
	return out, errs
}

// renderSource reads a skill's SKILL.md template + side files from its root dir,
// resolves replacement values from skills.yaml, renders the canonical tree, and
// attaches an advisory token-budget warning when the body estimate is large.
func renderSource(syaml *skill.SkillsYAML, s *skillSource) (*StagedSkill, error) {
	skillMD, err := os.ReadFile(filepath.Join(s.rootDir, render.SkillMDPath))
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	files, err := readSideFiles(s.rootDir)
	if err != nil {
		return nil, err
	}
	values, err := replacementValues(syaml, s.name)
	if err != nil {
		return nil, err
	}

	in := render.RenderInput{
		SkillMD:  skillMD,
		Values:   values,
		Files:    files,
		Resolver: render.NewFileRefResolver(s.rootDir),
	}
	tree, err := render.Render(in)
	if err != nil {
		return nil, err
	}

	st := &StagedSkill{
		Name:         s.name,
		Source:       s.sourceID,
		Commit:       s.commit,
		SkillVersion: tree.SkillVersion,
		TemplateHash: tree.TemplateHash,
		Files:        tree.Files,
		Replacements: tree.Replacements,
		FileRefs:     tree.FileRefs,
		Warnings:     append([]string(nil), tree.Warnings...),
	}
	if w := tokenBudgetWarning(s.name, tree.Files); w != "" {
		st.Warnings = append(st.Warnings, w)
	}
	return st, nil
}

// readSideFiles walks a skill directory and returns every regular file EXCEPT
// SKILL.md as a render.InputFile (render owns SKILL.md). Symlinks are skipped.
func readSideFiles(root string) ([]render.InputFile, error) {
	var files []render.InputFile
	err := walkFiles(root, func(rel string, data []byte, info os.FileInfo) {
		if rel == render.SkillMDPath {
			return
		}
		files = append(files, render.InputFile{Path: rel, Mode: modeForFileInfo(info), Data: data})
	})
	if err != nil {
		return nil, fmt.Errorf("read side files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// tokenBudgetWarning returns an advisory warning when the SKILL.md body estimate
// exceeds the warn threshold, or "" otherwise. It NEVER errors or blocks.
func tokenBudgetWarning(name string, files []render.TreeFile) string {
	for _, f := range files {
		if f.Path != render.SkillMDPath {
			continue
		}
		est := estimateTokens(string(f.Data))
		if est > tokenBudgetWarn {
			return fmt.Sprintf("skill %q: SKILL.md ~%d tokens exceeds the %d advisory budget (sync only warns; run `auto skill lint` to gate)", name, est, tokenBudgetWarn)
		}
		break
	}
	return ""
}

// estimateTokens is the shared chars/4 token estimate (mirrors skill.estimateTokens).
func estimateTokens(s string) int {
	chars := len([]rune(s))
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}

// replacementValues resolves the named replacement values for a skill from
// skills.yaml, merging shared.replacements (lowest precedence) with the skill's
// own replacements. The schema types `replacements` as a named map (var name →
// value) where value is a literal scalar or a file-ref mapping (`{file,
// section?, include_heading?, strip_frontmatter?}`); this is the design's NAMED
// replacement model (remote-skills-design §Customization). A skill's own value
// for a var overrides the shared default of the same name.
func replacementValues(syaml *skill.SkillsYAML, name string) (map[string]render.ReplacementValue, error) {
	out := map[string]render.ReplacementValue{}
	if syaml == nil {
		return out, nil
	}
	apply := func(reps map[string]yaml.Node) error {
		// Deterministic order for stable error reporting.
		varNames := make([]string, 0, len(reps))
		for varName := range reps {
			varNames = append(varNames, varName)
		}
		sort.Strings(varNames)
		for _, varName := range varNames {
			node := reps[varName]
			rv, err := nodeToReplacement(&node)
			if err != nil {
				return fmt.Errorf("replacement %q: %w", varName, err)
			}
			out[varName] = rv
		}
		return nil
	}
	if err := apply(syaml.Shared.Replacements); err != nil {
		return nil, err
	}
	if sc, ok := syaml.Skills[name]; ok {
		if err := apply(sc.Replacements); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// nodeToReplacement converts a value node into a ReplacementValue: a scalar is a
// literal, a mapping with a "file" key is a file-ref.
func nodeToReplacement(n *yaml.Node) (render.ReplacementValue, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		return render.ReplacementValue{Literal: n.Value}, nil
	case yaml.MappingNode:
		ref, err := nodeToFileRef(n)
		if err != nil {
			return render.ReplacementValue{}, err
		}
		return render.ReplacementValue{FileRef: ref}, nil
	default:
		return render.ReplacementValue{}, errors.New("replacement value must be a literal string or a file-ref mapping")
	}
}

// nodeToFileRef decodes a file-ref mapping node into a render.FileRef.
func nodeToFileRef(n *yaml.Node) (*render.FileRef, error) {
	var raw struct {
		File             string   `yaml:"file"`
		Section          []string `yaml:"section"`
		IncludeHeading   bool     `yaml:"include_heading"`
		StripFrontmatter *bool    `yaml:"strip_frontmatter"`
	}
	if err := n.Decode(&raw); err != nil {
		// section may be a scalar; retry with a single-string section.
		var raw2 struct {
			File             string `yaml:"file"`
			Section          string `yaml:"section"`
			IncludeHeading   bool   `yaml:"include_heading"`
			StripFrontmatter *bool  `yaml:"strip_frontmatter"`
		}
		if err2 := n.Decode(&raw2); err2 != nil {
			return nil, fmt.Errorf("decode file-ref: %w", err)
		}
		ref := &render.FileRef{File: raw2.File, IncludeHeading: raw2.IncludeHeading, StripFrontmatter: raw2.StripFrontmatter}
		if strings.TrimSpace(raw2.Section) != "" {
			ref.Section = []string{raw2.Section}
		}
		return ref, nil
	}
	return &render.FileRef{
		File:             raw.File,
		Section:          raw.Section,
		IncludeHeading:   raw.IncludeHeading,
		StripFrontmatter: raw.StripFrontmatter,
	}, nil
}

// renderVersionStale reports whether the manifest's recorded render_version for
// name is below the engine's current render_version constant — the trigger for a
// one-time lazy re-render.
func renderVersionStale(m *skill.Manifest, name string) bool {
	if m == nil {
		return false
	}
	ms, ok := m.Skills[name]
	if !ok {
		return false
	}
	rv, err := strconv.Atoi(strings.TrimSpace(ms.RenderVersion))
	if err != nil {
		// An unparseable recorded version is treated as stale (force re-render).
		return true
	}
	return rv < render.RenderVersion
}

// loadManifestBestEffort loads the existing manifest.json, returning nil when it
// is absent or unreadable (a missing/garbled manifest just means "render fresh").
func loadManifestBestEffort(env skill.Env) *skill.Manifest {
	data, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		return nil
	}
	m, err := skill.ParseManifest(data)
	if err != nil {
		return nil
	}
	return m
}

// failedRepoKeys returns the set of repo keys that failed phase B (their skills
// must not be rendered from a partial cache).
func failedRepoKeys(fetch *FetchResult) map[string]bool {
	out := map[string]bool{}
	if fetch == nil {
		return out
	}
	for _, f := range fetch.Failed {
		out[f.Key] = true
	}
	return out
}

// processable reports whether a planned skill has a usable target commit to
// render (errors / unavailable / intent-only entries are not rendered).
func (sp SkillPlan) processable() bool {
	switch sp.Action {
	case ActionError, ActionUnavailable, ActionIntentChanged:
		return false
	}
	return strings.TrimSpace(sp.TargetCommit) != ""
}

func indexes(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
