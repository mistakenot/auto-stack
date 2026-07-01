// Package add orchestrates the full skill-add pipeline: parse source → resolve
// version → discover skills → select → write lock.json + skills.yaml stubs.
// It wires the source, discovery, cache, trust, and schema layers but does not
// re-implement their logic.
package add

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/discovery"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/source"
	"github.com/mistakenot/auto-skill/internal/trace"
	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/mistakenot/auto-skill/internal/trust"
	"gopkg.in/yaml.v3"
)

// Options controls Run behavior.
type Options struct {
	Source         string
	Skills         []string // --skill filter (repeatable, or "*" for all)
	Paths          []string // --path filter (repeatable)
	List           bool     // preview + exit
	FullDepth      bool
	NoSync         bool   // accepted now (always true until T4)
	Force          bool   // allow overwriting existing ./skills/<name> on local import
	TrustRequested bool   // pass through to trust gate
	Version        string // --version override
	As             string // --as rename (single-skill only)
	Format         string // "json" or "text"
	Trace          *trace.Logger
}

// Result is the pipeline output.
type Result struct {
	Added  []AddedSkill  `json:"added,omitempty"`
	Listed []ListedSkill `json:"listed,omitempty"`
	Source string        `json:"source"`
}

// AddedSkill records a skill written to lock + skills.yaml.
type AddedSkill struct {
	Name        string `json:"name"`
	Subpath     string `json:"subpath"`
	Commit      string `json:"commit"`
	VersionSpec string `json:"version_spec"`
	Local       bool   `json:"local,omitempty"`
}

// ListedSkill records a discovered skill in --list mode.
type ListedSkill struct {
	Name      string `json:"name"`
	Subpath   string `json:"subpath"`
	NameValid bool   `json:"name_valid"`
	NeedsAs   bool   `json:"needs_as,omitempty"`
	Container string `json:"container"`
}

// ── Error types ─────────────────────────────────────────────────────────

// AddError is a typed pipeline error with a machine-readable code.
type AddError struct {
	Code    string
	Message string
}

func (e *AddError) Error() string { return e.Message }

const (
	CodeSkillNotFound    = "skill_not_found"
	CodeAsMultiSkill     = "as_multi_skill"
	CodeInvalidSkillName = "invalid_skill_name"
	CodeNameCollision    = "name_collision"
	CodeImportCollision  = "import_collision"
)

// ── Pipeline ────────────────────────────────────────────────────────────

