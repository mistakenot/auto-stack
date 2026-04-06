package runner_test

import (
	"testing"

	"github.com/mistakenot/auto-watch/internal/runner"
)

func TestScheduledRunName(t *testing.T) {
	got := runner.ScheduledRunName(42, "hello-from-cron")
	want := "autowatch-run-42--hello-from-cron"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
