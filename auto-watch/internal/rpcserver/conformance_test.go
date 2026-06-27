package rpcserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc/conformance"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-shared/version"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
	"github.com/mistakenot/auto-watch/internal/rpcserver"
)

// ---------------------------------------------------------------------------
// TestMain — build the autowatch binary once for the binary fixture
// ---------------------------------------------------------------------------

var testBinaryPath string

func TestMain(m *testing.M) {
	moduleRoot := findModuleRoot()

	tmpDir, err := os.MkdirTemp("", "autowatch-conformance-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	binPath := filepath.Join(tmpDir, "autowatch")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/autowatch")
	cmd.Dir = moduleRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build ./cmd/autowatch: %v\n%s\n", err, out)
		os.Exit(1)
	}
	testBinaryPath = binPath

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find go.mod")
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// In-process fixture
// ---------------------------------------------------------------------------

type inProcessFixture struct {
	handlers   *rpcmethods.Handlers
	client     *conformance.PeerClient
	ln         transport.Listener
	cancel     context.CancelFunc
	done       chan struct{}
	projectDir string
}

func seedDocsTree(t testing.TB, root string) {
	t.Helper()
	docsDir := filepath.Join(root, "docs")
	os.MkdirAll(docsDir, 0o755)
	os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Test\n"), 0o644)
	htmlContent := `<!doctype html>
<html><head>
<script type="application/json" id="pd-meta">
{"status":"planning","created":"2026-01-01"}
</script>
</head><body>
<pd-doc status="pending"></pd-doc>
</body></html>`
	os.MkdirAll(filepath.Join(docsDir, "tasks", "001-test"), 0o755)
	os.WriteFile(filepath.Join(docsDir, "tasks", "001-test", "plan.html"), []byte(htmlContent), 0o644)
}

func inProcessFactory(t testing.TB) conformance.Fixture {
	hub := bus.NewHub()
	hostID := sharedconfig.HostIDQuietly()
	projectRoot, err := os.MkdirTemp("", "autowatch-inproc-project-*")
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	seedDocsTree(t, projectRoot)
	regProvider := func() sharedconfig.ProjectsConfig {
		return sharedconfig.ProjectsConfig{
			Projects: []sharedconfig.ProjectRef{
				{ID: "conformance-project", Name: "Conformance", Path: projectRoot},
			},
		}
	}
	handlers := rpcmethods.New(hostID, version.Version, time.Now(), hub, false, regProvider)

	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := rpcserver.New(ln, handlers, hub, false)
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ctx)
		close(done)
	}()

	// Give the accept loop a moment to start.
	time.Sleep(20 * time.Millisecond)

	conn, err := transport.Dial(ctx, "tcp://"+ln.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	go client.Peer().Serve(ctx)

	return &inProcessFixture{
		handlers:   handlers,
		client:     client,
		ln:         ln,
		cancel:     cancel,
		done:       done,
		projectDir: projectRoot,
	}
}

func (f *inProcessFixture) Client() conformance.RPCClient { return f.client }
func (f *inProcessFixture) Obs() conformance.Observations { return f.handlers }
func (f *inProcessFixture) Close() error {
	f.cancel()
	<-f.done
	os.RemoveAll(f.projectDir)
	return nil
}

// ---------------------------------------------------------------------------
// Binary fixture
// ---------------------------------------------------------------------------

type binaryFixture struct {
	client *conformance.PeerClient
	cmd    *exec.Cmd
	cancel context.CancelFunc
	tmpDir string
}

