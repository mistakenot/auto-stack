package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mistakenot/auto-watch/internal/model"
	_ "modernc.org/sqlite"
)

var ErrActiveRunExists = errors.New("active run already exists")

type Store struct {
	db *sql.DB
}

type ReserveRunInput struct {
	ProjectID   string
	ProjectPath string
	TriggerID   string
	TriggerType string
	TaskID      string
	TaskType    string
	ResourceKey string
	Branch      string
	StartedAt   time.Time
}

type RunStartUpdate struct {
	SessionName  string
	RuntimeDir   string
	OutputPath   string
	ExitPath     string
	WorktreePath string
	Branch       string
}

type EventInput struct {
	Timestamp time.Time
	Level     string
	EventType string
	ProjectID string
	TriggerID string
	TaskID    string
	RunID     *int64
	Message   string
	Metadata  map[string]any
}

type EventFilter struct {
	Limit     int
	ProjectID string
	TaskID    string
	Level     string
	EventType string
	Since     *time.Time
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply sqlite pragma %q: %w", stmt, err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY
		);`,
		`CREATE TABLE IF NOT EXISTS runs (
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
		`CREATE INDEX IF NOT EXISTS idx_runs_state ON runs (state);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_project_started_at ON runs (project_id, started_at DESC);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_runs_active
		ON runs (project_id, task_id, resource_key)
		WHERE state IN ('pending', 'running');`,
		`CREATE TABLE IF NOT EXISTS trigger_state (
			project_id TEXT NOT NULL,
			trigger_id TEXT NOT NULL,
			last_due_minute DATETIME,
			last_branch_sha TEXT,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (project_id, trigger_id)
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			level TEXT NOT NULL,
			event_type TEXT NOT NULL,
			project_id TEXT,
			trigger_id TEXT,
			task_id TEXT,
			run_id INTEGER,
			message TEXT NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events (timestamp DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_events_project_timestamp ON events (project_id, timestamp DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_events_task_timestamp ON events (task_id, timestamp DESC);`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("run migration statement: %w", err)
		}
	}
	return nil
}

func (s *Store) ReserveRun(ctx context.Context, input *ReserveRunInput) (int64, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO runs (
		project_id, project_path, trigger_id, trigger_type, task_id, task_type,
		resource_key, branch, state, started_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ProjectID,
		input.ProjectPath,
		input.TriggerID,
		input.TriggerType,
		input.TaskID,
		input.TaskType,
		input.ResourceKey,
		nullIfEmpty(input.Branch),
		string(model.RunPending),
		formatTime(input.StartedAt),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return 0, ErrActiveRunExists
		}
		return 0, fmt.Errorf("reserve run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read reserved run id: %w", err)
	}
	return id, nil
}

func (s *Store) GetRun(ctx context.Context, id int64) (model.RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, project_id, project_path, trigger_id, trigger_type, task_id, task_type,
		resource_key, branch, state, session_name, runtime_dir, output_path, exit_path,
		worktree_path, started_at, completed_at, exit_code, error_message
	FROM runs WHERE id = ?`, id)
	return scanRun(row)
}

func (s *Store) UpdateRunStarted(ctx context.Context, id int64, update *RunStartUpdate) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs
	SET state = ?, session_name = ?, runtime_dir = ?, output_path = ?, exit_path = ?, worktree_path = ?, branch = ?
	WHERE id = ?`,
		string(model.RunRunning),
		nullIfEmpty(update.SessionName),
		nullIfEmpty(update.RuntimeDir),
		nullIfEmpty(update.OutputPath),
		nullIfEmpty(update.ExitPath),
		nullIfEmpty(update.WorktreePath),
		nullIfEmpty(update.Branch),
		id,
	)
	if err != nil {
		return fmt.Errorf("mark run started: %w", err)
	}
	return nil
}

