package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mistakenot/auto-search/internal/app"
	"github.com/mistakenot/auto-search/internal/cli"
	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/testutil"
)

func TestInitCreatesSettingsAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, stderr, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("first init failed with code %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	sharedPath := filepath.Join(home, ".auto", "settings.json")
	searchPath := filepath.Join(home, ".auto", "search", "settings.json")
	assertFileExists(t, sharedPath)
	assertFileExists(t, searchPath)

	firstSharedBytes, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("read shared settings: %v", err)
	}
	firstSearchBytes, err := os.ReadFile(searchPath)
	if err != nil {
		t.Fatalf("read search settings: %v", err)
	}

	sharedCfg, err := config.LoadSharedSettings(sharedPath)
	if err != nil {
		t.Fatalf("load shared settings: %v", err)
	}
	if sharedCfg.Host == "" {
		t.Fatal("shared settings host should not be empty")
	}
	searchCfg, err := config.LoadSearchSettings(searchPath)
	if err != nil {
		t.Fatalf("load search settings: %v", err)
	}
	if searchCfg.DefaultIndex != config.DefaultIndexName {
		t.Fatalf("default_index = %q, want %q", searchCfg.DefaultIndex, config.DefaultIndexName)
	}
	wantDefaultInput := filepath.Join(home, ".auto", "etl", "output")
	if searchCfg.DefaultInput != wantDefaultInput {
		t.Fatalf("default_input = %q, want %q", searchCfg.DefaultInput, wantDefaultInput)
	}

	stdout, stderr, code = runCLI(t, "init")
	if code != 0 {
		t.Fatalf("second init failed with code %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	assertFileExists(t, sharedPath)
	assertFileExists(t, searchPath)

	secondSharedBytes, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("read shared settings after rerun: %v", err)
	}
	secondSearchBytes, err := os.ReadFile(searchPath)
	if err != nil {
		t.Fatalf("read search settings after rerun: %v", err)
	}

	if !bytes.Equal(firstSharedBytes, secondSharedBytes) {
		t.Fatalf("shared settings changed across repeated init runs\nfirst:\n%s\nsecond:\n%s", firstSharedBytes, secondSharedBytes)
	}
	if !bytes.Equal(firstSearchBytes, secondSearchBytes) {
		t.Fatalf("search settings changed across repeated init runs\nfirst:\n%s\nsecond:\n%s", firstSearchBytes, secondSearchBytes)
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func runCLI(t *testing.T, args ...string) (stdout string, stderr string, code int) {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer

	application := app.New(&out, &errOut)
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs(args)
	err := rootCmd.ExecuteContext(context.Background())
	if err != nil {
		code = 1
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				errOut.WriteString(exitErr.Err.Error())
				errOut.WriteByte('\n')
			}
		} else {
			errOut.WriteString(err.Error())
			errOut.WriteByte('\n')
		}
	}
	return out.String(), errOut.String(), code
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

// fixtureInputDir returns the absolute path to testdata/etl-output.
func fixtureInputDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "etl-output")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	if _, err := os.Stat(testutil.SessionsFixturePath(abs)); err != nil {
		t.Skipf("fixture files not found: %v", err)
	}
	return abs
}

// setupIndexedFixtures creates a temp HOME, runs init + index against
// committed fixtures, and returns the temp HOME path.
func setupIndexedFixtures(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, stderr, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init failed: %s", stderr)
	}

	inputDir := fixtureInputDir(t)
	_, stderr, code = runCLI(t, "index", "--input", inputDir)
	if code != 0 {
		t.Fatalf("index failed: %s", stderr)
	}

	return home
}

// decodeJSON unmarshals JSON output into a generic map.
func decodeJSON(t *testing.T, data string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("json decode failed: %v\nraw:\n%s", err, data)
	}
	return m
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestIndexFullBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, _, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init failed")
	}

	inputDir := fixtureInputDir(t)
	stdout, stderr, code := runCLI(t, "index", "--input", inputDir)
	if code != 0 {
		t.Fatalf("index failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)

	if out["sessions_indexed"] != float64(3) {
		t.Fatalf("sessions_indexed = %v, want 3", out["sessions_indexed"])
	}
	if out["messages_indexed"] != float64(12) {
		t.Fatalf("messages_indexed = %v, want 12", out["messages_indexed"])
	}

	dbPath := filepath.Join(home, ".auto", "search", "default.sqlite")
	assertFileExists(t, dbPath)
}

func TestIndexIncremental(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, _, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init failed")
	}

	inputDir := fixtureInputDir(t)

	// First index (full build).
	_, stderr, code := runCLI(t, "index", "--input", inputDir)
	if code != 0 {
		t.Fatalf("first index failed: code=%d\nstderr:\n%s", code, stderr)
	}

	// Second index (incremental).
	stdout, stderr, code := runCLI(t, "index", "--input", inputDir)
	if code != 0 {
		t.Fatalf("second index failed: code=%d\nstderr:\n%s", code, stderr)
	}

	out := decodeJSON(t, stdout)
	// With only one partition per dataset, the newest-always-reindex policy
	// means both get reprocessed. Verify it is not a full rebuild.
	if out["full_rebuild"] == true {
		t.Fatal("expected incremental update, got full rebuild")
	}
}

