package hooks

import (
	"errors"
	"reflect"
	"testing"
)

func TestCaptureEnvFrom(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    map[string]string
	}{
		{
			name: "picks NTM and TMUX vars, ignores others",
			environ: []string{
				"NTM_SPAWN_BATCH_ID=spawn-123",
				"NTM_SPAWN_ORDER=2",
				"TMUX=/tmp/tmux-1002/default,1,0",
				"TMUX_PANE=%1",
				"HOME=/home/vscode",
				"PATH=/usr/bin",
				"TERM_PROGRAM=tmux", // not NTM_/TMUX-prefixed: excluded
			},
			want: map[string]string{
				"NTM_SPAWN_BATCH_ID": "spawn-123",
				"NTM_SPAWN_ORDER":    "2",
				"TMUX":               "/tmp/tmux-1002/default,1,0",
				"TMUX_PANE":          "%1",
			},
		},
		{
			name:    "no matching vars returns nil",
			environ: []string{"HOME=/home/vscode", "PATH=/usr/bin"},
			want:    nil,
		},
		{
			name:    "value containing equals is preserved",
			environ: []string{"NTM_CONFIG=a=b=c"},
			want:    map[string]string{"NTM_CONFIG": "a=b=c"},
		},
		{
			name:    "malformed entry without equals is skipped",
			environ: []string{"NOTAVAR", "TMUX_PANE=%2"},
			want:    map[string]string{"TMUX_PANE": "%2"},
		},
		{
			name:    "empty value is kept",
			environ: []string{"TMUX="},
			want:    map[string]string{"TMUX": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureEnvFrom(tt.environ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("captureEnvFrom = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCaptureEnvReadsProcessEnv(t *testing.T) {
	t.Setenv("NTM_SPAWN_BATCH_ID", "spawn-abc")
	got := CaptureEnv()
	if got["NTM_SPAWN_BATCH_ID"] != "spawn-abc" {
		t.Errorf("CaptureEnv[NTM_SPAWN_BATCH_ID] = %q, want %q", got["NTM_SPAWN_BATCH_ID"], "spawn-abc")
	}
}

func TestResolveTmuxTarget(t *testing.T) {
	tests := []struct {
		name string
		out  string
		err  error
		want map[string]string
	}{
		{
			name: "full line maps all fields",
			out:  "auto-stack\t0\t1\t%1",
			want: map[string]string{
				"tmux_session":      "auto-stack",
				"tmux_window_index": "0",
				"tmux_pane_index":   "1",
				"tmux_pane_id":      "%1",
			},
		},
		{
			name: "query error returns nil",
			err:  errors.New("tmux not found"),
			want: nil,
		},
		{
			name: "empty output returns nil",
			out:  "",
			want: nil,
		},
		{
			name: "wrong field count returns nil",
			out:  "auto-stack\t0",
			want: nil,
		},
		{
			name: "blank fields are dropped",
			out:  "auto-stack\t\t1\t%1",
			want: map[string]string{
				"tmux_session":    "auto-stack",
				"tmux_pane_index": "1",
				"tmux_pane_id":    "%1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTmuxTarget(func(string) (string, error) {
				return tt.out, tt.err
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveTmuxTarget = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTmuxTargetQueriesEveryField(t *testing.T) {
	var gotFormat string
	resolveTmuxTarget(func(format string) (string, error) {
		gotFormat = format
		return "s\t0\t1\t%1", nil
	})
	want := "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}"
	if gotFormat != want {
		t.Errorf("query format = %q, want %q", gotFormat, want)
	}
}

func TestCaptureTmuxTargetNotUnderTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := CaptureTmuxTarget(); got != nil {
		t.Errorf("CaptureTmuxTarget without $TMUX = %v, want nil", got)
	}
}

func TestCaptureTmuxTargetUnderTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1002/default,1,0")
	orig := tmuxRunner
	t.Cleanup(func() { tmuxRunner = orig })
	tmuxRunner = func(string) (string, error) { return "auto-stack\t0\t1\t%1", nil }

	got := CaptureTmuxTarget()
	want := map[string]string{
		"tmux_session":      "auto-stack",
		"tmux_window_index": "0",
		"tmux_pane_index":   "1",
		"tmux_pane_id":      "%1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CaptureTmuxTarget = %v, want %v", got, want)
	}
}

func TestCaptureContextMergesEnvAndTmux(t *testing.T) {
	t.Setenv("NTM_SPAWN_BATCH_ID", "spawn-xyz")
	t.Setenv("TMUX", "/tmp/tmux-1002/default,1,0")
	t.Setenv("TMUX_PANE", "%1")
	orig := tmuxRunner
	t.Cleanup(func() { tmuxRunner = orig })
	tmuxRunner = func(string) (string, error) { return "auto-stack\t0\t1\t%1", nil }

	got := CaptureContext()
	// Raw env vars survive...
	if got["NTM_SPAWN_BATCH_ID"] != "spawn-xyz" {
		t.Errorf("NTM_SPAWN_BATCH_ID = %q, want %q", got["NTM_SPAWN_BATCH_ID"], "spawn-xyz")
	}
	// ...alongside the resolved tmux targeting keys needed to reply.
	if got["tmux_session"] != "auto-stack" || got["tmux_pane_index"] != "1" {
		t.Errorf("missing tmux targeting: session=%q pane_index=%q", got["tmux_session"], got["tmux_pane_index"])
	}
}

func TestCaptureContextNilWhenNothingPresent(t *testing.T) {
	t.Setenv("TMUX", "")
	orig := tmuxRunner
	t.Cleanup(func() { tmuxRunner = orig })
	tmuxRunner = func(string) (string, error) { return "", errors.New("unreachable") }

	// Clearing TMUX means CaptureTmuxTarget short-circuits to nil; the test
	// process may still expose ambient NTM_/TMUX_ vars, so only assert the
	// tmux keys are absent rather than a fully nil map.
	got := CaptureContext()
	if _, ok := got["tmux_session"]; ok {
		t.Errorf("CaptureContext included tmux_session with TMUX unset: %v", got)
	}
}
