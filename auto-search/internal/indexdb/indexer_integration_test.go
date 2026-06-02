package indexdb_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/testutil"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "etl-output")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	if _, err := os.Stat(testutil.SessionsFixturePath(abs)); err != nil {
		t.Skipf("fixture files not found at %s: %v", abs, err)
	}
	return abs
}

func TestFullBuildFromFixtures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	result, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}
	if !result.FullRebuild {
		t.Error("expected full rebuild")
	}
	if result.SessionsIndexed != 3 {
		t.Errorf("sessions = %d, want 3", result.SessionsIndexed)
	}
	if result.MessagesIndexed != 12 {
		t.Errorf("messages = %d, want 12", result.MessagesIndexed)
	}
	if result.FilesProcessed != 2 {
		t.Errorf("files = %d, want 2", result.FilesProcessed)
	}

	// Verify DB integrity.
	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sessions, messages, indexState, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 3 {
		t.Errorf("session rows = %d, want 3", sessions)
	}
	if messages != 12 {
		t.Errorf("message rows = %d, want 12", messages)
	}
	if indexState != 2 {
		t.Errorf("index_state rows = %d, want 2", indexState)
	}
}

func TestToolUseResultJSONColumnAndRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Build an index from a fixture carrying the toolUseResult envelope.
	outputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateAUQFixtures(outputDir); err != nil {
		t.Fatalf("GenerateAUQFixtures: %v", err)
	}

	dbPath := filepath.Join(home, "test.sqlite")
	if _, err := indexdb.FullBuild(dbPath, outputDir, os.Stderr); err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// The messages table must carry the tool_use_result_json column.
	rows, err := db.Query("PRAGMA table_info('messages')")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "tool_use_result_json" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	if !found {
		t.Fatal("messages table is missing the tool_use_result_json column")
	}

	// The tool_result row round-trips the envelope verbatim.
	msg, err := indexdb.GetMessageByID(db, "auq-msg-result")
	if err != nil {
		t.Fatalf("GetMessageByID(auq-msg-result): %v", err)
	}
	if msg.ToolUseResultJSON != testutil.AUQEnvelopeJSON {
		t.Errorf("ToolUseResultJSON = %q, want %q", msg.ToolUseResultJSON, testutil.AUQEnvelopeJSON)
	}

	// The assistant tool_use row has no envelope.
	useMsg, err := indexdb.GetMessageByID(db, "auq-msg-use")
	if err != nil {
		t.Fatalf("GetMessageByID(auq-msg-use): %v", err)
	}
	if useMsg.ToolUseResultJSON != "" {
		t.Errorf("assistant tool_use ToolUseResultJSON = %q, want empty", useMsg.ToolUseResultJSON)
	}
}

func TestIncrementalUpdateIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")
	fixture := fixtureDir(t)

	// First build.
	result1, err := indexdb.IncrementalUpdate(dbPath, fixture, os.Stderr)
	if err != nil {
		t.Fatalf("IncrementalUpdate 1: %v", err)
	}
	if !result1.FullRebuild {
		t.Error("first run should be a full rebuild")
	}

	// Second run: should be incremental (newest partitions always reindex,
	// but row counts should not change).
	result2, err := indexdb.IncrementalUpdate(dbPath, fixture, os.Stderr)
	if err != nil {
		t.Fatalf("IncrementalUpdate 2: %v", err)
	}
	if result2.FullRebuild {
		t.Error("second run should not be a full rebuild")
	}

	// Row counts should remain the same.
	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sessions, messages, _, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 3 {
		t.Errorf("session rows after 2nd run = %d, want 3", sessions)
	}
	if messages != 12 {
		t.Errorf("message rows after 2nd run = %d, want 12", messages)
	}
}

func TestSchemaVersionRebuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")
	fixture := fixtureDir(t)

	// Initial build.
	_, err := indexdb.FullBuild(dbPath, fixture, os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	// Tamper with schema version.
	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_info SET schema_version = 0"); err != nil {
		t.Fatalf("update schema_version: %v", err)
	}
	db.Close()

	// IncrementalUpdate should detect stale schema and do full rebuild.
	result, err := indexdb.IncrementalUpdate(dbPath, fixture, os.Stderr)
	if err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	if !result.FullRebuild {
		t.Error("expected full rebuild after schema version change")
	}
}

func TestQuerySessionByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	_, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sess, err := indexdb.GetSessionByID(db, "test-session-1")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if sess.SessionID != "test-session-1" {
		t.Errorf("session_id = %q, want test-session-1", sess.SessionID)
	}
	if sess.Workspace != "/workspace/project-a" {
		t.Errorf("workspace = %q", sess.Workspace)
	}
}

func TestQueryMessageByID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	_, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	msg, err := indexdb.GetMessageByID(db, "msg-002")
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if msg.MessageID != "msg-002" {
		t.Errorf("message_id = %q, want msg-002", msg.MessageID)
	}
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if msg.SessionID != "test-session-1" {
		t.Errorf("session_id = %q, want test-session-1", msg.SessionID)
	}
}

func TestNeighborMessageIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	_, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// msg-002 is at index 1 in test-session-1.
	prev, next, err := indexdb.NeighborMessageIDs(db, "test-session-1", 1)
	if err != nil {
		t.Fatalf("NeighborMessageIDs: %v", err)
	}
	if prev != "msg-001" {
		t.Errorf("prev = %q, want msg-001", prev)
	}
	if next != "msg-003" {
		t.Errorf("next = %q, want msg-003", next)
	}
}

