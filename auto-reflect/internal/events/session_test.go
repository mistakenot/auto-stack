package events

import "testing"

func clearSessionEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"AUTO_SESSION_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		t.Setenv(key, "")
	}
}

func TestDetectSessionIDClaudeCode(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "5f03889e-5896-457a-a503-16643a17587e")

	if got := DetectSessionID(); got != "5f03889e-5896-457a-a503-16643a17587e" {
		t.Fatalf("DetectSessionID() = %q, want claude code session id", got)
	}
}

func TestDetectSessionIDPrecedence(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "from-claude-code")
	t.Setenv("AUTO_SESSION_ID", "from-auto")

	if got := DetectSessionID(); got != "from-auto" {
		t.Fatalf("DetectSessionID() = %q, want AUTO_SESSION_ID to take precedence", got)
	}
}

func TestDetectSessionIDEmpty(t *testing.T) {
	clearSessionEnv(t)

	if got := DetectSessionID(); got != "" {
		t.Fatalf("DetectSessionID() = %q, want empty when no env keys set", got)
	}
}

func TestDetectSessionIDTrimsWhitespace(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "  padded-id  ")

	if got := DetectSessionID(); got != "padded-id" {
		t.Fatalf("DetectSessionID() = %q, want trimmed id", got)
	}
}
