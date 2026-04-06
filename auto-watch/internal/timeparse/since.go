package timeparse

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var simplePattern = regexp.MustCompile(`^(\d+)([smhdw])$`)

func ParseSince(now time.Time, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("since value is required")
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return now.Add(-duration), nil
	}
	matches := simplePattern.FindStringSubmatch(value)
	if len(matches) != 3 {
		if ts, err := time.Parse(time.RFC3339, value); err == nil {
			return ts, nil
		}
		if ts, err := time.Parse("2006-01-02", value); err == nil {
			return ts, nil
		}
		return time.Time{}, fmt.Errorf("invalid since value %q", value)
	}
	count, err := strconv.Atoi(matches[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid since value %q", value)
	}
	unit := matches[2]
	multiplier := time.Second
	switch unit {
	case "m":
		multiplier = time.Minute
	case "h":
		multiplier = time.Hour
	case "d":
		multiplier = 24 * time.Hour
	case "w":
		multiplier = 7 * 24 * time.Hour
	case "s":
		multiplier = time.Second
	}
	return now.Add(-time.Duration(count) * multiplier), nil
}
