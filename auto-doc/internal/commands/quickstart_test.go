package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/commands"
)

func TestQuickstart(t *testing.T) {
	var buf bytes.Buffer
	commands.Quickstart(&buf)
	out := buf.String()

	required := []string{
		"autodoc init",
		"autodoc tree",
		"autodoc stale",
		"autodoc fix",
		"autodoc fixed",
		"autodoc agents",
		"autodoc search reindex",
		"autodoc search keyword",
	}
	for _, s := range required {
		if !strings.Contains(out, s) {
			t.Errorf("quickstart output missing expected section: %q", s)
		}
	}
}

func TestQuickstartContainsSearchExamples(t *testing.T) {
	var buf bytes.Buffer
	commands.Quickstart(&buf)
	out := buf.String()

	count := strings.Count(out, "autodoc search keyword")
	if count < 3 {
		t.Errorf("expected at least 3 'autodoc search keyword' examples, got %d", count)
	}
}

func TestQuickstartContainsWorkflow(t *testing.T) {
	var buf bytes.Buffer
	commands.Quickstart(&buf)
	out := buf.String()

	if !strings.Contains(out, "Typical Workflow") {
		t.Errorf("quickstart output missing 'Typical Workflow' section")
	}
}
