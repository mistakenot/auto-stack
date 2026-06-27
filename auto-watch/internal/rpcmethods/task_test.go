package rpcmethods

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/rpc/conformance"
	watchconfig "github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/runner"
	"github.com/mistakenot/auto-watch/internal/store"
)

// fakeBackend records Start/Kill calls and never writes an exit-code file, so
// dispatched runs stay "running" until a test drives them to a terminal state.
type fakeBackend struct {
	mu     sync.Mutex
	starts int
	kills  int
}

func (b *fakeBackend) Start(_ context.Context, spec *runner.StartSpec) (runner.Handle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.starts++
	return runner.Handle{SessionName: spec.SessionName, ExitPath: spec.ExitPath, OutputPath: spec.OutputPath}, nil
}

func (b *fakeBackend) Kill(_ context.Context, _ runner.Handle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.kills++
	return nil
}

func (b *fakeBackend) SessionExists(context.Context, string) (bool, error) {
	return false, nil
}

func (b *fakeBackend) killCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.kills
}

// setupWithService creates a full RPC test environment backed by a real
// daemon.Service with a temp store, fake backend, and a project on disk with a
// valid watch project config. Returns the client, the service, the registry
// snapshot, and a cleanup func.
func setupWithService(t *testing.T) (*conformance.PeerClient, *daemon.Service, config.ProjectsConfig, func()) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	if err := watchconfig.EnsureGlobalDirs(); err != nil {
		t.Fatalf("ensure global dirs: %v", err)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "logs.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	projectDir := t.TempDir()
	cfg := model.ProjectConfig{
		ID: "test-project",
		Tasks: map[string]model.TaskDef{
			"echo":  {Type: "bash", Command: "echo hi"},
			"echo2": {Type: "bash", Command: "echo two"},
			"echo3": {Type: "bash", Command: "echo three"},
		},
	}
	if err := watchconfig.SaveProjectConfig(projectDir, cfg); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "test-project", Name: "Test", Path: projectDir},
		},
	}

	hub := bus.NewHub()
	svc := daemon.New(db, &fakeBackend{}, nil, time.Now, hub)
	h := New(svc, "test-host", "1.2.3", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, func() config.ProjectsConfig { return reg })

	sConn, cConn := net.Pipe()
	serverPeer := rpc.NewPeer(sConn)
	h.Register(serverPeer)
	client := conformance.NewPeerClient(cConn)

	ctx, cancel := context.WithCancel(context.Background())
	sErr := make(chan error, 1)
	cErr := make(chan error, 1)
	go func() { sErr <- serverPeer.Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	cleanup := func() {
		cancel()
		<-sErr
		<-cErr
	}
	return client, svc, reg, cleanup
}

func callTask(t *testing.T, client *conformance.PeerClient, method string, params any) (json.RawMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.Call(ctx, method, params)
}

func dispatchTask(t *testing.T, client *conformance.PeerClient, taskID string) int64 {
	t.Helper()
	raw, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project", TaskID: taskID})
	if err != nil {
		t.Fatalf("task.dispatch %q: %v", taskID, err)
	}
	var res DispatchResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal dispatch result: %v", err)
	}
	return res.RunID
}

func TestTaskDispatch_CreatesRun(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	raw, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project", TaskID: "echo"})
	if err != nil {
		t.Fatalf("task.dispatch: %v", err)
	}
	var res DispatchResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.RunID <= 0 {
		t.Fatalf("runId = %d, want > 0", res.RunID)
	}
	if res.State != string(model.RunRunning) {
		t.Errorf("state = %q, want running", res.State)
	}

	run, err := svc.Store.GetRun(context.Background(), res.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != model.RunRunning {
		t.Errorf("run state = %q, want running", run.State)
	}
	if run.TaskID != "echo" || run.ProjectID != "test-project" {
		t.Errorf("unexpected run identity: %+v", run)
	}
	if run.ResourceKey != "rpc:echo" {
		t.Errorf("resource key = %q, want rpc:echo", run.ResourceKey)
	}
}

func TestTaskDispatch_UnknownProject(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	_, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "nope", TaskID: "echo"})
	assertRPCCode(t, err, rpc.InvalidParams)
}

