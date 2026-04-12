package testutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/etlscan"
	"github.com/mistakenot/auto-search/internal/testutil"
)

// findTestdataDir walks up from the test's working directory to find the
// module-root testdata/etl-output directory.
func findTestdataDir(t *testing.T) string {
	t.Helper()
	// When running tests, the working directory is the package directory.
	// The testdata dir lives at the module root: ../../testdata/etl-output
	dir := filepath.Join("..", "..", "testdata", "etl-output")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve testdata dir: %v", err)
	}
	return abs
}

// TestGenerateFixtures regenerates the fixture files. Run this test to
// recreate the parquet fixtures after schema changes:
//
//	go test -run TestGenerateFixtures ./internal/testutil/ -v
//
// Skipped during normal `go test ./...` when fixtures already exist to avoid
// a race condition with tests in other packages that read the same files.
func TestGenerateFixtures(t *testing.T) {
	outputDir := findTestdataDir(t)

	// Skip regeneration if fixtures already exist, to avoid a write/read race
	// when running `go test ./...` with parallel packages.
	sessPath := testutil.SessionsFixturePath(outputDir)
	if _, err := os.Stat(sessPath); err == nil {
		t.Skipf("fixtures already exist at %s; delete them to regenerate", outputDir)
	}

	if err := testutil.GenerateFixtures(outputDir); err != nil {
		t.Fatalf("GenerateFixtures: %v", err)
	}
	t.Logf("fixtures written to %s", outputDir)
}

// TestFixturesExist verifies that the committed fixture files are present.
func TestFixturesExist(t *testing.T) {
	outputDir := findTestdataDir(t)

	sessPath := testutil.SessionsFixturePath(outputDir)
	msgPath := testutil.MessagesFixturePath(outputDir)

	for _, p := range []string{sessPath, msgPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("fixture file missing: %s: %v", p, err)
		}
		if info.Size() == 0 {
			t.Fatalf("fixture file is empty: %s", p)
		}
		t.Logf("OK: %s (%d bytes)", p, info.Size())
	}
}

// TestFixturesReadable verifies that the fixture files can be read back
// using the etlscan readers and contain the expected data.
func TestFixturesReadable(t *testing.T) {
	outputDir := findTestdataDir(t)

	t.Run("sessions", func(t *testing.T) {
		sessions, err := etlscan.ReadSessions(testutil.SessionsFixturePath(outputDir))
		if err != nil {
			t.Fatalf("ReadSessions: %v", err)
		}
		if len(sessions) != 3 {
			t.Fatalf("expected 3 sessions, got %d", len(sessions))
		}

		// Verify parent session
		s1 := sessions[0]
		if s1.ID != "test-session-1" {
			t.Errorf("session 0 ID = %q, want %q", s1.ID, "test-session-1")
		}
		if s1.Agent != "claude" {
			t.Errorf("session 0 Agent = %q, want %q", s1.Agent, "claude")
		}
		if s1.Model != "opus" {
			t.Errorf("session 0 Model = %q, want %q", s1.Model, "opus")
		}
		if s1.Workspace != "/workspace/project-a" {
			t.Errorf("session 0 Workspace = %q, want %q", s1.Workspace, "/workspace/project-a")
		}

		// Verify subagent session
		s2 := sessions[1]
		if !s2.IsSubagent {
			t.Error("session 1 should be a subagent")
		}
		if s2.ParentSessionID != "test-session-1" {
			t.Errorf("session 1 ParentSessionID = %q, want %q", s2.ParentSessionID, "test-session-1")
		}
		if s2.SubagentName != "Explore" {
			t.Errorf("session 1 SubagentName = %q, want %q", s2.SubagentName, "Explore")
		}

		// Verify third session
		s3 := sessions[2]
		if s3.GitRemote != "git@github.com:test/other-repo" {
			t.Errorf("session 2 GitRemote = %q, want %q", s3.GitRemote, "git@github.com:test/other-repo")
		}
	})

	t.Run("messages", func(t *testing.T) {
		messages, err := etlscan.ReadMessages(testutil.MessagesFixturePath(outputDir))
		if err != nil {
			t.Fatalf("ReadMessages: %v", err)
		}
		if len(messages) != 12 {
			t.Fatalf("expected 12 messages, got %d", len(messages))
		}

		// Check roles are present
		roles := make(map[string]bool)
		for _, m := range messages {
			roles[m.Role] = true
		}
		for _, r := range []string{"user", "assistant", "tool", "system"} {
			if !roles[r] {
				t.Errorf("missing role %q in messages", r)
			}
		}

		// Check Bash tool message
		m5 := messages[4]
		if m5.ToolName != "Bash" {
			t.Errorf("msg 4 ToolName = %q, want %q", m5.ToolName, "Bash")
		}
		if m5.BashCommand != "go test ./pkg/auth/..." {
			t.Errorf("msg 4 BashCommand = %q, want %q", m5.BashCommand, "go test ./pkg/auth/...")
		}

		// Check Read tool message
		m4 := messages[3]
		if m4.ToolName != "Read" {
			t.Errorf("msg 3 ToolName = %q, want %q", m4.ToolName, "Read")
		}
		if m4.ToolFilePath != "/workspace/project-a/pkg/auth/middleware.go" {
			t.Errorf("msg 3 ToolFilePath = %q", m4.ToolFilePath)
		}

		// Check long content truncation
		m3 := messages[2]
		if len(m3.Content) <= 4096 {
			t.Errorf("msg 2 Content should be >4096 chars, got %d", len(m3.Content))
		}
		if len(m3.ContentTruncated) >= len(m3.Content) {
			t.Errorf("msg 2 ContentTruncated (%d) should be shorter than Content (%d)",
				len(m3.ContentTruncated), len(m3.Content))
		}

		// Check Skill tool messages
		m11 := messages[10]
		if m11.ToolName != "Skill" {
			t.Errorf("msg 10 ToolName = %q, want Skill", m11.ToolName)
		}
		if m11.SkillName != "contextual-commit" {
			t.Errorf("msg 10 SkillName = %q, want contextual-commit", m11.SkillName)
		}
		m12 := messages[11]
		if m12.ToolName != "Skill" {
			t.Errorf("msg 11 ToolName = %q, want Skill", m12.ToolName)
		}
		if m12.SkillName != "contextual-commit" {
			t.Errorf("msg 11 SkillName = %q, want contextual-commit", m12.SkillName)
		}

		// Check searchable content exists
		found := map[string]bool{"Exit code 0": false, "authentication middleware": false, "xyzzy": false}
		for _, m := range messages {
			for term := range found {
				if contains(m.Content, term) {
					found[term] = true
				}
			}
		}
		for term, ok := range found {
			if !ok {
				t.Errorf("no message content contains %q", term)
			}
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
