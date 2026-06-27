package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"gopkg.in/yaml.v3"
)

const (
	settingsFileName = "settings.json"
)

var (
	skillNameRE      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	triggerPhraseRE  = regexp.MustCompile(`(?i)\b(use when|prefer for|do not use|do not trigger|trigger when|run at|run when)\b`)
	autodocTagRE     = regexp.MustCompile(`\[autodoc\(([0-9a-f]{8})@([0-9a-f]{8}),\s*([0-9a-f]{8})\)\]`)
	markdownLinkRE   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]+)\)`)
	sideFilePathRE   = regexp.MustCompile(`\b((?:references|scripts|assets)/[A-Za-z0-9._\-/]+)\b`)
	hex8RE           = regexp.MustCompile(`^[0-9a-f]{8}$`)
	weakOpeningRE    = regexp.MustCompile(`(?i)^(this skill|this guide|this document|in this skill|the purpose of this skill|here is)\b`)
	secretPatternsRE = []*regexp.Regexp{
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|token)\s*[:=]\s*['"]?[A-Za-z0-9_\-\/+=]{16,}`),
		regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	}
)

// Env controls how auto skill resolves project/global paths.
type Env struct {
	Root         string
	RootOverride bool
}

func ResolveRoot(cwd, rootFlag string) (string, bool, error) {
	base := cwd
	if strings.TrimSpace(rootFlag) != "" {
		if filepath.IsAbs(rootFlag) {
			base = rootFlag
		} else {
			base = filepath.Join(cwd, rootFlag)
		}
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return "", false, fmt.Errorf("resolve --root: %w", err)
	}
	return abs, strings.TrimSpace(rootFlag) != "", nil
}

func (e Env) SkillsDir() string {
	return filepath.Join(e.Root, "skills")
}

func (e Env) SkillsConfigDir() string {
	return filepath.Join(e.Root, ".auto", "skills")
}

func (e Env) SkillsYAMLPath() string {
	return filepath.Join(e.SkillsConfigDir(), "skills.yaml")
}

func (e Env) LockPath() string {
	return filepath.Join(e.SkillsConfigDir(), "lock.json")
}

func (e Env) ManifestPath() string {
	return filepath.Join(e.SkillsConfigDir(), "manifest.json")
}

func (e Env) UpstreamCacheDir() string {
	if e.RootOverride {
		return filepath.Join(e.Root, ".auto", "skills", "upstream")
	}
	home, err := config.HomeDir()
	if err != nil {
		return filepath.Join(e.Root, ".auto", "skills", "upstream")
	}
	return filepath.Join(home, ".auto", "skills", "upstream")
}

func (e Env) TrustPath() string {
	if e.RootOverride {
		return filepath.Join(e.Root, ".auto", "skills", "trust.json")
	}
	home, err := config.HomeDir()
	if err != nil {
		return filepath.Join(e.Root, ".auto", "skills", "trust.json")
	}
	return filepath.Join(home, ".auto", "skills", "trust.json")
}

func (e Env) ProjectSettingsPath() string {
	return filepath.Join(e.Root, ".auto", "skills", settingsFileName)
}

func (e Env) GlobalSettingsPath() (string, error) {
	if e.RootOverride {
		return filepath.Join(e.Root, ".auto", "skills", settingsFileName), nil
	}
	home, err := config.HomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".auto", "skills", settingsFileName), nil
}

type InitResult struct {
	GlobalPath          string
	GlobalCreated       bool
	ProjectSettingsPath string
	ProjectCreated      bool
	SkillsPath          string
	SkillsCreated       bool
}

