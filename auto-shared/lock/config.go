package lock

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// GroupNamePattern is the slug every Group name must match: lowercase
// letters and digits, with single `-` or `_` separators, never leading or
// trailing (e.g. drizzle-schema, api_v2). A Group is referenced by name on the
// command line (`take <group>`) and in deny messages, so the same rule is
// enforced on the config (Validate) and on the `take`/`release`/`clear`
// argument (ValidGroupName) — one schema for stored data and filter input.
const GroupNamePattern = `^[a-z0-9]+(?:[-_][a-z0-9]+)*$`

var groupNameRE = regexp.MustCompile(GroupNamePattern)

// NormalizeGroupName applies the input normalization a command-line group
// argument gets before it is validated and looked up: trimmed and lowercased,
// so `take Drizzle-Schema ` finds drizzle-schema.
func NormalizeGroupName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ValidGroupName reports whether name matches GroupNamePattern.
func ValidGroupName(name string) bool {
	return groupNameRE.MatchString(name)
}

// ConfigPath returns <repoRoot>/.auto/lock/settings.json.
func ConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".auto", "lock", "settings.json")
}

// LoadConfig reads the project's lock settings. It returns (nil, nil) when the
// file is absent — the project has not opted in — and a
// *sharedconfig.ValidationErrorsError when the file parses but fails Validate.
// Identity defaults to "auto".
func LoadConfig(repoRoot string) (*Config, error) {
	path := ConfigPath(repoRoot)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := sharedconfig.DecodeJSONFile(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.Identity == "" {
		cfg.Identity = IdentityAuto
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, &sharedconfig.ValidationErrorsError{Path: path, Errors: errs}
	}
	return &cfg, nil
}

// Validate checks the config against the settings.json schema and returns every
// violation, so callers can report them together. It is the single validator
// shared by the CLI and the guard.
func (c *Config) Validate() []sharedconfig.ValidationError {
	var errs []sharedconfig.ValidationError
	add := func(code, path, field, message string, value any) {
		errs = append(errs, sharedconfig.ValidationError{Code: code, Path: path, Field: field, Message: message, Value: value})
	}

	switch c.Identity {
	case "", IdentityAuto, IdentityWorktree, IdentityAgent:
	default:
		add("invalid_identity", "identity", "identity",
			"identity must be one of auto, worktree, agent", c.Identity)
	}

	seen := map[string]int{}
	for i, g := range c.Groups {
		p := "groups[" + strconv.Itoa(i) + "]"
		switch {
		case strings.TrimSpace(g.Name) == "":
			add("missing_name", p+".name", "name", "group name is required", nil)
		case !ValidGroupName(g.Name):
			add("invalid_name", p+".name", "name",
				"group name must match "+GroupNamePattern+" (lowercase slug, e.g. drizzle-schema)", g.Name)
		default:
			if prev, dup := seen[g.Name]; dup {
				add("duplicate_name", p+".name", "name",
					"group name duplicates groups["+strconv.Itoa(prev)+"]", g.Name)
			}
			seen[g.Name] = i
		}
		if strings.TrimSpace(g.Description) == "" {
			add("missing_description", p+".description", "description",
				"group description is required (it is shown to blocked agents as the reason the files are serial)", nil)
		}
		if len(g.Globs) == 0 {
			add("missing_globs", p+".globs", "globs", "group needs at least one glob", nil)
		}
		for j, glob := range g.Globs {
			gp := p + ".globs[" + strconv.Itoa(j) + "]"
			if strings.TrimSpace(glob) == "" {
				add("empty_glob", gp, "globs", "glob must not be empty", glob)
				continue
			}
			if !doublestar.ValidatePattern(glob) {
				add("invalid_glob", gp, "globs", "glob is not a valid pattern", glob)
			}
		}
	}
	return errs
}

// Group returns the named group, or nil.
func (c *Config) Group(name string) *Group {
	if c == nil {
		return nil
	}
	for i := range c.Groups {
		if c.Groups[i].Name == name {
			return &c.Groups[i]
		}
	}
	return nil
}

// MatchGroups returns every group with a glob matching rel, a slash-separated
// repo-relative path, in config order.
func (c *Config) MatchGroups(rel string) []Group {
	if c == nil || rel == "" {
		return nil
	}
	var out []Group
	for _, g := range c.Groups {
		for _, glob := range g.Globs {
			if ok, err := doublestar.Match(glob, rel); err == nil && ok {
				out = append(out, g)
				break
			}
		}
	}
	return out
}
