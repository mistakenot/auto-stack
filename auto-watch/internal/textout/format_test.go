package textout_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/textout"
)

func TestFormatEventLine(t *testing.T) {
	line := textout.FormatEventLine(&model.EventRecord{
		Timestamp: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
		Level:     "info",
		EventType: "trigger_evaluated",
		ProjectID: "demo",
		TriggerID: "daily",
		Metadata:  map[string]any{"outcome": "launched"},
	})
	for _, want := range []string{"2026-03-20T10:00:00Z", "info", "trigger_evaluated", "project=demo", "trigger=daily", "outcome=launched"} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected %q in line %q", want, line)
		}
	}
}

// TestFormatEventLineRunAndMessage (AC-11): a task_failed Event for run 42 with
// exit code 3 and a message prints run=42, exit_code=3 (never a 0x… pointer
// address) and msg=%q.
func TestFormatEventLineRunAndMessage(t *testing.T) {
	runID := int64(42)
	code := 3
	line := textout.FormatEventLine(&model.EventRecord{
		Timestamp: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
		Level:     "error",
		EventType: "task_failed",
		ProjectID: "demo",
		TaskID:    "build",
		RunID:     &runID,
		Message:   "run 42 finished with exit code 3",
		// Pointer metadata must be dereferenced, not printed as an address.
		Metadata: map[string]any{"exit_code": &code},
	})
	for _, want := range []string{"run=42", "exit_code=3", `msg="run 42 finished with exit code 3"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected %q in line %q", want, line)
		}
	}
	if strings.Contains(line, "0x") {
		t.Fatalf("line must not contain a pointer address: %q", line)
	}
}

// TestFormatEventLineNilExitCode (AC-11): a nil exit code prints exit_code=null,
// whether stored as an untyped nil or a nil *int.
func TestFormatEventLineNilExitCode(t *testing.T) {
	var nilCode *int
	cases := map[string]any{
		"untyped nil": nil,
		"nil *int":    nilCode,
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			line := textout.FormatEventLine(&model.EventRecord{
				Timestamp: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
				Level:     "error",
				EventType: "task_failed",
				Metadata:  map[string]any{"exit_code": value},
			})
			if !strings.Contains(line, "exit_code=null") {
				t.Fatalf("expected exit_code=null in line %q", line)
			}
			if strings.Contains(line, "0x") || strings.Contains(line, "<nil>") {
				t.Fatalf("nil exit code must print as null, got %q", line)
			}
		})
	}
}
