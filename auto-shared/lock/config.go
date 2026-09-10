package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	sharedconfig "github.com/mistakenot/auto-shared/config"
)

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
		name := strings.TrimSpace(g.Name)
		switch {
		case name == "":
			add("missing_name", p+".name", "name", "group name is required", nil)
		default:
			if prev, dup := seen[name]; dup {
				add("duplicate_name", p+".name", "name",
					"group name duplicates groups["+strconv.Itoa(prev)+"]", name)
			}
			seen[name] = i
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