func Init(env Env, project bool) (InitResult, error) {
	result := InitResult{}

	if !project {
		globalPath, err := env.GlobalSettingsPath()
		if err != nil {
			return result, err
		}
		globalCreated, err := ensureSettingsFile(globalPath)
		if err != nil {
			return result, err
		}
		result.GlobalPath = globalPath
		result.GlobalCreated = globalCreated

		skillsPath := env.SkillsDir()
		skillsCreated, err := ensureDir(skillsPath)
		if err != nil {
			return result, err
		}
		result.SkillsPath = skillsPath
		result.SkillsCreated = skillsCreated
		return result, nil
	}

	projectPath := env.ProjectSettingsPath()
	projectCreated, err := ensureSettingsFile(projectPath)
	if err != nil {
		return result, err
	}
	result.ProjectSettingsPath = projectPath
	result.ProjectCreated = projectCreated

	skillsPath := env.SkillsDir()
	skillsCreated, err := ensureDir(skillsPath)
	if err != nil {
		return result, err
	}
	result.SkillsPath = skillsPath
	result.SkillsCreated = skillsCreated

	return result, nil
}

type CreateOptions struct {
	Name        string
	Description string
	WithDirs    bool
}

type CreateResult struct {
	SkillDir    string
	SkillFile   string
	CreatedDirs []string
	Diagnostics []Diagnostic
}

func Create(env Env, opts CreateOptions) (CreateResult, error) {
	result := CreateResult{}

	name := strings.TrimSpace(opts.Name)
	description := strings.TrimSpace(opts.Description)
	if err := ValidateSkillName(name); err != nil {
		return result, err
	}
	if description == "" {
		return result, errors.New("missing description: --description is required")
	}

	skillsDir := env.SkillsDir()
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, fmt.Errorf("skills directory does not exist at %s; run: auto skill init --project", displayPath(skillsDir))
		}
		return result, fmt.Errorf("stat %s: %w", displayPath(skillsDir), err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("%s is not a directory; run: auto skill init --project", displayPath(skillsDir))
	}

	skillDir := filepath.Join(skillsDir, name)
	if _, err := os.Stat(skillDir); err == nil {
		return result, fmt.Errorf("skill directory already exists: %s", displayPath(skillDir))
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("stat %s: %w", displayPath(skillDir), err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return result, fmt.Errorf("create skill directory %s: %w", displayPath(skillDir), err)
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	content, err := scaffoldSkillMarkdown(name, description)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return result, fmt.Errorf("write %s: %w", displayPath(skillFile), err)
	}

	createdDirs := []string{}
	if opts.WithDirs {
		for _, sub := range []string{"references", "scripts", "assets"} {
			full := filepath.Join(skillDir, sub)
			if err := os.MkdirAll(full, 0o755); err != nil {
				return result, fmt.Errorf("create %s: %w", displayPath(full), err)
			}
			createdDirs = append(createdDirs, full)
		}
	}

	diags, err := Lint(env, skillDir)
	if err != nil {
		return result, err
	}

	result.SkillDir = skillDir
	result.SkillFile = skillFile
	result.CreatedDirs = createdDirs
	result.Diagnostics = diags
	return result, nil
}

type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

func List(env Env) ([]SkillSummary, []string, error) {
	skillsDir := env.SkillsDir()
	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", displayPath(skillsDir), err)
	}
	if !info.IsDir() {
		return nil, []string{relPath(env.Root, skillsDir) + ": skills root is not a directory"}, nil
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", displayPath(skillsDir), err)
	}

	summaries := make([]SkillSummary, 0, len(entries))
	parseErrors := make([]string, 0)
	for _, entry := range entries {
		full := filepath.Join(skillsDir, entry.Name())
		if !entry.IsDir() {
			parseErrors = append(parseErrors, relPath(env.Root, full)+": not a skill directory")
			continue
		}

		parsed, err := readSkillFile(full)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %s", relPath(env.Root, full), err.Error()))
			continue
		}
		if parsed.Name == "" {
			parseErrors = append(parseErrors, relPath(env.Root, parsed.File)+": missing frontmatter name")
			continue
		}
		if parsed.Description == "" {
			parseErrors = append(parseErrors, relPath(env.Root, parsed.File)+": missing frontmatter description")
			continue
		}

		summaries = append(summaries, SkillSummary{
			Name:        parsed.Name,
			Description: parsed.Description,
			Path:        relPath(env.Root, full),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Name != summaries[j].Name {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].Path < summaries[j].Path
	})

	return summaries, parseErrors, nil
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
	Value    any      `json:"value,omitempty"`
}

