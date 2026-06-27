package rpcmethods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
)

type DispatchParams struct {
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId"`
}

type DispatchResult struct {
	RunID int64  `json:"runId"`
	State string `json:"state"`
}

type CancelParams struct {
	RunID int64 `json:"runId"`
}

type CancelResult struct {
	RunID int64  `json:"runId"`
	State string `json:"state"`
}

type StatusParams struct {
	RunID int64 `json:"runId"`
}

type ListParams struct {
	State string `json:"state,omitempty"`
}

type RunView struct {
	ID           int64  `json:"id"`
	ProjectID    string `json:"project_id"`
	TaskID       string `json:"task_id"`
	State        string `json:"state"`
	SessionName  string `json:"session_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at,omitempty"`
}

type OutputParams struct {
	RunID     int64 `json:"runId"`
	TailLines int   `json:"tailLines,omitempty"`
	MaxBytes  int   `json:"maxBytes,omitempty"`
}

type OutputResult struct {
	RunID     int64  `json:"runId"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
	Path      string `json:"path"`
}

func (h *Handlers) handleTaskDispatch(ctx context.Context, params json.RawMessage) (any, error) {
	var p DispatchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid params: " + err.Error()}
	}
	if p.ProjectID == "" || p.TaskID == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "projectId and taskId are required"}
	}

	reg := h.reg()
	var projectPath string
	for _, proj := range reg.Projects {
		if proj.ID == p.ProjectID {
			projectPath = proj.Path
			break
		}
	}
	if projectPath == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: fmt.Sprintf("unknown projectId %q", p.ProjectID)}
	}

	projectCfg, err := config.LoadProjectConfig(projectPath)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "load project config: " + err.Error()}
	}

	task, ok := projectCfg.Tasks[p.TaskID]
	if !ok {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: fmt.Sprintf("unknown taskId %q in project %q", p.TaskID, p.ProjectID)}
	}

	now := time.Now()
	runID, err := h.svc.Dispatch(ctx, &store.ReserveRunInput{
		ProjectID:   p.ProjectID,
		ProjectPath: projectPath,
		TriggerID:   "rpc",
		TriggerType: "rpc",
		TaskID:      p.TaskID,
		TaskType:    task.Type,
		ResourceKey: "rpc:" + p.TaskID,
		StartedAt:   now,
	}, task)
	if err != nil {
		if errors.Is(err, store.ErrActiveRunExists) {
			return nil, &rpc.Error{
				Code:    -32001,
				Message: fmt.Sprintf("active run already exists for task %q in project %q", p.TaskID, p.ProjectID),
			}
		}
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "dispatch failed: " + err.Error()}
	}

	return DispatchResult{RunID: runID, State: string(model.RunRunning)}, nil
}

func (h *Handlers) handleTaskCancel(ctx context.Context, params json.RawMessage) (any, error) {
	var p CancelParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid params: " + err.Error()}
	}
	if p.RunID == 0 {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "runId is required"}
	}

	state, err := h.svc.Cancel(ctx, p.RunID)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "cancel failed: " + err.Error()}
	}

	return CancelResult{RunID: p.RunID, State: string(state)}, nil
}

func (h *Handlers) handleTaskStatus(ctx context.Context, params json.RawMessage) (any, error) {
	var p StatusParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid params: " + err.Error()}
	}
	if p.RunID == 0 {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "runId is required"}
	}

	run, err := h.svc.Store.GetRun(ctx, p.RunID)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "get run: " + err.Error()}
	}

	return runToView(&run), nil
}

func (h *Handlers) handleTaskList(ctx context.Context, params json.RawMessage) (any, error) {
	var p ListParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid params: " + err.Error()}
		}
	}

	var states []model.RunState
	if p.State != "" {
		states = []model.RunState{model.RunState(p.State)}
	} else {
		states = []model.RunState{model.RunPending, model.RunRunning, model.RunCompleted, model.RunFailed}
	}

	runs, err := h.svc.Store.ListRunsByStates(ctx, states...)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "list runs: " + err.Error()}
	}

	views := make([]RunView, len(runs))
	for i := range runs {
		views[i] = runToView(&runs[i])
	}

	return map[string]any{"runs": views}, nil
}

func (h *Handlers) handleTaskOutput(ctx context.Context, params json.RawMessage) (any, error) {
	var p OutputParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid params: " + err.Error()}
	}
	if p.RunID == 0 {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "runId is required"}
	}

	run, err := h.svc.Store.GetRun(ctx, p.RunID)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "get run: " + err.Error()}
	}

	if run.State == model.RunPending || strings.TrimSpace(run.OutputPath) == "" {
		return OutputResult{RunID: p.RunID, Output: "", Truncated: false, Path: ""}, nil
	}

	tailLines := p.TailLines
	if tailLines <= 0 {
		tailLines = 200
	}
	maxBytes := p.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}

	data, err := os.ReadFile(run.OutputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return OutputResult{RunID: p.RunID, Output: "", Truncated: false, Path: run.OutputPath}, nil
		}
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "read output: " + err.Error()}
	}

	content := string(data)
	truncated := false

	if len(content) > maxBytes {
		content = content[len(content)-maxBytes:]
		truncated = true
	}

	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
		truncated = true
	}
	content = strings.Join(lines, "\n")

	return OutputResult{RunID: p.RunID, Output: content, Truncated: truncated, Path: run.OutputPath}, nil
}

func runToView(r *model.RunRecord) RunView {
	v := RunView{
		ID:           r.ID,
		ProjectID:    r.ProjectID,
		TaskID:       r.TaskID,
		State:        string(r.State),
		SessionName:  r.SessionName,
		WorktreePath: r.WorktreePath,
		Branch:       r.Branch,
		ExitCode:     r.ExitCode,
		ErrorMessage: r.ErrorMessage,
		StartedAt:    r.StartedAt.UTC().Format(time.RFC3339),
	}
	if !r.CompletedAt.IsZero() {
		v.CompletedAt = r.CompletedAt.UTC().Format(time.RFC3339)
	}
	return v
}
