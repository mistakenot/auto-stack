package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestDocsContainsAllCommands(t *testing.T) {
	var buf bytes.Buffer
	Docs(&buf)
	output := buf.String()

	cmds := []string{
		"autodoc init",
		"autodoc tree",
		"autodoc stale",
		"autodoc fix",
		"autodoc fixed",
		"autodoc agents",
		"autodoc search reindex",
		"autodoc search keyword",
		"autodoc doctor",
		"autodoc quickstart",
		"autodoc docs",
		"--json",
		"--project",
	}
	for _, cmd := range cmds {
		if !strings.Contains(output, cmd) {
			t.Errorf("docs output missing %q", cmd)
		}
	}
}

func TestDocsNonEmpty(t *testing.T) {
	var buf bytes.Buffer
	Docs(&buf)
	if buf.Len() < 1000 {
		t.Errorf("docs output too short: %d bytes", buf.Len())
	}
}
