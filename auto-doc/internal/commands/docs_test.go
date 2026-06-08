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
		"auto doc init",
		"auto doc tree",
		"auto doc stale",
		"auto doc fix",
		"auto doc fixed",
		"auto doc agents",
		"auto doc search reindex",
		"auto doc search keyword",
		"auto doc doctor",
		"auto doc quickstart",
		"auto doc docs",
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
