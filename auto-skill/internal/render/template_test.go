package render

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTemplate_Accept(t *testing.T) {
	cases := []struct {
		name string
		body string
		vars []string
	}{
		{"plain text", "hello world", nil},
		{"single field", "hi {{ .name }}!", []string{"name"}},
		{"repeated field deduped", "{{ .a }} {{ .a }}", []string{"a"}},
		{"two fields ordered", "{{ .b }}{{ .a }}", []string{"b", "a"}},
		{"chained field access", "{{ .a.b }}", []string{"a.b"}},
		{"literal brace escape", `use {{ "{{" }} .x }} in docs`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := ParseTemplate(tc.body)
			if err != nil {
				t.Fatalf("expected accept, got error: %v", err)
			}
			got := tmpl.Vars()
			if len(got) != len(tc.vars) {
				t.Fatalf("vars = %v, want %v", got, tc.vars)
			}
			for i := range got {
				if got[i] != tc.vars[i] {
					t.Fatalf("vars = %v, want %v", got, tc.vars)
				}
			}
		})
	}
}

func TestParseTemplate_RejectAtParse(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		nodePart string // substring expected in the rejected node description
	}{
		{"printf function call", `{{ printf "%s" .x }}`, "function call (printf)"},
		{"pipe to upper", `{{ .a | upper }}`, "upper"},
		{"pipe to builtin printf", `{{ .a | printf }}`, "pipeline"},
		{"if control action", `{{ if .x }}yes{{ end }}`, "if action"},
		{"range control action", `{{ range .x }}y{{ end }}`, "range action"},
		{"with control action", `{{ with .x }}y{{ end }}`, "with action"},
		{"index builtin", `{{ index .x 0 }}`, "function call (index)"},
		{"and builtin", `{{ and .x .y }}`, "function call (and)"},
		{"bare dot", `{{ . }}`, "dot (.)"},
		{"variable decl", `{{ $x := .y }}{{ $x }}`, "variable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTemplate(tc.body)
			if err == nil {
				t.Fatalf("expected rejection, got nil")
			}
			var tre *TemplateRejectedError
			if !errors.As(err, &tre) {
				t.Fatalf("expected *TemplateRejectedError, got %T: %v", err, err)
			}
			if tre.Code() != CodeTemplateRejected {
				t.Fatalf("code = %q, want %q", tre.Code(), CodeTemplateRejected)
			}
			full := tre.Error()
			if !strings.Contains(full, tc.nodePart) {
				t.Fatalf("error %q does not name offending node %q", full, tc.nodePart)
			}
		})
	}
}

func TestTemplate_Render_RawUnescaped(t *testing.T) {
	tmpl, err := ParseTemplate("value: {{ .v }}")
	if err != nil {
		t.Fatal(err)
	}
	// A value with HTML/shell metacharacters must pass through verbatim — render
	// uses text/template semantics (raw), never html escaping.
	raw := `<a href="x"> & $(rm -rf /) | tee 'q'`
	got, err := tmpl.Render(map[string]string{"v": raw})
	if err != nil {
		t.Fatal(err)
	}
	want := "value: " + raw
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestTemplate_Render_LiteralBrace(t *testing.T) {
	tmpl, err := ParseTemplate(`open {{ "{{" }} close`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tmpl.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "open {{ close" {
		t.Fatalf("render = %q, want %q", got, "open {{ close")
	}
}

func TestTemplate_Render_MissingKeyEmpty(t *testing.T) {
	tmpl, err := ParseTemplate("[{{ .x }}]")
	if err != nil {
		t.Fatal(err)
	}
	got, err := tmpl.Render(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[]" {
		t.Fatalf("render = %q, want %q", got, "[]")
	}
}
