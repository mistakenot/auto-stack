package model

import (
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// GlobalConfig and ProjectRef are aliases for the shared project registry types.
// The registry is canonical at ~/.auto/projects.json and shared across all auto tools.
type GlobalConfig = sharedconfig.ProjectsConfig

type ProjectRef = sharedconfig.ProjectRef

type ProjectConfig struct {
	ID       string                `json:"id"`
	Tasks    map[string]TaskDef    `json:"tasks"`
	Triggers map[string]TriggerDef `json:"triggers"`
}

type TaskDef struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	// Backend names the execution Backend this task runs on (e.g. "tmux"). Empty
	// means the default Backend ("tmux"), so existing configs are unchanged.
	Backend string `json:"backend,omitempty"`
	// Timeout is a Go duration string (e.g. "30m") bounding a run; empty means
	// the 2h default. Validated with time.ParseDuration and must be > 0.
	Timeout string `json:"timeout,omitempty"`
}

type TriggerDef struct {
	Type                string   `json:"type"`
	When                string   `json:"when,omitempty"`
	Glob                string   `json:"glob,omitempty"`
	Tasks               []string `json:"tasks"`
	OnlyIfBranchChanged string   `json:"onlyIfBranchHasChanged,omitempty"`
}

type FileSnapshotRecord struct {
	ProjectID   string    `json:"project_id"`
	TriggerID   string    `json:"trigger_id"`
	FilePath    string    `json:"file_path"`
	ModTime     time.Time `json:"mod_time"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DefaultBackend is the Backend a run uses when none is recorded. It mirrors
// backend.DefaultBackendName; legacy runs read back with this Backend.
const DefaultBackend = "tmux"

type RunState string

const (
	RunPending   RunState = "pending"
	RunRunning   RunState = "running"
	RunCompleted RunState = "completed"
	RunFailed    RunState = "failed"
)

type RunRecord struct {
	ID           int64     `json:"id"`
	ProjectID    string    `json:"project_id"`
	ProjectPath  string    `json:"project_path"`
	TriggerID    string    `json:"trigger_id"`
	TriggerType  string    `json:"trigger_type"`
	TaskID       string    `json:"task_id"`
	TaskType     string    `json:"task_type"`
	ResourceKey  string    `json:"resource_key"`
	Branch       string    `json:"branch,omitempty"`
	State        RunState  `json:"state"`
	SessionName  string    `json:"session_name,omitempty"`
	RuntimeDir   string    `json:"runtime_dir,omitempty"`
	OutputPath   string    `json:"output_path,omitempty"`
	ExitPath     string    `json:"exit_path,omitempty"`
	WorktreePath string    `json:"worktree_path,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at,omitzero"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	// Backend is the execution Backend the run was started on. Legacy rows read
	// back as "tmux".
	Backend string `json:"backend,omitempty"`
	// BackendTarget is the Backend-specific target (a Binding's Target, e.g. the
	// tmux session name). Legacy rows read back as the session name.
	BackendTarget string `json:"backend_target,omitempty"`
	// DeadlineAt is started_at + the run's timeout, fixed when the run starts.
	// Null on legacy rows.
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`
	// FailureReason classifies a failed run (set in a later phase).
	FailureReason string `json:"failure_reason,omitempty"`
}

// OwedFireRecord is a Trigger fire that was recorded but could not be dispatched
// this tick — because the TaskDef was deduped, backed off, or its Start failed —
// on a cursor-advancing Trigger (file_created, or a branch-changed cron) whose
// snapshot/cursor moved on regardless. It is coalesced per (Project, Trigger,
// TaskDef): the first reason and time are kept so repeated skips never pile up.
// The daemon drains owed fires at the start of each Project's evaluation.
type OwedFireRecord struct {
	ProjectID   string    `json:"project_id"`
	TriggerID   string    `json:"trigger_id"`
	TaskID      string    `json:"task_id"`
	Reason      string    `json:"reason"`
	FirstOwedAt time.Time `json:"first_owed_at"`
}

type TriggerStateRecord struct {
	ProjectID     string    `json:"project_id"`
	TriggerID     string    `json:"trigger_id"`
	LastDueMinute time.Time `json:"last_due_minute,omitzero"`
	LastBranchSHA string    `json:"last_branch_sha,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type EventRecord struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	EventType string         `json:"event_type"`
	ProjectID string         `json:"project_id,omitempty"`
	TriggerID string         `json:"trigger_id,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	RunID     *int64         `json:"run_id,omitempty"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata"`
	RawJSON   string         `json:"-"`
}

type DoctorCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	Details     string `json:"details,omitempty"`
}

type HealthIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	ProjectID   string `json:"project_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	ResourceKey string `json:"resource_key,omitempty"`
	RunID       int64  `json:"run_id,omitempty"`
	Count       int    `json:"count,omitempty"`
}