func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

func Lint(env Env, target string) ([]Diagnostic, error) {
	docsByID, err := discoverDocsByID(env.Root)
	if err != nil {
		return nil, err
	}

	targets, diags, err := discoverLintTargets(env, target)
	if err != nil {
		return nil, err
	}

	listingBuilder := strings.Builder{}
	for _, t := range targets {
		skillDiags, listingLine := lintSkill(env, t, docsByID)
		diags = append(diags, skillDiags...)
		if listingLine != "" {
			listingBuilder.WriteString(listingLine)
			listingBuilder.WriteByte('\n')
		}
	}

	if listingBuilder.Len() > 0 {
		tokens := estimateTokens(listingBuilder.String())
		switch {
		case tokens > 4000:
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "listing_too_large",
				Path:     relPath(env.Root, env.SkillsDir()),
				Field:    "listing",
				Message:  "aggregate skill listing exceeds 4000 token estimate",
				Value:    tokens,
			})
		case tokens > 2000:
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Code:     "listing_too_large",
				Path:     relPath(env.Root, env.SkillsDir()),
				Field:    "listing",
				Message:  "aggregate skill listing exceeds 2000 token estimate",
				Value:    tokens,
			})
		}
	}

	sortDiagnostics(diags)
	return diags, nil
}

func ValidateSkillName(name string) error {
	if len(name) == 0 {
		return errors.New("missing skill name")
	}
	if len(name) > 64 {
		return fmt.Errorf("invalid skill name %q: name must be <= 64 chars", name)
	}
	if !skillNameRE.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: expected format ^[a-z0-9]+(-[a-z0-9]+)*$", name)
	}
	return nil
}

func FormatDiagnosticsText(diags []Diagnostic) string {
	var b strings.Builder
	for i, d := range diags {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s[%s]: %s\n", d.Severity, d.Code, d.Path)
		fmt.Fprintf(&b, "  %s\n", d.Message)
		if d.Value != nil {
			v := fmt.Sprintf("%v", d.Value)
			for line := range strings.SplitSeq(v, "\n") {
				fmt.Fprintf(&b, "  | %s\n", line)
			}
		}
	}
	return b.String()
}

func EncodeJSON(v any) ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

type lintTarget struct {
	Dir  string
	File string
}

func discoverLintTargets(env Env, target string) ([]lintTarget, []Diagnostic, error) {
	out := []lintTarget{}
	diags := []Diagnostic{}
	seen := map[string]struct{}{}

	addSkillDir := func(dir string) {
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, lintTarget{
			Dir:  clean,
			File: filepath.Join(clean, "SKILL.md"),
		})
	}

	appendChildren := func(base string) error {
		entries, err := os.ReadDir(base)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			full := filepath.Join(base, entry.Name())
			if !entry.IsDir() {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "not_a_directory",
					Path:     relPath(env.Root, full),
					Field:    "path",
					Message:  "skill entry must be a directory containing SKILL.md",
				})
				continue
			}
			addSkillDir(full)
		}
		return nil
	}

	if strings.TrimSpace(target) == "" {
		skillsRoot := env.SkillsDir()
		info, err := os.Stat(skillsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return out, diags, nil
			}
			return nil, nil, fmt.Errorf("stat %s: %w", displayPath(skillsRoot), err)
		}
		if !info.IsDir() {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "not_a_directory",
				Path:     relPath(env.Root, skillsRoot),
				Field:    "path",
				Message:  "skills root must be a directory",
			})
			return out, diags, nil
		}
		if err := appendChildren(skillsRoot); err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", displayPath(skillsRoot), err)
		}
		return out, diags, nil
	}

	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(env.Root, resolved)
	}
	resolved = filepath.Clean(resolved)
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", displayPath(resolved), err)
	}

	if info.Mode().IsRegular() {
		if filepath.Base(resolved) == "SKILL.md" {
			addSkillDir(filepath.Dir(resolved))
			return out, diags, nil
		}
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "not_a_directory",
			Path:     relPath(env.Root, resolved),
			Field:    "path",
			Message:  "skill target must be a directory containing SKILL.md",
		})
		return out, diags, nil
	}

	if !info.IsDir() {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "not_a_directory",
			Path:     relPath(env.Root, resolved),
			Field:    "path",
			Message:  "skill target must be a directory containing SKILL.md",
		})
		return out, diags, nil
	}

	skillFile := filepath.Join(resolved, "SKILL.md")
	if _, err := os.Stat(skillFile); err == nil {
		addSkillDir(resolved)
		return out, diags, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("stat %s: %w", displayPath(skillFile), err)
	}

	if err := appendChildren(resolved); err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", displayPath(resolved), err)
	}
	return out, diags, nil
}

