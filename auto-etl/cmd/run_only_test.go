package cmd

import (
	"testing"
)

func TestParseOnlyFlag_Default(t *testing.T) {
	sources, err := parseOnlyFlag(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] || !sources["github"] || !sources["git"] || !sources["hooks"] {
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

func TestParseOnlyFlag_Git(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"git"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["git"] {
		t.Error("git should be enabled")
	}
	if sources["sessions"] {
		t.Error("sessions should not be enabled")
	}
	if sources["github"] {
		t.Error("github should not be enabled")
	}
}

func TestParseOnlyFlag_GitAndSessions(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"git", "sessions"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["git"] || !sources["sessions"] {
		t.Errorf("git and sessions should be enabled: %v", sources)
	}
	if sources["github"] {
		t.Error("github should not be enabled")
	}
}

func TestParseOnlyFlag_AllFour(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"sessions", "github", "git", "hooks"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["sessions"] || !sources["github"] || !sources["git"] || !sources["hooks"] {
		t.Errorf("all four should be enabled: %v", sources)
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

func TestParseOnlyFlag_Hooks(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"hooks"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["hooks"] {
		t.Error("hooks should be enabled")
	}
	if sources["sessions"] {
		t.Error("sessions should not be enabled")
	}
}

func TestParseOnlyFlag_HooksCaseInsensitive(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"HOOKS"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["hooks"] {
		t.Error("HOOKS should normalize to hooks")
	}
}

func TestParseOnlyFlag_DefaultIncludesHooks(t *testing.T) {
	sources, err := parseOnlyFlag(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sources["hooks"] {
		t.Error("default should include hooks")
	}
}

func TestParseOnlyFlag_HooksAndGit(t *testing.T) {
	sources, err := parseOnlyFlag([]string{"hooks", "git"})
	if err != nil {
		t.Fatal(err)
	}
	if !sources["hooks"] || !sources["git"] {
		t.Errorf("hooks and git should be enabled: %v", sources)
	}
	if sources["sessions"] {
		t.Error("sessions should not be enabled")
	}
}