// Run orchestrates: parse → resolve → discover → select → write lock + stub.
func Run(env skill.Env, opts Options) (Result, error) {
	tr := opts.Trace
	doneRun := trace.Spanf(tr, "add run")
	defer doneRun("")

	// 1. Parse source.
	done := trace.Spanf(tr, "add parse source")
	src, err := source.ParseSource(opts.Source, source.ParseOptions{
		Version: opts.Version,
	})
	if err != nil {
		done("error=%v", err)
		return Result{Source: opts.Source}, err
	}
	done("local=%t url=%s subpath=%s ref=%s", src.Local, src.URL, src.Subpath, src.Ref)

	// 2. Local source split.
	if src.Local {
		trace.Logf(tr, "add handling local source")
		return handleLocal(env, src, opts)
	}

	// 3. Resolve version.
	versionSpec := opts.Version
	if versionSpec == "" {
		versionSpec = "latest"
	}
	if ve := skill.ValidateVersionSpec(versionSpec); ve != nil {
		return Result{Source: src.URL}, fmt.Errorf("invalid version spec: %s", ve.Message)
	}
	trace.Logf(tr, "add version spec=%s", versionSpec)

	// Load skills.yaml up front so the trust gate can honor the project's
	// declared trusted_hosts: in non-TTY usage --trust-requested only
	// auto-approves an endpoint that the project opted into here.
	done = trace.Spanf(tr, "add load skills.yaml")
	syaml, err := loadOrCreateSkillsYAML(env)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	done("skills=%d trusted_hosts=%d", len(syaml.Skills), len(syaml.TrustedHosts))

	// Trust gate.
	done = trace.Spanf(tr, "add authorize source")
	ep, err := transport.Endpoint(src.URL)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	store := trust.NewStore(env.TrustPath())
	gate := &trust.Gate{Store: store}
	gio := trust.GateIO{IsTTY: false, TrustRequested: opts.TrustRequested}
	if err := gate.Authorize(ep, syaml.TrustedHosts, gio); err != nil {
		done("endpoint=%s error=%v", ep, err)
		return Result{Source: src.URL}, err
	}
	done("endpoint=%s", ep)

	// Open repo in cache.
	done = trace.Spanf(tr, "add canonicalize source")
	_, cacheID, err := transport.CanonicalizeURL(src.URL)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	done("cache_id=%s", cacheID.RelPath())
	c := cache.NewCache(env.UpstreamCacheDir()).WithTrace(tr)
	repo, err := c.Open(cacheID, src.URL)
	if err != nil {
		return Result{Source: src.URL}, fmt.Errorf("open cache: %w", err)
	}

	// Resolve a GitHub/GitLab deep-link (/tree/<ref>/<subpath>) now that the
	// repo's ref set is available. ParseSource can only split the ref from the
	// subpath with a live resolver; the initial parse above ran without one
	// (we needed the canonical URL first to open the cache). An explicit
	// --version pins the ref and takes precedence, so skip when it is set.
	if opts.Version == "" {
		done = trace.Spanf(tr, "add resolve deep-link source")
		resolved, err := source.ParseSource(opts.Source, source.ParseOptions{
			RefResolver: &repoRefResolver{repo: repo},
		})
		if err != nil {
			done("error=%v", err)
			return Result{Source: src.URL}, err
		}
		src.Ref = resolved.Ref
		src.Subpath = resolved.Subpath
		done("subpath=%s ref=%s", src.Subpath, src.Ref)
	}

	// Resolve ref.
	done = trace.Spanf(tr, "add resolve ref")
	sha, err := resolveRef(repo, versionSpec, src.Ref)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	done("commit=%s", shortTraceSHA(sha))

	// Realize objects.
	if err := repo.Realize(sha); err != nil {
		return Result{Source: src.URL}, fmt.Errorf("realize commit %s: %w", sha, err)
	}

	// 4. Extract to temp dir.
	done = trace.Spanf(tr, "add create temp dir")
	tmpDir, err := os.MkdirTemp("", "auto-skill-add-*")
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("create temp dir: %w", err)
	}
	done("dir=%s", tmpDir)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	subpath := src.Subpath
	done = trace.Spanf(tr, "add extract for discovery")
	if err := extractForDiscovery(repo, sha, subpath, tmpDir); err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("extract: %w", err)
	}
	done("subpath=%s", subpath)

	// 5. Discover.
	done = trace.Spanf(tr, "add discover skills")
	discOpts := discovery.Options{
		Paths:     opts.Paths,
		FullDepth: opts.FullDepth,
	}
	discovered, err := discovery.Discover(tmpDir, discOpts)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("discover: %w", err)
	}
	done("discovered=%d paths=%d full_depth=%t", len(discovered), len(opts.Paths), opts.FullDepth)

	// 6. Selection.
	done = trace.Spanf(tr, "add select skills")
	selected, err := applySelection(discovered, opts)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	done("selected=%d", len(selected))

	// 7. List mode.
	if opts.List {
		trace.Logf(tr, "add list mode selected=%d", len(selected))
		listed := make([]ListedSkill, len(selected))
		for i, d := range selected {
			listed[i] = ListedSkill{
				Name:      effectiveName(d, opts.As, len(selected)),
				Subpath:   d.Subpath,
				NameValid: d.NameValid,
				NeedsAs:   !d.NameValid,
				Container: d.Container,
			}
		}
		return Result{Listed: listed, Source: src.URL}, nil
	}

	// 8. Write lock + skills.yaml stubs.
	done = trace.Spanf(tr, "add load lock")
	lock, err := loadOrCreateLock(env)
	if err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, err
	}
	done("entries=%d", len(lock.Skills))

	var added []AddedSkill
	for _, d := range selected {
		name := effectiveName(d, opts.As, len(selected))

		// Validate final name.
		if err := skill.ValidateSkillName(name); err != nil {
			return Result{Source: src.URL}, &AddError{
				Code:    CodeInvalidSkillName,
				Message: fmt.Sprintf("skill %q has invalid name; use --as to provide a valid name: %s", d.Name, err),
			}
		}

		// Check collision from different source.
		if existing, ok := lock.Skills[name]; ok {
			if existing.URL != src.URL {
				return Result{Source: src.URL}, &AddError{
					Code:    CodeNameCollision,
					Message: fmt.Sprintf("skill %q already exists from %s; use --as to rename", name, existing.URL),
				}
			}
		}

		entrySubpath := d.Subpath
		if subpath != "" && entrySubpath != "." {
			entrySubpath = subpath + "/" + entrySubpath
		} else if subpath != "" {
			entrySubpath = subpath
		}

		lock.Skills[name] = skill.LockEntry{
			Source:      src.URL,
			URL:         src.URL,
			VersionSpec: versionSpec,
			Ref:         sha,
			Commit:      sha,
			Subpath:     entrySubpath,
			State:       "resolved",
		}

		// Write skills.yaml stub — preserve existing replacements.
		if _, exists := syaml.Skills[name]; !exists {
			if syaml.Skills == nil {
				syaml.Skills = make(map[string]skill.SkillConfig)
			}
			syaml.Skills[name] = skill.SkillConfig{
				Version: versionSpec,
			}
		}

		added = append(added, AddedSkill{
			Name:        name,
			Subpath:     entrySubpath,
			Commit:      sha,
			VersionSpec: versionSpec,
		})
	}

	// Validate before writing.
	if lockErrs := skill.ValidateLock(lock); len(lockErrs) > 0 {
		return Result{Source: src.URL}, &config.ValidationErrorsError{
			Path:   env.LockPath(),
			Errors: lockErrs,
		}
	}
	if yamlErrs := skill.ValidateSkillsYAML(syaml); len(yamlErrs) > 0 {
		return Result{Source: src.URL}, &config.ValidationErrorsError{
			Path:   env.SkillsYAMLPath(),
			Errors: yamlErrs,
		}
	}
	trace.Logf(tr, "add validation passed lock_entries=%d skills_yaml=%d", len(lock.Skills), len(syaml.Skills))

	// Ensure config dir exists.
	done = trace.Spanf(tr, "add write config")
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("create config dir: %w", err)
	}

	if err := config.WriteJSONFileAtomic(env.LockPath(), lock); err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("write lock: %w", err)
	}
	if err := writeSkillsYAML(env.SkillsYAMLPath(), syaml); err != nil {
		done("error=%v", err)
		return Result{Source: src.URL}, fmt.Errorf("write skills.yaml: %w", err)
	}
	done("added=%d", len(added))

	return Result{Added: added, Source: src.URL}, nil
}

func shortTraceSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// ── Helpers ─────────────────────────────────────────────────────────────

// extractForDiscovery materializes the repo content discovery needs into tmpDir.
// When the source pins a subpath (a deep-link URL), only that subtree is
// extracted. Otherwise, rather than archive the whole tree — which for a large
// monorepo is slow and may contain unrelated symlinks the safe extractor
// rejects — it lists the skill subtrees in the commit and extracts only those.
func extractForDiscovery(repo *cache.Repo, sha, subpath, tmpDir string) error {
	if subpath != "" {
		return repo.Extract(sha, subpath, tmpDir)
	}
	dirs, err := repo.ListSkillDirs(sha)
	if err != nil {
		return err
	}
	// No skill trees found, or a skill lives at the repo root: fall back to a
	// full extract so discovery (and its error reporting) behaves as before.
	if len(dirs) == 0 || slices.Contains(dirs, "") {
		return repo.Extract(sha, "", tmpDir)
	}
	return repo.ExtractPaths(sha, dirs, tmpDir)
}

// repoRefResolver adapts a cache.Repo to source.RefResolver for deep-link
// splitting: a candidate prefix is a ref iff the repo can resolve it.
type repoRefResolver struct{ repo *cache.Repo }

func (r *repoRefResolver) ResolveRef(ref string) bool {
	_, err := r.repo.ResolveRef(ref)
	return err == nil
}

// resolveRef maps a versionSpec + optional deep-link ref to a commit SHA.
func resolveRef(repo *cache.Repo, versionSpec, deepLinkRef string) (string, error) {
	// Deep-link ref takes precedence if version wasn't explicitly set.
	if deepLinkRef != "" && versionSpec == "latest" {
		sha, err := resolveRemoteRef(repo, deepLinkRef)
		if err != nil {
			return "", fmt.Errorf("resolve deep-link ref %q: %w", deepLinkRef, err)
		}
		return sha, nil
	}

	switch {
	case versionSpec == "latest":
		sha, err := resolveRemoteRef(repo, "HEAD")
		if err != nil {
			return "", fmt.Errorf("resolve HEAD: %w", err)
		}
		return sha, nil

	case strings.HasPrefix(versionSpec, "branch:"):
		branch := strings.TrimPrefix(versionSpec, "branch:")
		sha, err := resolveRemoteRef(repo, branch)
		if err != nil {
			return "", fmt.Errorf("resolve branch %q: %w", branch, err)
		}
		return sha, nil

	case strings.HasPrefix(versionSpec, "tag:"):
		tag := strings.TrimPrefix(versionSpec, "tag:")
		sha, err := resolveRemoteRef(repo, tag)
		if err != nil {
			return "", fmt.Errorf("resolve tag %q: %w", tag, err)
		}
		return sha, nil

	case strings.HasPrefix(versionSpec, "commit:"):
		commitHex := strings.TrimPrefix(versionSpec, "commit:")
		return commitHex, nil

	default:
		// Bare string — try as ref.
		sha, err := resolveRemoteRef(repo, versionSpec)
		if err != nil {
			return "", fmt.Errorf("resolve ref %q: %w", versionSpec, err)
		}
		return sha, nil
	}
}