func lintSkill(env Env, target lintTarget, docsByID map[string]string) ([]Diagnostic, string) {
	diags := []Diagnostic{}

	parsed, err := readSkillFile(target.Dir)
	if err != nil {
		diag := Diagnostic{
			Severity: SeverityError,
			Code:     "invalid_frontmatter",
			Path:     relPath(env.Root, target.File),
			Field:    "frontmatter",
			Message:  err.Error(),
		}
		if parsed.Content != "" {
			diag.Value = firstNLines(parsed.Content, 3)
		}
		diags = append(diags, diag)
		return diags, ""
	}

	if parsed.Name == "" {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "missing_name",
			Path:     relPath(env.Root, parsed.File),
			Field:    "name",
			Message:  "frontmatter field name is required",
		})
	} else {
		if len(parsed.Name) > 64 {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "name_too_long",
				Path:     relPath(env.Root, parsed.File),
				Field:    "name",
				Message:  "frontmatter name must be <= 64 chars",
			})
		}
		if !skillNameRE.MatchString(parsed.Name) {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "invalid_name",
				Path:     relPath(env.Root, parsed.File),
				Field:    "name",
				Message:  "frontmatter name must match ^[a-z0-9]+(-[a-z0-9]+)*$",
				Value:    parsed.Name,
			})
		}
		if parsed.Name != filepath.Base(parsed.Dir) {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "name_dir_mismatch",
				Path:     relPath(env.Root, parsed.File),
				Field:    "name",
				Message:  "frontmatter name must match skill directory name",
				Value:    filepath.Base(parsed.Dir),
			})
		}
	}

	if parsed.Description == "" {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "missing_description",
			Path:     relPath(env.Root, parsed.File),
			Field:    "description",
			Message:  "frontmatter field description is required",
		})
	} else {
		if len(parsed.Description) > 1024 {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "description_too_long",
				Path:     relPath(env.Root, parsed.File),
				Field:    "description",
				Message:  "frontmatter description must be <= 1024 chars",
				Value:    len(parsed.Description),
			})
		}
		if !triggerPhraseRE.MatchString(parsed.Description) {
			desc := parsed.Description
			if len(desc) > 80 {
				desc = desc[:80] + "..."
			}
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Code:     "missing_trigger_phrase",
				Path:     relPath(env.Root, parsed.File),
				Field:    "description",
				Message:  "description should include trigger phrases such as \"Use when\" or \"Prefer for\"",
				Value:    desc,
			})
		}
	}

	if parsed.ShortDescription != "" && len(parsed.ShortDescription) > 1024 {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "short_description_too_long",
			Path:     relPath(env.Root, parsed.File),
			Field:    "metadata.short-description",
			Message:  "metadata.short-description must be <= 1024 chars",
			Value:    len(parsed.ShortDescription),
		})
	}

	body := parsed.Body
	bodyTokens := estimateTokens(body)
	if strings.TrimSpace(body) == "" {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "empty_body",
			Path:     relPath(env.Root, parsed.File),
			Field:    "body",
			Message:  "skill body must not be empty",
		})
	} else if hasWeakOpening(body) {
		diags = append(diags, Diagnostic{
			Severity: SeverityWarning,
			Code:     "weak_opening",
			Path:     relPath(env.Root, parsed.File),
			Field:    "body",
			Message:  "body should open with actionable rules or workflow steps, not prose preamble",
			Value:    firstNLines(strings.TrimSpace(body), 2),
		})
	}
	switch {
	case bodyTokens > 8000:
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "body_too_large",
			Path:     relPath(env.Root, parsed.File),
			Field:    "body",
			Message:  "skill body exceeds 8000 token estimate",
			Value:    bodyTokens,
		})
	case bodyTokens > 4000:
		diags = append(diags, Diagnostic{
			Severity: SeverityWarning,
			Code:     "body_too_large",
			Path:     relPath(env.Root, parsed.File),
			Field:    "body",
			Message:  "skill body exceeds 4000 token estimate",
			Value:    bodyTokens,
		})
	}

	for _, match := range autodocTagRE.FindAllStringSubmatch(parsed.Content, -1) {
		docID := match[1]
		hash := match[2]
		if !hex8RE.MatchString(docID) || !hex8RE.MatchString(hash) {
			continue
		}
		currentHash, ok := docsByID[docID]
		if !ok {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "broken_autodoc_link",
				Path:     relPath(env.Root, parsed.File),
				Field:    "autodoc",
				Message:  fmt.Sprintf("autodoc doc id %s does not exist", docID),
			})
			continue
		}
		if hash != currentHash {
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Code:     "stale_autodoc_link",
				Path:     relPath(env.Root, parsed.File),
				Field:    "autodoc",
				Message:  fmt.Sprintf("autodoc hash for doc id %s is stale", docID),
				Value:    currentHash,
			})
		}
	}

	links := extractMarkdownLinks(parsed.Content)
	seenBrokenLinks := map[string]struct{}{}
	for _, rawLink := range links {
		resolved, skip := resolveLocalLink(env.Root, parsed.Dir, rawLink)
		if skip {
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			key := rawLink + "|" + resolved
			if _, ok := seenBrokenLinks[key]; ok {
				continue
			}
			seenBrokenLinks[key] = struct{}{}
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Code:     "broken_local_link",
				Path:     relPath(env.Root, parsed.File),
				Field:    "link",
				Message:  "local markdown link does not resolve: " + rawLink,
				Value:    rawLink,
			})
		}
	}

	for _, sidePath := range extractSideFileMentions(parsed.Body) {
		full := filepath.Join(parsed.Dir, filepath.FromSlash(sidePath))
		if _, err := os.Stat(full); err == nil {
			continue
		}
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "broken_side_file",
			Path:     relPath(env.Root, parsed.File),
			Field:    "side_file",
			Message:  "referenced side file does not exist: " + sidePath,
			Value:    sidePath,
		})
	}

	if hasSecret(parsed.Content) {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Code:     "secret_detected",
			Path:     relPath(env.Root, parsed.File),
			Field:    "content",
			Message:  "potential secret or API key detected in skill content",
		})
	}

	listingLine := ""
	if parsed.Name != "" && parsed.Description != "" {
		listingLine = fmt.Sprintf("- %s: %s", parsed.Name, parsed.Description)
	}
	return diags, listingLine
}

