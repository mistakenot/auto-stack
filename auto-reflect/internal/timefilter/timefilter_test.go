package timefilter

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		since string
		want  time.Time
	}{
		{since: "5m", want: now.Add(-5 * time.Minute)},
		{since: "2h", want: now.Add(-2 * time.Hour)},
		{since: "7d", want: now.Add(-7 * 24 * time.Hour)},
		{since: "1w", want: now.Add(-7 * 24 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.since, func(t *testing.T) {
			window, err := Parse(now, tc.since, "", "")
			if err != nil {
				t.Fatalf("parse since: %v", err)
			}
			if window.After == nil {
				t.Fatal("expected after")
			}
			if !window.After.Equal(tc.want) {
				t.Fatalf("unexpected since start: got=%s want=%s", window.After.UTC().Format(time.RFC3339), tc.want.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestParseRejectsMixedModes(t *testing.T) {
	_, err := Parse(time.Now().UTC(), "7d", "2026-04-01", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseAfterBefore(t *testing.T) {
	window, err := Parse(time.Now().UTC(), "", "2026-04-01", "2026-04-03T00:00:00Z")
	if err != nil {
		t.Fatalf("parse absolute range: %v", err)
	}
	if window.After == nil || window.Before == nil {
		t.Fatal("expected after and before bounds")
	}
}

func TestParseRejectsUppercaseSinceUnit(t *testing.T) {
	_, err := Parse(time.Now().UTC(), "3H", "", "")
	if err == nil {
		t.Fatal("expected uppercase unit to be rejected")
	}
}