func TestSessionMessages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	_, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	msgs, err := indexdb.SessionMessages(db, "test-session-1")
	if err != nil {
		t.Fatalf("SessionMessages: %v", err)
	}
	if len(msgs) != 8 {
		t.Fatalf("expected 8 messages for test-session-1, got %d", len(msgs))
	}
	// Verify ordering.
	for i := 1; i < len(msgs); i++ {
		if msgs[i].MessageIndex <= msgs[i-1].MessageIndex {
			t.Errorf("messages not in order: index[%d]=%d, index[%d]=%d",
				i-1, msgs[i-1].MessageIndex, i, msgs[i].MessageIndex)
		}
	}
}

func TestCountSessionMessages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dbPath := filepath.Join(home, "test.sqlite")

	_, err := indexdb.FullBuild(dbPath, fixtureDir(t), os.Stderr)
	if err != nil {
		t.Fatalf("FullBuild: %v", err)
	}

	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	counts, err := indexdb.CountSessionMessages(db, "test-session-1")
	if err != nil {
		t.Fatalf("CountSessionMessages: %v", err)
	}
	if counts.Total != 8 {
		t.Errorf("total = %d, want 8", counts.Total)
	}
	if counts.Tool == 0 {
		t.Error("expected tool message count > 0")
	}
	if counts.Skill != 2 {
		t.Errorf("skill = %d, want 2", counts.Skill)
	}
	if len(counts.SkillsUsed) != 1 || counts.SkillsUsed[0] != "contextual-commit" {
		t.Errorf("SkillsUsed = %v, want [contextual-commit]", counts.SkillsUsed)
	}
}

func TestDuplicateMessageSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Generate fixtures with duplicate message IDs.
	outputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateDuplicateMessageFixtures(outputDir); err != nil {
		t.Fatalf("GenerateDuplicateMessageFixtures: %v", err)
	}
	// Also need sessions so Discover finds both datasets.
	if err := testutil.GenerateDuplicateSessionFixtures(outputDir); err != nil {
		t.Fatalf("GenerateDuplicateSessionFixtures: %v", err)
	}

	dbPath := filepath.Join(home, "test.sqlite")
	var stderr bytes.Buffer

	result, err := indexdb.FullBuild(dbPath, outputDir, &stderr)
	if err != nil {
		t.Fatalf("FullBuild should not error on duplicates, got: %v", err)
	}

	// Should have indexed 2 unique messages, skipped 1 duplicate.
	if result.MessagesIndexed != 2 {
		t.Errorf("messages indexed = %d, want 2", result.MessagesIndexed)
	}
	if result.MessagesSkipped != 1 {
		t.Errorf("messages skipped = %d, want 1", result.MessagesSkipped)
	}

	// Warning should appear on stderr with contextual info.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "WARNING: skipping duplicate message") {
		t.Errorf("expected duplicate warning on stderr, got: %s", stderrStr)
	}
	if !strings.Contains(stderrStr, "msg-001") {
		t.Errorf("expected message_id in warning, got: %s", stderrStr)
	}
	if !strings.Contains(stderrStr, "test-session-1") {
		t.Errorf("expected session_id in warning, got: %s", stderrStr)
	}
	if !strings.Contains(stderrStr, "messages.parquet") {
		t.Errorf("expected source path in warning, got: %s", stderrStr)
	}

	// DB should contain exactly the non-duplicate rows.
	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, messages, _, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if messages != 2 {
		t.Errorf("message rows = %d, want 2", messages)
	}
}

func TestDuplicateSessionSkipped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Generate fixtures with duplicate session IDs.
	outputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateDuplicateSessionFixtures(outputDir); err != nil {
		t.Fatalf("GenerateDuplicateSessionFixtures: %v", err)
	}

	dbPath := filepath.Join(home, "test.sqlite")
	var stderr bytes.Buffer

	result, err := indexdb.FullBuild(dbPath, outputDir, &stderr)
	if err != nil {
		t.Fatalf("FullBuild should not error on duplicates, got: %v", err)
	}

	// Should have indexed 2 unique sessions, skipped 1 duplicate.
	if result.SessionsIndexed != 2 {
		t.Errorf("sessions indexed = %d, want 2", result.SessionsIndexed)
	}
	if result.SessionsSkipped != 1 {
		t.Errorf("sessions skipped = %d, want 1", result.SessionsSkipped)
	}

	// Warning should appear on stderr.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "WARNING: skipping duplicate session") {
		t.Errorf("expected duplicate warning on stderr, got: %s", stderrStr)
	}
	if !strings.Contains(stderrStr, "test-session-1") {
		t.Errorf("expected session_id in warning, got: %s", stderrStr)
	}

	// DB should contain exactly the non-duplicate rows.
	db, err := indexdb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sessions, _, _, err := indexdb.RowCounts(db)
	if err != nil {
		t.Fatalf("RowCounts: %v", err)
	}
	if sessions != 2 {
		t.Errorf("session rows = %d, want 2", sessions)
	}
}

// TestInsertSessionTotalTurnDurationRoundtrip verifies that InsertSession
// persists TotalTurnDurationMs and GetSessionByID reads it back. Catches a
// regression where the column DDL is added but the INSERT or SELECT statement
// forgets to include it.
func TestInsertSessionTotalTurnDurationRoundtrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := indexdb.Create(dbPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := indexdb.InsertSession(tx,
		"/data/sessions/year=2026/month=04/part-0.parquet",
		"sess-ttd", "", "host-x", "claude", "",
		false,
		"/work/proj", "git@example.com:proj.git", "claude-opus-4-7", "/src.jsonl",
		1700000000000, 1700000100000, 73000,
		10, 20, 30,
		400, 200, 200,
		"some transcript",
		int(indexdb.SchemaVersion),
	); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	sess, err := indexdb.GetSessionByID(db, "sess-ttd")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if sess.TotalTurnDurationMs != 73000 {
		t.Errorf("TotalTurnDurationMs = %d, want 73000", sess.TotalTurnDurationMs)
	}
}
