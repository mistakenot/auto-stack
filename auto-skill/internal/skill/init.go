package skill

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/git"
	"gopkg.in/yaml.v3"
)

const autoskillSnippet = `**auto skill** — Author and lint reusable agent skills. Run ` + "`auto skill quickstart`" + ` to learn more.`

var agentFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"}

// InitProjectOptions holds the wizard/flag values for project initialization.
type InitProjectOptions struct {
	Targets        []string
	AutoUpdate     bool
	CommitTargets  bool
	DefaultVersion string
}

// DefaultInitProjectOptions returns the default values for project init.
func DefaultInitProjectOptions() InitProjectOptions {
	return InitProjectOptions{
		Targets:        []string{"claude", "agents"},
		AutoUpdate:     true,
		CommitTargets:  true,
		DefaultVersion: "latest",
	}
}

// GlobalInitResult reports what global init created or found.
type GlobalInitResult struct {
	SettingsPath    string
	SettingsCreated bool
	AgentFiles      []string
}

// ProjectInitResult reports what project init created or found.
type ProjectInitResult struct {
	SkillsYAMLPath    string
	SkillsYAMLCreated bool
	LockPath          string
	LockCreated       bool
	SkillsDir         string
	SkillsDirCreated  bool
	ProjectID         string
	AgentFiles        []string
}

// InitGlobal writes ~/.auto/skills/settings.json with machine defaults.
// Idempotent — skips if the file already exists and is valid.
func InitGlobal(env Env) (GlobalInitResult, error) {
	var result GlobalInitResult

	globalPath, err := env.GlobalSettingsPath()
	if err != nil {
		return result, err
	}
	result.SettingsPath = globalPath

	parent := filepath.Dir(globalPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return result, err
	}

	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		defaults := map[string]any{
			"schemaVersion": 1,
			"cache_dir":     "",
			"jobs":          8,
		}
		data, marshalErr := EncodeJSON(defaults)
		if marshalErr != nil {
			return result, marshalErr
		}
		if writeErr := os.WriteFile(globalPath, data, 0o644); writeErr != nil {
			return result, writeErr
		}
		result.SettingsCreated = true
	} else if err != nil {
		return result, err
	}

	result.AgentFiles = ensureAgentSnippets(env.Root)
	return result, nil
}

// InitProject scaffolds the .auto/skills/ tree, writes skills.yaml + empty
// lock.json, creates ./skills/, manages .gitignore entries, ensures the
// projects.json registry entry, and applies agent-file snippets. Idempotent.
func InitProject(env Env, opts InitProjectOptions) (ProjectInitResult, error) {
	var result ProjectInitResult

	// Build and validate skills.yaml content.
	cfg := SkillsYAML{
		AutoUpdate:    opts.AutoUpdate,
		Targets:       opts.Targets,
		CommitTargets: opts.CommitTargets,
		Shared: SharedConfig{
			Version: opts.DefaultVersion,
		},
	}
	if verrs := ValidateSkillsYAML(&cfg); len(verrs) > 0 {
		return result, &config.ValidationErrorsError{
			Path:   "skills.yaml",
			Errors: verrs,
		}
	}

	// Ensure .auto/skills/ directory.
	configDir := env.SkillsConfigDir()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return result, err
	}

	// Write skills.yaml (idempotent — skip if already exists).
	yamlPath := env.SkillsYAMLPath()
	result.SkillsYAMLPath = yamlPath
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		data, marshalErr := yaml.Marshal(&cfg)
		if marshalErr != nil {
			return result, marshalErr
		}
		if writeErr := os.WriteFile(yamlPath, data, 0o644); writeErr != nil {
			return result, writeErr
		}
		result.SkillsYAMLCreated = true
	} else if err != nil {
		return result, err
	}

	// Write empty valid lock.json (idempotent).
	lockPath := env.LockPath()
	result.LockPath = lockPath
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		emptyLock := Lock{Version: 1, Skills: map[string]LockEntry{}}
		data, marshalErr := EncodeJSON(emptyLock)
		if marshalErr != nil {
			return result, marshalErr
		}
		if writeErr := os.WriteFile(lockPath, data, 0o644); writeErr != nil {
			return result, writeErr
		}
		result.LockCreated = true
	} else if err != nil {
		return result, err
	}

	// Ensure ./skills/ directory.
	skillsDir := env.SkillsDir()
	result.SkillsDir = skillsDir
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(skillsDir, 0o755); mkErr != nil {
			return result, mkErr
		}
		result.SkillsDirCreated = true
	} else if err != nil {
		return result, err
	}

	// Manage .gitignore entries.
	if err := ensureGitignoreEntries(env.Root, opts.CommitTargets); err != nil {
		return result, err
	}

	// Ensure projects.json entry (create-if-absent, append "skill" tool).
	projectID, err := ensureProjectRegistration(env)
	if err != nil {
		return result, err
	}
	result.ProjectID = projectID

	// Apply agent-file snippets (CLAUDE.md, AGENTS.md, GEMINI.md).
	result.AgentFiles = ensureAgentSnippets(env.Root)

	return result, nil
}

