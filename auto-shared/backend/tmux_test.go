package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// requireTmux skips when tmux is unavailable and points tmux at a private
// server under t.TempDir() so these tests never touch the real tmux server.
// $TMUX must be cleared too: inside a tmux pane it names the live server's
// socket and takes precedence over TMUX_TMPDIR, so kill-server would take down
// the session running the tests.
func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
}

// startRun writes a launch script running cmd and starts it as a tmux run.
func startRun(t *testing.T, tm *Tmux, cmd string) (Binding, string) {
	t.Helper()
	runDir := t.TempDir()
	script, err := WriteLaunchScript(&LaunchSpec{
		RunDir:     runDir,
		WorkDir:    runDir,
		Command:    cmd,
		OutputPath: filepath.Join(runDir, "output.log"),
		ExitPath:   filepath.Join(runDir, ExitCodeFile),
	})
	if err != nil {
		t.Fatalf("WriteLaunchScript: %v", err)
	}
	name := "autowatch-test-" + sanitize(t.Name())
	binding, err := tm.Start(context.Background(), &StartSpec{
		Name:         name,
		WorkDir:      runDir,
		LaunchScript: script,
		RunDir:       runDir,
		Owner:        "backend-test",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = tm.Stop(context.Background(), binding) })
	return binding, runDir
}

