package bus

// Data-plane event types for the auto-watch daemon task lifecycle.
// These are always-on (NOT gated by --ctl-events).
const (
	TypeWatchTaskStarted   = "watch.task.started"
	TypeWatchTaskCompleted = "watch.task.completed"
	TypeWatchTaskFailed    = "watch.task.failed"
)

// WatchTaskData is the data-plane task lifecycle payload per §6.5.
type WatchTaskData struct {
	TaskID      string `json:"task_id"`
	RunID       int64  `json:"run_id"`
	TriggerID   string `json:"trigger_id"`
	SessionName string `json:"session_name,omitempty"`
	ResourceKey string `json:"resource_key"`
	Message     string `json:"message,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
}

// RunProvenance carries envelope provenance from a run record, keeping bus
// free of auto-watch/model imports.
type RunProvenance struct {
	Project  string
	Branch   string
	Worktree string
	Remote   string
	Commit   string
}

// NewWatchTask constructs a watch.task.* event with the given type, run
// provenance, and task data payload.
func NewWatchTask(typ string, p RunProvenance, d WatchTaskData) (Event, error) {
	ev, err := NewEvent(typ, "auto/watch/daemon", d)
	if err != nil {
		return Event{}, err
	}
	ev.Project = p.Project
	ev.Branch = p.Branch
	ev.Worktree = p.Worktree
	ev.Remote = p.Remote
	ev.Commit = p.Commit
	return ev, nil
}