func TestSearchMessages(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "Exit code")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["scope"] != "messages" {
		t.Fatalf("scope = %v, want messages", meta["scope"])
	}
	totalHits := meta["total_hits"].(float64)
	if totalHits <= 0 {
		t.Fatalf("total_hits = %v, want > 0 for query 'Exit code'", totalHits)
	}
	totalMatches := meta["total_matches"].(float64)
	if totalMatches != totalHits {
		t.Fatalf("total_matches = %v, want %v", totalMatches, totalHits)
	}
	distinctSessions := meta["distinct_sessions"].(float64)
	if distinctSessions <= 0 {
		t.Fatalf("distinct_sessions = %v, want > 0", distinctSessions)
	}
	distinctMessages := meta["distinct_messages"].(float64)
	if distinctMessages <= 0 {
		t.Fatalf("distinct_messages = %v, want > 0", distinctMessages)
	}
	if meta["is_capped"] != false {
		t.Fatalf("is_capped = %v, want false", meta["is_capped"])
	}

	hits := out["hits"].([]any)
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit")
	}
	hit := hits[0].(map[string]any)
	for _, field := range []string{"id", "sessionId", "messageId", "messageType", "score", "snippet"} {
		if _, ok := hit[field]; !ok {
			t.Errorf("missing field %q in hit", field)
		}
	}
}

func TestSearchMessagesPaginationMeta(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "Exit code 0", "--offset", "1")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["total_hits"] != float64(2) {
		t.Fatalf("total_hits = %v, want 2", meta["total_hits"])
	}
	if meta["returned_hits"] != float64(1) {
		t.Fatalf("returned_hits = %v, want 1", meta["returned_hits"])
	}
	if meta["page_size"] != float64(20) {
		t.Fatalf("page_size = %v, want 20", meta["page_size"])
	}
	if meta["offset"] != float64(1) {
		t.Fatalf("offset = %v, want 1", meta["offset"])
	}
	if meta["has_more"] != false {
		t.Fatalf("has_more = %v, want false", meta["has_more"])
	}
	if _, ok := meta["next_offset"]; ok {
		t.Fatalf("next_offset should be omitted when has_more=false, got %v", meta["next_offset"])
	}
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
}

func TestSearchSessions(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "--scope", "sessions", "authentication")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["scope"] != "sessions" {
		t.Fatalf("scope = %v, want sessions", meta["scope"])
	}
	totalHits := meta["total_hits"].(float64)
	if totalHits <= 0 {
		t.Fatalf("total_hits = %v, want > 0 for query 'authentication'", totalHits)
	}
	totalMatches := meta["total_matches"].(float64)
	if totalMatches != totalHits {
		t.Fatalf("total_matches = %v, want %v", totalMatches, totalHits)
	}
	distinctSessions := meta["distinct_sessions"].(float64)
	if distinctSessions != totalHits {
		t.Fatalf("distinct_sessions = %v, want %v", distinctSessions, totalHits)
	}
	distinctMessages := meta["distinct_messages"].(float64)
	if distinctMessages <= 0 {
		t.Fatalf("distinct_messages = %v, want > 0", distinctMessages)
	}
	if meta["is_capped"] != false {
		t.Fatalf("is_capped = %v, want false", meta["is_capped"])
	}

	hits := out["hits"].([]any)
	hit := hits[0].(map[string]any)
	for _, field := range []string{"id", "sessionId", "score", "workspace", "firstMessageAt", "lastMessageAt", "totalMessages"} {
		if _, ok := hit[field]; !ok {
			t.Errorf("missing field %q in session hit", field)
		}
	}
}

func TestSearchSessionsPaginationMeta(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "--scope", "sessions", "--offset", "1", "User")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["total_hits"] != float64(3) {
		t.Fatalf("total_hits = %v, want 3", meta["total_hits"])
	}
	if meta["returned_hits"] != float64(2) {
		t.Fatalf("returned_hits = %v, want 2", meta["returned_hits"])
	}
	if meta["page_size"] != float64(20) {
		t.Fatalf("page_size = %v, want 20", meta["page_size"])
	}
	if meta["offset"] != float64(1) {
		t.Fatalf("offset = %v, want 1", meta["offset"])
	}
	if meta["has_more"] != false {
		t.Fatalf("has_more = %v, want false", meta["has_more"])
	}
}

