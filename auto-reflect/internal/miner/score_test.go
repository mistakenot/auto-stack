package miner

import (
	"math"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/etlread"
	"github.com/mistakenot/auto-reflect/internal/events"
)

func TestComputeSignals_Deterministic(t *testing.T) {
	msgs := []etlread.MsgSignalRow{
		{SessionID: "s1", Role: "user", ContentTruncated: "fix this bug please"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "sure"},
		{SessionID: "s1", Role: "user", ContentTruncated: "no, not like that, wrong approach"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "sorry, let me try again"},
		{SessionID: "s1", Role: "tool", ToolName: "Bash", IsError: true, ContentTruncated: "error: build failed"},
		{SessionID: "s1", Role: "user", ContentTruncated: "that's fine now"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "great"},
		{SessionID: "s1", Role: "tool", ToolName: "AskUserQuestion", ContentTruncated: "which approach?"},
		{SessionID: "s1", Role: "user", ContentTruncated: "use the first one"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "ok, doing that"},
	}

	sig1 := ComputeSignals(msgs)
	sig2 := ComputeSignals(msgs)

	if sig1 != sig2 {
		t.Fatalf("non-deterministic: %+v vs %+v", sig1, sig2)
	}
}

func TestComputeSignals_Values(t *testing.T) {
	msgs := []etlread.MsgSignalRow{
		{SessionID: "s1", Role: "user", ContentTruncated: "fix this bug please"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "sure"},
		{SessionID: "s1", Role: "user", ContentTruncated: "no, not like that"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "sorry"},
		{SessionID: "s1", Role: "tool", ToolName: "Bash", IsError: true, ContentTruncated: "error: build failed"},
		{SessionID: "s1", Role: "user", ContentTruncated: "ok good"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "done"},
		{SessionID: "s1", Role: "tool", ToolName: "AskUserQuestion", ContentTruncated: "which?"},
		{SessionID: "s1", Role: "user", ContentTruncated: "first"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "ok"},
		{SessionID: "s1", Role: "user", ContentTruncated: "wrong, try again"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "retrying"},
	}

	sig := ComputeSignals(msgs)

	if sig.MessageCount != 12 {
		t.Errorf("MessageCount = %d, want 12", sig.MessageCount)
	}
	// user messages: "fix this bug please", "no, not like that", "ok good", "first", "wrong, try again" → 5
	// corrections: "fix this" (matches "fix this"), "no, not like that" (matches "not"),
	//              "wrong, try again" (matches "wrong") → 3
	// CorrectionDensity = 3/5 * 100 = 60.0
	if sig.CorrectionDensity != 60.0 {
		t.Errorf("CorrectionDensity = %f, want 60.0", sig.CorrectionDensity)
	}
	if sig.ToolErrorCount != 1 {
		t.Errorf("ToolErrorCount = %d, want 1", sig.ToolErrorCount)
	}
	// "build failed" in tool error message
	if sig.FailureMarkerCount != 1 {
		t.Errorf("FailureMarkerCount = %d, want 1", sig.FailureMarkerCount)
	}
	if sig.AskUserCount != 1 {
		t.Errorf("AskUserCount = %d, want 1", sig.AskUserCount)
	}
	if sig.LengthFloorApplied {
		t.Error("LengthFloorApplied should be false for 12 messages")
	}
}

func TestComputeSignals_LengthFloor(t *testing.T) {
	msgs := []etlread.MsgSignalRow{
		{SessionID: "s1", Role: "user", ContentTruncated: "hello"},
		{SessionID: "s1", Role: "assistant", ContentTruncated: "hi"},
	}

	sig := ComputeSignals(msgs)

	if sig.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", sig.MessageCount)
	}
	if !sig.LengthFloorApplied {
		t.Error("LengthFloorApplied should be true for 2 messages")
	}
}

func TestComputeSignals_ZeroMessages(t *testing.T) {
	sig := ComputeSignals(nil)

	if sig.MessageCount != 0 {
		t.Errorf("MessageCount = %d, want 0", sig.MessageCount)
	}
	if sig.CorrectionDensity != 0 {
		t.Errorf("CorrectionDensity = %f, want 0", sig.CorrectionDensity)
	}
	if !sig.LengthFloorApplied {
		t.Error("LengthFloorApplied should be true for 0 messages")
	}
}

func TestScore_NoNaNOrInf(t *testing.T) {
	cases := []events.Signals{
		{},
		{MessageCount: 0},
		{MessageCount: 5, LengthFloorApplied: true},
		{MessageCount: 100, CorrectionDensity: 50, ToolErrorCount: 10, FailureMarkerCount: 5, AskUserCount: 3},
	}

	for i, sig := range cases {
		s := Score(sig)
		if math.IsNaN(s) || math.IsInf(s, 0) {
			t.Errorf("case %d: Score produced NaN or Inf: %f", i, s)
		}
	}
}

func TestScore_HigherCorrectionsHigherScore(t *testing.T) {
	low := events.Signals{MessageCount: 20, CorrectionDensity: 5}
	high := events.Signals{MessageCount: 20, CorrectionDensity: 50}

	scoreLow := Score(low)
	scoreHigh := Score(high)

	if scoreHigh <= scoreLow {
		t.Errorf("expected higher corrections → higher score: low=%f high=%f", scoreLow, scoreHigh)
	}
}

func TestScore_LengthFloorUsesFloor(t *testing.T) {
	// With LengthFloorApplied, effective count should be lengthFloor (10), not actual
	sig := events.Signals{
		MessageCount:       3,
		LengthFloorApplied: true,
		ToolErrorCount:     1,
	}

	score := Score(sig)
	// ToolErrorCount=1, effectiveCount=10 → 1/10*100*0.25 = 2.5
	expected := safeDiv(float64(1)*100, float64(lengthFloor)) * 0.25
	if math.Abs(score-expected) > 0.001 {
		t.Errorf("Score = %f, want %f (length floor applied)", score, expected)
	}
}
