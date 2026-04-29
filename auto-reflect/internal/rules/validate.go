package rules

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	idRegex        = regexp.MustCompile(idPattern)
	tagRegex       = regexp.MustCompile(tagPattern)
	categoryRegex  = regexp.MustCompile(catPattern)
	timestampRegex = regexp.MustCompile(timePattern)
)

func validateRule(path string, index int, rule *Rule) []ValidationError {
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

	content := strings.TrimSpace(rule.Content)
	if content == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".content",
			Message: "content is required",
		})
	}

	category := strings.TrimSpace(rule.Category)
	if category == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   prefix + ".category",
			Message: "category is required",
		})
	} else if !categoryRegex.MatchString(category) {
		errs = append(errs, ValidationError{
			Code:    "invalid_format",
			Path:    path,
			Field:   prefix + ".category",
			Message: "category must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
			Value:   rule.Category,
		})
	}

	seenTags := make(map[string]struct{}, len(rule.Tags))
	for i, tag := range rule.Tags {
		field := fmt.Sprintf("%s.tags[%d]", prefix, i)
		normalized := strings.TrimSpace(strings.ToLower(tag))
		if normalized == "" {
			errs = append(errs, ValidationError{
				Code:    "required",
				Path:    path,
				Field:   field,
				Message: "tag cannot be empty",
			})
			continue
		}
		if !tagRegex.MatchString(normalized) {
			errs = append(errs, ValidationError{
				Code:    "invalid_format",
				Path:    path,
				Field:   field,
				Message: "tag must match ^[a-z0-9]+(?:-[a-z0-9]+)*$",
				Value:   tag,
			})
			continue
		}
		if _, ok := seenTags[normalized]; ok {
			errs = append(errs, ValidationError{
				Code:    "duplicate",
				Path:    path,
				Field:   field,
				Message: "duplicate tag after normalization",
				Value:   normalized,
			})
			continue
		}
		seenTags[normalized] = struct{}{}
	}

	if !timestampRegex.MatchString(strings.TrimSpace(rule.CreatedAt)) {
		errs = append(errs, ValidationError{
			Code:    "invalid_format",
			Path:    path,
			Field:   prefix + ".created_at",
			Message: "created_at must be an RFC3339 UTC timestamp",
			Value:   rule.CreatedAt,
		})
	}

	if !timestampRegex.MatchString(strings.TrimSpace(rule.UpdatedAt)) {
		errs = append(errs, ValidationError{
			Code:    "invalid_format",
			Path:    path,
			Field:   prefix + ".updated_at",
			Message: "updated_at must be an RFC3339 UTC timestamp",
			Value:   rule.UpdatedAt,
		})
	}

	return errs
}

func normalizeCreateInput(in CreateInput) (CreateInput, []ValidationError) {
	out := CreateInput{
		Content:  strings.TrimSpace(in.Content),
		Category: strings.ToLower(strings.TrimSpace(in.Category)),
		Tags:     make([]string, 0, len(in.Tags)),
	}

	errs := make([]ValidationError, 0)
	if out.Content == "" {
		errs = append(errs, ValidationError{Code: "required", Path: "", Field: "content", Message: "content is required"})
	}
	if out.Category == "" {
		errs = append(errs, ValidationError{Code: "required", Path: "", Field: "category", Message: "category is required"})
	} else if !categoryRegex.MatchString(out.Category) {
		errs = append(errs, ValidationError{Code: "invalid_format", Path: "", Field: "category", Message: "category must match ^[a-z0-9]+(?:-[a-z0-9]+)*$", Value: in.Category})
	}

	seen := make(map[string]struct{}, len(in.Tags))
	for i, raw := range in.Tags {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		field := fmt.Sprintf("tags[%d]", i)
		if normalized == "" {
			errs = append(errs, ValidationError{Code: "required", Field: field, Message: "tag cannot be empty", Value: raw})
			continue
		}
		if !tagRegex.MatchString(normalized) {
			errs = append(errs, ValidationError{Code: "invalid_format", Field: field, Message: "tag must match ^[a-z0-9]+(?:-[a-z0-9]+)*$", Value: raw})
			continue
		}
		if _, ok := seen[normalized]; ok {
			errs = append(errs, ValidationError{Code: "duplicate", Field: field, Message: "duplicate tag after normalization", Value: normalized})
			continue
		}
		seen[normalized] = struct{}{}
		out.Tags = append(out.Tags, normalized)
	}

	return out, errs
}

func validatePlaybook(path string, playbook Playbook) []ValidationError {
	errs := make([]ValidationError, 0)
	if playbook.SchemaVersion != 1 {
		errs = append(errs, ValidationError{
			Code:    "invalid_schema_version",
			Path:    path,
			Field:   "schema_version",
			Message: "schema_version must be 1",
			Value:   playbook.SchemaVersion,
		})
	}
	for i := range playbook.Rules {
		errs = append(errs, validateRule(path, i, &playbook.Rules[i])...)
	}
	return errs
}