func (s *Store) MarkRunTerminal(ctx context.Context, id int64, state model.RunState, exitCode *int, completedAt time.Time, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs
	SET state = ?, exit_code = ?, completed_at = ?, error_message = ?
	WHERE id = ?`,
		string(state),
		exitCode,
		formatTime(completedAt),
		nullIfEmpty(errorMessage),
		id,
	)
	if err != nil {
		return fmt.Errorf("mark run terminal: %w", err)
	}
	return nil
}

func (s *Store) UpsertTriggerState(ctx context.Context, state *model.TriggerStateRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO trigger_state (
		project_id, trigger_id, last_due_minute, last_branch_sha, updated_at
	) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(project_id, trigger_id) DO UPDATE SET
		last_due_minute = excluded.last_due_minute,
		last_branch_sha = excluded.last_branch_sha,
		updated_at = excluded.updated_at`,
		state.ProjectID,
		state.TriggerID,
		nullableTime(state.LastDueMinute),
		nullIfEmpty(state.LastBranchSHA),
		formatTime(state.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert trigger state: %w", err)
	}
	return nil
}

func (s *Store) GetTriggerState(ctx context.Context, projectID, triggerID string) (model.TriggerStateRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT project_id, trigger_id, last_due_minute, last_branch_sha, updated_at
	FROM trigger_state WHERE project_id = ? AND trigger_id = ?`, projectID, triggerID)
	var record model.TriggerStateRecord
	var lastDue sql.NullString
	var lastBranch sql.NullString
	var updated string
	err := row.Scan(&record.ProjectID, &record.TriggerID, &lastDue, &lastBranch, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TriggerStateRecord{ProjectID: projectID, TriggerID: triggerID}, nil
	}
	if err != nil {
		return model.TriggerStateRecord{}, fmt.Errorf("load trigger state: %w", err)
	}
	if lastDue.Valid {
		record.LastDueMinute = mustParseTime(lastDue.String)
	}
	if lastBranch.Valid {
		record.LastBranchSHA = lastBranch.String
	}
	record.UpdatedAt = mustParseTime(updated)
	return record, nil
}

func (s *Store) InsertEvent(ctx context.Context, input *EventInput) (int64, error) {
	metadata := "{}"
	if len(input.Metadata) > 0 {
		data, err := json.Marshal(input.Metadata)
		if err != nil {
			return 0, fmt.Errorf("marshal event metadata: %w", err)
		}
		metadata = string(data)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO events (
		timestamp, level, event_type, project_id, trigger_id, task_id, run_id, message, metadata_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(input.Timestamp),
		input.Level,
		input.EventType,
		nullIfEmpty(input.ProjectID),
		nullIfEmpty(input.TriggerID),
		nullIfEmpty(input.TaskID),
		input.RunID,
		input.Message,
		metadata,
	)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read event id: %w", err)
	}
	return id, nil
}

func (s *Store) ListEvents(ctx context.Context, filter *EventFilter) ([]model.EventRecord, error) {
	clauses := []string{"1=1"}
	args := []any{}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.TaskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Level != "" {
		clauses = append(clauses, "level = ?")
		args = append(args, filter.Level)
	}
	if filter.EventType != "" {
		clauses = append(clauses, "event_type = ?")
		args = append(args, filter.EventType)
	}
	if filter.Since != nil {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, formatTime(*filter.Since))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	//nolint:gosec // SQL built from constant clause strings only, no user input
	query := fmt.Sprintf(`SELECT
		id, timestamp, level, event_type, project_id, trigger_id, task_id, run_id, message, metadata_json
	FROM events
	WHERE %s
	ORDER BY timestamp DESC
	LIMIT ?`, strings.Join(clauses, " AND "))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []model.EventRecord{}
	for rows.Next() {
		record, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

func (s *Store) ListRunsByStates(ctx context.Context, states ...model.RunState) ([]model.RunRecord, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(states))
	args := make([]any, 0, len(states))
	for _, state := range states {
		placeholders = append(placeholders, "?")
		args = append(args, string(state))
	}
	//nolint:gosec // SQL built from constant "?" placeholders only
	query := fmt.Sprintf(`SELECT
		id, project_id, project_path, trigger_id, trigger_type, task_id, task_type,
		resource_key, branch, state, session_name, runtime_dir, output_path, exit_path,
		worktree_path, started_at, completed_at, exit_code, error_message
	FROM runs
	WHERE state IN (%s)
	ORDER BY started_at ASC`, strings.Join(placeholders, ","))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query runs by state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	runs := []model.RunRecord{}
	for rows.Next() {
		record, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs by state: %w", err)
	}
	return runs, nil
}

func (s *Store) ListPendingBefore(ctx context.Context, cutoff time.Time) ([]model.RunRecord, error) {
	return s.listRunsWhere(ctx, "state = ? AND started_at < ?", string(model.RunPending), formatTime(cutoff))
}

func (s *Store) ListTerminalRunsOlderThan(ctx context.Context, cutoff time.Time) ([]model.RunRecord, error) {
	return s.listRunsWhere(ctx, "state IN (?, ?) AND completed_at IS NOT NULL AND completed_at < ?", string(model.RunCompleted), string(model.RunFailed), formatTime(cutoff))
}

func (s *Store) listRunsWhere(ctx context.Context, where string, args ...any) ([]model.RunRecord, error) {
	//nolint:gosec // SQL WHERE clause is a constant string from internal callers only
	query := fmt.Sprintf(`SELECT
		id, project_id, project_path, trigger_id, trigger_type, task_id, task_type,
		resource_key, branch, state, session_name, runtime_dir, output_path, exit_path,
		worktree_path, started_at, completed_at, exit_code, error_message
	FROM runs
	WHERE %s
	ORDER BY started_at ASC`, where)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	runs := []model.RunRecord{}
	for rows.Next() {
		record, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

func (s *Store) RecentRunCounts(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM runs WHERE started_at >= ? GROUP BY state`, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("query recent run counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan recent run counts: %w", err)
		}
		counts[state] = count
	}
	return counts, rows.Err()
}

