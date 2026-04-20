package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-env/internal/cli"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "dummy"), []byte("x"), 0644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func setupEnv(t *testing.T, dir string, config string, templates map[string]string) {
	t.Helper()
	envDir := filepath.Join(dir, ".auto", "env")
	filesDir := filepath.Join(envDir, "files")
	os.MkdirAll(filesDir, 0755)
	os.WriteFile(filepath.Join(envDir, "config.json"), []byte(config), 0644)
	for name, content := range templates {
		path := filepath.Join(filesDir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(content), 0644)
	}
}

func execute(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	origArgs := os.Args
	os.Args = append([]string{"autoenv"}, args...)
	defer func() { os.Args = origArgs }()

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := cli.Execute(context.Background(), &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

func TestInitScaffold(t *testing.T) {
	dir := initGitRepo(t)

	_, stderr, code := execute(t, dir, "init")
	if code != 0 {
		t.Fatalf("init failed (exit %d): %s", code, stderr)
	}
	if !strings.Contains(stderr, "Initialized") {
		t.Errorf("expected initialization message, got: %s", stderr)
	}

	configPath := filepath.Join(dir, ".auto", "env", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config.json not created: %v", err)
	}
	filesDir := filepath.Join(dir, ".auto", "env", "files")
	if _, err := os.Stat(filesDir); err != nil {
		t.Fatalf("files/ not created: %v", err)
	}

	// Idempotent
	_, _, code2 := execute(t, dir, "init")
	if code2 != 0 {
		t.Fatal("second init should succeed")
	}
}

func TestUpDownHappyPath(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up > marker.txt",
		"down_command": "echo down > marker.txt"
	}`, map[string]string{
		"ecosystem.config.js": `module.exports = { port: {{.Port.web}}, db: {{.Port.db}}, name: "{{.Name}}" };`,
	})

	// Up
	stdout, stderr, code := execute(t, dir, "up")
	if code != 0 {
		t.Fatalf("up failed (exit %d): %s", code, stderr)
	}

	var upResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &upResult); err != nil {
		t.Fatalf("parse up output: %v\nstdout: %s", err, stdout)
	}
	ports, ok := upResult["ports"].(map[string]any)
	if !ok {
		t.Fatal("missing ports in output")
	}
	if ports["db"] == nil || ports["web"] == nil {
		t.Errorf("expected db and web ports, got %v", ports)
	}

	// Check rendered file exists
	rendered := filepath.Join(dir, "ecosystem.config.js")
	data, err := os.ReadFile(rendered)
	if err != nil {
		t.Fatalf("rendered file missing: %v", err)
	}
	if !strings.Contains(string(data), "port:") {
		t.Errorf("rendered file doesn't contain port: %s", string(data))
	}

	// Check marker
	marker, _ := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if !strings.Contains(string(marker), "up") {
		t.Error("up_command didn't run")
	}

	// Status
	stdout, _, code = execute(t, dir, "status")
	if code != 0 {
		t.Fatal("status failed")
	}
	var status map[string]any
	json.Unmarshal([]byte(stdout), &status)
	if status["provisioned"] != true {
		t.Error("expected provisioned: true")
	}

	// Down
	_, stderr, code = execute(t, dir, "down")
	if code != 0 {
		t.Fatalf("down failed (exit %d): %s", code, stderr)
	}
	if _, err := os.Stat(rendered); !os.IsNotExist(err) {
		t.Error("rendered file should be deleted after down")
	}

	// Status after down
	stdout, _, code = execute(t, dir, "status")
	if code != 0 {
		t.Fatal("status after down failed")
	}
	json.Unmarshal([]byte(stdout), &status)
	if status["provisioned"] != false {
		t.Error("expected provisioned: false after down")
	}
}

func TestUpAutoRestart(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up >> log.txt",
		"down_command": "echo down >> log.txt"
	}`, map[string]string{
		"config.txt": `port={{.Port.web}}`,
	})

	// First up
	_, _, code := execute(t, dir, "up")
	if code != 0 {
		t.Fatal("first up failed")
	}

	// Second up without explicit down
	_, stderr, code := execute(t, dir, "up")
	if code != 0 {
		t.Fatalf("second up failed: %s", stderr)
	}
	if !strings.Contains(stderr, "running down first") {
		t.Error("expected auto-restart message")
	}

	log, _ := os.ReadFile(filepath.Join(dir, "log.txt"))
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	// Should have: up, down, up
	if len(lines) != 3 {
		t.Errorf("expected 3 log lines (up, down, up), got %d: %v", len(lines), lines)
	}
}