type parsedSkill struct {
	Dir              string
	File             string
	Content          string
	Body             string
	Name             string
	Description      string
	ShortDescription string
}

func readSkillFile(skillDir string) (parsedSkill, error) {
	result := parsedSkill{
		Dir:  filepath.Clean(skillDir),
		File: filepath.Join(filepath.Clean(skillDir), "SKILL.md"),
	}

	data, err := os.ReadFile(result.File)
	if err != nil {
		if os.IsNotExist(err) {
			return result, errors.New("missing SKILL.md in skill directory")
		}
		return result, fmt.Errorf("read %s: %w", displayPath(result.File), err)
	}

	result.Content = string(data)
	front, body, err := parseFrontmatterAndBody(result.Content)
	if err != nil {
		return result, fmt.Errorf("invalid frontmatter: %w", err)
	}
	result.Body = body
	result.Name = readString(front, "name")
	result.Description = readString(front, "description")
	result.ShortDescription = readNestedString(front, "metadata", "short-description")
	return result, nil
}

func parseFrontmatterAndBody(content string) (map[string]any, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, normalized, errors.New("frontmatter must start with ---")
	}

	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	endLen := len("\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			endLen = len("\n---")
		} else {
			return nil, normalized, errors.New("frontmatter closing --- not found")
		}
	}

	yamlBlock := rest[:end]
	body := ""
	if end+endLen < len(rest) {
		body = rest[end+endLen:]
	}

	decoded := map[string]any{}
	if strings.TrimSpace(yamlBlock) != "" {
		if err := yaml.Unmarshal([]byte(yamlBlock), &decoded); err != nil {
			return nil, body, err
		}
	}
	return decoded, body, nil
}

