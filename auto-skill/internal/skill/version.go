package skill

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mistakenot/auto-shared/config"
)

// Validation error code constants shared across all schema validators.
const (
	CodeRequired           = "required"
	CodeUnknownField       = "unknown_field"
	CodeInvalidSkillName   = "invalid_skill_name"
	CodeInvalidVersionSpec = "invalid_version_spec"
	CodeInvalidLiteral     = "invalid_literal"
	CodeInvalidFileRef     = "invalid_file_ref"
	CodeInvalidVarName     = "invalid_var_name"
	CodeCredentialsInURL   = "credentials_in_url"
	CodeInvalidState       = "invalid_state"
	CodeInvalidHash        = "invalid_hash"
	CodeUnknownSkillRef    = "unknown_skill_ref"
	CodeDuplicateValue     = "duplicate_value"
)

var commitHexRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// ValidateVersionSpec checks the grammar of a version specifier without
// resolving it. Accepted forms: "latest", "branch:<name>", "tag:<name>",
// "commit:<hex≥7>", or a bare best-effort string.
func ValidateVersionSpec(s string) *config.ValidationError {
	s = strings.TrimSpace(s)
	if s == "" {
		return &config.ValidationError{
			Code:    CodeInvalidVersionSpec,
			Message: "version spec must not be empty; use \"latest\", \"branch:<name>\", \"tag:<name>\", or \"commit:<hex>\"",
		}
	}
	if s == "latest" {
		return nil
	}

	prefix, payload, hasSep := strings.Cut(s, ":")
	if !hasSep {
		// Bare string — best-effort, accepted.
		return nil
	}

	switch prefix {
	case "branch", "tag":
		if strings.TrimSpace(payload) == "" {
			return &config.ValidationError{
				Code:    CodeInvalidVersionSpec,
				Message: fmt.Sprintf("%s: payload must not be empty; use \"%s:<name>\"", prefix, prefix),
				Value:   s,
			}
		}
		return nil
	case "commit":
		payload = strings.TrimSpace(payload)
		if !commitHexRE.MatchString(payload) {
			return &config.ValidationError{
				Code:    CodeInvalidVersionSpec,
				Message: "commit: payload must be 7-40 hex characters; use \"commit:<hex>\"",
				Value:   s,
			}
		}
		return nil
	default:
		return &config.ValidationError{
			Code:    CodeInvalidVersionSpec,
			Message: fmt.Sprintf("unknown version prefix %q; use \"latest\", \"branch:<name>\", \"tag:<name>\", or \"commit:<hex>\"", prefix),
			Value:   s,
		}
	}
}
