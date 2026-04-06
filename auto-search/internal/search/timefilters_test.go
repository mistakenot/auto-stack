package search

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseTimeFilterSinceValid(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	tests := []struct {
		name      string
		since     string
		wantDelta int64
	}{
		{name: "minutes", since: "5m", wantDelta: int64(5 * time.Minute / time.Millisecond)},
		{name: "hours", since: "12h", wantDelta: int64(12 * time.Hour / time.Millisecond)},
		{name: "days", since: "2d", wantDelta: int64(48 * time.Hour / time.Millisecond)},
		{name: "weeks", since: "1w", wantDelta: int64(7 * 24 * time.Hour / time.Millisecond)},
		{name: "uppercase unit", since: "3H", wantDelta: int64(3 * time.Hour / time.Millisecond)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tf, err := ParseTimeFilter(now, tc.since, "", "")
			if err != nil {
				t.Fatalf("ParseTimeFilter: %v", err)
			}
			if tf.StartMs == nil {
				t.Fatal("StartMs is nil, want non-nil")
			}
			if tf.EndMs != nil {
				t.Fatalf("EndMs = %v, want nil", *tf.EndMs)
			}

			wantStart := nowMs - tc.wantDelta
			if *tf.StartMs != wantStart {
				t.Fatalf("StartMs = %d, want %d", *tf.StartMs, wantStart)
			}
			wantCanonical := "since=" + strconv.FormatInt(wantStart, 10)
			if tf.Canonical != wantCanonical {
				t.Fatalf("Canonical = %q, want %q", tf.Canonical, wantCanonical)
			}
		})
	}
}

func TestParseTimeFilterSinceInvalid(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		since string
	}{
		{name: "zero", since: "0d"},
		{name: "nonnumeric", since: "abc"},
		{name: "bad unit", since: "7x"},
		{name: "missing numeric", since: "d"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTimeFilter(now, tc.since, "", "")
			if err == nil {
				t.Fatalf("expected error for since=%q", tc.since)
			}
			if !strings.Contains(err.Error(), "--since") {
				t.Fatalf("error = %q, want mention of --since", err.Error())
			}
		})
	}
}

func TestParseTimeFilterAbsoluteValid(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		after        string
		before       string
		wantStartSet bool
		wantStartMs  int64
		wantEndSet   bool
		wantEndMs    int64
	}{
		{
			name:         "after date",
			after:        "2026-03-01",
			wantStartSet: true,
			wantStartMs:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
		{
			name:       "before rfc3339 timezone",
			before:     "2026-03-01T01:00:00-05:00",
			wantEndSet: true,
			wantEndMs:  time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC).UnixMilli(),
		},
		{
			name:         "after and before",
			after:        "2026-03-01T12:00:00Z",
			before:       "2026-03-02",
			wantStartSet: true,
			wantStartMs:  time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).UnixMilli(),
			wantEndSet:   true,
			wantEndMs:    time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).UnixMilli(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tf, err := ParseTimeFilter(now, "", tc.after, tc.before)
			if err != nil {
				t.Fatalf("ParseTimeFilter: %v", err)
			}

			assertMaybeInt64(t, tf.StartMs, tc.wantStartSet, tc.wantStartMs, "StartMs")
			assertMaybeInt64(t, tf.EndMs, tc.wantEndSet, tc.wantEndMs, "EndMs")
			wantCanonical := expectedCanonical(tc.wantStartSet, tc.wantStartMs, tc.wantEndSet, tc.wantEndMs)
			if tf.Canonical != wantCanonical {
				t.Fatalf("Canonical = %q, want %q", tf.Canonical, wantCanonical)
			}
		})
	}
}

func TestParseTimeFilterAbsoluteInvalid(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	_, err := ParseTimeFilter(now, "", "03/01/2026", "")
	if err == nil {
		t.Fatal("expected invalid --after error")
	}
	if !strings.Contains(err.Error(), "--after") {
		t.Fatalf("error = %q, want mention of --after", err.Error())
	}

	_, err = ParseTimeFilter(now, "", "", "2026-03-01T01:00:00")
	if err == nil {
		t.Fatal("expected invalid --before error")
	}
	if !strings.Contains(err.Error(), "--before") {
		t.Fatalf("error = %q, want mention of --before", err.Error())
	}
}

func TestParseTimeFilterInvalidMixedMode(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	_, err := ParseTimeFilter(now, "7d", "2026-03-01", "")
	if err == nil {
		t.Fatal("expected mixed mode error")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %q, want combined-mode message", err.Error())
	}
}

func TestParseTimeFilterInvalidRange(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	_, err := ParseTimeFilter(now, "", "2026-03-01", "2026-03-01")
	if err == nil {
		t.Fatal("expected invalid range error for equal bounds")
	}

	_, err = ParseTimeFilter(now, "", "2026-03-02", "2026-03-01")
	if err == nil {
		t.Fatal("expected invalid range error for inverted bounds")
	}
}

func TestParseTimeFilterDateParsesToUTCMidnight(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	tf, err := ParseTimeFilter(now, "", "2026-03-01", "")
	if err != nil {
		t.Fatalf("ParseTimeFilter: %v", err)
	}
	if tf.StartMs == nil {
		t.Fatal("StartMs is nil")
	}
	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	if *tf.StartMs != want {
		t.Fatalf("StartMs = %d, want %d", *tf.StartMs, want)
	}
}

func TestParseTimeFilterCanonicalStability(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)

	a, err := ParseTimeFilter(now, "", "2026-03-01", "2026-03-02")
	if err != nil {
		t.Fatalf("ParseTimeFilter date form: %v", err)
	}
	b, err := ParseTimeFilter(now, "", "2026-03-01T00:00:00Z", "2026-03-02T00:00:00Z")
	if err != nil {
		t.Fatalf("ParseTimeFilter rfc3339 form: %v", err)
	}

	if a.Canonical != b.Canonical {
		t.Fatalf("canonical mismatch: %q vs %q", a.Canonical, b.Canonical)
	}
}

func assertMaybeInt64(t *testing.T, got *int64, wantSet bool, want int64, field string) {
	t.Helper()
	if !wantSet {
		if got != nil {
			t.Fatalf("%s = %d, want nil", field, *got)
		}
		return
	}
	if got == nil {
		t.Fatalf("%s is nil, want %d", field, want)
	}
	if *got != want {
		t.Fatalf("%s = %d, want %d", field, *got, want)
	}
}

func expectedCanonical(startSet bool, startMs int64, endSet bool, endMs int64) string {
	switch {
	case startSet && endSet:
		return fmt.Sprintf("after=%d;before=%d", startMs, endMs)
	case startSet:
		return fmt.Sprintf("after=%d", startMs)
	case endSet:
		return fmt.Sprintf("before=%d", endMs)
	default:
		return ""
	}
}
