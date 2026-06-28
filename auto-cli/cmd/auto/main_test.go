package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeExitErr implements the exitCoder interface to exercise exitCodeFor without
// pulling an internal cli.ExitError (subcommand error types are internal).
type fakeExitErr struct {
	code int
	msg  string
}

func (e *fakeExitErr) Error() string { return e.msg }
func (e *fakeExitErr) ExitCode() int { return e.code }

// TestExitCodeForPropagatesSubcommandCode asserts the dispatcher honors a
// subcommand's declared exit code (e.g. `auto reflect gate check`'s 2) instead
// of collapsing every failure to 1, and falls back to 1 otherwise.
func TestExitCodeForPropagatesSubcommandCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"plain error falls back to 1", errors.New("boom"), 1},
		{"exit code 2 propagates", &fakeExitErr{code: 2, msg: ""}, 2},
		{"exit code 1 stays 1", &fakeExitErr{code: 1, msg: "bad input"}, 1},
		{"zero code falls back to 1", &fakeExitErr{code: 0, msg: ""}, 1},
		{"wrapped exit coder propagates", fmt.Errorf("context: %w", &fakeExitErr{code: 3, msg: "x"}), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// runAuto builds a fresh root `auto` command wired to a capture buffer and runs
// it with the given args. Content assertions are reliable only for --help/usage
// output (cobra routes that through the command writer). Executed (non-help)
// doc/etl commands write to the process os.Stdout, not the buffer, so those are
// asserted by error/exit only. See solution.md AC-2 "Capture caveat".
func runAuto(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := newRootCmd(&buf, &buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

// TestAllToolsMounted asserts every tool stem mounts under `auto` and its usage
// line is re-rooted to `auto <stem>` (AC-1). Covers config + ui too (AC-6).
func TestAllToolsMounted(t *testing.T) {
	stems := []string{"artifact", "config", "doc", "env", "etl", "graph", "reflect", "search", "skill", "ui", "watch"}
	for _, s := range stems {
		out, err := runAuto(t, s, "--help")
		if err != nil {
			t.Errorf("auto %s --help: unexpected error: %v", s, err)
		}
		if !strings.Contains(out, "auto "+s) {
			t.Errorf("auto %s --help: usage missing %q; got:\n%s", s, "auto "+s, out)
		}
	}
}

// TestTopLevelUpdateMounted asserts the canonical top-level `auto update` exists.
func TestTopLevelUpdateMounted(t *testing.T) {
	out, err := runAuto(t, "--help")
	if err != nil {
		t.Fatalf("auto --help: %v", err)
	}
	if !strings.Contains(out, "update") {
		t.Errorf("auto --help: missing top-level update command; got:\n%s", out)
	}
}

// TestEtlRunFlagsPreserved guards the high-risk auto-etl init()→NewRootCmd
// refactor: all run flags + the persistent --debug must survive the move into
// the builder, and zen/update must stay mounted (AC-2).
func TestEtlRunFlagsPreserved(t *testing.T) {
	out, err := runAuto(t, "etl", "run", "--help")
	if err != nil {
		t.Fatalf("auto etl run --help: %v", err)
	}
	for _, flag := range []string{"--input", "--output", "--full", "--only", "--repo-path", "--since"} {
		if !strings.Contains(out, flag) {
			t.Errorf("auto etl run --help: missing flag %q; got:\n%s", flag, out)
		}
	}

	rootOut, err := runAuto(t, "etl", "--help")
	if err != nil {
		t.Fatalf("auto etl --help: %v", err)
	}
	if !strings.Contains(rootOut, "--debug") {
		t.Errorf("auto etl --help: missing persistent --debug; got:\n%s", rootOut)
	}

	if _, err := runAuto(t, "etl", "zen", "--help"); err != nil {
		t.Errorf("auto etl zen --help: %v", err)
	}
	if _, err := runAuto(t, "etl", "update", "--help"); err != nil {
		t.Errorf("auto etl update --help: %v", err)
	}
}

// TestDocSubcommandsPreserved guards the high-risk auto-doc inline-main→extracted
// NewRootCmd refactor: all 12 subcommands + the persistent --json must survive.
// Content is checked on --help; executed `tree` invocations are exit-code-only
// (auto-doc writes to the real os.Stdout, not the capture buffer).
func TestDocSubcommandsPreserved(t *testing.T) {
	out, err := runAuto(t, "doc", "--help")
	if err != nil {
		t.Fatalf("auto doc --help: %v", err)
	}
	subs := []string{"init", "tree", "stale", "agents", "fix", "fixed", "graph", "search", "quickstart", "docs", "doctor", "update"}
	for _, sub := range subs {
		if !strings.Contains(out, sub) {
			t.Errorf("auto doc --help: missing subcommand %q; got:\n%s", sub, out)
		}
	}
	if !strings.Contains(out, "--json") {
		t.Errorf("auto doc --help: missing persistent --json; got:\n%s", out)
	}

	if _, err := runAuto(t, "doc", "tree", "--json"); err != nil {
		t.Errorf("auto doc tree --json: %v", err)
	}
	if _, err := runAuto(t, "doc", "tree"); err != nil {
		t.Errorf("auto doc tree: %v", err)
	}
}
