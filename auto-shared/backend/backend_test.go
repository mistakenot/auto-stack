package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadExit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		write      bool
		contents   string
		wantCode   *int
		wantExited bool
	}{
		{name: "absent file", write: false, wantExited: false},
		{name: "code zero", write: true, contents: "0\n", wantCode: new(0), wantExited: true},
		{name: "code nonzero", write: true, contents: "137\n", wantCode: new(137), wantExited: true},
		{name: "empty is exited unknown", write: true, contents: "", wantExited: true},
		{name: "whitespace is exited unknown", write: true, contents: "  \n", wantExited: true},
		{name: "malformed is exited unknown", write: true, contents: "killed", wantExited: true},
		{name: "nul byte is exited unknown", write: true, contents: "\x00", wantExited: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.write {
				if err := os.WriteFile(filepath.Join(dir, ExitCodeFile), []byte(tc.contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			code, exited, err := ReadExit(dir)
			if err != nil {
				t.Fatalf("ReadExit error: %v", err)
			}
			if exited != tc.wantExited {
				t.Fatalf("exited = %v, want %v", exited, tc.wantExited)
			}
			switch {
			case tc.wantCode == nil && code != nil:
				t.Fatalf("code = %d, want nil", *code)
			case tc.wantCode != nil && code == nil:
				t.Fatalf("code = nil, want %d", *tc.wantCode)
			case tc.wantCode != nil && *code != *tc.wantCode:
				t.Fatalf("code = %d, want %d", *code, *tc.wantCode)
			}
		})
	}
}

func TestReadExitEmptyRunDir(t *testing.T) {
	code, exited, err := ReadExit("")
	if err != nil || exited || code != nil {
		t.Fatalf("ReadExit(\"\") = (%v, %v, %v), want (nil, false, nil)", code, exited, err)
	}
}

func TestWriteLaunchScriptAtomicExit(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteLaunchScript(&LaunchSpec{
		RunDir:     dir,
		WorkDir:    "/tmp",
		Command:    `bash -lc "echo hi"`,
		OutputPath: filepath.Join(dir, "output.log"),
		ExitPath:   filepath.Join(dir, ExitCodeFile),
	})
	if err != nil {
		t.Fatalf("WriteLaunchScript: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"set -u",
		`cd "$WORK_DIR" || exit 97`,
		`tee -a "$OUTPUT_FILE"`,
		`> "$EXIT_FILE.tmp" && mv "$EXIT_FILE.tmp" "$EXIT_FILE"`,
		`bash -lc "echo hi"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("launch script missing %q, got:\n%s", want, content)
		}
	}
}

func TestWriteLaunchScriptEmptyCommand(t *testing.T) {
	if _, err := WriteLaunchScript(&LaunchSpec{RunDir: t.TempDir()}); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestRegistry(t *testing.T) {
	tmux := &Tmux{}
	reg := NewRegistry(tmux)
	if got, ok := reg.Get(""); !ok || got != tmux {
		t.Fatalf("Get(\"\") should default to tmux, got ok=%v", ok)
	}
	if got, ok := reg.Get("tmux"); !ok || got != tmux {
		t.Fatalf("Get(\"tmux\") failed, ok=%v", ok)
	}
	if _, ok := reg.Get("herdr"); ok {
		t.Fatal("Get(\"herdr\") should not be found")
	}
	if !reg.Has("") || !reg.Has("tmux") || reg.Has("nope") {
		t.Fatal("Has mismatch")
	}
	names := reg.Names()
	if len(names) != 1 || names[0] != "tmux" {
		t.Fatalf("Names = %v, want [tmux]", names)
	}
}

// TestSessionAbsent moves the classifier cases from the old
// auto-watch/internal/runner/session_absent_test.go. Every "not there" phrasing
// must be a plain absent; a genuine failure must stay an error. Returning an
// error for an absent session crash-loops the daemon (the 33k-restart incident).
func TestSessionAbsent(t *testing.T) {
	absent := []string{
		"can't find session: autowatch-run-2958--run-etl",
		"no server running on /tmp/tmux-1002/default",
		"error connecting to /tmp/tmux-1002/default (No such file or directory)",
		"No server running on /tmp/tmux-0/default", // capitalised variant
	}
	for _, msg := range absent {
		if !sessionAbsent(msg) {
			t.Errorf("sessionAbsent(%q) = false, want true", msg)
		}
	}
	present := []string{"", "permission denied", "lost server"}
	for _, msg := range present {
		if sessionAbsent(msg) {
			t.Errorf("sessionAbsent(%q) = true, want false", msg)
		}
	}
}
