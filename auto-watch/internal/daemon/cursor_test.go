package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-shared/backend/backendtest"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

// cursorEnv writes the shared project registry plus a project.json at repoRoot's
// .auto/watch, then opens and migrates the store at ~/.auto/watch/logs.sqlite.
// The caller has already set HOME (directly for a non-git project, or via
// testutil.NewEnv for a git project).
func cursorEnv(t *testing.T, repoRoot, projectJSON string) *store.Store {
	t.Helper()
	ctx := context.Background()
	home := os.Getenv("HOME")
	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := fmt.Sprintf(`{"projects":[{"id":"demo","path":%q}]}`, repoRoot)
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(repoRoot, ".auto", "watch")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.json"), []byte(projectJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(autoDir, "watch", "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func runsForTask(t *testing.T, db *store.Store, taskID string) []model.RunRecord {
	t.Helper()
	all, err := db.ListRunsByStates(context.Background(),
		model.RunPending, model.RunRunning, model.RunCompleted, model.RunFailed)
	if err != nil {
		t.Fatalf("ListRunsByStates: %v", err)
	}
	var out []model.RunRecord
	for i := range all {
		if all[i].TaskID == taskID {
			out = append(out, all[i])
		}
	}
	return out
}

func owedFires(t *testing.T, db *store.Store) []model.OwedFireRecord {
	t.Helper()
	owed, err := db.ListOwedFires(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListOwedFires: %v", err)
	}
	return owed
}

// startManualActiveRun reserves and starts a run directly (leaving it running,
// no runtime dir so Reap never inspects it) so a later dispatch of the same
// (task, resource_key) deduplicates against it. Returns the run id.
func startManualActiveRun(t *testing.T, db *store.Store, repoRoot, taskID, resourceKey, triggerType string, now time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := db.ReserveRun(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "watch",
		TriggerType: triggerType,
		TaskID:      taskID,
		TaskType:    "bash",
		ResourceKey: resourceKey,
		StartedAt:   now,
	})
	if err != nil {
		t.Fatalf("reserve manual run: %v", err)
	}
	if err := db.UpdateRunStarted(ctx, id, &store.RunStartUpdate{
		SessionName:   "manual-" + taskID,
		Backend:       "tmux",
		BackendTarget: "manual-" + taskID,
	}); err != nil {
		t.Fatalf("update manual run started: %v", err)
	}
	return id
}

const fileCreatedTwoTaskConfig = `{"id":"demo",
"tasks":{
  "process-a":{"type":"bash","command":"echo a"},
  "process-b":{"type":"bash","command":"echo b"}
},
"triggers":{"watch":{"type":"file_created","glob":"docs/*.md","tasks":["process-a","process-b"]}}}`

// TestFileCreatedDedupedTaskOwesFireAndDrains (AC-6): on a file_created fire,
// task A dispatches while task B — whose previous run is still active — is
// deduped. B records one owed fire, the snapshot advances, and once B's previous
// run ends the next tick drains B exactly once and deletes the owed fire; A does
// not rerun.
func TestFileCreatedDedupedTaskOwesFireAndDrains(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	db := cursorEnv(t, repoRoot, fileCreatedTwoTaskConfig)
	ctx := context.Background()

	current := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	svc := daemon.New(db, backend.NewRegistry(backendtest.New(0)), nil, func() time.Time { return current }, nil)

	// Seed tick with an empty docs dir: a baseline with no files, no fire.
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("seed tick: %v", err)
	}
	svc.WaitWorkers()

	// B has a previous run still active (same resource key the fire will use).
	manualB := startManualActiveRun(t, db, repoRoot, "process-b", "file_created:watch", "file_created", current)

	// A new file fires the trigger.
	current = current.Add(time.Minute)
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "new.md"), []byte("# new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick: %v", err)
	}
	svc.WaitWorkers()

	owed := owedFires(t, db)
	if len(owed) != 1 || owed[0].TaskID != "process-b" {
		t.Fatalf("expected one owed fire for process-b, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1 (dispatched on the fire)", n)
	}

	// End B's previous run, freeing its resource key.
	exit := 0
	if _, err := db.MarkRunTerminal(ctx, manualB, model.RunCompleted, &exit, current, ""); err != nil {
		t.Fatalf("end manual B run: %v", err)
	}

	// Next tick: drain dispatches B once and deletes the owed fire; no new file so
	// the trigger does not fire and A does not rerun.
	current = current.Add(time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("drain tick: %v", err)
	}
	svc.WaitWorkers()

	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("expected owed fire drained, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-b")); n != 2 {
		t.Fatalf("process-b runs = %d, want 2 (ended manual run + one drained)", n)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1 (must not rerun)", n)
	}

	// A further tick drains nothing and fires nothing: B dispatched exactly once.
	current = current.Add(time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("idle tick: %v", err)
	}
	svc.WaitWorkers()
	if n := len(runsForTask(t, db, "process-b")); n != 2 {
		t.Fatalf("process-b runs = %d after idle tick, want still 2 (drained exactly once)", n)
	}
}