func TestTaskDispatch_UnknownTask(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	_, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project", TaskID: "nope"})
	assertRPCCode(t, err, rpc.InvalidParams)
}

func TestTaskDispatch_MissingFields(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	_, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project"})
	assertRPCCode(t, err, rpc.InvalidParams)
}

func TestTaskDispatch_ActiveDuplicate(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	dispatchTask(t, client, "echo")

	_, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project", TaskID: "echo"})
	assertRPCCode(t, err, -32001)
}

func TestTaskStatus_ReturnsRunView(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	runID := dispatchTask(t, client, "echo")

	raw, err := callTask(t, client, "task.status", StatusParams{RunID: runID})
	if err != nil {
		t.Fatalf("task.status: %v", err)
	}
	var view RunView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.ID != runID {
		t.Errorf("id = %d, want %d", view.ID, runID)
	}
	if view.TaskID != "echo" {
		t.Errorf("taskId = %q, want echo", view.TaskID)
	}
	if view.State != string(model.RunRunning) {
		t.Errorf("state = %q, want running", view.State)
	}
}

func TestTaskStatus_MissingRunID(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	_, err := callTask(t, client, "task.status", StatusParams{})
	assertRPCCode(t, err, rpc.InvalidParams)
}

func TestTaskList_All(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	dispatchTask(t, client, "echo")
	dispatchTask(t, client, "echo2")

	raw, err := callTask(t, client, "task.list", nil)
	if err != nil {
		t.Fatalf("task.list: %v", err)
	}
	runs := decodeRuns(t, raw)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestTaskList_StateFilter(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	dispatchTask(t, client, "echo") // stays running
	cancelID := dispatchTask(t, client, "echo2")

	if _, err := callTask(t, client, "task.cancel", CancelParams{RunID: cancelID}); err != nil {
		t.Fatalf("task.cancel: %v", err)
	}

	rawRunning, err := callTask(t, client, "task.list", ListParams{State: string(model.RunRunning)})
	if err != nil {
		t.Fatalf("task.list running: %v", err)
	}
	running := decodeRuns(t, rawRunning)
	if len(running) != 1 {
		t.Fatalf("expected 1 running run, got %d", len(running))
	}
	if running[0].TaskID != "echo" {
		t.Errorf("running task = %q, want echo", running[0].TaskID)
	}

	rawFailed, err := callTask(t, client, "task.list", ListParams{State: string(model.RunFailed)})
	if err != nil {
		t.Fatalf("task.list failed: %v", err)
	}
	failed := decodeRuns(t, rawFailed)
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed run, got %d", len(failed))
	}
}

