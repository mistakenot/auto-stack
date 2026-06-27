package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-ui/internal/config"
)

// runBackends executes the backends command tree with the given args, returning
// captured stdout, stderr, and the error. HOME must be isolated by the caller.
func runBackends(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newBackendsCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestBackendsListJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := config.BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	seed := config.BackendsConfig{Backends: []config.Backend{
		{URI: "tcp://127.0.0.1:9001", Name: "one", HostID: "host-a"},
	}}
	if err := config.SaveBackends(path, seed); err != nil {
		t.Fatalf("seed SaveBackends: %v", err)
	}

	stdout, stderr, err := runBackends(t, "list")
	if err != nil {
		t.Fatalf("list: %v\nstderr: %s", err, stderr)
	}
	var got config.BackendsConfig
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("list stdout not JSON: %v\nstdout: %s", err, stdout)
	}
	if len(got.Backends) != 1 || got.Backends[0].HostID != "host-a" {
		t.Fatalf("unexpected list output: %+v", got)
	}
}

func TestBackendsAddNoVerifyRegistersWithoutDialing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// An unreachable address: succeeding proves no dial was attempted.
	stdout, stderr, err := runBackends(t, "add", "tcp://127.0.0.1:1", "--no-verify", "--name", "x")
	if err != nil {
		t.Fatalf("add --no-verify: %v\nstderr: %s", err, stderr)
	}
	var added config.Backend
	if err := json.Unmarshal([]byte(stdout), &added); err != nil {
		t.Fatalf("add stdout not JSON: %v\nstdout: %s", err, stdout)
	}
	if added.URI != "tcp://127.0.0.1:1" || added.HostID != "" {
		t.Fatalf("unexpected add output: %+v", added)
	}

	path, _ := config.BackendsPath()
	cfg, err := config.LoadBackends(path)
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if len(cfg.Backends) != 1 || cfg.Backends[0].URI != "tcp://127.0.0.1:1" {
		t.Fatalf("backend not persisted: %+v", cfg.Backends)
	}
}

func TestBackendsAddRejectsInvalidURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stdout, stderr, err := runBackends(t, "add", "http://example.com", "--no-verify")
	if err == nil {
		t.Fatalf("expected error for invalid uri, got nil (stdout: %s)", stdout)
	}
	if stderr == "" {
		t.Fatal("expected validation diagnostics on stderr")
	}
}

func TestBackendsAddRejectsDuplicateURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, _ := config.BackendsPath()
	if err := config.SaveBackends(path, config.BackendsConfig{Backends: []config.Backend{
		{URI: "tcp://127.0.0.1:9001"},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, stderr, err := runBackends(t, "add", "tcp://127.0.0.1:9001", "--no-verify")
	if err == nil {
		t.Fatal("expected duplicate rejection, got nil")
	}
	if stderr == "" {
		t.Fatal("expected duplicate diagnostics on stderr")
	}
}

func TestBackendsRemove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, _ := config.BackendsPath()
	if err := config.SaveBackends(path, config.BackendsConfig{Backends: []config.Backend{
		{URI: "tcp://127.0.0.1:9001"},
		{URI: "tcp://127.0.0.1:9002"},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, stderr, err := runBackends(t, "remove", "tcp://127.0.0.1:9001"); err != nil {
		t.Fatalf("remove: %v\nstderr: %s", err, stderr)
	}
	cfg, _ := config.LoadBackends(path)
	if len(cfg.Backends) != 1 || cfg.Backends[0].URI != "tcp://127.0.0.1:9002" {
		t.Fatalf("remove did not delete entry: %+v", cfg.Backends)
	}

	// Removing a non-existent uri errors with diagnostics.
	_, stderr, err := runBackends(t, "remove", "tcp://127.0.0.1:9999")
	if err == nil {
		t.Fatal("expected error removing unknown uri, got nil")
	}
	if stderr == "" {
		t.Fatal("expected not-found diagnostics on stderr")
	}
}

func TestBackendsAddVerifyConnectsAndStoresHostID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sockPath := filepath.Join(t.TempDir(), "fake-aw.sock")
	uri := "unix://" + sockPath
	stop := startFakeBackend(t, uri, "fake-host", 3)
	defer stop()

	stdout, stderr, err := runBackends(t, "add", uri, "--name", "fake")
	if err != nil {
		t.Fatalf("add verify: %v\nstderr: %s", err, stderr)
	}
	var added config.Backend
	if err := json.Unmarshal([]byte(stdout), &added); err != nil {
		t.Fatalf("add stdout not JSON: %v\nstdout: %s", err, stdout)
	}
	if added.HostID != "fake-host" {
		t.Fatalf("expected learned hostId fake-host, got %q", added.HostID)
	}
	if !bytes.Contains([]byte(stderr), []byte("connected to fake-host")) {
		t.Fatalf("expected connect confirmation on stderr, got: %s", stderr)
	}

	// Persisted entry carries the learned hostId.
	path, _ := config.BackendsPath()
	cfg, _ := config.LoadBackends(path)
	if len(cfg.Backends) != 1 || cfg.Backends[0].HostID != "fake-host" {
		t.Fatalf("hostId not persisted: %+v", cfg.Backends)
	}
}

func TestBackendsAddVerifyUnreachableFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// No backend listening; verify should fail and nothing should be persisted.
	_, stderr, err := runBackends(t, "add", "unix:///tmp/does-not-exist-42.sock")
	if err == nil {
		t.Fatal("expected verify failure for unreachable backend, got nil")
	}
	if stderr == "" {
		t.Fatal("expected diagnostics on stderr")
	}
	path, _ := config.BackendsPath()
	cfg, _ := config.LoadBackends(path)
	if len(cfg.Backends) != 0 {
		t.Fatalf("unreachable backend should not be persisted: %+v", cfg.Backends)
	}
}

// startFakeBackend stands up an in-process autowatch-like RPC server on uri that
// answers daemon.status (returning hostID) and project.list (returning
// projectCount empty entries). It returns a stop function that tears it down.
func startFakeBackend(t *testing.T, uri, hostID string, projectCount int) func() {
	t.Helper()
	lis, err := transport.Listen(uri)
	if err != nil {
		t.Fatalf("transport.Listen(%q): %v", uri, err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			peer := rpc.NewPeer(conn)
			peer.Register("daemon.status", func(_ context.Context, _ json.RawMessage) (any, error) {
				return map[string]any{"hostId": hostID, "version": "test"}, nil
			})
			peer.Register("project.list", func(_ context.Context, _ json.RawMessage) (any, error) {
				entries := make([]map[string]any, projectCount)
				for i := range entries {
					entries[i] = map[string]any{"id": "p", "host": hostID}
				}
				return entries, nil
			})
			go func() { _ = peer.Serve(ctx) }()
		}
	}()

	return func() {
		cancel()
		_ = lis.Close()
	}
}
