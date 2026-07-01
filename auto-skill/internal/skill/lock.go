package skill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/transport"
)

// Lock is the typed representation of .auto/skills/lock.json — dependency
// identity only, no derived render state.
type Lock struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
}

// LockEntry pins a single skill to a resolved git source.
type LockEntry struct {
	Source      string `json:"source"`
	URL         string `json:"url"`
	VersionSpec string `json:"version_spec"`
	Ref         string `json:"ref"`
	Commit      string `json:"commit"`
	Subpath     string `json:"subpath"`
	Private     bool   `json:"private"`
	Local       bool   `json:"local"`
	State       string `json:"state"`
}

// ParseLock strictly decodes lock.json, rejecting unknown keys (including any
// derived render fields, which belong in manifest.json). It also rejects
// structurally-invalid skill keys at parse time — an empty name, a name failing
// the canonical `^[a-z0-9]+(-[a-z0-9]+)*$` regex (path separators, mixed case),
// or two keys that differ only in case — so a hand-edited or merge-corrupted lock
// can never reach the sync engine and crash it with a raw `rename ... invalid
// argument` (M8) or silently overwrite one target with another on a
// case-insensitive filesystem (H4).
func ParseLock(data []byte) (*Lock, error) {
	var lock Lock
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lock); err != nil {
		return nil, err
	}
	if err := validateLockSkillKeys(lock.Skills); err != nil {
		return nil, err
	}
	return &lock, nil
}

// validateLockSkillKeys enforces skill-name integrity on the raw lock keys before
// any command uses them: each key must be a valid skill name, and no two keys may
// collide when lowercased.
func validateLockSkillKeys(skills map[string]LockEntry) error {
	seen := make(map[string]string, len(skills))
	for name := range skills {
		if name == "" {
			return errors.New("lock.json has a skill with an empty name; remove or rename the entry")
		}
		if !skillNameRE.MatchString(name) {
			return fmt.Errorf("lock.json skill key %q must match %s; rename it to lowercase kebab-case", name, skillNameRE.String())
		}
		lower := strings.ToLower(name)
		if prev, ok := seen[lower]; ok {
			return fmt.Errorf("lock.json has skill keys %q and %q that differ only in case; keep one", prev, name)
		}
		seen[lower] = name
	}
	return nil
}

// ValidateLock checks structural rules and credential-free URLs. It does not
// resolve or fetch anything.
func ValidateLock(lock *Lock) []config.ValidationError {
	var errs []config.ValidationError
	if lock == nil {
		return errs
	}

	for name, entry := range lock.Skills {
		path := "skills." + name
		if !skillNameRE.MatchString(name) {
			errs = append(errs, config.ValidationError{
				Code:    CodeInvalidSkillName,
				Path:    path,
				Field:   "name",
				Message: fmt.Sprintf("skill key %q must match %s; rename the key to lowercase kebab-case", name, skillNameRE.String()),
				Value:   name,
			})
		}

		switch entry.State {
		case "resolved", "unresolved":
		default:
			errs = append(errs, config.ValidationError{
				Code:    CodeInvalidState,
				Path:    path + ".state",
				Field:   "state",
				Message: fmt.Sprintf("state %q is invalid; set state to \"resolved\" or \"unresolved\"", entry.State),
				Value:   entry.State,
			})
		}

		if entry.State == "resolved" {
			errs = append(errs, requireField(entry.Source, path, "source")...)
			errs = append(errs, requireField(entry.URL, path, "url")...)
			errs = append(errs, requireField(entry.Commit, path, "commit")...)
		}

		if ve := checkURLCredentials(entry.URL, path+".url"); ve != nil {
			errs = append(errs, *ve)
		}
	}

	return errs
}

// requireField reports a required error when a string field is empty.
func requireField(value, basePath, field string) []config.ValidationError {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	return []config.ValidationError{{
		Code:    CodeRequired,
		Path:    basePath + "." + field,
		Field:   field,
		Message: fmt.Sprintf("%s is required when state is \"resolved\"; set %s or mark the entry unresolved", field, field),
	}}
}

// checkURLCredentials delegates to transport.ContainsCredentials (single
// source of truth for credential detection — decision D-2).
func checkURLCredentials(rawURL, path string) *config.ValidationError {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	if transport.ContainsCredentials(rawURL) {
		return &config.ValidationError{
			Code:    CodeCredentialsInURL,
			Path:    path,
			Field:   "url",
			Message: "url must not embed credentials; use git's credential helper instead",
			Value:   rawURL,
		}
	}
	return nil
}