// resolveRemoteRef refreshes a floating remote ref before resolving it. A cached
// bare repo's HEAD can be stale for long-lived users; fetching the requested ref
// keeps `add <source> --skill X` aligned with the upstream repository.
func resolveRemoteRef(repo *cache.Repo, ref string) (string, error) {
	if err := repo.Realize(ref); err != nil {
		return "", err
	}
	if sha, err := repo.ResolveRef("FETCH_HEAD^{commit}"); err == nil {
		return sha, nil
	}
	return repo.ResolveRef("FETCH_HEAD")
}

// applySelection filters discovered skills by --skill and validates --as.
func applySelection(discovered []discovery.Discovered, opts Options) ([]discovery.Discovered, error) {
	if len(discovered) == 0 {
		return nil, &AddError{
			Code:    CodeSkillNotFound,
			Message: "no skills found in source; ensure the source contains a SKILL.md file",
		}
	}

	// No --skill filter or --skill "*" → take all.
	if len(opts.Skills) == 0 {
		return validateAs(discovered, opts)
	}
	if len(opts.Skills) == 1 && opts.Skills[0] == "*" {
		return validateAs(discovered, opts)
	}

	// Filter by name.
	nameSet := make(map[string]bool, len(opts.Skills))
	for _, s := range opts.Skills {
		nameSet[strings.ToLower(strings.TrimSpace(s))] = true
	}

	var selected []discovery.Discovered
	for _, d := range discovered {
		if nameSet[strings.ToLower(d.Name)] {
			selected = append(selected, d)
			delete(nameSet, strings.ToLower(d.Name))
		}
	}

	if len(nameSet) > 0 {
		var available []string
		for _, d := range discovered {
			available = append(available, d.Name)
		}
		var missing []string
		for k := range nameSet {
			missing = append(missing, k)
		}
		return nil, &AddError{
			Code:    CodeSkillNotFound,
			Message: fmt.Sprintf("skill(s) not found: %s; available: %s", strings.Join(missing, ", "), strings.Join(available, ", ")),
		}
	}

	return validateAs(selected, opts)
}

// validateAs ensures --as is used only with single-skill selection.
func validateAs(selected []discovery.Discovered, opts Options) ([]discovery.Discovered, error) {
	if opts.As != "" && len(selected) != 1 {
		return nil, &AddError{
			Code:    CodeAsMultiSkill,
			Message: fmt.Sprintf("--as can only be used when exactly 1 skill is selected, but %d were found", len(selected)),
		}
	}
	return selected, nil
}

// effectiveName returns the name to use for a discovered skill, applying --as
// when applicable.
func effectiveName(d discovery.Discovered, as string, total int) string {
	if as != "" && total == 1 {
		return as
	}
	return d.Name
}

// loadOrCreateLock reads existing lock.json or returns a new empty Lock.
func loadOrCreateLock(env skill.Env) (*skill.Lock, error) {
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.Lock{Version: 1, Skills: map[string]skill.LockEntry{}}, nil
		}
		return nil, fmt.Errorf("read lock: %w", err)
	}
	lock, err := skill.ParseLock(data)
	if err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.LockEntry{}
	}
	return lock, nil
}

// loadOrCreateSkillsYAML reads existing skills.yaml or returns a new empty one.
func loadOrCreateSkillsYAML(env skill.Env) (*skill.SkillsYAML, error) {
	data, err := os.ReadFile(env.SkillsYAMLPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.SkillsYAML{Skills: map[string]skill.SkillConfig{}}, nil
		}
		return nil, fmt.Errorf("read skills.yaml: %w", err)
	}
	cfg, err := skill.ParseSkillsYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse skills.yaml: %w", err)
	}
	if cfg.Skills == nil {
		cfg.Skills = make(map[string]skill.SkillConfig)
	}
	return cfg, nil
}

// writeSkillsYAML marshals and writes skills.yaml.
func writeSkillsYAML(path string, cfg *skill.SkillsYAML) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal skills.yaml: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
