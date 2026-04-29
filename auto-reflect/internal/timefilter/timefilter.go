package timefilter

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sincePattern = regexp.MustCompile(`^([0-9]+)([mhdw])$`)

type Window struct {
	After  *time.Time
	Before *time.Time
}

func Parse(now time.Time, since, after, before string) (Window, error) {
	since = strings.TrimSpace(since)
	after = strings.TrimSpace(after)
	before = strings.TrimSpace(before)

	if since != "" && (after != "" || before != "") {
		return Window{}, errors.New("invalid time filter: --since cannot be combined with --after/--before; use one mode (for example: --since 7d OR --after 2026-03-01 --before 2026-03-07)")
	}

	window := Window{}
	if since != "" {
		start, err := parseSinceStart(now, since)
		if err != nil {
			return Window{}, err
		}
		window.After = &start
		return window, nil
	}

	if after != "" {
		parsed, err := parseAbsoluteTime("--after", after)
		if err != nil {
			return Window{}, err
		}
		window.After = &parsed
	}
	if before != "" {
		parsed, err := parseAbsoluteTime("--before", before)
		if err != nil {
			return Window{}, err
		}
		window.Before = &parsed
	}

	if window.After != nil && window.Before != nil && !window.After.Before(*window.Before) {
		return Window{}, errors.New("invalid time range: --after must be earlier than --before (for example: --after 2026-03-01 --before 2026-03-07)")
	}

	return window, nil
}

func parseSinceStart(now time.Time, raw string) (time.Time, error) {
	matches := sincePattern.FindStringSubmatch(raw)
	if len(matches) != 3 {
		return time.Time{}, fmt.Errorf("invalid --since value %q: expected <int><unit> with unit in m|h|d|w (for example: 5m, 12h, 7d, 1w)", raw)
	}

	n, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("invalid --since value %q: duration must be a positive integer (for example: 7d)", raw)
	}

	unit := strings.ToLower(matches[2])
	var unitDuration time.Duration
	switch unit {
	case "m":
		unitDuration = time.Minute
	case "h":
		unitDuration = time.Hour
	case "d":
		unitDuration = 24 * time.Hour
	case "w":
		unitDuration = 7 * 24 * time.Hour
	default:
		return time.Time{}, fmt.Errorf("invalid --since value %q: expected unit m|h|d|w", raw)
	}

	if n > math.MaxInt64/int64(unitDuration) {
		return time.Time{}, fmt.Errorf("invalid --since value %q: duration is too large", raw)
	}

	return now.UTC().Add(-time.Duration(n) * unitDuration), nil
}

func parseAbsoluteTime(flagName, raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid %s value %q: expected YYYY-MM-DD or RFC3339 timestamp with timezone (for example: 2026-03-01 or 2026-03-01T12:00:00Z)", flagName, raw)
}