func binaryFactory(t testing.TB) conformance.Fixture {
	tmpDir, err := os.MkdirTemp("", "autowatch-binary-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	home := filepath.Join(tmpDir, "home")
	autoDir := filepath.Join(home, ".auto")
	watchDir := filepath.Join(autoDir, "watch")
	runsDir := filepath.Join(watchDir, "runs")
	for _, d := range []string{home, autoDir, watchDir, runsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Seed host.json
	hostJSON := fmt.Sprintf(`{"hostId":"test-binary-%d"}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(autoDir, "host.json"), []byte(hostJSON), 0o644); err != nil {
		t.Fatalf("write host.json: %v", err)
	}

	// Stub executables
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	for name, script := range map[string]string{
		"tmux":   "#!/bin/sh\necho 'tmux 3.4'\n",
		"claude": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
			t.Fatalf("write stub %s: %v", name, err)
		}
	}

	// Create a minimal git repo for doctor's git check
	repoDir := filepath.Join(tmpDir, "repo")
	os.MkdirAll(repoDir, 0o755)
	for _, gitArgs := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		gitCmd := exec.Command("git", gitArgs...)
		gitCmd.Dir = repoDir
		if out, err := gitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", gitArgs, err, out)
		}
	}
	// Seed docs tree in the repo for doc.* methods
	seedDocsTree(t, repoDir)

	// Seed projects.json with the repo registered (after repoDir exists)
	projectsJSON := fmt.Sprintf(`{"projects":[{"id":"binary-project","name":"Binary","path":%q}]}`, repoDir)
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(projectsJSON), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}

	// Commit something so we have a HEAD
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test"), 0o644)
	for _, gitArgs := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		gitCmd := exec.Command("git", gitArgs...)
		gitCmd.Dir = repoDir
		if out, err := gitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", gitArgs, err, out)
		}
	}

	readyPath := filepath.Join(tmpDir, "ready.json")

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, testBinaryPath,
		"start",
		"--rpc-addr", "tcp://127.0.0.1:0",
		"--hook-addr", "127.0.0.1:0",
		"--ready-file", readyPath,
	)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	// Capture stderr for debugging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start binary: %v", err)
	}

	// Wait for ready-file
	deadline := time.After(15 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		select {
		case <-deadline:
			cancel()
			cmd.Wait()
			t.Fatalf("ready-file not written within timeout; stderr: %s", stderr.String())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Read ready-file
	data, err := os.ReadFile(readyPath)
	if err != nil {
		cancel()
		cmd.Wait()
		t.Fatalf("read ready-file: %v", err)
	}
	var ready map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(data), &ready); err != nil {
		cancel()
		cmd.Wait()
		t.Fatalf("parse ready-file: %v; data: %s", err, data)
	}

	addr := ready["addr"]
	if addr == "" {
		cancel()
		cmd.Wait()
		t.Fatal("empty addr in ready-file")
	}

	conn, err := transport.Dial(ctx, "tcp://"+addr)
	if err != nil {
		cancel()
		cmd.Wait()
		t.Fatalf("dial binary: %v; stderr: %s", err, stderr.String())
	}
	client := conformance.NewPeerClient(conn)
	go client.Peer().Serve(ctx)

	return &binaryFixture{
		client: client,
		cmd:    cmd,
		cancel: cancel,
		tmpDir: tmpDir,
	}
}

func (f *binaryFixture) Client() conformance.RPCClient { return f.client }
func (f *binaryFixture) Obs() conformance.Observations {
	return &noopObs{}
}
func (f *binaryFixture) Close() error {
	f.cancel()
	_ = f.cmd.Wait()
	os.RemoveAll(f.tmpDir)
	return nil
}

type noopObs struct{}

func (o *noopObs) DispatchCount(method string) int { return -1 }

// ---------------------------------------------------------------------------
// Shared scenario: daemon.status
// ---------------------------------------------------------------------------

type statusScenario struct{}

func (s *statusScenario) Name() string { return "daemon.status" }

func (s *statusScenario) Run(t testing.TB, f conformance.Fixture) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := f.Client().Call(ctx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("call daemon.status: %v", err)
	}

	var status rpcmethods.StatusResult
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Stable invariants
	if status.HostID == "" {
		t.Fatal("empty hostId")
	}
	if status.Version == "" {
		t.Fatal("empty version")
	}

	// Dynamic fields — present and well-formed, not compared for equality
	if status.PID <= 0 {
		t.Fatalf("invalid pid: %d", status.PID)
	}
	if status.UptimeSeconds < 0 {
		t.Fatalf("negative uptime: %d", status.UptimeSeconds)
	}
	if status.StartedAt == "" {
		t.Fatal("empty startedAt")
	}
	if _, err := time.Parse(time.RFC3339, status.StartedAt); err != nil {
		t.Fatalf("startedAt not RFC3339: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Shared scenario: doc + project methods
// ---------------------------------------------------------------------------

type docProjectScenario struct{}

func (s *docProjectScenario) Name() string { return "doc-project" }

func (s *docProjectScenario) Run(t testing.TB, f conformance.Fixture) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// project.list: should return at least one project with a host stamp
	projResult, err := f.Client().Call(ctx, "project.list", nil)
	if err != nil {
		t.Fatalf("call project.list: %v", err)
	}

	var projects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
		Host string `json:"host"`
	}
	if err := json.Unmarshal(projResult, &projects); err != nil {
		t.Fatalf("unmarshal project.list: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("project.list returned empty")
	}
	if projects[0].Host == "" {
		t.Fatal("project.list entry missing host stamp")
	}
	projectID := projects[0].ID

	// doc.list: should return entries for the seeded docs/ tree
	docListResult, err := f.Client().Call(ctx, "doc.list", map[string]string{"project": projectID})
	if err != nil {
		t.Fatalf("call doc.list: %v", err)
	}

	var docs []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(docListResult, &docs); err != nil {
		t.Fatalf("unmarshal doc.list: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("doc.list returned empty for seeded project")
	}

	// Find a markdown doc and get it
	for _, d := range docs {
		if d.Type == "markdown" {
			getResult, err := f.Client().Call(ctx, "doc.get", map[string]string{
				"project": projectID,
				"path":    d.Path,
			})
			if err != nil {
				t.Fatalf("call doc.get(%s): %v", d.Path, err)
			}
			var got map[string]string
			json.Unmarshal(getResult, &got)
			if got["path"] != d.Path {
				t.Errorf("doc.get path = %q, want %q", got["path"], d.Path)
			}
			if got["markdown"] == "" {
				t.Error("doc.get returned empty markdown")
			}
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Test functions
// ---------------------------------------------------------------------------

func TestConformance(t *testing.T) {
	if testBinaryPath == "" {
		t.Skip("binary not built (TestMain failed)")
	}

	conformance.RunAcrossFixtures(t, &statusScenario{}, inProcessFactory, binaryFactory)
	conformance.RunAcrossFixtures(t, &docProjectScenario{}, inProcessFactory, binaryFactory)
}

func TestCrossFixtureStableInvariants(t *testing.T) {
	if testBinaryPath == "" {
		t.Skip("binary not built")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	inProc := inProcessFactory(t)
	defer inProc.Close()

	bin := binaryFactory(t)
	defer bin.Close()

	result1, err := inProc.Client().Call(ctx, "daemon.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := bin.Client().Call(ctx, "daemon.status", nil)
	if err != nil {
		t.Fatal(err)
	}

	var s1, s2 rpcmethods.StatusResult
	json.Unmarshal(result1, &s1)
	json.Unmarshal(result2, &s2)

	// Version must be equal (both use version.Version)
	if s1.Version != s2.Version {
		t.Fatalf("version mismatch: in-process=%q, binary=%q", s1.Version, s2.Version)
	}

	// PID must differ (different processes)
	if s1.PID == s2.PID {
		t.Fatal("PID should differ between in-process and binary fixtures")
	}
}
