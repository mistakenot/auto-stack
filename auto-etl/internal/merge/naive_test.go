package merge

import (
	"sort"
	"testing"
)

// naiveMergeMessages mimics the current mergeByID behavior:
// incoming (second arg) always wins on ID collision.
// No schema_version comparison. No tombstone propagation.
func naiveMergeMessages(existing, incoming []MessageRecord) []MessageRecord {
	if len(existing) == 0 {
		return sortedMessages(incoming)
	}

	incomingIDs := make(map[string]bool, len(incoming))
	for _, m := range incoming {
		incomingIDs[m.ID] = true
	}

	result := make([]MessageRecord, 0, len(existing)+len(incoming))
	for _, m := range existing {
		if !incomingIDs[m.ID] {
			result = append(result, m)
		}
	}
	result = append(result, incoming...)

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// naiveMergeSessions mimics the current mergeByID behavior for sessions:
// incoming always wins on ID collision.
func naiveMergeSessions(existing, incoming []SessionRecord) []SessionRecord {
	if len(existing) == 0 {
		return sortedSessions(incoming)
	}

	incomingIDs := make(map[string]bool, len(incoming))
	for _, s := range incoming {
		incomingIDs[s.ID] = true
	}

	result := make([]SessionRecord, 0, len(existing)+len(incoming))
	for _, s := range existing {
		if !incomingIDs[s.ID] {
			result = append(result, s)
		}
	}
	result = append(result, incoming...)

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

func sortedMessages(msgs []MessageRecord) []MessageRecord {
	cp := make([]MessageRecord, len(msgs))
	copy(cp, msgs)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].ID < cp[j].ID
	})
	return cp
}

func sortedSessions(sess []SessionRecord) []SessionRecord {
	cp := make([]SessionRecord, len(sess))
	copy(cp, sess)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].ID < cp[j].ID
	})
	return cp
}

func naiveMergeFuncs() MergeFuncs {
	return MergeFuncs{
		MergeMessages: naiveMergeMessages,
		MergeSessions: naiveMergeSessions,
	}
}

func TestNaiveMerge_ReplayTraces(t *testing.T) {
	files := collectTraceFiles(t)
	if len(files) == 0 {
		t.Fatal("no trace files found")
	}

	funcs := naiveMergeFuncs()
	var totalDivs []Divergence
	totalTraces := 0
	totalSteps := 0
	tracesWithDivs := 0

	// Categorize divergences by type
	tombstoneDivs := 0
	schemaDivs := 0
	sessionDivs := 0

	for _, f := range files {
		trace, err := ParseITFFile(f)
		if err != nil {
			t.Errorf("parsing %s: %v", f, err)
			continue
		}
		totalTraces++
		totalSteps += len(trace.States) - 1

		divs := replayTrace(trace, funcs, f)
		if len(divs) > 0 {
			tracesWithDivs++
		}
		for _, d := range divs {
			switch {
			case d.Variable == "sessA" || d.Variable == "sessB" || d.Variable == "sessC":
				sessionDivs++
			default:
				// Check if it looks like a tombstone or schema issue
				// We classify by looking at whether expected has del>0 where got has del=0
				tombstoneDivs++
				schemaDivs++ // conservative: count both since naive lacks both
			}
		}
		totalDivs = append(totalDivs, divs...)
	}

	t.Logf("=== Naive merge (incoming-wins) replay results ===")
	t.Logf("Traces parsed: %d", totalTraces)
	t.Logf("Steps replayed: %d", totalSteps)
	t.Logf("Total divergences: %d", len(totalDivs))
	t.Logf("Traces with divergences: %d / %d (%.1f%%)", tracesWithDivs, totalTraces, float64(tracesWithDivs)/float64(totalTraces)*100)
	t.Logf("Message store divergences: %d", tombstoneDivs)
	t.Logf("Session store divergences: %d", sessionDivs)

	// Show first 5 divergences as examples
	shown := 0
	for _, d := range totalDivs {
		if shown >= 5 {
			break
		}
		t.Logf("  example divergence %d: %s", shown+1, d)
		shown++
	}
	if len(totalDivs) > 5 {
		t.Logf("  ... and %d more divergences", len(totalDivs)-5)
	}

	// This test intentionally does NOT fail on divergence.
	// The purpose is to measure the gap between naive and spec merge.
	if len(totalDivs) == 0 {
		t.Log("WARNING: no divergences detected. Traces may not cover schema/tombstone scenarios.")
	}
}