// TestFileCreatedStartFailingTaskOwesFireAndDrains (AC-6): on a fire, A
// dispatches and B's Start fails (marked failed + backed off by failRun). B
// records an owed fire; once B's backend recovers and its backoff clears, the
// next tick drains B exactly once and deletes the owed fire.
func TestFileCreatedStartFailingTaskOwesFireAndDrains(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	cfg := `{"id":"demo",
"tasks":{
  "process-a":{"type":"bash","command":"echo a","backend":"ok"},
  "process-b":{"type":"bash","command":"echo b","backend":"flaky"}
},
"triggers":{"watch":{"type":"file_created","glob":"docs/*.md","tasks":["process-a","process-b"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	okFake := &backendtest.Fake{NameVal: "ok"}
	okFake.SetExit(0)
	flaky := &backendtest.Fake{NameVal: "flaky"}
	flaky.SetStartErr(errors.New("start failed"))

	current := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	svc := daemon.New(db, backend.NewRegistry(okFake, flaky), nil, func() time.Time { return current }, nil)

	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("seed tick: %v", err)
	}
	svc.WaitWorkers()

	// Fire: A dispatches, B's Start fails -> owed fire + B backed off.
	current = current.Add(time.Minute)
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "new.md"), []byte("# new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick: %v", err)
	}
	svc.WaitWorkers()

	owed := owedFires(t, db)
	if len(owed) != 1 || owed[0].TaskID != "process-b" || owed[0].Reason != "start_failed" {
		t.Fatalf("expected one start_failed owed fire for process-b, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-b")); n != 1 {
		t.Fatalf("process-b runs = %d, want 1 (the failed start)", n)
	}

	// Backend recovers; advance past the 1-minute backoff window and drain.
	flaky.SetStartErr(nil)
	flaky.SetExit(0)
	current = current.Add(2 * time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("drain tick: %v", err)
	}
	svc.WaitWorkers()

	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("expected owed fire drained, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-b")); n != 2 {
		t.Fatalf("process-b runs = %d, want 2 (failed + drained)", n)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1 (must not rerun)", n)
	}
}

// TestFileCreatedBackedOffTaskOwesFireAndDrains (AC-6): B is already backed off
// (from a prior failing dispatch with no owed fire) when a fire skips it, so the
// skip records an owed fire with reason "backoff"; once the window clears and the
// backend recovers, the next tick drains B.
func TestFileCreatedBackedOffTaskOwesFireAndDrains(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	cfg := `{"id":"demo",
"tasks":{
  "process-a":{"type":"bash","command":"echo a","backend":"ok"},
  "process-b":{"type":"bash","command":"echo b","backend":"flaky"}
},
"triggers":{"watch":{"type":"file_created","glob":"docs/*.md","tasks":["process-a","process-b"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	okFake := &backendtest.Fake{NameVal: "ok"}
	okFake.SetExit(0)
	flaky := &backendtest.Fake{NameVal: "flaky"}
	flaky.SetStartErr(errors.New("start failed"))

	current := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	svc := daemon.New(db, backend.NewRegistry(okFake, flaky), nil, func() time.Time { return current }, nil)

	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("seed tick: %v", err)
	}
	svc.WaitWorkers()

	// Back B off via a direct failing dispatch (Dispatch records no owed fire, so
	// the later backoff skip is the FIRST owed fire and keeps reason "backoff").
	_, derr := svc.Dispatch(ctx, &store.ReserveRunInput{
		ProjectID:   "demo",
		ProjectPath: repoRoot,
		TriggerID:   "seed",
		TriggerType: "cron",
		TaskID:      "process-b",
		TaskType:    "bash",
		ResourceKey: "manual:seed",
		StartedAt:   current,
	}, model.TaskDef{Type: "bash", Command: "echo b", Backend: "flaky"})
	if derr == nil {
		t.Fatal("expected the seeding dispatch to fail (flaky start)")
	}
	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("direct Dispatch must not record an owed fire, got %+v", owed)
	}

	// Within B's 1-minute backoff window a new file fires: A dispatches, B is
	// skipped for backoff and records an owed fire with reason "backoff".
	current = current.Add(30 * time.Second)
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "new.md"), []byte("# new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick: %v", err)
	}
	svc.WaitWorkers()

	owed := owedFires(t, db)
	if len(owed) != 1 || owed[0].TaskID != "process-b" || owed[0].Reason != "backoff" {
		t.Fatalf("expected one backoff owed fire for process-b, got %+v", owed)
	}

	// Recover the backend and clear the window, then drain.
	flaky.SetStartErr(nil)
	flaky.SetExit(0)
	current = current.Add(2 * time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("drain tick: %v", err)
	}
	svc.WaitWorkers()

	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("expected owed fire drained, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1 (must not rerun)", n)
	}
	// process-b: the seeding failure + the drained run (both carry task process-b).
	if n := len(runsForTask(t, db, "process-b")); n != 2 {
		t.Fatalf("process-b runs = %d, want 2 (seed failure + drained)", n)
	}
}