// ensureAgentSnippets appends the auto skill snippet to CLAUDE.md, AGENTS.md,
// and GEMINI.md if not already present. Idempotent and symlink-safe.
// Returns the list of files that were updated.
func ensureAgentSnippets(root string) []string {
	var updated []string
	seen := make(map[string]bool)
	for _, name := range agentFiles {
		p := filepath.Join(root, name)
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
		}
		ok, _ := ensureSkillSnippet(p)
		if ok {
			updated = append(updated, name)
		}
	}
	return updated
}

// ensureSkillSnippet checks if the file contains the auto skill snippet.
// If not, it appends it. Returns true if the file was modified.
func ensureSkillSnippet(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, os.WriteFile(path, []byte(autoskillSnippet+"\n"), 0o644)
		}
		return false, err
	}
	content := string(data)
	if strings.Contains(content, autoskillSnippet) {
		return false, nil
	}
	if !strings.HasSuffix(content, "\n") && len(content) > 0 {
		content += "\n"
	}
	content += "\n" + autoskillSnippet + "\n"
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// ensureGitignoreEntries adds .auto/skills/.sync-journal to .gitignore, and
// optionally target dirs when commit_targets is false.
func ensureGitignoreEntries(root string, commitTargets bool) error {
	gitignorePath := filepath.Join(root, ".gitignore")
	existing := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	entries := []string{".auto/skills/.sync-journal"}
	if !commitTargets {
		entries = append(entries, "CLAUDE.md", "AGENTS.md", "GEMINI.md")
	}

	var additions []string
	for _, entry := range entries {
		if !containsLine(existing, entry) {
			additions = append(additions, entry)
		}
	}

	if len(additions) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(existing)
	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		b.WriteByte('\n')
	}
	for _, a := range additions {
		b.WriteString(a)
		b.WriteByte('\n')
	}
	return os.WriteFile(gitignorePath, []byte(b.String()), 0o644)
}

func containsLine(content, line string) bool {
	for l := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// ensureProjectRegistration creates or updates the ~/.auto/projects.json
// entry for this repo, appending "skill" to tools if not present.
func ensureProjectRegistration(env Env) (string, error) {
	path, cfg, _, err := config.EnsureProjects()
	if err != nil {
		return "", err
	}

	root := env.Root
	existing := cfg.FindProjectByExactPath(root)
	if existing != nil {
		if !slices.Contains(existing.Tools, "skill") {
			existing.Tools = append(existing.Tools, "skill")
			config.UpsertProject(&cfg, *existing)
			if saveErr := config.SaveProjects(path, cfg); saveErr != nil {
				return "", saveErr
			}
		}
		return existing.ID, nil
	}

	// Build a new ProjectRef.
	name := filepath.Base(root)
	id := config.SlugifyID(name)
	remote := discoverRemote(root)

	ref := config.ProjectRef{
		ID:     id,
		Path:   root,
		Remote: remote,
		Name:   name,
		Tools:  []string{"skill"},
	}
	config.UpsertProject(&cfg, ref)
	if saveErr := config.SaveProjects(path, cfg); saveErr != nil {
		return "", saveErr
	}
	return id, nil
}

// discoverRemote tries to get the normalized git remote origin URL.
func discoverRemote(root string) string {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return git.NormalizeRemoteURL(strings.TrimSpace(string(out)))
}