func TestSearchModeVectorReturnsError(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "search", "--mode", "vector", "test query")
	if code == 0 {
		t.Fatal("expected non-zero exit code for --mode vector")
	}
	if !strings.Contains(stderr, "invalid --mode") {
		t.Fatalf("expected error message about invalid --mode, got: %s", stderr)
	}
}

func TestSearchWithSinceFilter(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "Exit code", "--since", "7d")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["scope"] != "messages" {
		t.Fatalf("scope = %v, want messages", meta["scope"])
	}
	if _, ok := out["hits"]; !ok {
		t.Fatal("expected hits field in response")
	}
}

func TestSearchWithAbsoluteDateWindow(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(
		t,
		"search",
		"Exit code",
		"--after", "2024-03-21T08:00:00Z",
		"--before", "2024-03-21T09:00:00Z",
	)
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["scope"] != "messages" {
		t.Fatalf("scope = %v, want messages", meta["scope"])
	}
	totalHits := meta["total_hits"].(float64)
	if totalHits <= 0 {
		t.Fatalf("total_hits = %v, want > 0", totalHits)
	}
}

func TestSearchRejectsMixedTimeFilterModes(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(
		t,
		"search",
		"Exit code",
		"--since", "7d",
		"--after", "2024-03-21",
	)
	if code == 0 {
		t.Fatal("expected non-zero exit code for mixed time filter modes")
	}
	if !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("expected combined-mode error in stderr, got:\n%s", stderr)
	}
}

func TestSearchRejectsInvalidDateFormat(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "search", "Exit code", "--after", "03/21/2024")
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid --after format")
	}
	if !strings.Contains(stderr, "invalid --after value") {
		t.Fatalf("expected invalid --after message in stderr, got:\n%s", stderr)
	}
}

func TestSearchRejectsInvalidDateRange(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(
		t,
		"search",
		"Exit code",
		"--after", "2024-03-21T09:00:00Z",
		"--before", "2024-03-21T08:00:00Z",
	)
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid date range")
	}
	if !strings.Contains(stderr, "invalid time range") {
		t.Fatalf("expected invalid range message in stderr, got:\n%s", stderr)
	}
}

func TestSearchWildcardFallback(t *testing.T) {
	setupIndexedFixtures(t)

	// "xyzzy" appears in fixture msg-006 content: "The xyzzy token validation..."
	// This is a rare term that returns < 3 exact hits. Prefix fallback is
	// attempted but only triggers wildcard_fallback=true if the prefix query
	// returns MORE hits than the exact query.
	stdout, stderr, code := runCLI(t, "search", "xyzzy")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	totalHits := meta["total_hits"].(float64)
	if totalHits == 0 {
		t.Fatal("expected at least 1 hit for 'xyzzy'")
	}
	// Verify wildcard_fallback field is present.
	if _, ok := meta["wildcard_fallback"]; !ok {
		t.Fatal("expected wildcard_fallback field in meta")
	}
}

func TestSearchWithRoleFilter(t *testing.T) {
	setupIndexedFixtures(t)

	// "Exit code" appears in tool messages. Filter to only tool role.
	stdout, stderr, code := runCLI(t, "search", "Exit code", "--role", "tool")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	totalHits := meta["total_hits"].(float64)
	if totalHits == 0 {
		t.Fatal("expected at least 1 hit for --role tool")
	}

	hits := out["hits"].([]any)
	for _, h := range hits {
		hit := h.(map[string]any)
		if hit["messageType"] != "tool" {
			t.Errorf("expected messageType=tool, got %v", hit["messageType"])
		}
	}
}

func TestSearchWithRoleFilterNoResults(t *testing.T) {
	setupIndexedFixtures(t)

	// "Exit code" does not appear in user messages.
	stdout, stderr, code := runCLI(t, "search", "Exit code", "--role", "user")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	totalHits := meta["total_hits"].(float64)
	if totalHits != 0 {
		t.Errorf("expected 0 hits for 'Exit code' with --role user, got %v", totalHits)
	}
}

func TestSearchSessionsWithRoleFilter(t *testing.T) {
	setupIndexedFixtures(t)

	// "authentication" appears in session transcripts. Filter to sessions that
	// contain tool-role messages (all fixture sessions do).
	stdout, stderr, code := runCLI(t, "search", "--scope", "sessions", "--role", "tool", "authentication")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	totalHits := meta["total_hits"].(float64)
	if totalHits == 0 {
		t.Fatal("expected at least 1 session hit with --role tool")
	}
}

func TestSearchWithFieldFilter(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "contextual-commit", "--field", "tool_input")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["total_hits"] != float64(1) {
		t.Fatalf("total_hits = %v, want 1", meta["total_hits"])
	}
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	hit := hits[0].(map[string]any)
	if hit["messageId"] != "msg-011" {
		t.Fatalf("messageId = %v, want msg-011", hit["messageId"])
	}
}