func discoverDocsByID(root string) (map[string]string, error) {
	docsByID := map[string]string{}

	docRoots, err := discoverDocRoots(root)
	if err != nil {
		return nil, err
	}
	for _, docRoot := range docRoots {
		err := filepath.WalkDir(docRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == ".tmp" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			front, _, parseErr := parseFrontmatterAndBody(string(data))
			if parseErr != nil {
				return nil //nolint:nilerr // intentional: skip non-autodoc markdown
			}
			docID := strings.TrimSpace(readString(front, "id"))
			hash := strings.TrimSpace(readString(front, "hash"))
			if !hex8RE.MatchString(docID) || !hex8RE.MatchString(hash) {
				return nil
			}
			if _, exists := docsByID[docID]; !exists {
				docsByID[docID] = hash
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover docs in %s: %w", displayPath(docRoot), err)
		}
	}

	return docsByID, nil
}

func discoverDocRoots(root string) ([]string, error) {
	roots := map[string]struct{}{}

	addIfDir := func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			roots[filepath.Clean(path)] = struct{}{}
		}
		return nil
	}

	if err := addIfDir(filepath.Join(root, "docs")); err != nil {
		return nil, fmt.Errorf("stat docs root: %w", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read project root %s: %w", displayPath(root), err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch name {
		case ".git", ".tmp", ".claude", ".agents", ".auto", "node_modules", "vendor", "dist", "build", "bin":
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		if err := addIfDir(filepath.Join(root, name, "docs")); err != nil {
			return nil, fmt.Errorf("stat nested docs under %s: %w", name, err)
		}
	}

	out := make([]string, 0, len(roots))
	for path := range roots {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func readString(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func readNestedString(m map[string]any, topKey, nestedKey string) string {
	raw, ok := m[topKey]
	if !ok {
		return ""
	}
	switch typed := raw.(type) {
	case map[string]any:
		value, ok := typed[nestedKey]
		if !ok {
			return ""
		}
		s, ok := value.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(s)
	case map[any]any:
		for k, v := range typed {
			ks, ok := k.(string)
			if !ok || ks != nestedKey {
				continue
			}
			vs, ok := v.(string)
			if !ok {
				return ""
			}
			return strings.TrimSpace(vs)
		}
	}
	return ""
}

func extractMarkdownLinks(content string) []string {
	matches := markdownLinkRE.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		link := strings.TrimSpace(m[1])
		if link == "" {
			continue
		}
		if strings.HasPrefix(link, "<") {
			if end := strings.Index(link, ">"); end > 0 {
				link = strings.TrimSpace(link[1:end])
			}
		}
		if fields := strings.Fields(link); len(fields) > 0 {
			link = fields[0]
		}
		link = strings.Trim(link, `"`)
		if link != "" {
			out = append(out, link)
		}
	}
	return out
}

func resolveLocalLink(root, skillDir, link string) (string, bool) {
	lower := strings.ToLower(link)
	if strings.HasPrefix(lower, "#") {
		return "", true
	}
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "tel:") {
		return "", true
	}

	pathOnly := link
	if idx := strings.Index(pathOnly, "#"); idx >= 0 {
		pathOnly = pathOnly[:idx]
	}
	if idx := strings.Index(pathOnly, "?"); idx >= 0 {
		pathOnly = pathOnly[:idx]
	}
	pathOnly = strings.TrimSpace(pathOnly)
	if pathOnly == "" {
		return "", true
	}

	if strings.HasPrefix(pathOnly, "/") {
		return filepath.Clean(filepath.Join(root, pathOnly)), false
	}
	return filepath.Clean(filepath.Join(skillDir, filepath.FromSlash(pathOnly))), false
}

func extractSideFileMentions(body string) []string {
	matches := sideFilePathRE.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	uniq := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		p := strings.TrimSpace(strings.TrimSuffix(m[1], "."))
		if p == "" {
			continue
		}
		p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
		if strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || p == "." {
			continue
		}
		if _, ok := uniq[p]; ok {
			continue
		}
		uniq[p] = struct{}{}
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func hasWeakOpening(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized := strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
		return weakOpeningRE.MatchString(normalized)
	}
	return false
}

func hasSecret(content string) bool {
	for _, pattern := range secretPatternsRE {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func firstNLines(s string, n int) string {
	const maxChars = 200
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxChars {
		out = out[:maxChars] + "..."
	}
	return out
}

func estimateTokens(s string) int {
	chars := len([]rune(s))
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}

func relPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func sortDiagnostics(diags []Diagnostic) {
	rank := func(s Severity) int {
		switch s {
		case SeverityError:
			return 0
		case SeverityWarning:
			return 1
		default:
			return 2
		}
	}

	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Path != diags[j].Path {
			return diags[i].Path < diags[j].Path
		}
		ri, rj := rank(diags[i].Severity), rank(diags[j].Severity)
		if ri != rj {
			return ri < rj
		}
		if diags[i].Code != diags[j].Code {
			return diags[i].Code < diags[j].Code
		}
		return diags[i].Message < diags[j].Message
	})
}

func scaffoldSkillMarkdown(name, description string) (string, error) {
	type metadata struct {
		ShortDescription string `yaml:"short-description"`
	}
	type frontmatter struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Metadata    metadata `yaml:"metadata"`
	}
	frontmatterValue := frontmatter{
		Name:        name,
		Description: description,
		Metadata: metadata{
			ShortDescription: "",
		},
	}

	yamlBytes, err := yaml.Marshal(frontmatterValue)
	if err != nil {
		return "", fmt.Errorf("marshal frontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yamlBytes)
	b.WriteString("---\n\n")
	b.WriteString("## When to use\n\n")
	b.WriteString("Use when the user needs this workflow and constraints should be applied consistently.\n\n")
	b.WriteString("## Workflow\n\n")
	b.WriteString("1. Confirm scope, constraints, and expected output.\n")
	b.WriteString("2. Execute the workflow steps directly.\n")
	b.WriteString("3. Verify results and report any follow-up actions.\n\n")
	b.WriteString("## Load on demand\n\n")
	b.WriteString("Load references and scripts only when they are required for the current request.\n\n")
	b.WriteString("## Output requirements\n\n")
	b.WriteString("Return concise, actionable output with explicit errors and remediation hints.\n\n")
	b.WriteString("## Avoid\n\n")
	b.WriteString("Avoid speculative steps, hidden assumptions, and irreversible actions without confirmation.\n")
	return b.String(), nil
}

func ensureSettingsFile(path string) (bool, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", displayPath(parent), err)
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", displayPath(path), err)
	}

	defaults := map[string]any{
		"schemaVersion": 1,
	}
	data, err := EncodeJSON(defaults)
	if err != nil {
		return false, fmt.Errorf("marshal %s: %w", displayPath(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", displayPath(path), err)
	}
	return true, nil
}

func ensureDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s exists and is not a directory", displayPath(path))
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", displayPath(path), err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", displayPath(path), err)
	}
	return true, nil
}

func DisplayPath(cwd, path string) string {
	normalized := filepath.ToSlash(path)
	cleanCWD := strings.TrimSpace(cwd)
	if cleanCWD == "" {
		return normalized
	}
	rel, err := filepath.Rel(cleanCWD, path)
	if err != nil {
		return normalized
	}
	if rel == "." {
		return "."
	}
	if strings.HasPrefix(rel, "..") {
		return normalized
	}
	return filepath.ToSlash(rel)
}

func displayPath(path string) string {
	return DisplayPath("", path)
}
