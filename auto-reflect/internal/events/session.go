package events

import (
	"os"
	"strings"
)

// DetectSessionID resolves the current session id from the environment, in
// precedence order AUTO_SESSION_ID -> CODEX_SESSION_ID -> CLAUDE_SESSION_ID ->
// CLAUDE_CODE_SESSION_ID. AUTO_SESSION_ID is the manual override;
// CLAUDE_CODE_SESSION_ID is what Claude Code actually exports (the session
// UUID auto-etl/auto-search index, so events join to transcripts). Returns ""
// when none are set. A --session flag override is handled by the caller, not
// here.
func DetectSessionID() string {
	for _, key := range []string{"AUTO_SESSION_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

// DetectAgent resolves the current agent name from the environment, in
// precedence order AUTO_AGENT -> CODEX_AGENT -> CLAUDE_AGENT. Returns "" when
// none are set.
func DetectAgent() string {
	for _, key := range []string{"AUTO_AGENT", "CODEX_AGENT", "CLAUDE_AGENT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
