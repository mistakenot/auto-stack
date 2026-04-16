package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

func TestInitCreatesGlobalAndProjectConfig(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	env.AddRemote(repoRoot, "git@github.com:example/demo.git")

	stdout, stderr, code := env.RunCLI(repoRoot, "init", "--project-id", "demo-project")
	if code != 0 {
		t.Fatalf("init failed with code %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	settingsPath := filepath.Join(env.Home, ".auto", "watch", "settings.json")
	projectPath := filepath.Join(repoRoot, ".auto", "watch", "project.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	if _, err := os.Stat(projectPath); err != nil {
		t.Fatalf("project.json missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".auto", "watch", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), "worktrees/") {
		t.Fatalf("expected .gitignore to contain worktrees/, got %q", string(data))
	}

	cfg, err := config.LoadGlobalConfig(settingsPath)
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "demo-project" {
		t.Fatalf("unexpected projects in settings.json: %#v", cfg.Projects)
	}
}

func TestTaskAndTriggerCRUDJSONList(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "Run-Etl", "--bash", "echo hi"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--id", "Daily", "--cron", "0 9 * * 1"); code != 0 {
		t.Fatalf("trigger create failed: %s", stderr)
	}
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "add-task", "--trigger", "daily", "--task", "run-etl"); code != 0 {
		t.Fatalf("trigger add-task failed: %s", stderr)
	}

	stdout, stderr, code := env.RunCLI(repoRoot, "task", "list", "--json")
	if code != 0 {
		t.Fatalf("task list failed: %s", stderr)
	}
	var taskPayload struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
		Errors []model.ValidationError `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &taskPayload); err != nil {
		t.Fatalf("unmarshal task list: %v", err)
	}
	if len(taskPayload.Tasks) != 1 || taskPayload.Tasks[0].ID != "run-etl" {
		t.Fatalf("unexpected task list payload: %+v", taskPayload)
	}
	if len(taskPayload.Errors) != 0 {
		t.Fatalf("unexpected task list errors: %+v", taskPayload.Errors)
	}

	stdout, stderr, code = env.RunCLI(repoRoot, "trigger", "list", "--json")
	if code != 0 {
		t.Fatalf("trigger list failed: %s", stderr)
	}
	var triggerPayload struct {
		Triggers []struct {
			ID    string   `json:"id"`
			Tasks []string `json:"tasks"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(stdout), &triggerPayload); err != nil {
		t.Fatalf("unmarshal trigger list: %v", err)
	}
	if len(triggerPayload.Triggers) != 1 || triggerPayload.Triggers[0].ID != "daily" {
		t.Fatalf("unexpected trigger list payload: %+v", triggerPayload)
	}
	if len(triggerPayload.Triggers[0].Tasks) != 1 || triggerPayload.Triggers[0].Tasks[0] != "run-etl" {
		t.Fatalf("unexpected linked tasks: %+v", triggerPayload.Triggers[0].Tasks)
	}
}