func TestTaskOutput_WithContent(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	runID := dispatchTask(t, client, "echo")
	run, err := svc.Store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.OutputPath == "" {
		t.Fatal("expected output path on running run")
	}

	var sb strings.Builder
	for range 500 {
		sb.WriteString("line\n")
	}
	if err := os.WriteFile(run.OutputPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	raw, err := callTask(t, client, "task.output", OutputParams{RunID: runID, TailLines: 10})
	if err != nil {
		t.Fatalf("task.output: %v", err)
	}
	var out OutputResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Truncated {
		t.Error("expected truncated=true for 500 lines tailed to 10")
	}
	gotLines := strings.Count(strings.TrimSpace(out.Output), "\n") + 1
	if gotLines != 10 {
		t.Errorf("tailed output has %d lines, want 10", gotLines)
	}
	if out.Path != run.OutputPath {
		t.Errorf("path = %q, want %q", out.Path, run.OutputPath)
	}
}

func TestTaskOutput_ByteCap(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	runID := dispatchTask(t, client, "echo")
	run, _ := svc.Store.GetRun(context.Background(), runID)

	content := strings.Repeat("x", 5000)
	if err := os.WriteFile(run.OutputPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	raw, err := callTask(t, client, "task.output", OutputParams{RunID: runID, MaxBytes: 1000})
	if err != nil {
		t.Fatalf("task.output: %v", err)
	}
	var out OutputResult
	json.Unmarshal(raw, &out)
	if !out.Truncated {
		t.Error("expected truncated=true with MaxBytes cap")
	}
	if len(out.Output) > 1000 {
		t.Errorf("output len = %d, want <= 1000", len(out.Output))
	}
}

func TestTaskOutput_PendingRun(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	runID, err := svc.Store.ReserveRun(context.Background(), &store.ReserveRunInput{
		ProjectID:   "test-project",
		ProjectPath: "/tmp",
		TriggerID:   "rpc",
		TriggerType: "rpc",
		TaskID:      "echo",
		TaskType:    "bash",
		ResourceKey: "rpc:pending",
		StartedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("ReserveRun: %v", err)
	}

	raw, err := callTask(t, client, "task.output", OutputParams{RunID: runID})
	if err != nil {
		t.Fatalf("task.output: %v", err)
	}
	var out OutputResult
	json.Unmarshal(raw, &out)
	if out.Output != "" {
		t.Errorf("expected empty output for pending run, got %q", out.Output)
	}
	if out.Truncated {
		t.Error("pending run output should not be truncated")
	}
}

func TestTaskCancel_KillsAndTransitions(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	runID := dispatchTask(t, client, "echo")

	raw, err := callTask(t, client, "task.cancel", CancelParams{RunID: runID})
	if err != nil {
		t.Fatalf("task.cancel: %v", err)
	}
	var res CancelResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.State != string(model.RunFailed) {
		t.Errorf("state = %q, want failed", res.State)
	}

	run, _ := svc.Store.GetRun(context.Background(), runID)
	if run.State != model.RunFailed {
		t.Errorf("run state = %q, want failed", run.State)
	}
}

func TestTaskCancel_IdempotentOnTerminal(t *testing.T) {
	client, svc, _, cleanup := setupWithService(t)
	defer cleanup()

	be := svc.Backend.(*fakeBackend)
	runID := dispatchTask(t, client, "echo")

	if _, err := callTask(t, client, "task.cancel", CancelParams{RunID: runID}); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	killsAfterFirst := be.killCount()

	raw, err := callTask(t, client, "task.cancel", CancelParams{RunID: runID})
	if err != nil {
		t.Fatalf("second cancel: %v", err)
	}
	var res CancelResult
	json.Unmarshal(raw, &res)
	if res.State != string(model.RunFailed) {
		t.Errorf("state = %q, want failed", res.State)
	}
	if be.killCount() != killsAfterFirst {
		t.Errorf("second cancel on terminal run should not kill: before=%d after=%d", killsAfterFirst, be.killCount())
	}
}

func TestTaskDispatch_ConcurrentRaceFree(t *testing.T) {
	client, _, _, cleanup := setupWithService(t)
	defer cleanup()

	tasks := []string{"echo", "echo2", "echo3"}
	var wg sync.WaitGroup
	errs := make(chan error, len(tasks))
	for _, taskID := range tasks {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := callTask(t, client, "task.dispatch", DispatchParams{ProjectID: "test-project", TaskID: id}); err != nil {
				errs <- err
			}
		}(taskID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent dispatch failed: %v", err)
	}

	raw, err := callTask(t, client, "task.list", nil)
	if err != nil {
		t.Fatalf("task.list: %v", err)
	}
	runs := decodeRuns(t, raw)
	if len(runs) != len(tasks) {
		t.Errorf("expected %d runs, got %d", len(tasks), len(runs))
	}
}

func assertRPCCode(t *testing.T, err error, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %d, got nil", wantCode)
	}
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *rpc.Error, got %T: %v", err, err)
	}
	if rpcErr.Code != wantCode {
		t.Errorf("error code = %d, want %d (%s)", rpcErr.Code, wantCode, rpcErr.Message)
	}
}

func decodeRuns(t *testing.T, raw json.RawMessage) []RunView {
	t.Helper()
	var wrapper struct {
		Runs []RunView `json:"runs"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal runs: %v", err)
	}
	return wrapper.Runs
}
