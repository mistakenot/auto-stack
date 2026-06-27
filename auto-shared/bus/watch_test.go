package bus

import "testing"

func TestWatchTaskTypeConstantsMatchDottedType(t *testing.T) {
	types := []string{
		TypeWatchTaskStarted,
		TypeWatchTaskCompleted,
		TypeWatchTaskFailed,
	}
	for _, typ := range types {
		if !dottedTypeRe.MatchString(typ) {
			t.Errorf("type constant %q does not match dottedType regex", typ)
		}
	}
}

func TestNewWatchTask(t *testing.T) {
	prov := RunProvenance{
		Project:  "auto-stack",
		Branch:   "main",
		Worktree: "/home/vscode/src/auto-stack",
		Remote:   "git@github.com:datadyne/auto-stack.git",
		Commit:   "abc1234",
	}
	exit := 0
	data := WatchTaskData{
		TaskID:      "039",
		RunID:       42,
		TriggerID:   "trig-1",
		SessionName: "exec-039",
		ResourceKey: "auto-stack:main",
		Message:     "started",
		ExitCode:    &exit,
	}

	ev, err := NewWatchTask(TypeWatchTaskStarted, prov, data)
	if err != nil {
		t.Fatalf("NewWatchTask: %v", err)
	}

	if errs := ev.Validate(); len(errs) != 0 {
		t.Errorf("Validate should pass, got %+v", errs)
	}
	if ev.Host == "" {
		t.Error("Host should be populated by NewEvent")
	}
	if ev.Type != TypeWatchTaskStarted {
		t.Errorf("Type = %q, want %q", ev.Type, TypeWatchTaskStarted)
	}
	if ev.Source != "auto/watch/daemon" {
		t.Errorf("Source = %q, want auto/watch/daemon", ev.Source)
	}

	if ev.Project != prov.Project {
		t.Errorf("Project = %q, want %q", ev.Project, prov.Project)
	}
	if ev.Branch != prov.Branch {
		t.Errorf("Branch = %q, want %q", ev.Branch, prov.Branch)
	}
	if ev.Worktree != prov.Worktree {
		t.Errorf("Worktree = %q, want %q", ev.Worktree, prov.Worktree)
	}
	if ev.Remote != prov.Remote {
		t.Errorf("Remote = %q, want %q", ev.Remote, prov.Remote)
	}
	if ev.Commit != prov.Commit {
		t.Errorf("Commit = %q, want %q", ev.Commit, prov.Commit)
	}
}

func TestNewWatchTaskRoundTrip(t *testing.T) {
	exit := 1
	data := WatchTaskData{
		TaskID:      "039",
		RunID:       7,
		TriggerID:   "trig-2",
		SessionName: "exec-039",
		ResourceKey: "auto-stack:main",
		Message:     "boom",
		ExitCode:    &exit,
	}

	ev, err := NewWatchTask(TypeWatchTaskFailed, RunProvenance{Project: "auto-stack"}, data)
	if err != nil {
		t.Fatalf("NewWatchTask: %v", err)
	}

	got, err := DecodeData[WatchTaskData](ev)
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.TaskID != data.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, data.TaskID)
	}
	if got.RunID != data.RunID {
		t.Errorf("RunID = %d, want %d", got.RunID, data.RunID)
	}
	if got.TriggerID != data.TriggerID {
		t.Errorf("TriggerID = %q, want %q", got.TriggerID, data.TriggerID)
	}
	if got.SessionName != data.SessionName {
		t.Errorf("SessionName = %q, want %q", got.SessionName, data.SessionName)
	}
	if got.ResourceKey != data.ResourceKey {
		t.Errorf("ResourceKey = %q, want %q", got.ResourceKey, data.ResourceKey)
	}
	if got.Message != data.Message {
		t.Errorf("Message = %q, want %q", got.Message, data.Message)
	}
	if got.ExitCode == nil || *got.ExitCode != exit {
		t.Errorf("ExitCode = %v, want %d", got.ExitCode, exit)
	}
}
