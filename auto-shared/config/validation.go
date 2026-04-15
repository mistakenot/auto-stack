package config

import "strings"

// ValidationError represents a single field-level validation failure.
type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

// ValidationErrorsError wraps multiple validation errors as an error.
type ValidationErrorsError struct {
	Path   string
	Errors []ValidationError
}

func (e *ValidationErrorsError) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("invalid settings")
	if e.Path != "" {
		builder.WriteString(" in ")
		builder.WriteString(e.Path)
	}
	builder.WriteString(": ")
	builder.WriteString(e.Errors[0].Message)
	return builder.String()
}
