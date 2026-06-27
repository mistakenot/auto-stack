package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// --- helpers ---------------------------------------------------------------

// initRepo creates a throwaway git repo at dir with a single commit. It scopes
// identity via env so the test never depends on the host's global git config.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "initial")
}

// seedRemotes writes ~/.auto/etl/settings.json with the given workspace→remote map.
func seedRemotes(t *testing.T, remotes map[string]string) {
	t.Helper()
	path := etlSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir etl dir: %v", err)
	}
	data, err := json.MarshalIndent(etlSettings{Remotes: remotes}, "", "  ")
	if err != nil {
		t.Fatalf("marshal remotes: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write remotes cache: %v", err)
	}
}

// seedRegistry writes ~/.auto/projects.json with the given projects.
func seedRegistry(t *testing.T, projects ...sharedconfig.ProjectRef) {
	t.Helper()
	path, err := sharedconfig.ProjectsConfigPath()
	if err != nil {
		t.Fatalf("ProjectsConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir auto dir: %v", err)
	}
	if err := sharedconfig.SaveProjects(path, sharedconfig.ProjectsConfig{Projects: projects}); err != nil {
		t.Fatalf("SaveProjects: %v", err)
	}
}

// capture redirects os.Stdout/os.Stderr to temp files for the duration of fn and
// returns what was written to each. Temp files (not pipes) avoid buffer deadlocks.
func capture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outF, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	errF, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
		_ = outF.Close()
		_ = errF.Close()
	}()
	fn()
	ob, _ := os.ReadFile(outF.Name())
	eb, _ := os.ReadFile(errF.Name())
	return string(ob), string(eb)
}

