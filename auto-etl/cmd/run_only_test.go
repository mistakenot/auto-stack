package cmd

import (
	"testing"
)

func TestParseOnlyFlag_Default(t *testing.T) {
	sources, err := parseOnlyFlag(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] || !sources["github"] {
		t.Errorf("default should enable all: %v", sources)
	}
}

func TestParseOnlyFlag_Sessions(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"sessions"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] {
		t.Error("sessions should be enabled")
	}
	if sources["github"] {
		t.Error("github should not be enabled")
	}
}

func TestParseOnlyFlag_GitHub(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	if sources["sessions"] {
		t.Error("sessions should not be enabled")
	}
	if !sources["github"] {
		t.Error("github should be enabled")
	}
}

func TestParseOnlyFlag_Both(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"sessions", "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] || !sources["github"] {
		t.Errorf("both should be enabled: %v", sources)
	}
}

func TestParseOnlyFlag_CaseInsensitive(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"SESSIONS", " GitHub "})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] || !sources["github"] {
		t.Errorf("should normalize case: %v", sources)
	}
}

func TestParseOnlyFlag_InvalidValue(t *testing.T) {
	_, err := parseOnlyFlag([]string{"invalid"})
	if err == nil {
		t.Error("expected error for invalid value")
	}
}

func TestParseOnlyFlag_DuplicatesDedupe(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"sessions", "sessions"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] {
		t.Error("sessions should be enabled")
	}
}
