package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-watch/internal/daemon"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
)

// collectSink implements bus.Sink and collects delivered events for assertions.
type collectSink struct {
	mu     sync.Mutex
	events []bus.Event
}

func (s *collectSink) Deliver(ev bus.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *collectSink) byType(typ string) []bus.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []bus.Event
	for i := range s.events {
		if s.events[i].Type == typ {
			out = append(out, s.events[i])
		}
	}
	return out
}

func eventTypeCount(t *testing.T, db *store.Store, eventType string) int {
	t.Helper()
	events, err := db.ListEvents(context.Background(), &store.EventFilter{Limit: 100, EventType: eventType})
	if err != nil {
		t.Fatalf("ListEvents(%s): %v", eventType, err)
	}
	return len(events)
}

func TestDispatchEmitsWatchTaskStarted(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	hub := bus.NewHub()
	sink := &collectSink{}
	cancel := hub.Subscribe(sink)
	defer cancel()

	// No --ctl-events knob exists on the daemon Service: watch.task.* is a
	// data-plane stream that is always emitted when a hub is present.
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, hub)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	started := sink.byType(bus.TypeWatchTaskStarted)
	if len(started) != 1 {
		t.Fatalf("expected 1 watch.task.started, got %d", len(started))
	}
	ev := started[0]
	if ev.Project != "demo" {
		t.Fatalf("started provenance project = %q, want demo", ev.Project)
	}
	var data bus.WatchTaskData
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("unmarshal watch task data: %v", err)
	}
	if data.RunID != runID {
		t.Fatalf("started run id = %d, want %d", data.RunID, runID)
	}
	if data.TaskID != "run-etl" || data.TriggerID != "daily" || data.ResourceKey != "cron:daily" {
		t.Fatalf("unexpected watch task data: %+v", data)
	}

	if eventTypeCount(t, db, "task_started") != 1 {
		t.Fatalf("expected SQLite task_started row to persist alongside the bus event")
	}
}

func TestReapEmitsWatchTaskCompleted(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	hub := bus.NewHub()
	sink := &collectSink{}
	cancel := hub.Subscribe(sink)
	defer cancel()
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, hub)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := os.WriteFile(run.ExitPath, []byte("0\n"), 0o644); err != nil {
		t.Fatalf("write exit file: %v", err)
	}
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	completed := sink.byType(bus.TypeWatchTaskCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 watch.task.completed, got %d", len(completed))
	}
	var data bus.WatchTaskData
	if err := json.Unmarshal(completed[0].Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.ExitCode == nil || *data.ExitCode != 0 {
		t.Fatalf("completed exit code = %v, want 0", data.ExitCode)
	}
	if eventTypeCount(t, db, "task_completed") != 1 {
		t.Fatalf("expected SQLite task_completed row to persist")
	}
}

func TestReapEmitsWatchTaskFailed(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	hub := bus.NewHub()
	sink := &collectSink{}
	cancel := hub.Subscribe(sink)
	defer cancel()
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, hub)

	runID, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "boom"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	run, err := db.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := os.WriteFile(run.ExitPath, []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write exit file: %v", err)
	}
	if err := svc.Reap(context.Background()); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	failed := sink.byType(bus.TypeWatchTaskFailed)
	if len(failed) != 1 {
		t.Fatalf("expected 1 watch.task.failed, got %d", len(failed))
	}
	var data bus.WatchTaskData
	if err := json.Unmarshal(failed[0].Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.ExitCode == nil || *data.ExitCode != 1 {
		t.Fatalf("failed exit code = %v, want 1", data.ExitCode)
	}
	if eventTypeCount(t, db, "task_failed") != 1 {
		t.Fatalf("expected SQLite task_failed row to persist")
	}
}

func TestNilHubDispatchDoesNotPanic(t *testing.T) {
	db := newTestStore(t)
	projectPath := t.TempDir()
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	svc := daemon.New(db, &recordingBackend{}, nil, func() time.Time { return now }, nil)

	if _, err := svc.Dispatch(context.Background(), bashReserveInput(projectPath, now), model.TaskDef{Type: "bash", Command: "echo hi"}); err != nil {
		t.Fatalf("Dispatch with nil hub: %v", err)
	}
}
