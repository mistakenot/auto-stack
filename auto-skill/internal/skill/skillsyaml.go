package skill

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/mistakenot/auto-shared/config"
	"gopkg.in/yaml.v3"
)

// SkillsYAML is the typed representation of a project's .auto/skills/skills.yaml.
type SkillsYAML struct {
	AutoUpdate    bool                   `yaml:"auto_update"`
	Targets       []string               `yaml:"targets"`
	CommitTargets bool                   `yaml:"commit_targets"`
	TrustedHosts  []string               `yaml:"trusted_hosts"`
	Shared        SharedConfig           `yaml:"shared"`
	Skills        map[string]SkillConfig `yaml:"skills"`
}

// SharedConfig holds defaults applied across every managed skill.
type SharedConfig struct {
	Version string `yaml:"version"`
	// Replacements is the named replacement map (var name → value), where value
	// is a literal scalar or a file-ref mapping. omitempty so a skill with no
	// replacements round-trips without an empty `replacements:` key.
	Replacements ReplacementMap `yaml:"replacements,omitempty"`
}

// SkillConfig holds per-skill overrides.
type SkillConfig struct {
	Version string `yaml:"version"`
	// Replacements is the named replacement map (var name → value); see
	// SharedConfig.Replacements.
	Replacements ReplacementMap `yaml:"replacements,omitempty"`
}

// ReplacementMap is the named replacement map (var name → value node). It
// decodes the canonical mapping form and, for backward compatibility, accepts
// the legacy empty-sequence form (`replacements: []`) that the pre-reconciliation
// add/migrate writers emitted for replacement-free skills — without it,
// upgrading an existing project would fail to parse before any command could
// rewrite the file.
type ReplacementMap map[string]yaml.Node

// UnmarshalYAML decodes a mapping into the named map, treats null/absent and the
// legacy empty sequence as an empty map, and rejects a populated legacy sequence
// with a remediation hint (those unnamed entries never bound to a var and cannot
// be migrated automatically).
func (r *ReplacementMap) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.MappingNode:
		m := map[string]yaml.Node{}
		if err := value.Decode(&m); err != nil {
			return err
		}
		*r = m
	case yaml.SequenceNode:
		if len(value.Content) > 0 {
			return errors.New("replacements must be a named map (var: value); the legacy unnamed list form is no longer supported — rewrite each entry as `<var>: <value>`")
		}
		*r = ReplacementMap{}
	case yaml.ScalarNode:
		if value.Tag == "!!null" || value.Value == "" {
			*r = ReplacementMap{}
			break
		}
		return fmt.Errorf("replacements must be a named map (var: value), got scalar %q", value.Value)
	default:
		*r = ReplacementMap{}
	}
	return nil
}

// replacementVarRE is the accepted form of a replacement var name — a template
// field identifier ({{ .var }}). It mirrors Go's identifier rules so a declared
// var can bind to a customize: placeholder.
var replacementVarRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// knownFileRefKeys is the closed set of keys allowed on a file-ref replacement.
var knownFileRefKeys = map[string]bool{
	"file":              true,
	"section":           true,
	"include_heading":   true,
	"strip_frontmatter": true,
}

// ParseSkillsYAML strictly decodes skills.yaml, rejecting unknown keys.
func ParseSkillsYAML(data []byte) (*SkillsYAML, error) {
	var cfg SkillsYAML
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateSkillsYAML checks structural and grammar rules. It does not resolve
// versions or read referenced files.
func ValidateSkillsYAML(cfg *SkillsYAML) []config.ValidationError {
	var errs []config.ValidationError
	if cfg == nil {
		return errs
	}

	errs = append(errs, checkDuplicates(cfg.Targets, "targets", "remove the duplicate target")...)
	errs = append(errs, checkDuplicates(cfg.TrustedHosts, "trusted_hosts", "remove the duplicate trusted host")...)

	if cfg.Shared.Version != "" {
		if ve := ValidateVersionSpec(cfg.Shared.Version); ve != nil {
			ve.Path = "shared.version"
			ve.Field = "version"
			errs = append(errs, *ve)
		}
	}
	errs = append(errs, validateReplacements(cfg.Shared.Replacements, "shared.replacements")...)

	for name, sc := range cfg.Skills {
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
		if sc.Version != "" {
			if ve := ValidateVersionSpec(sc.Version); ve != nil {
				ve.Path = path + ".version"
				ve.Field = "version"
				errs = append(errs, *ve)
			}
		}
		errs = append(errs, validateReplacements(sc.Replacements, path+".replacements")...)
	}

	return errs
}

// validateReplacements checks each named replacement (var name → value): the var
// name must be a valid template identifier and the value must be either a literal
// string or a well-formed file-ref mapping. Keys are visited in sorted order so
// multi-error output is deterministic.
func validateReplacements(reps map[string]yaml.Node, basePath string) []config.ValidationError {
	var errs []config.ValidationError
	names := make([]string, 0, len(reps))
	for name := range reps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := basePath + "." + name
		if !replacementVarRE.MatchString(name) {
			errs = append(errs, config.ValidationError{
				Code:    CodeInvalidVarName,
				Path:    path,
				Field:   "name",
				Message: fmt.Sprintf("replacement var name %q must match %s; rename it to a valid template identifier", name, replacementVarRE.String()),
				Value:   name,
			})
		}
		node := reps[name]
		switch node.Kind {
		case yaml.ScalarNode:
			if node.Tag != "" && node.Tag != "!!str" {
				errs = append(errs, config.ValidationError{
					Code:    CodeInvalidLiteral,
					Path:    path,
					Message: fmt.Sprintf("replacement literal must be a string, got %s; quote the value to make it a string", node.Tag),
					Value:   node.Value,
				})
			}
		case yaml.MappingNode:
			errs = append(errs, validateFileRefNode(&node, path)...)
		default:
			errs = append(errs, config.ValidationError{
				Code:    CodeInvalidFileRef,
				Path:    path,
				Message: "replacement must be a literal string or a file-ref mapping with a \"file:\" key",
			})
		}
	}
	return errs
}

// validateFileRefNode enforces the file-ref mapping shape: a required "file"
// key and only known keys.
func validateFileRefNode(node *yaml.Node, path string) []config.ValidationError {
	var errs []config.ValidationError
	hasFile := false
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !knownFileRefKeys[key] {
			errs = append(errs, config.ValidationError{
				Code:    CodeUnknownField,
				Path:    path,
				Field:   key,
				Message: fmt.Sprintf("unknown file-ref key %q; allowed keys: file, section, include_heading, strip_frontmatter", key),
				Value:   key,
			})
			continue
		}
		if key == "file" {
			hasFile = true
		}
	}
	if !hasFile {
		errs = append(errs, config.ValidationError{
			Code:    CodeInvalidFileRef,
			Path:    path,
			Field:   "file",
			Message: "file-ref mapping must have a \"file\" key; add file: <path>",
		})
	}
	return errs
}

// checkDuplicates reports a duplicate_value error for each repeated entry in a
// string slice.
func checkDuplicates(items []string, path, hint string) []config.ValidationError {
	var errs []config.ValidationError
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item] {
			errs = append(errs, config.ValidationError{
				Code:    CodeDuplicateValue,
				Path:    path,
				Message: fmt.Sprintf("duplicate value %q in %s; %s", item, path, hint),
				Value:   item,
			})
			continue
		}
		seen[item] = true
	}
	return errs
}