func TestUpFileConflict(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up",
		"down_command": "echo down"
	}`, map[string]string{
		"existing.txt": `port={{.Port.web}}`,
	})

	// Pre-create conflicting file
	os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("existing"), 0644)

	_, stderr, code := execute(t, dir, "up")
	if code == 0 {
		t.Fatal("expected error for file conflict")
	}
	if !strings.Contains(stderr, "already exist") {
		t.Errorf("expected conflict error, got: %s", stderr)
	}
}

func TestUpForce(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up",
		"down_command": "echo down"
	}`, map[string]string{
		"existing.txt": `port={{.Port.web}}`,
	})

	os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("existing"), 0644)

	_, _, code := execute(t, dir, "up", "--force")
	if code != 0 {
		t.Fatal("up --force should succeed")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "existing.txt"))
	if strings.Contains(string(data), "existing") {
		t.Error("file should have been overwritten")
	}
}

func TestUpDryRun(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up > marker.txt",
		"down_command": "echo down"
	}`, map[string]string{
		"config.txt": `port={{.Port.web}}`,
	})

	stdout, _, code := execute(t, dir, "up", "--dry-run")
	if code != 0 {
		t.Fatal("dry-run failed")
	}
	if !strings.Contains(stdout, "config.txt") {
		t.Error("dry-run should print file headers")
	}
	if !strings.Contains(stdout, "port=") {
		t.Error("dry-run should print rendered content")
	}

	// No files written
	if _, err := os.Stat(filepath.Join(dir, "config.txt")); !os.IsNotExist(err) {
		t.Error("dry-run should not write files")
	}
	// No manifest
	if _, err := os.Stat(filepath.Join(dir, ".auto", "env", ".generated")); !os.IsNotExist(err) {
		t.Error("dry-run should not create manifest")
	}
	// No marker
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); !os.IsNotExist(err) {
		t.Error("dry-run should not run up_command")
	}
}

func TestDownCommandFailure(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "echo up",
		"down_command": "exit 1"
	}`, map[string]string{
		"config.txt": `port={{.Port.web}}`,
	})

	// First up
	_, _, code := execute(t, dir, "up")
	if code != 0 {
		t.Fatal("up failed")
	}

	// Down with failing command
	_, stderr, code := execute(t, dir, "down")
	if code == 0 {
		t.Fatal("down should fail")
	}
	if !strings.Contains(stderr, "down_command failed") {
		t.Errorf("expected down_command error, got: %s", stderr)
	}

	// Files should still exist
	if _, err := os.Stat(filepath.Join(dir, "config.txt")); os.IsNotExist(err) {
		t.Error("generated files should be preserved on down_command failure")
	}
	if _, err := os.Stat(filepath.Join(dir, ".auto", "env", ".generated")); os.IsNotExist(err) {
		t.Error("manifest should be preserved on down_command failure")
	}
}

func TestWorktreeSlotAssignment(t *testing.T) {
	dir := initGitRepo(t)
	setupEnv(t, dir, `{
		"up_command": "true",
		"down_command": "true"
	}`, map[string]string{
		"config.txt": `port={{.Port.web}}`,
	})

	// Main worktree gets slot 0
	stdout, _, code := execute(t, dir, "up")
	if code != 0 {
		t.Fatal("up on main failed")
	}

	var result map[string]any
	json.Unmarshal([]byte(stdout), &result)
	if result["slot"].(float64) != 0 {
		t.Error("main worktree should get slot 0")
	}

	// Create a linked worktree
	run(t, dir, "git", "branch", "test-branch")
	wtDir := filepath.Join(t.TempDir(), "linked-wt")
	run(t, dir, "git", "worktree", "add", wtDir, "test-branch")
	t.Cleanup(func() {
		cmd := exec.Command("git", "worktree", "remove", "--force", wtDir)
		cmd.Dir = dir
		cmd.Run()
	})

	// Copy env config to linked worktree
	setupEnv(t, wtDir, `{
		"up_command": "true",
		"down_command": "true"
	}`, map[string]string{
		"config.txt": `port={{.Port.web}}`,
	})

	// Clean up main first
	execute(t, dir, "down")

	stdout, _, code = execute(t, wtDir, "up")
	if code != 0 {
		t.Fatal("up on linked worktree failed")
	}
	json.Unmarshal([]byte(stdout), &result)
	slot := result["slot"].(float64)
	if slot == 0 {
		t.Error("linked worktree should not get slot 0")
	}

	execute(t, wtDir, "down")
}
