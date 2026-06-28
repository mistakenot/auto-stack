package rules

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	idRegex  = regexp.MustCompile(idPattern)
	tagRegex = regexp.MustCompile(tagPattern)
)

// validRuleTypes / validLifecycles back the enum checks.
var (
	validRuleTypes  = map[string]struct{}{RuleTypeHard: {}, RuleTypeSoft: {}}
	validLifecycles = map[string]struct{}{LifecycleDraft: {}, LifecycleConfirmed: {}, LifecycleStale: {}, LifecycleEnforced: {}}
)

// NormalizeDomain trims and lowercases each entry, dropping empties. Order is
// preserved. Duplicate detection is left to validation so duplicates surface as
// errors rather than being silently collapsed.
func NormalizeDomain(domain []string) []string {
	out := make([]string, 0, len(domain))
	for _, d := range domain {
		normalized := strings.ToLower(strings.TrimSpace(d))
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// ValidateRule checks a fully-formed Rule, returning structured errors. It
// enforces the id format, domain tag format and dedupe, non-empty
// use_when/content/causal_note, the rule_type and lifecycle enums, and the
// invariant that a hard rule must declare at least one domain (otherwise it
// would be unreachable by the domain-scoped hard-rule injection).
func ValidateRule(path string, index int, rule *Rule) []ValidationError {
	errs := make([]ValidationError, 0)
	if rule == nil {
		return append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   fmt.Sprintf("rules[%d]", index),
			Message: "rule is required",
		})
	}
	prefix := fmt.Sprintf("rules[%d]", index)

	if !idRegex.MatchString(rule.ID) {
		errs = append(errs, ValidationError{
			Code:    "invalid_format",
			Path:    path,
			Field:   prefix + ".id",
			Message: "id must match ^r-[0-9a-f]{8}$",
			Value:   rule.ID,
		})
	}

	errs = append(errs, validateDomain(path, prefix, rule)...)

	if strings.TrimSpace(rule.UseWhen) == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".use_when",
			Message: "use_when is required",
		})
	}
	if strings.TrimSpace(rule.Content) == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".content",
			Message: "content is required",
		})
	}
	if strings.TrimSpace(rule.CausalNote) == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".causal_note",
			Message: "causal_note is required",
		})
	}

	if _, ok := validRuleTypes[rule.RuleType]; !ok {
		errs = append(errs, ValidationError{
			Code:    "enum",
			Path:    path,
			Field:   prefix + ".rule_type",
			Message: "rule_type must be one of hard, soft",
			Value:   rule.RuleType,
		})
	}

	if _, ok := validLifecycles[rule.Lifecycle]; !ok {
		errs = append(errs, ValidationError{
			Code:    "enum",
			Path:    path,
			Field:   prefix + ".lifecycle",
			Message: "lifecycle must be one of draft, confirmed, stale, enforced",
			Value:   rule.Lifecycle,
		})
	}

	if rule.RuleType == RuleTypeHard && len(rule.Domain) == 0 {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".domain",
			Message: "hard rules must declare at least one domain: pass --domain <tag> so the rule can be surfaced",
		})
	}

	return errs
}

func validateDomain(path, prefix string, rule *Rule) []ValidationError {
	errs := make([]ValidationError, 0)
	seen := make(map[string]struct{}, len(rule.Domain))
	for i, tag := range rule.Domain {
		field := fmt.Sprintf("%s.domain[%d]", prefix, i)
		normalized := strings.TrimSpace(strings.ToLower(tag))
		if normalized == "" {
			errs = append(errs, ValidationError{
				Code:    "required",
				Path:    path,
				Field:   field,
				Message: "domain tag cannot be empty",
			})
			continue
		}
		if !tagRegex.MatchString(normalized) {
			errs = append(errs, ValidationError{
				Code:    "invalid_format",
				Path:    path,
				Field:   field,
				Message: "domain tag must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
				Value:   tag,
			})
			continue
		}
		if _, ok := seen[normalized]; ok {
			errs = append(errs, ValidationError{
				Code:    "duplicate",
				Path:    path,
				Field:   field,
				Message: "duplicate domain tag after normalization",
				Value:   normalized,
			})
			continue
		}
		seen[normalized] = struct{}{}
	}
	return errs
}

// ValidatePlaybook validates the schema version and every folded rule.
func ValidatePlaybook(path string, playbook Playbook) []ValidationError {
	errs := make([]ValidationError, 0)
	if playbook.SchemaVersion != SchemaVersion {
		errs = append(errs, ValidationError{
			Code:    "invalid_schema_version",
			Path:    path,
			Field:   "schema_version",
			Message: fmt.Sprintf("schema_version must be %d", SchemaVersion),
			Value:   playbook.SchemaVersion,
		})
	}
	for i := range playbook.Rules {
		errs = append(errs, ValidateRule(path, i, &playbook.Rules[i])...)
	}
	return errs
}