func TestSearchSessionsWithFieldFilter(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "--scope", "sessions", "--field", "tool_output", "authentication")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["total_hits"] != float64(1) {
		t.Fatalf("total_hits = %v, want 1", meta["total_hits"])
	}
	hits := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	hit := hits[0].(map[string]any)
	if hit["sessionId"] != "test-session-1" {
		t.Fatalf("sessionId = %v, want test-session-1", hit["sessionId"])
	}
}

func TestSearchRejectsInvalidField(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "search", "auth", "--field", "bad_field")
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid --field")
	}
	if !strings.Contains(stderr, "invalid --field value") {
		t.Fatalf("expected invalid field error in stderr, got:\n%s", stderr)
	}
}

func TestSearchCwdRemoteConflict(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "search", "--cwd", "/workspace", "--remote", "origin", "test")
	if code == 0 {
		t.Fatal("expected non-zero exit code for --cwd + --remote conflict")
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("expected 'mutually exclusive' in stderr, got:\n%s", stderr)
	}
}

func TestSearchRejectsNegativeOffset(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "search", "Exit code", "--offset", "-1")
	if code == 0 {
		t.Fatal("expected non-zero exit code for negative offset")
	}
	if !strings.Contains(stderr, "--offset must be >= 0") {
		t.Fatalf("expected offset error in stderr, got:\n%s", stderr)
	}
}