func waitStatus(t *testing.T, tm *Tmux, binding Binding, runDir string, want StatusState) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last Status
	for time.Now().Before(deadline) {
		st, err := tm.Status(context.Background(), binding, runDir)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		last = st
		if st.State == want {
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Status did not reach %d within deadline, last = %+v", want, last)
	return last
}

func TestTmuxStartThenExit(t *testing.T) {
	requireTmux(t)
	for _, code := range []int{0, 3} {
		t.Run(sanitizeInt(code), func(t *testing.T) {
			tm := &Tmux{}
			// The inner command is always a child process in real use
			// (runner composes `bash -lc "…"` / `claude …`), so its exit
			// reaches the wrapper's `$?`. A bare `exit N` would terminate the
			// wrapper before it writes the exit-code file, so run it in a child
			// shell, as the daemon does.
			binding, runDir := startRun(t, tm, "sh -c 'exit "+sanitizeInt(code)+"'")
			if binding.Manager != "tmux" || binding.Target == "" {
				t.Fatalf("unexpected binding %+v", binding)
			}
			st := waitStatus(t, tm, binding, runDir, StatusExited)
			if st.ExitCode == nil || *st.ExitCode != code {
				t.Fatalf("exit code = %v, want %d", st.ExitCode, code)
			}
		})
	}
}

func TestTmuxStatusLostAfterKill(t *testing.T) {
	requireTmux(t)
	tm := &Tmux{}
	binding, runDir := startRun(t, tm, "sleep 30")
	// Running before the kill.
	st, err := tm.Status(context.Background(), binding, runDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != StatusRunning {
		t.Fatalf("state = %d, want running", st.State)
	}
	if err := tm.Stop(context.Background(), binding); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// No exit file was written (we killed it), and the session is gone → lost.
	st, err = tm.Status(context.Background(), binding, runDir)
	if err != nil {
		t.Fatalf("Status after kill: %v", err)
	}
	if st.State != StatusLost {
		t.Fatalf("state = %d, want lost", st.State)
	}
}

func TestTmuxStatusLostNoServer(t *testing.T) {
	requireTmux(t)
	tm := &Tmux{}
	binding, runDir := startRun(t, tm, "sleep 30")
	// Tear the whole server down: has-session then says "no server running".
	_ = exec.Command("tmux", "kill-server").Run()
	st, err := tm.Status(context.Background(), binding, runDir)
	if err != nil {
		t.Fatalf("Status with no server: %v", err)
	}
	if st.State != StatusLost {
		t.Fatalf("state = %d, want lost", st.State)
	}
}

func TestTmuxStopAbsentIsNil(t *testing.T) {
	requireTmux(t)
	tm := &Tmux{}
	if err := tm.Stop(context.Background(), Binding{Manager: "tmux", Target: "autowatch-test-never-created"}); err != nil {
		t.Fatalf("Stop on absent session = %v, want nil", err)
	}
}

// --- fake tmux on PATH (no real tmux needed) ---

// writeFakeTmux installs a fake `tmux` earlier on PATH. The script's behaviour
// per subcommand is baked in from the given exit codes.
func writeFakeTmux(t *testing.T, startRC, hasRC, killRC int, sleep bool) {
	t.Helper()
	dir := t.TempDir()
	sleepLine := ""
	if sleep {
		sleepLine = "  sleep 2\n"
	}
	script := "#!/usr/bin/env bash\n" +
		"case \"$1\" in\n" +
		"  new-session)\n" + sleepLine + "    exit " + sanitizeInt(startRC) + " ;;\n" +
		"  has-session)\n" + sleepLine +
		"    if [ " + sanitizeInt(hasRC) + " -ne 0 ]; then echo \"can't find session\" >&2; fi\n" +
		"    exit " + sanitizeInt(hasRC) + " ;;\n" +
		"  kill-session)\n" +
		"    exit " + sanitizeInt(killRC) + " ;;\n" +
		"  *) exit 0 ;;\n" +
		"esac\n"
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTmuxStartSetOptionFailsConfirmed(t *testing.T) {
	// new-session invocation fails, and has-session reports absent: no session
	// was left behind, so Start returns a confirmed error (empty binding).
	writeFakeTmux(t, 1 /*start*/, 1 /*has: absent*/, 0, false)
	tm := &Tmux{}
	runDir := t.TempDir()
	binding, err := tm.Start(context.Background(), &StartSpec{Name: "s", WorkDir: runDir, LaunchScript: "x", RunDir: runDir})
	if err == nil {
		t.Fatal("expected a confirmed error")
	}
	if errors.Is(err, ErrStartUncertain) {
		t.Fatalf("expected a confirmed error, got uncertain: %v", err)
	}
	if binding.Target != "" {
		t.Fatalf("expected empty binding on confirmed failure, got %+v", binding)
	}
}

func TestTmuxStartUncertainWhenKillFails(t *testing.T) {
	// new-session fails but has-session reports the session EXISTS, and the
	// follow-up kill also fails: Start returns the Binding with ErrStartUncertain.
	writeFakeTmux(t, 1 /*start*/, 0 /*has: exists*/, 1 /*kill fails*/, false)
	tm := &Tmux{}
	runDir := t.TempDir()
	binding, err := tm.Start(context.Background(), &StartSpec{Name: "sess", WorkDir: runDir, LaunchScript: "x", RunDir: runDir})
	if !errors.Is(err, ErrStartUncertain) {
		t.Fatalf("expected ErrStartUncertain, got %v", err)
	}
	if binding.Target != "sess" || binding.Manager != "tmux" {
		t.Fatalf("expected binding carried back, got %+v", binding)
	}
}

func TestTmuxTimeoutProducesError(t *testing.T) {
	// A fake tmux whose has-session hangs past the per-call timeout turns into a
	// Status error, not an indefinite hang.
	writeFakeTmux(t, 0, 0, 0, true /*sleep*/)
	tm := &Tmux{Timeout: 100 * time.Millisecond}
	runDir := t.TempDir() // no exit file → Status asks has-session
	_, err := tm.Status(context.Background(), Binding{Manager: "tmux", Target: "sess"}, runDir)
	if err == nil {
		t.Fatal("expected a timeout error from a wedged tmux")
	}
}

func sanitize(s string) string {
	return strings.NewReplacer("/", "-", " ", "-").Replace(s)
}

func sanitizeInt(n int) string { return strconv.Itoa(n) }
