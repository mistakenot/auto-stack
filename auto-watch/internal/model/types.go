package model

import "time"

type GlobalConfig struct {
	Projects []ProjectRef `json:"projects"`
}

type ProjectRef struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Remote string `json:"remote"`
}

type ProjectConfig struct {
	ID       string                `json:"id"`
	Tasks    map[string]TaskDef    `json:"tasks"`
	Triggers map[string]TriggerDef `json:"triggers"`
}

type TaskDef struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
}

type TriggerDef struct {
	Type                string   `json:"type"`
	When                string   `json:"when,omitempty"`
	Tasks               []string `json:"tasks"`
	OnlyIfBranchChanged string   `json:"onlyIfBranchHasChanged,omitempty"`
}

type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

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