// TestBranchChangedCronDedupedTaskOwesFireAndDrains (AC-6): a branch-changed
// cron whose new SHA reaches A (dispatched) but not B (deduped) records an owed
// fire for B while its cursor advances; once B's previous run ends the next tick
// drains B even though the branch has not changed again.
func TestBranchChangedCronDedupedTaskOwesFireAndDrains(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	cfg := `{"id":"demo",
"tasks":{
  "process-a":{"type":"bash","command":"echo a"},
  "process-b":{"type":"bash","command":"echo b"}
},
"triggers":{"watch":{"type":"cron","when":"* * * * *","onlyIfBranchHasChanged":"main","tasks":["process-a","process-b"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	current := time.Date(2026, 7, 4, 12, 0, 0, 0, time.Local)
	svc := daemon.New(db, backend.NewRegistry(backendtest.New(0)), nil, func() time.Time { return current }, nil)

	// B already has an active run holding the cron resource key.
	manualB := startManualActiveRun(t, db, repoRoot, "process-b", "cron:watch", "cron", current)

	// First due minute: branch changed ("" -> head SHA) -> fire. A dispatches, B
	// deduped -> owed fire; last_branch_sha advances.
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick: %v", err)
	}
	svc.WaitWorkers()

	owed := owedFires(t, db)
	if len(owed) != 1 || owed[0].TaskID != "process-b" {
		t.Fatalf("expected one owed fire for process-b, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1", n)
	}

	// End B's previous run and advance a minute. The branch has not changed, so the
	// trigger does not fire, but the drain still dispatches B once.
	exit := 0
	if _, err := db.MarkRunTerminal(ctx, manualB, model.RunCompleted, &exit, current, ""); err != nil {
		t.Fatalf("end manual B run: %v", err)
	}
	current = current.Add(time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("drain tick: %v", err)
	}
	svc.WaitWorkers()

	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("expected owed fire drained, got %+v", owed)
	}
	if n := len(runsForTask(t, db, "process-b")); n != 2 {
		t.Fatalf("process-b runs = %d, want 2 (ended manual + drained)", n)
	}
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("process-a runs = %d, want 1 (branch unchanged, no rerun)", n)
	}
	// Sanity: the branch head is what the cursor recorded.
	if _, err := gitx.BranchHeadSHA(repoRoot, "main"); err != nil {
		t.Fatalf("branch head sha: %v", err)
	}
}

// TestOwedFiresCoalesceOnRepeatedSkips (AC-6): repeated dedup skips of the same
// task on a cursor-advancing trigger coalesce into a single owed-fire row that
// keeps the first reason and time.
func TestOwedFiresCoalesceOnRepeatedSkips(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	cfg := `{"id":"demo",
"tasks":{
  "process-a":{"type":"bash","command":"echo a"},
  "process-b":{"type":"bash","command":"echo b"}
},
"triggers":{"watch":{"type":"cron","when":"* * * * *","onlyIfBranchHasChanged":"main","tasks":["process-a","process-b"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	current := time.Date(2026, 7, 5, 12, 0, 0, 0, time.Local)
	firstOwed := current
	svc := daemon.New(db, backend.NewRegistry(backendtest.New(0)), nil, func() time.Time { return current }, nil)

	// B stays active for the whole test so it is deduped on every fire.
	startManualActiveRun(t, db, repoRoot, "process-b", "cron:watch", "cron", current)

	// First fire (branch "" -> SHA1): B deduped -> owed fire #1.
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick 1: %v", err)
	}
	svc.WaitWorkers()

	// New commit changes the SHA; next minute fires again and B is deduped again.
	env.CommitFile(repoRoot, "f.txt", "x", "change")
	current = current.Add(time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick 2: %v", err)
	}
	svc.WaitWorkers()

	owed := owedFires(t, db)
	if len(owed) != 1 {
		t.Fatalf("expected repeated skips to coalesce into 1 owed fire, got %d (%+v)", len(owed), owed)
	}
	if owed[0].TaskID != "process-b" {
		t.Fatalf("owed fire task = %q, want process-b", owed[0].TaskID)
	}
	if !owed[0].FirstOwedAt.Equal(firstOwed.UTC()) {
		t.Fatalf("first_owed_at = %v, want the first skip time %v", owed[0].FirstOwedAt, firstOwed.UTC())
	}
}

// TestPlainCronRecordsNoOwedFire (AC-6): a plain cron (no branch filter) that
// dedups a task records NO owed fire — its skip semantics are unchanged.
func TestPlainCronRecordsNoOwedFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	cfg := `{"id":"demo",
"tasks":{"process-b":{"type":"bash","command":"echo b"}},
"triggers":{"watch":{"type":"cron","when":"* * * * *","tasks":["process-b"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	current := time.Date(2026, 7, 6, 12, 0, 0, 0, time.Local)
	svc := daemon.New(db, backend.NewRegistry(backendtest.New(0)), nil, func() time.Time { return current }, nil)

	// B already active, so the cron fire dedups it.
	startManualActiveRun(t, db, repoRoot, "process-b", "cron:watch", "cron", current)

	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("fire tick: %v", err)
	}
	svc.WaitWorkers()

	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("plain cron must record no owed fire, got %+v", owed)
	}
	// The dedup still happened (the active run was kept, nothing new dispatched).
	if n, err := db.CountEventsByType(ctx, "task_skipped_dedup"); err != nil {
		t.Fatalf("count dedup events: %v", err)
	} else if n != 1 {
		t.Fatalf("expected 1 dedup skip, got %d", n)
	}
}

// TestFileCreatedEmptyBaselineThenFiresOnce (AC-6): an empty baseline is recorded
// without firing, and the first new file afterwards fires the task exactly once
// (seeding behaviour preserved).
func TestFileCreatedEmptyBaselineThenFiresOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	cfg := `{"id":"demo",
"tasks":{"process-a":{"type":"bash","command":"echo a"}},
"triggers":{"watch":{"type":"file_created","glob":"docs/*.md","tasks":["process-a"]}}}`
	db := cursorEnv(t, repoRoot, cfg)
	ctx := context.Background()

	current := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	svc := daemon.New(db, backend.NewRegistry(backendtest.New(0)), nil, func() time.Time { return current }, nil)

	// Empty baseline: no docs at all.
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("baseline tick: %v", err)
	}
	svc.WaitWorkers()
	if n := len(runsForTask(t, db, "process-a")); n != 0 {
		t.Fatalf("baseline tick dispatched %d runs, want 0", n)
	}

	// First file fires exactly once.
	current = current.Add(time.Minute)
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "first.md"), []byte("# first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("first-file tick: %v", err)
	}
	svc.WaitWorkers()
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("first-file tick dispatched %d runs, want 1", n)
	}
	if owed := owedFires(t, db); len(owed) != 0 {
		t.Fatalf("a clean fire must record no owed fire, got %+v", owed)
	}

	// A further tick with no new file does not re-fire.
	current = current.Add(time.Minute)
	if err := svc.Tick(ctx); err != nil {
		t.Fatalf("idle tick: %v", err)
	}
	svc.WaitWorkers()
	if n := len(runsForTask(t, db, "process-a")); n != 1 {
		t.Fatalf("idle tick changed run count to %d, want still 1", n)
	}
}