func TestFileCreatedTriggerCRUD(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "process-doc", "--bash", "echo ok"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}

	// Create a file_created trigger.
	stdout, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--type", "file_created", "--glob", "docs/*.md", "--id", "watch-docs")
	if code != 0 {
		t.Fatalf("trigger create failed (code %d): %s", code, stderr)
	}
	if !strings.Contains(stdout, "Saved trigger watch-docs") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	// Link task.
	if _, stderr, code := env.RunCLI(repoRoot, "trigger", "add-task", "--trigger", "watch-docs", "--task", "process-doc"); code != 0 {
		t.Fatalf("trigger add-task failed: %s", stderr)
	}

	// Verify trigger list JSON output.
	stdout, stderr, code = env.RunCLI(repoRoot, "trigger", "list", "--json")
	if code != 0 {
		t.Fatalf("trigger list failed: %s", stderr)
	}
	var payload struct {
		Triggers []struct {
			ID    string   `json:"id"`
			Type  string   `json:"type"`
			Glob  string   `json:"glob"`
			Tasks []string `json:"tasks"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal trigger list: %v\nstdout: %s", err, stdout)
	}
	if len(payload.Triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(payload.Triggers))
	}
	trig := payload.Triggers[0]
	if trig.ID != "watch-docs" || trig.Type != "file_created" || trig.Glob != "docs/*.md" {
		t.Fatalf("unexpected trigger: %+v", trig)
	}
	if len(trig.Tasks) != 1 || trig.Tasks[0] != "process-doc" {
		t.Fatalf("unexpected tasks: %+v", trig.Tasks)
	}

	// Verify project.json persisted correctly.
	cfg, err := config.LoadProjectConfig(repoRoot)
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	trigDef, ok := cfg.Triggers["watch-docs"]
	if !ok {
		t.Fatalf("trigger watch-docs not found in project config")
	}
	if trigDef.Type != "file_created" || trigDef.Glob != "docs/*.md" {
		t.Fatalf("unexpected trigger def: %+v", trigDef)
	}
}

func TestTriggerCreateRejectsMissingGlob(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}

	_, stderr, code := env.RunCLI(repoRoot, "trigger", "create", "--type", "file_created", "--id", "bad")
	if code == 0 {
		t.Fatal("expected non-zero exit for missing --glob")
	}
	if !strings.Contains(stderr, "--glob is required") {
		t.Fatalf("expected glob required error, got: %s", stderr)
	}
}

func TestTaskRunBashReturnsUnderlyingExitCode(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "exit-seven", "--bash", "echo hello && exit 7"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}
	stdout, stderr, code := env.RunCLI(repoRoot, "task", "run", "--id", "exit-seven")
	if code != 7 {
		t.Fatalf("expected exit code 7, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("expected stdout to contain command output, got %q", stdout)
	}
}

func TestTaskRunClaudeUsesTemporaryWorktreeAndLeavesNoDB(t *testing.T) {
	env := testutil.NewEnv(t)
	repoRoot := env.NewRepo("demo")
	if _, _, code := env.RunCLI(repoRoot, "init"); code != 0 {
		t.Fatal("init failed")
	}
	claudeCWDFile := filepath.Join(env.Home, "claude.cwd")
	claudeArgsFile := filepath.Join(env.Home, "claude.args")
	t.Setenv("AUTOWATCH_TEST_CLAUDE_CWD", claudeCWDFile)
	t.Setenv("AUTOWATCH_TEST_CLAUDE_ARGS", claudeArgsFile)
	env.WriteExecutable("claude", "#!/usr/bin/env bash\nset -eu\nprintf '%s\\n' \"$PWD\" > \"$AUTOWATCH_TEST_CLAUDE_CWD\"\nprintf '%s\\n' \"$*\" > \"$AUTOWATCH_TEST_CLAUDE_ARGS\"\n")
	if _, stderr, code := env.RunCLI(repoRoot, "task", "create", "--id", "review", "--claude", "/review the repo"); code != 0 {
		t.Fatalf("task create failed: %s", stderr)
	}

	if _, stderr, code := env.RunCLI(repoRoot, "task", "run", "--id", "review"); code != 0 {
		t.Fatalf("task run failed: %s", stderr)
	}

	cwdData, err := os.ReadFile(claudeCWDFile)
	if err != nil {
		t.Fatalf("read recorded claude cwd: %v", err)
	}
	if !strings.Contains(string(cwdData), filepath.Join(repoRoot, ".auto", "watch", "worktrees")) {
		t.Fatalf("expected claude to run in worktree, got %q", string(cwdData))
	}
	argsData, err := os.ReadFile(claudeArgsFile)
	if err != nil {
		t.Fatalf("read recorded claude args: %v", err)
	}
	if !strings.Contains(string(argsData), "--dangerously-skip-permissions") || !strings.Contains(string(argsData), "<autowatch-context>") {
		t.Fatalf("expected augmented prompt args, got %q", string(argsData))
	}
	worktreesDir := filepath.Join(repoRoot, ".auto", "watch", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		t.Fatalf("read worktrees dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected task run cleanup to remove manual worktree, found %d entries", len(entries))
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".auto", "watch", "logs.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("task run should not create daemon-managed logs.sqlite")
	}
}
