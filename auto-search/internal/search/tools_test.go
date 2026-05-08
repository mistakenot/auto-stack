package search

import (
	"strings"
	"testing"
)

func TestNormalizeToolNamesEmpty(t *testing.T) {
	got, err := NormalizeToolNames(nil)
	if err != nil {
		t.Fatalf("nil input: %v", err)
	}
	if got != nil {
		t.Errorf("nil input got %v, want nil", got)
	}

	got, err = NormalizeToolNames([]string{})
	if err != nil {
		t.Fatalf("empty slice: %v", err)
	}
	if got != nil {
		t.Errorf("empty slice got %v, want nil", got)
	}

	got, err = NormalizeToolNames([]string{"", "  ", "\t"})
	if err != nil {
		t.Fatalf("whitespace-only: %v", err)
	}
	if got != nil {
		t.Errorf("whitespace-only got %v, want nil", got)
	}
}

func TestNormalizeToolNamesValidSingle(t *testing.T) {
	got, err := NormalizeToolNames([]string{"Edit"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "Edit" {
		t.Errorf("got %v, want [Edit]", got)
	}
}

func TestNormalizeToolNamesCaseInsensitive(t *testing.T) {
	got, err := NormalizeToolNames([]string{"edit", "BASH"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sorted alphabetically.
	if len(got) != 2 || got[0] != "Bash" || got[1] != "Edit" {
		t.Errorf("got %v, want [Bash Edit]", got)
	}
}

func TestNormalizeToolNamesDeduplicates(t *testing.T) {
	got, err := NormalizeToolNames([]string{"Edit", "edit", "EDIT", "Write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "Edit" || got[1] != "Write" {
		t.Errorf("got %v, want [Edit Write]", got)
	}
}

func TestNormalizeToolNamesSplitsCommas(t *testing.T) {
	// pflag StringSlice typically pre-splits, but our normalizer should be
	// resilient to a single comma-separated entry too.
	got, err := NormalizeToolNames([]string{"Edit,Write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "Edit" || got[1] != "Write" {
		t.Errorf("got %v, want [Edit Write]", got)
	}
}

func TestNormalizeToolNamesUnknown(t *testing.T) {
	_, err := NormalizeToolNames([]string{"NotARealTool"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	msg := err.Error()
	if !strings.Contains(msg, "NotARealTool") {
		t.Errorf("error message should mention the bad value, got: %s", msg)
	}
	// Error should list every known tool name, so user knows what's valid.
	for _, name := range KnownToolNames {
		if !strings.Contains(msg, name) {
			t.Errorf("error message should list known tool %q, got: %s", name, msg)
		}
	}
}

func TestNormalizeToolNamesUnknownMixedWithValid(t *testing.T) {
	// A single unknown should still fail-fast even when valid tools are present.
	_, err := NormalizeToolNames([]string{"Edit", "Bogus", "Write"})
	if err == nil {
		t.Fatal("expected error when any tool is unknown")
	}
	if !strings.Contains(err.Error(), "Bogus") {
		t.Errorf("error should mention Bogus, got: %s", err.Error())
	}
}
