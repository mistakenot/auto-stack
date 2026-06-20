package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/rpc/conformance"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/cli"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
	"github.com/mistakenot/auto-watch/internal/testutil"
)

// setupDaemonEnv creates a test environment with all doctor prerequisites
// satisfied (tmux stub, claude stub, host.json, projects.json via init).
func setupDaemonEnv(t *testing.T) (*testutil.Env, string) {
	t.Helper()
	env := testutil.NewEnv(t)

	// Stub tmux (doctor requires >= 3.0).
	env.WriteExecutable("tmux", "#!/bin/sh\necho 'tmux 3.4'\n")

	// Stub claude (doctor requires it on PATH).
	env.WriteExecutable("claude", "#!/bin/sh\nexit 0\n")

	// Create a repo and init the project to satisfy settings + host_config + project_config checks.
	repoRoot := env.NewRepo("test")
	env.AddRemote(repoRoot, "git@github.com:test/test.git")

	_, stderr, code := env.RunCLI(repoRoot, "init", "--project-id", "test")
	if code != 0 {
		t.Fatalf("init failed (code %d): %s", code, stderr)
	}

	return env, repoRoot
}

func TestStartReadyFileAndPIDMetadata(t *testing.T) {
	_, repoRoot := setupDaemonEnv(t)

	readyPath := filepath.Join(t.TempDir(), "ready.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		var out, errOut bytes.Buffer
		application := app.New(&out, &errOut)
		application.CWD = repoRoot
		rootCmd := cli.NewRootCmd(application)
		rootCmd.SetArgs([]string{
			"start",
			"--rpc-addr", "tcp://127.0.0.1:0",
			"--hook-addr", "127.0.0.1:0",
			"--ready-file", readyPath,
		})
		errCh <- rootCmd.ExecuteContext(ctx)
	}()

	// Wait for the ready-file to appear.
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ready-file not written within timeout")
		case err := <-errCh:
			t.Fatalf("start exited early: %v", err)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Read and verify ready-file contents (AC-7).
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	var ready map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(data), &ready); err != nil {
		t.Fatalf("parse ready-file: %v, data: %s", err, data)
	}
	if ready["addr"] == "" {
		t.Fatal("missing addr in ready-file")
	}
	if ready["hookAddr"] == "" {
		t.Fatal("missing hookAddr in ready-file")
	}

	// Verify daemon.pid.json includes rpcAddr and hookAddr (AC-7).
	home := os.Getenv("HOME")
	pidPath := filepath.Join(home, ".auto", "watch", "daemon.pid.json")
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read daemon.pid.json: %v", err)
	}
	var pidMeta map[string]any
	if err := json.Unmarshal(pidData, &pidMeta); err != nil {
		t.Fatalf("parse daemon.pid.json: %v", err)
	}
	if pidMeta["rpcAddr"] == nil || pidMeta["rpcAddr"].(string) == "" {
		t.Fatal("missing rpcAddr in daemon.pid.json")
	}
	if pidMeta["hookAddr"] == nil || pidMeta["hookAddr"].(string) == "" {
		t.Fatal("missing hookAddr in daemon.pid.json")
	}

	// Verify daemon.status via RPC (AC-2 via ops wiring).
	conn, dialErr := transport.Dial(ctx, "tcp://"+ready["addr"])
	if dialErr != nil {
		t.Fatalf("dial RPC: %v", dialErr)
	}
	client := conformance.NewPeerClient(conn)
	go func() { _ = client.Peer().Serve(ctx) }()

	result, callErr := client.Call(ctx, "daemon.status", nil)
	if callErr != nil {
		t.Fatalf("daemon.status call: %v", callErr)
	}
	var status rpcmethods.StatusResult
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.HostID == "" {
		t.Fatal("empty hostId in daemon.status response")
	}
	if status.Version == "" {
		t.Fatal("empty version in daemon.status response")
	}
	if status.PID == 0 {
		t.Fatal("zero pid in daemon.status response")
	}

	// Cancel and wait for clean exit.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			var exitErr *cli.ExitError
			if !(errors.As(err, &exitErr) && (exitErr.Err == nil || exitErr.Err.Error() == "")) {
				t.Fatalf("start returned error: %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start didn't exit after cancel")
	}
}

func TestStartUnbindableRPCAddr(t *testing.T) {
	_, repoRoot := setupDaemonEnv(t)

	// 192.0.2.1 is TEST-NET-1 (RFC 5737), never routable locally.
	var out, errOut bytes.Buffer
	application := app.New(&out, &errOut)
	application.CWD = repoRoot
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs([]string{"start", "--rpc-addr", "tcp://192.0.2.1:1"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected non-zero exit for unbindable addr")
	}
	combined := errOut.String() + err.Error()
	if !strings.Contains(combined, "bind RPC listener") {
		t.Fatalf("expected bind error, got err=%v stderr=%s", err, errOut.String())
	}
}
