package feedback

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-shared/config"
)

type ValidationError = config.ValidationError

func ValidateAddInput(in *AddInput) []ValidationError {
	errs := make([]ValidationError, 0)
	if in == nil {
		return append(errs, ValidationError{
			Code:    "required",
			Field:   "input",
			Message: "input is required",
		})
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "kind", Message: "kind is required"})
	} else if kind != KindHelpful && kind != KindHarmful && kind != KindMissing {
		errs = append(errs, ValidationError{Code: "invalid_enum", Field: "kind", Message: "kind must be one of: helpful|harmful|missing", Value: in.Kind})
	}

	if strings.TrimSpace(in.Comment) == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "comment", Message: "comment is required"})
	}

	hasStart := in.Start != nil
	hasEnd := in.End != nil
	if (hasStart || hasEnd) && strings.TrimSpace(in.File) == "" {
		errs = append(errs, ValidationError{Code: "invalid_flags", Field: "file", Message: "--file is required when --start or --end is provided; add --file <repo-relative-path>"})
	}
	if in.Start != nil && *in.Start < 1 {
		errs = append(errs, ValidationError{Code: "invalid_value", Field: "start", Message: "start_line must be >= 1", Value: *in.Start})
	}
	if in.End != nil && *in.End < 1 {
		errs = append(errs, ValidationError{Code: "invalid_value", Field: "end", Message: "end_line must be >= 1", Value: *in.End})
	}
	if in.Start != nil && in.End != nil && *in.End < *in.Start {
		errs = append(errs, ValidationError{Code: "invalid_value", Field: "end", Message: "end_line must be >= start_line", Value: *in.End})
	}
	if strings.TrimSpace(in.File) != "" {
		if _, err := NormalizeRepoRelativePath(in.File); err != nil {
			errs = append(errs, ValidationError{Code: "invalid_path", Field: "file", Message: err.Error(), Value: in.File})
		}
	}

	return errs
}

func ValidateListInput(in ListInput) []ValidationError {
	errs := make([]ValidationError, 0)
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind != "" && kind != KindHelpful && kind != KindHarmful && kind != KindMissing {
		errs = append(errs, ValidationError{
			Code:    "invalid_enum",
			Field:   "kind",
			Message: "kind must be one of: helpful|harmful|missing",
			Value:   in.Kind,
		})
	}
	if in.Limit < 0 {
		errs = append(errs, ValidationError{
			Code:    "invalid_value",
			Field:   "limit",
			Message: "limit must be >= 0",
			Value:   in.Limit,
		})
	}
	return errs
}

func NormalizeRepoRelativePath(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", nil
	}
	if filepath.IsAbs(clean) {
		return "", errors.New("file must be repo-relative, not absolute")
	}
	clean = filepath.ToSlash(filepath.Clean(clean))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errors.New("file must stay within the repository; use a repo-relative path")
	}
	return clean, nil
}
