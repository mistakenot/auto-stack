package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"

	_ "modernc.org/sqlite"
)

// writeV2Runs builds a pre-v3 runs table (no backend/backend_target/deadline_at/
// failure_reason columns) with one completed, one failed and one running row,
// and records schema versions 1 and 2. It mirrors the v1/v2 runs DDL in
// store.Migrate so a later store.Migrate exercises the real v3 ALTER path.
func writeV2Runs(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	for _, stmt := range []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);`,
		`CREATE TABLE runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			project_path TEXT NOT NULL,
			trigger_id TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			task_id TEXT NOT NULL,
			task_type TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			branch TEXT,
			state TEXT NOT NULL,
			session_name TEXT,
			runtime_dir TEXT,
			output_path TEXT,
			exit_path TEXT,
			worktree_path TEXT,
			started_at DATETIME NOT NULL,
			completed_at DATETIME,
			exit_code INTEGER,
			error_message TEXT
		);`,
		`INSERT INTO schema_migrations(version) VALUES (1);`,
		`INSERT INTO schema_migrations(version) VALUES (2);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("build v2 schema: %v", err)
		}
	}

	rows := []struct {
		id          int64
		state       string
		session     string
		completedAt any
		exitCode    any
	}{
		{1, "completed", "autowatch-run-1--etl", "2026-03-20T10:05:00Z", 0},
		{2, "failed", "autowatch-run-2--etl", "2026-03-20T10:06:00Z", 3},
		{3, "running", "autowatch-run-3--etl", nil, nil},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO runs (
			id, project_id, project_path, trigger_id, trigger_type, task_id, task_type,
			resource_key, state, session_name, runtime_dir, output_path, exit_path,
			started_at, completed_at, exit_code
		) VALUES (?, 'demo', '/tmp/demo', 'daily', 'cron', 'etl', 'bash',
			'cron:daily', ?, ?, ?, ?, ?, '2026-03-20T10:00:00Z', ?, ?)`,
			r.id, r.state, r.session,
			"/tmp/demo/runs/"+r.session, "/tmp/demo/runs/out.log", "/tmp/demo/runs/exit-code",
			r.completedAt, r.exitCode,
		); err != nil {
			t.Fatalf("insert v2 run %d: %v", r.id, err)
		}
	}
}

// TestMigrateV3PreservesLegacyRuns (AC-15): migrating a v2 database to v3 keeps
// every row, reads legacy rows back as the tmux Backend with backend_target set
// to the session name, and leaves deadline_at and failure_reason null.
func TestMigrateV3PreservesLegacyRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.sqlite")
	writeV2Runs(t, path)

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate to v3: %v", err)
	}

	all, err := db.ListRunsByStates(ctx, model.RunCompleted, model.RunFailed, model.RunRunning)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 preserved runs, got %d", len(all))
	}
	for i := range all {
		run := all[i]
		if run.Backend != "tmux" {
			t.Errorf("run %d backend = %q, want tmux (legacy contract)", run.ID, run.Backend)
		}
		if run.BackendTarget != run.SessionName {
			t.Errorf("run %d backend_target = %q, want session name %q", run.ID, run.BackendTarget, run.SessionName)
		}
		if run.DeadlineAt != nil {
			t.Errorf("run %d deadline_at = %v, want nil on legacy row", run.ID, run.DeadlineAt)
		}
		if run.FailureReason != "" {
			t.Errorf("run %d failure_reason = %q, want empty on legacy row", run.ID, run.FailureReason)
		}
	}

	// A run that was running before the upgrade is still reaped correctly by the
	// tmux adapter: its backend resolves to the registered tmux Backend.
	running, err := db.ListRunsByStates(ctx, model.RunRunning)
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) != 1 || running[0].Backend != "tmux" {
		t.Fatalf("expected one legacy running run on tmux, got %+v", running)
	}
}

// TestMigrateOwedFiresTable (AC-6): Migrate creates the owed_fires table with
// its columns and (project_id, trigger_id, task_id) primary key, and a second
// Migrate is a no-op that preserves an existing row. The coalescing UpsertOwedFire
// keeps the FIRST reason/time on a repeated skip.
func TestMigrateOwedFiresTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The owed_fires table has exactly the documented columns.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() { _ = raw.Close() }()
	rows, err := raw.QueryContext(ctx, `PRAGMA table_info(owed_fires)`)
	if err != nil {
		t.Fatalf("pragma table_info(owed_fires): %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	pkCols := map[string]int{}
	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			dflt       sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &primaryKey); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
		if primaryKey > 0 {
			pkCols[name] = primaryKey
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate owed_fires columns: %v", err)
	}
	for _, want := range []string{"project_id", "trigger_id", "task_id", "reason", "first_owed_at"} {
		if !cols[want] {
			t.Errorf("owed_fires missing column %q", want)
		}
	}
	for _, want := range []string{"project_id", "trigger_id", "task_id"} {
		if _, ok := pkCols[want]; !ok {
			t.Errorf("owed_fires primary key missing column %q", want)
		}
	}
	if len(pkCols) != 3 {
		t.Errorf("owed_fires primary key = %v, want the 3 identity columns", pkCols)
	}

	// Coalescing: the first reason/time wins, a repeated skip does not add a row.
	first := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	if err := db.UpsertOwedFire(ctx, "demo", "watch-docs", "process", "dedup", first); err != nil {
		t.Fatalf("first upsert owed fire: %v", err)
	}
	if err := db.UpsertOwedFire(ctx, "demo", "watch-docs", "process", "backoff", first.Add(time.Hour)); err != nil {
		t.Fatalf("second upsert owed fire: %v", err)
	}
	owed, err := db.ListOwedFires(ctx, "demo")
	if err != nil {
		t.Fatalf("list owed fires: %v", err)
	}
	if len(owed) != 1 {
		t.Fatalf("expected 1 coalesced owed fire, got %d (%+v)", len(owed), owed)
	}
	if owed[0].Reason != "dedup" || !owed[0].FirstOwedAt.Equal(first) {
		t.Fatalf("coalesce kept %q/%v, want dedup/%v", owed[0].Reason, owed[0].FirstOwedAt, first)
	}

	// A second Migrate is a no-op and the row survives.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second migrate (must be a no-op): %v", err)
	}
	owed, err = db.ListOwedFires(ctx, "demo")
	if err != nil {
		t.Fatalf("list owed fires after second migrate: %v", err)
	}
	if len(owed) != 1 {
		t.Fatalf("expected owed fire preserved across migrate, got %d", len(owed))
	}

	// Delete removes exactly the one row.
	if err := db.DeleteOwedFire(ctx, "demo", "watch-docs", "process"); err != nil {
		t.Fatalf("delete owed fire: %v", err)
	}
	owed, err = db.ListOwedFires(ctx, "demo")
	if err != nil {
		t.Fatalf("list owed fires after delete: %v", err)
	}
	if len(owed) != 0 {
		t.Fatalf("expected 0 owed fires after delete, got %d", len(owed))
	}
}

// TestMigrateV3IsIdempotent (AC-15): running Migrate a second time is a no-op —
// the v3 ALTERs are gated on PRAGMA table_info and are not re-applied.
func TestMigrateV3IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.sqlite")
	writeV2Runs(t, path)

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second migrate (must be a no-op): %v", err)
	}

	all, err := db.ListRunsByStates(ctx, model.RunCompleted, model.RunFailed, model.RunRunning)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 runs after double migrate, got %d", len(all))
	}

	// New columns persist a round-trip. UpdateRunStarted now guards on
	// state='pending' (it is the reserve->start transition), so start a freshly
	// reserved pending run and read the new columns back.
	deadline := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	newID, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: "/tmp/demo",
		TriggerID:   "daily",
		TriggerType: "cron",
		TaskID:      "etl",
		TaskType:    "bash",
		ResourceKey: "cron:daily:v3",
		StartedAt:   time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("reserve run: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, newID, &store.RunStartUpdate{
		SessionName:   "autowatch-run-4--etl",
		RuntimeDir:    "/tmp/demo/runs/4",
		Backend:       "tmux",
		BackendTarget: "autowatch-run-4--etl",
		DeadlineAt:    deadline,
	}); err != nil {
		t.Fatalf("update run started: %v", err)
	}
	run, err := db.GetRun(ctx, newID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.DeadlineAt == nil || !run.DeadlineAt.Equal(deadline) {
		t.Fatalf("deadline_at round-trip failed: got %v, want %v", run.DeadlineAt, deadline)
	}
}