// runETL constructs and executes a fresh run command (resetting the package flag
// vars to their defaults) with the given args.
func runETL(args ...string) error {
	cmd := newRunCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

// --- git phase: gate narrows discovery, cache stays whole -------------------

// TestRunGate_GitPhaseGatesUnregistered builds two local repos — one registered,
// one not — and runs the git-history phase. Only the registered repo should be
// discovered/extracted (AC-1/2/4), the unfiltered remotes cache must survive
// (AC-6), and the gate summary must land on stderr with stdout left clean (AC-8).
func TestRunGate_GitPhaseGatesUnregistered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	base := t.TempDir()
	alpha := filepath.Join(base, "alpha") // registered
	beta := filepath.Join(base, "beta")   // unregistered
	initRepo(t, alpha)
	initRepo(t, beta)

	seedRemotes(t, map[string]string{alpha: "", beta: ""})
	seedRegistry(t, sharedconfig.ProjectRef{ID: "alpha", Path: alpha})

	out := filepath.Join(t.TempDir(), "out")
	stdout, stderr := capture(t, func() {
		if err := runETL("--only", "git", "--output", out); err != nil {
			t.Fatalf("run --only git: %v", err)
		}
	})

	// AC-1/2/4: registered repo discovered, unregistered one never reaches git.
	if !strings.Contains(stderr, "discovered 1 repo") {
		t.Errorf("expected exactly 1 repo discovered, stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, alpha) {
		t.Errorf("registered repo %q missing from stderr:\n%s", alpha, stderr)
	}
	if strings.Contains(stderr, beta) {
		t.Errorf("unregistered repo %q must not be processed, stderr:\n%s", beta, stderr)
	}

	// AC-8: gate summary on stderr only; stdout free of it.
	if !strings.Contains(stderr, "registry gate") {
		t.Errorf("gate summary missing from stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "kept 1") || !strings.Contains(stderr, "skipped 1") {
		t.Errorf("gate summary counts wrong, stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "registry gate") {
		t.Errorf("gate summary leaked onto stdout:\n%s", stdout)
	}

	// AC-6: canonical remotes cache still lists BOTH workspaces, unfiltered.
	cache := readRemotesCache(t)
	if _, ok := cache[alpha]; !ok {
		t.Errorf("cache lost registered workspace %q: %v", alpha, cache)
	}
	if _, ok := cache[beta]; !ok {
		t.Errorf("cache lost unregistered workspace %q — gate narrowed the cache: %v", beta, cache)
	}
}

// TestRunGate_RepoPathBypassesGate confirms --repo-path indexes a repo even when
// it is unregistered: the explicit path flows ungated into runGitETL (AC-7).
func TestRunGate_RepoPathBypassesGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	base := t.TempDir()
	alpha := filepath.Join(base, "alpha") // registered
	beta := filepath.Join(base, "beta")   // unregistered, reached only via --repo-path
	initRepo(t, alpha)
	initRepo(t, beta)

	seedRemotes(t, map[string]string{alpha: ""})
	seedRegistry(t, sharedconfig.ProjectRef{ID: "alpha", Path: alpha})

	out := filepath.Join(t.TempDir(), "out")
	_, stderr := capture(t, func() {
		if err := runETL("--only", "git", "--repo-path", beta, "--output", out); err != nil {
			t.Fatalf("run --only git --repo-path: %v", err)
		}
	})

	if !strings.Contains(stderr, beta) {
		t.Errorf("--repo-path repo %q was not indexed (gate should not apply to explicit paths):\n%s", beta, stderr)
	}
}

// TestRunGate_GitHubGatedExplicitError confirms GitHub discovery is gated too: an
// empty registry filters every remote out, and the explicit-only path returns a
// registry-aware error. A dummy token short-circuits auth so no network is hit.
func TestRunGate_GitHubGatedExplicitError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "dummy-token-no-network")

	seedRemotes(t, map[string]string{"/ws/repo": "https://github.com/me/repo.git"})
	// Registry intentionally absent → every remote is gated out.

	out := filepath.Join(t.TempDir(), "out")
	stdout, stderr := capture(t, func() {
		err := runETL("--only", "github", "--output", out)
		if err == nil {
			t.Fatal("expected explicit-only github run to error when registry gates all repos")
		}
		if !strings.Contains(err.Error(), "auto init --project") {
			t.Errorf("github empty-result error not registry-aware: %v", err)
		}
	})
	if !strings.Contains(stderr, "registry gate") {
		t.Errorf("gate summary missing from stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "registry gate") {
		t.Errorf("gate summary leaked onto stdout:\n%s", stdout)
	}
}

// --- sessions phase: never gated -------------------------------------------

// TestRunGate_SessionsNotGated runs --only sessions over a fixture holding one
// registered and one unregistered workspace and asserts BOTH are transformed:
// the registry gate must not touch session ETL (AC-5).
func TestRunGate_SessionsNotGated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	input := filepath.Join(t.TempDir(), "input")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}
	writeSessionFixture(t, filepath.Join(input, "registered.jsonl"),
		"11111111-1111-1111-1111-111111111111", "/repos/registered")
	writeSessionFixture(t, filepath.Join(input, "unregistered.jsonl"),
		"22222222-2222-2222-2222-222222222222", "/repos/unregistered")

	// Only the first workspace is registered — irrelevant to sessions, which
	// must be processed in full regardless.
	seedRegistry(t, sharedconfig.ProjectRef{ID: "registered", Path: "/repos/registered"})

	out := filepath.Join(t.TempDir(), "out")
	stdout, _ := capture(t, func() {
		if err := runETL("--only", "sessions", "--input", input, "--output", out); err != nil {
			t.Fatalf("run --only sessions: %v", err)
		}
	})

	// AC-5: both sessions parsed and transformed (gate never narrows sessions).
	if !strings.Contains(stdout, "parsed 2 sessions") {
		t.Errorf("expected both sessions parsed, stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "2 sessions") {
		t.Errorf("expected 2 sessions transformed, stdout:\n%s", stdout)
	}
	if dirEmpty(t, filepath.Join(out, "sessions")) {
		t.Errorf("no sessions parquet written under %s", out)
	}
	if dirEmpty(t, filepath.Join(out, "messages")) {
		t.Errorf("no messages parquet written under %s", out)
	}
}

// --- small assertion helpers ------------------------------------------------

func readRemotesCache(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(etlSettingsPath())
	if err != nil {
		t.Fatalf("read remotes cache: %v", err)
	}
	var s etlSettings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal remotes cache: %v", err)
	}
	return s.Remotes
}

// writeSessionFixture writes a minimal but valid Claude Code session JSONL with a
// user+assistant exchange in the given workspace.
func writeSessionFixture(t *testing.T, path, sessionID, workspace string) {
	t.Helper()
	lines := []string{
		`{"type":"user","sessionId":"` + sessionID + `","timestamp":"2026-03-10T10:00:00.000Z","cwd":"` + workspace + `","isSidechain":false,"message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"2026-03-10T10:00:05.000Z","cwd":"` + workspace + `","isSidechain":false,"message":{"role":"assistant","model":"claude-opus-4-6","content":"hi","usage":{"input_tokens":10,"output_tokens":5}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session fixture %s: %v", path, err)
	}
}

func dirEmpty(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}
