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
