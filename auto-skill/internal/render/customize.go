package render

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// Error codes for customize-block resolution failures.
const (
	// CodeUndeclaredPlaceholder is returned when the body references a {{ .var }}
	// that the template's customize: block does not declare.
	CodeUndeclaredPlaceholder = "undeclared_placeholder"
	// CodeRequiredValueMissing is returned when a required customize var has no
	// supplied value and no default.
	CodeRequiredValueMissing = "required_value_missing"
)

// CustomizeError is a typed, structured resolution error carrying a remediation
// hint (which var, what to supply).
type CustomizeError struct {
	ErrCode string
	Var     string
	Message string
}

func (e *CustomizeError) Error() string { return e.Message }

// Code returns the stable error code.
func (e *CustomizeError) Code() string { return e.ErrCode }

// CustomizeVar declares a single customizable placeholder.
type CustomizeVar struct {
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
	Description string `yaml:"description"`
	// hasDefault records whether a default key was present (distinguishing an
	// explicit empty default from an absent one).
	hasDefault bool `yaml:"-"`
}

// CustomizeSchema is the declared customize: block keyed by var name.
type CustomizeSchema map[string]CustomizeVar

// ParseCustomize extracts and decodes the customize: block from a template's
// YAML frontmatter. Frontmatter without a customize: key yields an empty (but
// non-nil) schema. Other frontmatter keys are ignored.
func ParseCustomize(frontmatter string) (CustomizeSchema, error) {
	schema := CustomizeSchema{}
	if frontmatter == "" {
		return schema, nil
	}

	// Decode into a raw node tree so we can detect whether each var supplied a
	// default key at all (an explicit `default: ""` differs from no default
	// only for required vars, but we keep the distinction precise).
	var doc struct {
		Customize map[string]yaml.Node `yaml:"customize"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &doc); err != nil {
		return nil, fmt.Errorf("parse customize block: %w", err)
	}
	for name := range doc.Customize {
		node := doc.Customize[name]
		var cv CustomizeVar
		if err := node.Decode(&cv); err != nil {
			return nil, fmt.Errorf("parse customize var %q: %w", name, err)
		}
		cv.hasDefault = mappingHasKey(&node, "default")
		schema[name] = cv
	}
	return schema, nil
}

// mappingHasKey reports whether a YAML mapping node contains the given key.
func mappingHasKey(node *yaml.Node, key string) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// ResolveValues applies the customize resolution rules and returns a flat
// name→value map covering every declared var, suitable for Template.Render.
//
// Rules:
//   - An undeclared placeholder (referenced in used but absent from schema) is
//     a hard error (CodeUndeclaredPlaceholder).
//   - A declared required var with no supplied value and no default is a hard
//     error (CodeRequiredValueMissing).
//   - A declared non-required var with no supplied value and no default resolves
//     to the empty string.
//   - A supplied value always wins over a default.
//
// used is the set of var keys referenced by the body; supplied is the set of
// values from skills.yaml (literal text, file-refs already resolved to text).
func ResolveValues(schema CustomizeSchema, used []string, supplied map[string]string) (map[string]string, error) {
	for _, name := range used {
		if _, ok := schema[name]; !ok {
			return nil, &CustomizeError{
				ErrCode: CodeUndeclaredPlaceholder,
				Var:     name,
				Message: fmt.Sprintf("%s: placeholder {{ .%s }} is used in the body but not declared in the customize: block; declare %q under customize: or remove the placeholder", CodeUndeclaredPlaceholder, name, name),
			}
		}
	}

	out := make(map[string]string, len(schema))
	// Iterate in sorted order so a multi-error scenario reports deterministically.
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cv := schema[name]
		if val, ok := supplied[name]; ok {
			out[name] = val
			continue
		}
		if cv.hasDefault {
			out[name] = cv.Default
			continue
		}
		if cv.Required {
			return nil, &CustomizeError{
				ErrCode: CodeRequiredValueMissing,
				Var:     name,
				Message: fmt.Sprintf("%s: required customize var %q has no value and no default; supply skills.<skill>.replacements.%s in skills.yaml or add a default to the template's customize: block", CodeRequiredValueMissing, name, name),
			}
		}
		out[name] = ""
	}
	return out, nil
}