func (s *Store) OldRunningRuns(ctx context.Context, cutoff time.Time) ([]model.RunRecord, error) {
	return s.listRunsWhere(ctx, "state = ? AND started_at < ?", string(model.RunRunning), formatTime(cutoff))
}

func (s *Store) OldPendingRuns(ctx context.Context, cutoff time.Time) ([]model.RunRecord, error) {
	return s.listRunsWhere(ctx, "state = ? AND started_at < ?", string(model.RunPending), formatTime(cutoff))
}

func (s *Store) FailedRunGroups(ctx context.Context, since time.Time, minCount int) ([]model.HealthIssue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, task_id, COUNT(*) FROM runs
	WHERE state = ? AND started_at >= ?
	GROUP BY project_id, task_id
	HAVING COUNT(*) >= ?
	ORDER BY COUNT(*) DESC`, string(model.RunFailed), formatTime(since), minCount)
	if err != nil {
		return nil, fmt.Errorf("query failed run groups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	issues := []model.HealthIssue{}
	for rows.Next() {
		var projectID string
		var taskID string
		var count int
		if err := rows.Scan(&projectID, &taskID, &count); err != nil {
			return nil, fmt.Errorf("scan failed run group: %w", err)
		}
		issues = append(issues, model.HealthIssue{
			Type:      "repeated_failures",
			Severity:  "warn",
			Message:   fmt.Sprintf("%d failed runs in the last 24h", count),
			ProjectID: projectID,
			TaskID:    taskID,
			Count:     count,
		})
	}
	return issues, rows.Err()
}

func (s *Store) DedupSkipGroups(ctx context.Context, since time.Time, minCount int) ([]model.HealthIssue, error) {
	events, err := s.ListEvents(ctx, &EventFilter{
		Limit:     1000,
		EventType: "task_skipped_dedup",
		Since:     &since,
	})
	if err != nil {
		return nil, err
	}
	type key struct {
		project string
		task    string
		rk      string
	}
	counts := map[key]int{}
	for i := range events {
		resourceKey, _ := events[i].Metadata["resource_key"].(string)
		counts[key{project: events[i].ProjectID, task: events[i].TaskID, rk: resourceKey}]++
	}
	issues := []model.HealthIssue{}
	for k, count := range counts {
		if count < minCount {
			continue
		}
		issues = append(issues, model.HealthIssue{
			Type:        "repeated_dedup_skips",
			Severity:    "warn",
			Message:     fmt.Sprintf("%d dedup skips in the last 24h", count),
			ProjectID:   k.project,
			TaskID:      k.task,
			ResourceKey: k.rk,
			Count:       count,
		})
	}
	return issues, nil
}

func scanRun(scanner interface{ Scan(dest ...any) error }) (model.RunRecord, error) {
	var record model.RunRecord
	var branch sql.NullString
	var session sql.NullString
	var runtimeDir sql.NullString
	var outputPath sql.NullString
	var exitPath sql.NullString
	var worktreePath sql.NullString
	var completedAt sql.NullString
	var exitCode sql.NullInt64
	var errorMessage sql.NullString
	var startedAt string
	var state string
	if err := scanner.Scan(
		&record.ID,
		&record.ProjectID,
		&record.ProjectPath,
		&record.TriggerID,
		&record.TriggerType,
		&record.TaskID,
		&record.TaskType,
		&record.ResourceKey,
		&branch,
		&state,
		&session,
		&runtimeDir,
		&outputPath,
		&exitPath,
		&worktreePath,
		&startedAt,
		&completedAt,
		&exitCode,
		&errorMessage,
	); err != nil {
		return model.RunRecord{}, fmt.Errorf("scan run: %w", err)
	}
	record.State = model.RunState(state)
	record.StartedAt = mustParseTime(startedAt)
	if branch.Valid {
		record.Branch = branch.String
	}
	if session.Valid {
		record.SessionName = session.String
	}
	if runtimeDir.Valid {
		record.RuntimeDir = runtimeDir.String
	}
	if outputPath.Valid {
		record.OutputPath = outputPath.String
	}
	if exitPath.Valid {
		record.ExitPath = exitPath.String
	}
	if worktreePath.Valid {
		record.WorktreePath = worktreePath.String
	}
	if completedAt.Valid {
		record.CompletedAt = mustParseTime(completedAt.String)
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		record.ExitCode = &value
	}
	if errorMessage.Valid {
		record.ErrorMessage = errorMessage.String
	}
	return record, nil
}

func scanEvent(scanner interface{ Scan(dest ...any) error }) (model.EventRecord, error) {
	var record model.EventRecord
	var project sql.NullString
	var trigger sql.NullString
	var task sql.NullString
	var runID sql.NullInt64
	var timestamp string
	var metadata string
	if err := scanner.Scan(
		&record.ID,
		&timestamp,
		&record.Level,
		&record.EventType,
		&project,
		&trigger,
		&task,
		&runID,
		&record.Message,
		&metadata,
	); err != nil {
		return model.EventRecord{}, fmt.Errorf("scan event: %w", err)
	}
	record.Timestamp = mustParseTime(timestamp)
	if project.Valid {
		record.ProjectID = project.String
	}
	if trigger.Valid {
		record.TriggerID = trigger.String
	}
	if task.Valid {
		record.TaskID = task.String
	}
	if runID.Valid {
		value := runID.Int64
		record.RunID = &value
	}
	record.RawJSON = metadata
	record.Metadata = map[string]any{}
	if metadata != "" {
		_ = json.Unmarshal([]byte(metadata), &record.Metadata)
	}
	return record, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func mustParseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isUniqueConstraint(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE") || strings.Contains(msg, "uniq_runs_active")
}
