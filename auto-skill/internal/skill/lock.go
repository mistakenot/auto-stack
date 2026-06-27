package skill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mistakenot/auto-shared/config"
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

// credentialQueryKeys are query parameters that carry secrets in a clone URL.
var credentialQueryKeys = []string{"token", "access_token", "private_token"}

// ParseLock strictly decodes lock.json, rejecting unknown keys (including any
// derived render fields, which belong in manifest.json).
func ParseLock(data []byte) (*Lock, error) {
	var lock Lock
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lock); err != nil {
		return nil, err
	}
	return &lock, nil
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

// checkURLCredentials rejects URLs that embed userinfo or credential query
// parameters. Suggests the normalized, credential-free form as remediation.
func checkURLCredentials(rawURL, path string) *config.ValidationError {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// Unparseable URLs can't be credential-checked here; ParseLock already
		// accepted the document structurally, so leave deeper URL validation to
		// resolution.
		return nil
	}
	if u.User != nil {
		return &config.ValidationError{
			Code:    CodeCredentialsInURL,
			Path:    path,
			Field:   "url",
			Message: "url must not embed credentials; store the credential-free URL (strip user:pass@)",
			Value:   rawURL,
		}
	}
	q := u.Query()
	for _, key := range credentialQueryKeys {
		if q.Has(key) {
			return &config.ValidationError{
				Code:    CodeCredentialsInURL,
				Path:    path,
				Field:   "url",
				Message: fmt.Sprintf("url must not embed credentials; remove the %q query parameter", key),
				Value:   rawURL,
			}
		}
	}
	return nil
}
