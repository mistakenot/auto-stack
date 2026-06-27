package render

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCustomize(t *testing.T) {
	front := `name: demo
description: a demo
customize:
  org:
    required: true
    description: org name
  greeting:
    default: "hello"
  empty_default:
    default: ""
  bare: {}
`
	schema, err := ParseCustomize(front)
	if err != nil {
		t.Fatal(err)
	}
	if !schema["org"].Required {
		t.Fatalf("org should be required")
	}
	if schema["greeting"].Default != "hello" || !schema["greeting"].hasDefault {
		t.Fatalf("greeting default not parsed: %+v", schema["greeting"])
	}
	if !schema["empty_default"].hasDefault || schema["empty_default"].Default != "" {
		t.Fatalf("empty_default should record an explicit empty default: %+v", schema["empty_default"])
	}
	if schema["bare"].hasDefault {
		t.Fatalf("bare should have no default")
	}
}

func TestResolveValues_UndeclaredPlaceholder(t *testing.T) {
	schema := CustomizeSchema{"known": {}}
	_, err := ResolveValues(schema, []string{"known", "mystery"}, map[string]string{})
	if err == nil {
		t.Fatal("expected undeclared error")
	}
	var ce *CustomizeError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CustomizeError, got %T", err)
	}
	if ce.Code() != CodeUndeclaredPlaceholder {
		t.Fatalf("code = %q, want %q", ce.Code(), CodeUndeclaredPlaceholder)
	}
	if ce.Var != "mystery" || !strings.Contains(ce.Error(), "mystery") {
		t.Fatalf("error must name the offending var: %v", ce)
	}
}

func TestResolveValues_RequiredMissing(t *testing.T) {
	schema := CustomizeSchema{"org": {Required: true}}
	_, err := ResolveValues(schema, []string{"org"}, map[string]string{})
	if err == nil {
		t.Fatal("expected required-missing error")
	}
	var ce *CustomizeError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CustomizeError, got %T", err)
	}
	if ce.Code() != CodeRequiredValueMissing {
		t.Fatalf("code = %q, want %q", ce.Code(), CodeRequiredValueMissing)
	}
	// remediation hint present
	if !strings.Contains(ce.Error(), "skills.yaml") && !strings.Contains(ce.Error(), "default") {
		t.Fatalf("error missing remediation hint: %v", ce)
	}
}

func TestResolveValues_RequiredSatisfied(t *testing.T) {
	schema := CustomizeSchema{"org": {Required: true}}
	out, err := ResolveValues(schema, []string{"org"}, map[string]string{"org": "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if out["org"] != "Acme" {
		t.Fatalf("org = %q, want Acme", out["org"])
	}
}

func TestResolveValues_DeclaredNoDefaultEmpty(t *testing.T) {
	schema := CustomizeSchema{"opt": {Required: false}}
	out, err := ResolveValues(schema, []string{"opt"}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := out["opt"]; !ok || v != "" {
		t.Fatalf("opt = %q (present=%v), want empty string", v, ok)
	}
}

func TestResolveValues_DefaultUsedAndOverridden(t *testing.T) {
	schema := CustomizeSchema{
		"g": {Default: "hello", hasDefault: true},
	}
	// default applies when not supplied
	out, err := ResolveValues(schema, []string{"g"}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if out["g"] != "hello" {
		t.Fatalf("g = %q, want hello (default)", out["g"])
	}
	// supplied wins over default
	out, err = ResolveValues(schema, []string{"g"}, map[string]string{"g": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if out["g"] != "hi" {
		t.Fatalf("g = %q, want hi (supplied)", out["g"])
	}
}

// TestRender_LiteralBraceEmitsLiteral exercises the customize → render path end
// to end: {{ "{{" }} emits a literal {{ and a declared replacement substitutes.
func TestRender_CustomizeEndToEnd(t *testing.T) {
	skillMD := `---
name: demo
description: demo skill
customize:
  org:
    required: true
---
Org is {{ .org }}. Literal {{ "{{" }} here.
`
	tree, err := Render(RenderInput{
		SkillMD: []byte(skillMD),
		Values:  map[string]ReplacementValue{"org": {Literal: "Acme & Co <hq>"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := skillBody(t, tree)
	if !strings.Contains(body, "Org is Acme & Co <hq>.") {
		t.Fatalf("substitution wrong; body=%q", body)
	}
	if !strings.Contains(body, "Literal {{ here.") {
		t.Fatalf("literal brace not emitted; body=%q", body)
	}
	// customize: block must be stripped from emitted frontmatter
	skillContent := string(skillFile(t, tree).Data)
	if strings.Contains(skillContent, "customize:") {
		t.Fatalf("customize block leaked into output:\n%s", skillContent)
	}
}

func TestRender_UndeclaredPlaceholderHardError(t *testing.T) {
	skillMD := `---
name: demo
description: demo
---
{{ .missing }}
`
	_, err := Render(RenderInput{SkillMD: []byte(skillMD)})
	if err == nil {
		t.Fatal("expected undeclared placeholder error")
	}
	var ce *CustomizeError
	if !errors.As(err, &ce) || ce.Code() != CodeUndeclaredPlaceholder {
		t.Fatalf("want undeclared_placeholder CustomizeError, got %T: %v", err, err)
	}
}

// skillFile returns the SKILL.md tree file.
func skillFile(t *testing.T, tree Tree) TreeFile {
	t.Helper()
	for _, f := range tree.Files {
		if f.Path == SkillMDPath {
			return f
		}
	}
	t.Fatalf("SKILL.md not found in tree")
	return TreeFile{}
}

// skillBody returns the body portion (after frontmatter) of SKILL.md.
func skillBody(t *testing.T, tree Tree) string {
	t.Helper()
	_, body, err := splitFrontmatter(skillFile(t, tree).Data)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
