package indexdb_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/testutil"
)

// auqAcceptanceQuestion is the question text reused across the fixture pairs so
// the recommended-acceptance join and the per-question annotation lookup both
// key off a stable, space-containing question string.
const auqAcceptanceQuestion = "Which database should we use?"

// auqPairs hand-authors 5 AskUserQuestion call/result pairs:
//   - 3 offer an option whose label ends in " (Recommended)" (pairs 1-3)
//   - of those 3, the user PICKED the recommended option in exactly 2 (pairs 1-2)
//   - pair 2 also carries per-question annotation notes (AC-8)
//   - pairs 4-5 offer no recommended option (must not be counted)
//
// Expected: calls_with_rec == 3, rec_picked == 2.
func auqPairs() []testutil.AUQAcceptancePair {
	return []testutil.AUQAcceptancePair{
		{
			// Recommended offered and picked.
			SessionID: "auq-acc-1",
			ToolInput: `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres (Recommended)"},{"label":"SQLite"}]}]}`,
			ResultJSON: `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres (Recommended)"},{"label":"SQLite"}]}],` +
				`"answers":{"Which database should we use?":"Postgres (Recommended)"}}`,
		},
		{
			// Recommended offered and picked, plus annotation notes.
			SessionID: "auq-acc-2",
			ToolInput: `{"questions":[{"question":"Which database should we use?","options":[{"label":"MySQL"},{"label":"Postgres (Recommended)"}]}]}`,
			ResultJSON: `{"questions":[{"question":"Which database should we use?","options":[{"label":"MySQL"},{"label":"Postgres (Recommended)"}]}],` +
				`"answers":{"Which database should we use?":"Postgres (Recommended)"},` +
				`"annotations":{"Which database should we use?":{"notes":"prefer managed instance"}}}`,
		},
		{
			// Recommended offered but NOT picked.
			SessionID: "auq-acc-3",
			ToolInput: `{"questions":[{"question":"Which database should we use?","options":[{"label":"SQLite (Recommended)"},{"label":"Postgres"}]}]}`,
			ResultJSON: `{"questions":[{"question":"Which database should we use?","options":[{"label":"SQLite (Recommended)"},{"label":"Postgres"}]}],` +
				`"answers":{"Which database should we use?":"Postgres"}}`,
		},
		{
			// No recommended option offered.
			SessionID: "auq-acc-4",
			ToolInput: `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres"},{"label":"SQLite"}]}]}`,
			ResultJSON: `{"questions":[{"question":"Which database should we use?","options":[{"label":"Postgres"},{"label":"SQLite"}]}],` +
				`"answers":{"Which database should we use?":"Postgres"}}`,
		},
		{
			// No recommended option offered.
			SessionID: "auq-acc-5",
			ToolInput: `{"questions":[{"question":"Which database should we use?","options":[{"label":"MySQL"},{"label":"SQLite"}]}]}`,
			ResultJSON: `{"questions":[{"question":"Which database should we use?","options":[{"label":"MySQL"},{"label":"SQLite"}]}],` +
				`"answers":{"Which database should we use?":"MySQL"}}`,
		},
	}
}

// recommendedAcceptanceSQL computes, per AUQ call, whether a recommended option
// was offered and whether the user's answer matched it. It joins the assistant
// tool_use row (which carries the questions/options in tool_input) to the tool
// tool_result row (which carries the answers in tool_use_result_json) on
// session_id, derives the recommended label by enumerating the option labels in
// tool_input with json_each and matching the " (Recommended)" suffix, and
// compares that label against the answer extracted from the envelope.
const recommendedAcceptanceSQL = `
WITH calls AS (
  SELECT
    use.session_id AS session_id,
    (
      SELECT json_extract(opt.value, '$.label')
      FROM json_each(use.tool_input, '$.questions') q,
           json_each(q.value, '$.options') opt
      WHERE json_extract(opt.value, '$.label') LIKE '% (Recommended)'
      LIMIT 1
    ) AS recommended_label,
    json_extract(res.tool_use_result_json, '$.answers."' || ? || '"') AS picked_label
  FROM messages use
  JOIN messages res
    ON res.session_id = use.session_id
   AND res.tool_use_result_json != ''
  WHERE use.tool_name = 'AskUserQuestion'
    AND use.role = 'assistant'
    AND use.tool_input != ''
)
SELECT
  SUM(CASE WHEN recommended_label IS NOT NULL THEN 1 ELSE 0 END) AS calls_with_rec,
  SUM(CASE WHEN recommended_label IS NOT NULL
            AND picked_label = recommended_label THEN 1 ELSE 0 END) AS rec_picked
FROM calls;`

func TestRecommendedAcceptanceFromFixture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	outputDir := filepath.Join(home, "etl-output")
	if err := testutil.GenerateAUQAcceptanceFixtures(outputDir, auqPairs()); err != nil {
		t.Fatalf("GenerateAUQAcceptanceFixtures: %v", err)
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

	// AC-7: recommended-acceptance computed purely in SQL over the index.
	var callsWithRec, recPicked int
	if err := db.QueryRow(recommendedAcceptanceSQL, auqAcceptanceQuestion).Scan(&callsWithRec, &recPicked); err != nil {
		t.Fatalf("recommended-acceptance query: %v", err)
	}
	if callsWithRec != 3 {
		t.Errorf("calls_with_rec = %d, want 3", callsWithRec)
	}
	if recPicked != 2 {
		t.Errorf("rec_picked = %d, want 2", recPicked)
	}

	// AC-8: per-question annotation notes are recoverable from the envelope,
	// keyed by the original question text.
	var notes sql.NullString
	err = db.QueryRow(
		`SELECT json_extract(tool_use_result_json, '$.annotations."` + auqAcceptanceQuestion + `".notes')
		 FROM messages WHERE message_id = 'auq-acc-2-result'`,
	).Scan(&notes)
	if err != nil {
		t.Fatalf("annotation notes query: %v", err)
	}
	if !notes.Valid {
		t.Fatal("annotation notes were NULL, want a string")
	}
	if notes.String != "prefer managed instance" {
		t.Errorf("annotation notes = %q, want %q", notes.String, "prefer managed instance")
	}

	// A row without annotations returns NULL for the same path.
	var noNotes sql.NullString
	err = db.QueryRow(
		`SELECT json_extract(tool_use_result_json, '$.annotations."` + auqAcceptanceQuestion + `".notes')
		 FROM messages WHERE message_id = 'auq-acc-1-result'`,
	).Scan(&noNotes)
	if err != nil {
		t.Fatalf("no-annotation notes query: %v", err)
	}
	if noNotes.Valid {
		t.Errorf("expected NULL notes for un-annotated row, got %q", noNotes.String)
	}
}
