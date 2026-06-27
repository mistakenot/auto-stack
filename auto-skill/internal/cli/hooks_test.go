package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// hookEnv is a hermetic harness for driving the real repo Makefile hook targets
// with a stubbed `auto` binary on PATH, so the tests assert hook WIRING (which
// auto subcommands run, with which flags, and the resulting exit code) without
// invoking the real sync/lint/update engine.
type hookEnv struct {
	t        *testing.T
	project  string // temp project dir; make runs with -C here so $(CURDIR) == this
	makefile string // absolute path to the repo Makefile
	binDir   string // dir holding the stub `auto`; prefixed onto PATH
	logFile  string // stub appends its argv here, one invocation per line
}

// newHookEnv builds the temp project, the stub `auto`, and resolves the Makefile.
// autoExit is the exit code the stub returns for `auto skill ...` invocations.
func newHookEnv(t *testing.T, autoExit int) *hookEnv {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}

	project := t.TempDir()
	binDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "auto-argv.log")

	// Resolve the repo Makefile relative to this test file's package dir
	// (<repo>/auto-skill/internal/cli → repoRoot = ../../..).
	makefile, err := filepath.Abs(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("resolve Makefile: %v", err)
	}
	if _, err := os.Stat(makefile); err != nil {
		t.Fatalf("repo Makefile not found at %s: %v", makefile, err)
	}

	// Stub `auto`: append argv to the log, then exit with the scripted code.
	stub := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$*\" >> \"" + logFile + "\"\n" +
		"exit " + strconv.Itoa(autoExit) + "\n"
	stubPath := filepath.Join(binDir, "auto")
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub auto: %v", err)
	}

	return &hookEnv{t: t, project: project, makefile: makefile, binDir: binDir, logFile: logFile}
}

// withLock creates the native lock.json guard file in the temp project.
func (h *hookEnv) withLock() *hookEnv {
	h.t.Helper()
	lockPath := filepath.Join(h.project, ".auto", "skills", "lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("{\"version\":1,\"skills\":{}}\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return h
}

// run invokes `make -C <project> -f <repoMakefile> <target> [extraArgs...]` with
// the stub bin dir prepended to PATH. Returns the combined output and exit code.
func (h *hookEnv) run(target string, extraArgs ...string) (string, int) {
	h.t.Helper()
	args := append([]string{"-C", h.project, "-f", h.makefile, target}, extraArgs...)
	cmd := exec.Command("make", args...)
	cmd.Env = append(os.Environ(), "PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			h.t.Fatalf("run make %s: %v\n%s", target, err, out)
		}
	}
	return string(out), code
}

// log returns the recorded stub argv lines (auto invocations).
func (h *hookEnv) log() string {
	data, err := os.ReadFile(h.logFile)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		h.t.Fatalf("read argv log: %v", err)
	}
	return string(data)
}

// AC-8: pre-commit gate runs sync --check + lint (check-only) and fails on stale.
func TestHookPreCommitGateFailsOnStale(t *testing.T) {
	h := newHookEnv(t, 1).withLock() // stub exits non-zero == stale

	out, code := h.run("skills-check")
	if code == 0 {
		t.Fatalf("expected non-zero exit when stub auto fails (stale), got 0\n%s", out)
	}

	log := h.log()
	if !strings.Contains(log, "skill sync --check") {
		t.Errorf("expected 'skill sync --check' in argv log, got:\n%s", log)
	}
	// lint should NOT run because sync --check failed first (|| exit 1 short-circuit).
	// Either way, the gate must never mutate the tree: no git add, no add/render.
	if strings.Contains(log, "git add") || strings.Contains(log, "skill add") || strings.Contains(log, "sync --locked") {
		t.Errorf("check-only gate must not mutate or render; argv log:\n%s", log)
	}
}

// AC-8: with a clean (exit 0) auto, the gate passes and runs both check + lint.
func TestHookPreCommitGatePassesWhenClean(t *testing.T) {
	h := newHookEnv(t, 0).withLock()

	out, code := h.run("skills-check")
	if code != 0 {
		t.Fatalf("expected exit 0 when stub auto succeeds, got %d\n%s", code, out)
	}

	log := h.log()
	if !strings.Contains(log, "skill sync --check") {
		t.Errorf("expected 'skill sync --check' in argv log, got:\n%s", log)
	}
	if !strings.Contains(log, "skill lint") {
		t.Errorf("expected 'skill lint' in argv log, got:\n%s", log)
	}
}

// AC-11: with no lock.json, every hook target no-ops cleanly and never calls auto.
func TestHookNoOpWhenLockAbsent(t *testing.T) {
	for _, target := range []string{"skills-check", "skills-sync-locked", "skills-update-check"} {
		h := newHookEnv(t, 1) // no withLock(); stub would fail IF called
		out, code := h.run(target)
		if code != 0 {
			t.Errorf("%s: expected exit 0 with no lock.json, got %d\n%s", target, code, out)
		}
		if log := h.log(); log != "" {
			t.Errorf("%s: expected auto never invoked, argv log:\n%s", target, log)
		}
	}
}

// AC-9: post-merge / post-checkout re-materialize via sync --locked only, and are
// non-blocking even when the stub auto exits non-zero.
func TestHookPostMergeCheckoutLockedNonBlocking(t *testing.T) {
	for _, target := range []string{"post-merge", "post-checkout"} {
		h := newHookEnv(t, 1).withLock() // stub fails, but target must still pass
		out, code := h.run(target)
		if code != 0 {
			t.Errorf("%s: expected exit 0 (non-blocking), got %d\n%s", target, code, out)
		}
		log := h.log()
		if !strings.Contains(log, "skill sync --locked") {
			t.Errorf("%s: expected 'skill sync --locked' in argv log, got:\n%s", target, log)
		}
		if strings.Contains(log, "--check") {
			t.Errorf("%s: re-materialize must not run --check; argv log:\n%s", target, log)
		}
	}
}

// AC-10: pre-push is opt-in. Default → auto never called, exit 0.
func TestHookPrePushOptInDefaultSkips(t *testing.T) {
	h := newHookEnv(t, 1).withLock() // lock present, but flag off

	out, code := h.run("pre-push")
	if code != 0 {
		t.Fatalf("expected exit 0 by default, got %d\n%s", code, out)
	}
	if log := h.log(); log != "" {
		t.Errorf("expected auto never invoked by default, argv log:\n%s", log)
	}
}

// AC-10: enabling SKILLS_UPDATE_CHECK=1 runs update --check, warn-only (exit 0
// even when the stub auto fails).
func TestHookPrePushOptInEnabledWarnOnly(t *testing.T) {
	h := newHookEnv(t, 1).withLock()

	out, code := h.run("pre-push", "SKILLS_UPDATE_CHECK=1")
	if code != 0 {
		t.Fatalf("expected exit 0 (warn-only), got %d\n%s", code, out)
	}
	log := h.log()
	if !strings.Contains(log, "skill update --check") {
		t.Errorf("expected 'skill update --check' in argv log, got:\n%s", log)
	}
}
