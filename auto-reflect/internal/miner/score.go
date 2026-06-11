package miner

import (
	"regexp"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/etlread"
	"github.com/mistakenot/auto-reflect/internal/events"
)

const lengthFloor = 10 // sessions below this message count get capped signal density

var correctionPatterns = regexp.MustCompile(`(?i)\b(no[, ]+not|wrong|incorrect|don'?t|stop|instead|actually|fix this|that'?s not|you should have)\b`)

// ComputeSignals computes deterministic signals from message rows for a session.
func ComputeSignals(msgs []etlread.MsgSignalRow) events.Signals {
	var sig events.Signals
	sig.MessageCount = len(msgs)

	userMsgCount := 0
	corrections := 0

	for i := range msgs {
		m := &msgs[i]

		if m.Role == "user" {
			userMsgCount++
			if correctionPatterns.MatchString(m.ContentTruncated) {
				corrections++
			}
		}

		if m.IsError {
			sig.ToolErrorCount++
		}

		lower := strings.ToLower(m.ContentTruncated)
		if containsFailureMarker(lower) {
			sig.FailureMarkerCount++
		}

		if m.ToolName == "AskUserQuestion" {
			sig.AskUserCount++
		}
	}

	if sig.MessageCount < lengthFloor {
		sig.LengthFloorApplied = true
	}

	// Corrections per 100 user messages
	if userMsgCount > 0 {
		sig.CorrectionDensity = safeDiv(float64(corrections)*100, float64(userMsgCount))
	}

	return sig
}

func containsFailureMarker(lower string) bool {
	markers := []string{
		"build failed", "compilation error", "compile error",
		"test failed", "tests failed", "failing test",
		"exit code 1", "exit code 2", "non-zero exit",
		"error:", "fatal:", "panic:",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// Score computes a deterministic priority score from signals.
// Higher score = more likely to yield useful observations.
func Score(sig events.Signals) float64 {
	effectiveCount := sig.MessageCount
	if sig.LengthFloorApplied {
		effectiveCount = lengthFloor
	}

	// Weights: correction density is the strongest signal
	score := 0.0
	score += sig.CorrectionDensity * 0.4 // corrections per 100 user msgs, weight 0.4
	score += safeDiv(float64(sig.ToolErrorCount)*100, float64(effectiveCount)) * 0.25
	score += safeDiv(float64(sig.FailureMarkerCount)*100, float64(effectiveCount)) * 0.2
	score += safeDiv(float64(sig.AskUserCount)*100, float64(effectiveCount)) * 0.15

	return score
}

func safeDiv(num, den float64) float64 {
	if den <= 0 {
		return 0
	}
	return num / den
}
