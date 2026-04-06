package timeparse_test

import (
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/timeparse"
)

func TestParseSinceSupportsDaysAndWeeks(t *testing.T) {
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	got, err := timeparse.ParseSince(now, "5d")
	if err != nil {
		t.Fatalf("ParseSince returned error: %v", err)
	}
	want := now.Add(-5 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}

	got, err = timeparse.ParseSince(now, "1w")
	if err != nil {
		t.Fatalf("ParseSince returned error: %v", err)
	}
	want = now.Add(-7 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
