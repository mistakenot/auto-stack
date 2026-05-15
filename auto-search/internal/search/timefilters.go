package search

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sincePattern = regexp.MustCompile(`^([0-9]+)([mhdwMHDW])$`)

// TimeFilter contains normalized time-filter bounds and canonical filter text.
// StartMs is an inclusive lower bound (>=) and EndMs is an exclusive upper bound (<).
type TimeFilter struct {
	StartMs   *int64
	EndMs     *int64
	Canonical string
}

// ParseTimeFilter parses and validates --since / --after / --before.
func ParseTimeFilter(now time.Time, since, after, before string) (TimeFilter, error) {
	since = strings.TrimSpace(since)
	after = strings.TrimSpace(after)
	before = strings.TrimSpace(before)

	if since != "" && (after != "" || before != "") {
		return TimeFilter{}, errors.New(
			"invalid time filter: --since cannot be combined with --after/--before; use one mode (for example: --since 7d OR --after 2026-03-01 --before 2026-03-07)",
		)
	}

	if since != "" {
		startMs, err := parseSinceStart(now, since)
		if err != nil {
			return TimeFilter{}, err
		}
		startPtr := new(int64)
		*startPtr = startMs
		return TimeFilter{
			StartMs:   startPtr,
			Canonical: fmt.Sprintf("since=%d", startMs),
		}, nil
	}

	var tf TimeFilter
	if after != "" {
		startMs, err := parseAbsoluteTime("--after", after)
		if err != nil {
			return TimeFilter{}, err
		}
		startPtr := new(int64)
		*startPtr = startMs
		tf.StartMs = startPtr
		tf.Canonical = fmt.Sprintf("after=%d", startMs)
	}
	if before != "" {
		endMs, err := parseAbsoluteTime("--before", before)
		if err != nil {
			return TimeFilter{}, err
		}
		endPtr := new(int64)
		*endPtr = endMs
		tf.EndMs = endPtr
		if tf.Canonical == "" {
			tf.Canonical = fmt.Sprintf("before=%d", endMs)
		} else {
			tf.Canonical += fmt.Sprintf(";before=%d", endMs)
		}
	}

	if tf.StartMs != nil && tf.EndMs != nil && *tf.StartMs >= *tf.EndMs {
		return TimeFilter{}, errors.New(
			"invalid time range: --after must be earlier than --before (for example: --after 2026-03-01 --before 2026-03-07)",
		)
	}

	return tf, nil
}

// ParseDurationMs parses a duration string like "10m", "1h", "5d", "1w" and
// returns the equivalent number of milliseconds. Uses the same format as --since.
func ParseDurationMs(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	matches := sincePattern.FindStringSubmatch(raw)
	if len(matches) != 3 {
		return 0, fmt.Errorf(
			"invalid duration %q: expected <int><unit> with unit in m|h|d|w (for example: 10m, 1h, 5d, 1w)",
			raw,
		)
	}

	n, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf(
			"invalid duration %q: must be a positive integer (for example: 10m)",
			raw,
		)
	}

	unitMs, err := unitToMs(strings.ToLower(matches[2]))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
	}

	if n > math.MaxInt64/unitMs {
		return 0, fmt.Errorf("invalid duration %q: value is too large", raw)
	}

	return n * unitMs, nil
}

func unitToMs(unit string) (int64, error) {
	switch unit {
	case "m":
		return int64(time.Minute / time.Millisecond), nil
	case "h":
		return int64(time.Hour / time.Millisecond), nil
	case "d":
		return 24 * int64(time.Hour/time.Millisecond), nil
	case "w":
		return 7 * 24 * int64(time.Hour/time.Millisecond), nil
	default:
		return 0, errors.New("expected unit m|h|d|w")
	}
}

func parseSinceStart(now time.Time, raw string) (int64, error) {
	deltaMs, err := ParseDurationMs(raw)
	if err != nil {
		return 0, fmt.Errorf(
			"invalid --since value %q: expected <int><unit> with unit in m|h|d|w (for example: 5m, 12h, 7d, 1w)",
			raw,
		)
	}
	startMs := now.UTC().UnixMilli() - deltaMs
	return startMs, nil
}

func parseAbsoluteTime(flagName, raw string) (int64, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC().UnixMilli(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().UnixMilli(), nil
	}

	return 0, fmt.Errorf(
		"invalid %s value %q: expected YYYY-MM-DD or RFC3339 timestamp with timezone (for example: 2026-03-01 or 2026-03-01T12:00:00Z)",
		flagName,
		raw,
	)
}