func TestSessionGet(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "get", "test-session-1")
	if code != 0 {
		t.Fatalf("session get failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// Session get renders XML-like role tags with closing tags.
	if !strings.Contains(stdout, "<user index=") {
		t.Fatalf("expected <user> role tag in session get output, got:\n%.500s", stdout)
	}
	if !strings.Contains(stdout, "</user>") {
		t.Fatal("expected </user> closing tag in session get output")
	}
	if !strings.Contains(stdout, "<agent index=") {
		t.Fatal("expected <agent> role tag in session get output")
	}
	if !strings.Contains(stdout, "</agent>") {
		t.Fatal("expected </agent> closing tag in session get output")
	}
	if !strings.Contains(stdout, "<tool") {
		t.Fatal("expected <tool> tag in session get output")
	}
	if !strings.Contains(stdout, "</tool>") {
		t.Fatal("expected </tool> closing tag in session get output")
	}

	// Problem 2: tool tags include arg previews.
	if !strings.Contains(stdout, `cmd="go test`) {
		t.Fatalf("expected cmd= attribute on Bash tool tag, got:\n%.500s", stdout)
	}
	if !strings.Contains(stdout, `path="/workspace/project-a/pkg/auth/middleware.go"`) {
		t.Fatal("expected path= attribute on Read tool tag")
	}

	// Problem 3: message ID is derivable from session_id + index, so not in tag.
	if strings.Contains(stdout, `id="`) {
		t.Fatal("id= attribute should not be in role tags (derivable from session_id + index)")
	}

	// Problem 5: truncated messages include drill-down hint.
	if strings.Contains(stdout, "…[truncated]…") {
		t.Fatal("old truncation marker found; expected drill-down hint")
	}
	if strings.Contains(stdout, "autosearch message get") {
		// Long content from msg-003 should be truncated with hint.
		if !strings.Contains(stdout, "autosearch message get msg-003") {
			t.Fatal("expected truncation hint to reference msg-003")
		}
	}
}

func TestSessionDescribe(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "describe", "test-session-1", "--request-id", "desc-sess-1")
	if code != 0 {
		t.Fatalf("session describe failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	session := out["session"].(map[string]any)

	totalMessages := session["totalMessages"].(float64)
	if totalMessages <= 0 {
		t.Fatalf("totalMessages = %v, want > 0", totalMessages)
	}
	if session["id"] != "test-session-1" {
		t.Fatalf("session.id = %v, want test-session-1", session["id"])
	}
	if session["transcriptSummary"] == "" {
		t.Fatal("expected non-empty transcriptSummary")
	}

	meta := out["_meta"].(map[string]any)
	if meta["request_id"] != "desc-sess-1" {
		t.Fatalf("request_id = %v, want desc-sess-1", meta["request_id"])
	}
}

func TestSessionListNewFields(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "list")
	if code != 0 {
		t.Fatalf("session list failed: code=%d\nstderr:\n%s", code, stderr)
	}

	out := decodeJSON(t, stdout)
	sessions := out["sessions"].([]any)

	// Find the subagent session (test-session-2)
	var found bool
	for _, s := range sessions {
		sess := s.(map[string]any)
		if sess["session_id"] == "test-session-2" {
			found = true
			if sess["is_subagent"] != true {
				t.Fatalf("expected is_subagent=true for test-session-2, got %v", sess["is_subagent"])
			}
			if sess["parent_session_id"] != "test-session-1" {
				t.Fatalf("expected parent_session_id=test-session-1, got %v", sess["parent_session_id"])
			}
			if sess["subagent_name"] != "Explore" {
				t.Fatalf("expected subagent_name=Explore, got %v", sess["subagent_name"])
			}
			durMs, ok := sess["duration_ms"].(float64)
			if !ok || durMs <= 0 {
				t.Fatalf("expected positive duration_ms, got %v", sess["duration_ms"])
			}
			break
		}
	}
	if !found {
		t.Fatal("test-session-2 not found in session list output")
	}

	// Verify parent sessions have is_subagent=false
	for _, s := range sessions {
		sess := s.(map[string]any)
		if sess["session_id"] == "test-session-1" {
			if sess["is_subagent"] != false {
				t.Fatalf("expected is_subagent=false for test-session-1")
			}
			break
		}
	}
}

func TestSessionListSubagentFilter(t *testing.T) {
	setupIndexedFixtures(t)

	// --subagent: only subagent sessions
	stdout, stderr, code := runCLI(t, "session", "list", "--subagent")
	if code != 0 {
		t.Fatalf("session list --subagent failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	sessions := out["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 subagent session, got %d", len(sessions))
	}
	sess := sessions[0].(map[string]any)
	if sess["session_id"] != "test-session-2" {
		t.Fatalf("expected test-session-2, got %v", sess["session_id"])
	}

	// --no-subagent: only parent sessions
	stdout, stderr, code = runCLI(t, "session", "list", "--no-subagent")
	if code != 0 {
		t.Fatalf("session list --no-subagent failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out = decodeJSON(t, stdout)
	sessions = out["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 parent sessions, got %d", len(sessions))
	}

	// --subagent --no-subagent: mutual exclusion error
	_, stderr, code = runCLI(t, "session", "list", "--subagent", "--no-subagent")
	if code == 0 {
		t.Fatal("expected error for --subagent --no-subagent")
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Fatalf("expected mutual exclusion error, got: %s", stderr)
	}
}

func TestSessionListMinDuration(t *testing.T) {
	setupIndexedFixtures(t)

	// Fixtures: session-1 = 3600s (60min), session-2 = 1000s (~16.7min), session-3 = 3600s (60min)
	stdout, stderr, code := runCLI(t, "session", "list", "--min-duration", "30m")
	if code != 0 {
		t.Fatalf("session list --min-duration failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	sessions := out["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions >= 30min, got %d", len(sessions))
	}
	for _, s := range sessions {
		sess := s.(map[string]any)
		if sess["session_id"] == "test-session-2" {
			t.Fatal("test-session-2 (~16.7min) should have been filtered out")
		}
	}
}

func TestSessionListSortBy(t *testing.T) {
	setupIndexedFixtures(t)

	// Sort by duration
	stdout, stderr, code := runCLI(t, "session", "list", "--sort-by", "duration")
	if code != 0 {
		t.Fatalf("session list --sort-by duration failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	sessions := out["sessions"].([]any)
	if len(sessions) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(sessions))
	}
	// First two should be the 60-min sessions, last should be the 16.7-min subagent
	lastSess := sessions[len(sessions)-1].(map[string]any)
	if lastSess["session_id"] != "test-session-2" {
		t.Fatalf("expected test-session-2 last by duration, got %v", lastSess["session_id"])
	}

	// Sort by tokens
	stdout, stderr, code = runCLI(t, "session", "list", "--sort-by", "tokens")
	if code != 0 {
		t.Fatalf("session list --sort-by tokens failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out = decodeJSON(t, stdout)
	sessions = out["sessions"].([]any)
	// session-1 has 8000 tokens, session-3 has 6500, session-2 has 3500
	firstSess := sessions[0].(map[string]any)
	if firstSess["session_id"] != "test-session-1" {
		t.Fatalf("expected test-session-1 first by tokens, got %v", firstSess["session_id"])
	}

	// Invalid sort-by
	_, stderr, code = runCLI(t, "session", "list", "--sort-by", "invalid")
	if code == 0 {
		t.Fatal("expected error for --sort-by invalid")
	}
	if !strings.Contains(stderr, "invalid --sort-by") {
		t.Fatalf("expected sort-by error, got: %s", stderr)
	}
}

func TestSessionListSortByToolDurationAccepted(t *testing.T) {
	setupIndexedFixtures(t)

	// The fixture's total_turn_duration_ms is zero (predates PR 1) so we
	// don't assert ordering here — we only assert the flag is wired and
	// returns rows without erroring. The ordering correctness is covered
	// by indexdb_test.go::TestListSessionsSortByToolDuration.
	stdout, stderr, code := runCLI(t, "session", "list", "--sort-by", "tool_duration")
	if code != 0 {
		t.Fatalf("session list --sort-by tool_duration failed: code=%d\nstderr:\n%s", code, stderr)
	}
	out := decodeJSON(t, stdout)
	if _, ok := out["sessions"]; !ok {
		t.Fatalf("expected sessions key, got %v", out)
	}
}

func TestSessionListNewFlagsRejectBadInput(t *testing.T) {
	setupIndexedFixtures(t)

	cases := []struct {
		name     string
		args     []string
		wantSnip string
	}{
		{
			name:     "min-tool-duration unparseable",
			args:     []string{"session", "list", "--min-tool-duration", "foo"},
			wantSnip: "min-tool-duration",
		},
		{
			name:     "min-tool-duration unknown unit",
			args:     []string{"session", "list", "--min-tool-duration", "60x"},
			wantSnip: "min-tool-duration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Fatalf("expected non-zero exit for %v", tc.args)
			}
			if !strings.Contains(stderr, tc.wantSnip) {
				t.Fatalf("stderr = %q, want substring %q", stderr, tc.wantSnip)
			}
		})
	}
}

func TestSearchNewFlagsRejectBadInput(t *testing.T) {
	setupIndexedFixtures(t)

	cases := []struct {
		name     string
		args     []string
		wantSnip string
	}{
		{
			name:     "min-tool-duration unparseable",
			args:     []string{"search", "foo", "--min-tool-duration", "wat"},
			wantSnip: "min-tool-duration",
		},
		{
			name:     "duration flags rejected on sessions scope",
			args:     []string{"search", "foo", "--scope", "sessions", "--min-tool-duration", "60s"},
			wantSnip: "only apply to --scope messages",
		},
		{
			name:     "interrupted rejected on sessions scope",
			args:     []string{"search", "foo", "--scope", "sessions", "--interrupted"},
			wantSnip: "only apply to --scope messages",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Fatalf("expected non-zero exit for %v", tc.args)
			}
			if !strings.Contains(stderr, tc.wantSnip) {
				t.Fatalf("stderr = %q, want substring %q", stderr, tc.wantSnip)
			}
		})
	}
}

func TestSessionDescribeNewFields(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "describe", "test-session-2")
	if code != 0 {
		t.Fatalf("session describe failed: code=%d\nstderr:\n%s", code, stderr)
	}

	out := decodeJSON(t, stdout)
	session := out["session"].(map[string]any)

	if session["isSubagent"] != true {
		t.Fatalf("expected isSubagent=true, got %v", session["isSubagent"])
	}
	if session["parentSessionId"] != "test-session-1" {
		t.Fatalf("expected parentSessionId=test-session-1, got %v", session["parentSessionId"])
	}
	if session["subagentName"] != "Explore" {
		t.Fatalf("expected subagentName=Explore, got %v", session["subagentName"])
	}

	durMs, ok := session["durationMs"].(float64)
	if !ok || durMs <= 0 {
		t.Fatalf("expected positive durationMs, got %v", session["durationMs"])
	}

	if _, ok := session["totalTurnDurationMs"].(float64); !ok {
		t.Fatalf("expected totalTurnDurationMs field in session describe output, got %v", session["totalTurnDurationMs"])
	}

	userMsgs, ok := session["userMessages"].(float64)
	if !ok {
		t.Fatalf("expected userMessages field, got %v", session["userMessages"])
	}
	// test-session-2 has 1 user message (msg-007: "Explore the auth module structure")
	if userMsgs != 1 {
		t.Fatalf("expected userMessages=1, got %v", userMsgs)
	}
}

func TestMessageGet(t *testing.T) {
	setupIndexedFixtures(t)

	// msg-002 content: "Help me debug the authentication middleware retry logic"
	stdout, stderr, code := runCLI(t, "message", "get", "msg-002")
	if code != 0 {
		t.Fatalf("message get failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, "authentication middleware") {
		t.Fatalf("expected message content in output, got:\n%s", stdout)
	}
}

func TestMessageDescribe(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "message", "describe", "--request-id", "test-req-1", "msg-002")
	if code != 0 {
		t.Fatalf("message describe failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)

	meta := out["_meta"].(map[string]any)
	if meta["request_id"] != "test-req-1" {
		t.Fatalf("request_id = %v, want test-req-1", meta["request_id"])
	}

	msg := out["message"].(map[string]any)
	if msg["id"] != "msg-002" {
		t.Fatalf("message.id = %v, want msg-002", msg["id"])
	}
	if msg["sessionId"] != "test-session-1" {
		t.Fatalf("message.sessionId = %v, want test-session-1", msg["sessionId"])
	}
}

func TestSearchHighlight(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, _, code := runCLI(t, "search", "authentication", "--highlight")
	if code != 0 {
		t.Fatal("search --highlight failed")
	}

	if !strings.Contains(stdout, "**") {
		t.Error("expected ** highlight markers in output")
	}
}

func TestIndexSkipsDuplicatesE2E(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, _, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init failed")
	}

	// Generate fixtures containing duplicate message and session IDs.
	inputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateDuplicateMessageFixtures(inputDir); err != nil {
		t.Fatalf("GenerateDuplicateMessageFixtures: %v", err)
	}
	if err := testutil.GenerateDuplicateSessionFixtures(inputDir); err != nil {
		t.Fatalf("GenerateDuplicateSessionFixtures: %v", err)
	}

	stdout, stderr, code := runCLI(t, "index", "--input", inputDir)
	if code != 0 {
		t.Fatalf("index should succeed despite duplicates: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)

	// Verify JSON output includes skip counts.
	if out["messages_skipped"] != float64(1) {
		t.Errorf("messages_skipped = %v, want 1", out["messages_skipped"])
	}
	if out["sessions_skipped"] != float64(1) {
		t.Errorf("sessions_skipped = %v, want 1", out["sessions_skipped"])
	}
	if out["messages_indexed"] != float64(2) {
		t.Errorf("messages_indexed = %v, want 2", out["messages_indexed"])
	}
	if out["sessions_indexed"] != float64(2) {
		t.Errorf("sessions_indexed = %v, want 2", out["sessions_indexed"])
	}

	// Warnings should appear on stderr.
	if !strings.Contains(stderr, "WARNING: skipping duplicate message") {
		t.Errorf("expected duplicate message warning on stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "WARNING: skipping duplicate session") {
		t.Errorf("expected duplicate session warning on stderr, got:\n%s", stderr)
	}
}

func TestMessageDescribeSkillName(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "message", "describe", "msg-012")
	if code != 0 {
		t.Fatalf("message describe failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	msg := out["message"].(map[string]any)
	if msg["skillName"] != "contextual-commit" {
		t.Errorf("skillName = %v, want contextual-commit", msg["skillName"])
	}
	if msg["toolName"] != "Skill" {
		t.Errorf("toolName = %v, want Skill", msg["toolName"])
	}
}

func TestSessionDescribeSkillCounts(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "describe", "test-session-1")
	if code != 0 {
		t.Fatalf("session describe failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	session := out["session"].(map[string]any)

	skillMessages := session["skillMessages"].(float64)
	if skillMessages != 2 {
		t.Errorf("skillMessages = %v, want 2", skillMessages)
	}

	skillsUsed := session["skillsUsed"].([]any)
	if len(skillsUsed) != 1 || skillsUsed[0] != "contextual-commit" {
		t.Errorf("skillsUsed = %v, want [contextual-commit]", skillsUsed)
	}
}

func TestSessionGetSkillAttribute(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "session", "get", "test-session-1")
	if code != 0 {
		t.Fatalf("session get failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	if !strings.Contains(stdout, `skill="contextual-commit"`) {
		t.Errorf("expected skill attribute in session get output, got:\n%.500s", stdout)
	}
}

func TestSearchWithSkillFilter(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "search", "--skill", "contextual-commit", "commit")
	if code != 0 {
		t.Fatalf("search failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	totalHits := meta["total_hits"].(float64)
	if totalHits == 0 {
		t.Fatal("expected at least 1 hit for skill filter")
	}
}

func TestStatsMessagesSuccess(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "stats", "--scope", "messages", "--group-by", "session_id")
	if code != 0 {
		t.Fatalf("stats failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["scope"] != "messages" {
		t.Fatalf("scope = %v, want messages", meta["scope"])
	}
	if meta["group_by"] != "session_id" {
		t.Fatalf("group_by = %v, want session_id", meta["group_by"])
	}
	if meta["total_matches"] != float64(12) {
		t.Fatalf("total_matches = %v, want 12", meta["total_matches"])
	}
	buckets := out["buckets"].([]any)
	if len(buckets) != 3 {
		t.Fatalf("len(buckets) = %d, want 3", len(buckets))
	}
}

func TestStatsMetadataPagination(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t,
		"stats",
		"--scope", "messages",
		"--group-by", "session_id",
		"--query", `"Exit code 0"`,
		"--limit", "1",
	)
	if code != 0 {
		t.Fatalf("stats failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	meta := out["_meta"].(map[string]any)
	if meta["total_matches"] != float64(2) {
		t.Fatalf("total_matches = %v, want 2", meta["total_matches"])
	}
	if meta["total_buckets_unfiltered"] != float64(2) {
		t.Fatalf("total_buckets_unfiltered = %v, want 2", meta["total_buckets_unfiltered"])
	}
	if meta["total_buckets"] != float64(2) {
		t.Fatalf("total_buckets = %v, want 2", meta["total_buckets"])
	}
	if meta["returned_buckets"] != float64(1) {
		t.Fatalf("returned_buckets = %v, want 1", meta["returned_buckets"])
	}
	if meta["has_more"] != true {
		t.Fatalf("has_more = %v, want true", meta["has_more"])
	}
	if meta["next_offset"] != float64(1) {
		t.Fatalf("next_offset = %v, want 1", meta["next_offset"])
	}
}

func TestStatsDrilldownSampleIDs(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t,
		"stats",
		"--scope", "messages",
		"--group-by", "session_id",
		"--query", `"Exit code 0"`,
		"--limit", "1",
	)
	if code != 0 {
		t.Fatalf("stats failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	out := decodeJSON(t, stdout)
	buckets := out["buckets"].([]any)
	if len(buckets) == 0 {
		t.Fatal("expected at least one bucket")
	}
	first := buckets[0].(map[string]any)
	sampleSessionID, _ := first["sample_session_id"].(string)
	sampleMessageID, _ := first["sample_message_id"].(string)
	if sampleSessionID == "" || sampleMessageID == "" {
		t.Fatalf("missing sample ids in bucket: %+v", first)
	}

	sessionOut, sessionErr, sessionCode := runCLI(t, "session", "get", sampleSessionID)
	if sessionCode != 0 {
		t.Fatalf("session get failed for sample_session_id=%s: stderr=%s", sampleSessionID, sessionErr)
	}
	if !strings.Contains(sessionOut, "<") {
		t.Fatalf("expected rendered transcript for sample_session_id=%s", sampleSessionID)
	}

	messageOut, messageErr, messageCode := runCLI(t, "message", "get", sampleMessageID)
	if messageCode != 0 {
		t.Fatalf("message get failed for sample_message_id=%s: stderr=%s", sampleMessageID, messageErr)
	}
	if strings.TrimSpace(messageOut) == "" {
		t.Fatalf("expected message content for sample_message_id=%s", sampleMessageID)
	}
}

func TestStatsInvalidGroupBy(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "stats", "--scope", "messages", "--group-by", "nope")
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid --group-by")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "invalid --group-by value") {
		t.Fatalf("expected invalid group-by error, got:\n%s", stderr)
	}
}

func TestStatsScopeKeyMismatch(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "stats", "--scope", "sessions", "--group-by", "bash_command")
	if code == 0 {
		t.Fatal("expected non-zero exit code for scope/key mismatch")
	}
	if !strings.Contains(stderr, "invalid --group-by value") {
		t.Fatalf("expected invalid --group-by error, got:\n%s", stderr)
	}
}

func TestStatsCWDRemoteConflict(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "stats", "--scope", "messages", "--group-by", "session_id", "--cwd", "/workspace/project-a", "--remote", "git@github.com:test/repo")
	if code == 0 {
		t.Fatal("expected non-zero exit code for --cwd + --remote conflict")
	}
	if !strings.Contains(stderr, "--cwd and --remote are mutually exclusive") {
		t.Fatalf("expected cwd/remote conflict, got:\n%s", stderr)
	}
}

func TestStatsInvalidTimeFlags(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "stats", "--scope", "messages", "--group-by", "session_id", "--since", "7d", "--after", "2026-03-01")
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid time mode mix")
	}
	if !strings.Contains(stderr, "--since") {
		t.Fatalf("expected --since error, got:\n%s", stderr)
	}
}

func TestStatsInvalidRole(t *testing.T) {
	setupIndexedFixtures(t)

	_, stderr, code := runCLI(t, "stats", "--scope", "messages", "--group-by", "session_id", "--role", "system")
	if code == 0 {
		t.Fatal("expected non-zero exit code for invalid role")
	}
	if !strings.Contains(stderr, "invalid --role value") {
		t.Fatalf("expected invalid --role error, got:\n%s", stderr)
	}
}

func TestSkillsList(t *testing.T) {
	setupIndexedFixtures(t)

	stdout, stderr, code := runCLI(t, "skills")
	if code != 0 {
		t.Fatalf("skills failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSON(t, stdout)
	skills := out["skills"].([]any)
	if len(skills) == 0 {
		t.Fatal("expected at least 1 skill")
	}
	first := skills[0].(map[string]any)
	if first["skillName"] != "contextual-commit" {
		t.Errorf("first skill = %v, want contextual-commit", first["skillName"])
	}
	if first["count"] != float64(2) {
		t.Errorf("count = %v, want 2", first["count"])
	}
}

func TestIndexOnEmptyInputFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runCLI(t, "init")

	emptyDir := filepath.Join(home, "empty-input")
	os.MkdirAll(emptyDir, 0o755)

	_, _, code := runCLI(t, "index", "--input", emptyDir)
	if code == 0 {
		t.Error("expected failure on empty input directory")
	}
}
