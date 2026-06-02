package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Divergence records a mismatch between Go state and Quint expected state.
type Divergence struct {
	TraceFile string
	StepIndex int
	Action    string
	Variable  string
	Expected  string
	Got       string
}

func (d Divergence) String() string {
	return fmt.Sprintf("trace=%s step=%d action=%s var=%s\n  expected: %s\n  got:      %s",
		d.TraceFile, d.StepIndex, d.Action, d.Variable, d.Expected, d.Got)
}

// traceReplayState holds the Go-side state during trace replay.
type traceReplayState struct {
	StoreA []MessageRecord
	StoreB []MessageRecord
	StoreC []MessageRecord
	SessA  []SessionRecord
	SessB  []SessionRecord
	SessC  []SessionRecord
}

// copyMessages returns a deep copy of a message slice.
func copyMessages(msgs []MessageRecord) []MessageRecord {
	if msgs == nil {
		return nil
	}
	cp := make([]MessageRecord, len(msgs))
	copy(cp, msgs)
	return cp
}

// copySessions returns a deep copy of a session slice.
func copySessions(sess []SessionRecord) []SessionRecord {
	if sess == nil {
		return nil
	}
	cp := make([]SessionRecord, len(sess))
	copy(cp, sess)
	return cp
}

// sortMessages sorts messages by ID for deterministic comparison.
func sortMessages(msgs []MessageRecord) []MessageRecord {
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].ID < msgs[j].ID
	})
	return msgs
}

// sortSessions sorts sessions by ID for deterministic comparison.
func sortSessions(sess []SessionRecord) []SessionRecord {
	sort.Slice(sess, func(i, j int) bool {
		return sess[i].ID < sess[j].ID
	})
	return sess
}

func msgsEqual(a, b []MessageRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sessEqual(a, b []SessionRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fmtMsgs(msgs []MessageRecord) string {
	if len(msgs) == 0 {
		return "[]"
	}
	s := "["
	for i, m := range msgs {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("{id:%s sv:%d del:%d}", m.ID, m.SchemaVersion, m.DeletedAt)
	}
	s += "]"
	return s
}

func fmtSess(sess []SessionRecord) string {
	if len(sess) == 0 {
		return "[]"
	}
	s := "["
	for i, r := range sess {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("{id:%s sv:%d lma:%d mc:%d del:%d}", r.ID, r.SchemaVersion, r.LastMessageAt, r.MessageCount, r.DeletedAt)
	}
	s += "]"
	return s
}

// MergeFuncs abstracts the merge implementation to allow testing different strategies.
type MergeFuncs struct {
	MergeMessages func(a, b []MessageRecord) []MessageRecord
	MergeSessions func(a, b []SessionRecord) []SessionRecord
}

// replayTrace replays a single ITF trace against the given merge functions.
// Returns a list of divergences (empty = perfect match).
func replayTrace(trace *ITFTrace, funcs MergeFuncs, traceFile string) []Divergence {
	if len(trace.States) == 0 {
		return nil
	}

	// Initialize from state 0
	s := traceReplayState{
		StoreA: copyMessages(trace.States[0].StoreA),
		StoreB: copyMessages(trace.States[0].StoreB),
		StoreC: copyMessages(trace.States[0].StoreC),
		SessA:  copySessions(trace.States[0].SessA),
		SessB:  copySessions(trace.States[0].SessB),
		SessC:  copySessions(trace.States[0].SessC),
	}

	var divs []Divergence

	for i := 1; i < len(trace.States); i++ {
		expected := trace.States[i]
		action := expected.ActionTaken
		picks := expected.NondetPicks

		switch action {
		case "syncAB":
			origA := copyMessages(s.StoreA)
			origB := copyMessages(s.StoreB)
			s.StoreA = funcs.MergeMessages(origA, origB)
			s.StoreB = funcs.MergeMessages(origB, origA)
			origSA := copySessions(s.SessA)
			origSB := copySessions(s.SessB)
			s.SessA = funcs.MergeSessions(origSA, origSB)
			s.SessB = funcs.MergeSessions(origSB, origSA)

		case "syncAC":
			origA := copyMessages(s.StoreA)
			origC := copyMessages(s.StoreC)
			s.StoreA = funcs.MergeMessages(origA, origC)
			s.StoreC = funcs.MergeMessages(origC, origA)
			origSA := copySessions(s.SessA)
			origSC := copySessions(s.SessC)
			s.SessA = funcs.MergeSessions(origSA, origSC)
			s.SessC = funcs.MergeSessions(origSC, origSA)

		case "syncBC":
			origB := copyMessages(s.StoreB)
			origC := copyMessages(s.StoreC)
			s.StoreB = funcs.MergeMessages(origB, origC)
			s.StoreC = funcs.MergeMessages(origC, origB)
			origSB := copySessions(s.SessB)
			origSC := copySessions(s.SessC)
			s.SessB = funcs.MergeSessions(origSB, origSC)
			s.SessC = funcs.MergeSessions(origSC, origSB)

		case "ingestToA":
			id, _ := DecodeNondetString(picks, "id")
			v, _ := DecodeNondetInt(picks, "v")
			d, _ := DecodeNondetInt(picks, "d")
			newMsg := MessageRecord{ID: id, Content: "x", SchemaVersion: v, DeletedAt: d}
			s.StoreA = funcs.MergeMessages(s.StoreA, []MessageRecord{newMsg})
			// Sessions unchanged for ingestToA

		default:
			// Unknown action - skip (e.g. "init" is handled by initialization)
			continue
		}

		// Sort Go state for comparison
		sortMessages(s.StoreA)
		sortMessages(s.StoreB)
		sortMessages(s.StoreC)
		sortSessions(s.SessA)
		sortSessions(s.SessB)
		sortSessions(s.SessC)

		// Compare against expected Quint state
		if !msgsEqual(s.StoreA, expected.StoreA) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "storeA",
				Expected: fmtMsgs(expected.StoreA), Got: fmtMsgs(s.StoreA),
			})
		}
		if !msgsEqual(s.StoreB, expected.StoreB) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "storeB",
				Expected: fmtMsgs(expected.StoreB), Got: fmtMsgs(s.StoreB),
			})
		}
		if !msgsEqual(s.StoreC, expected.StoreC) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "storeC",
				Expected: fmtMsgs(expected.StoreC), Got: fmtMsgs(s.StoreC),
			})
		}
		if !sessEqual(s.SessA, expected.SessA) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "sessA",
				Expected: fmtSess(expected.SessA), Got: fmtSess(s.SessA),
			})
		}
		if !sessEqual(s.SessB, expected.SessB) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "sessB",
				Expected: fmtSess(expected.SessB), Got: fmtSess(s.SessB),
			})
		}
		if !sessEqual(s.SessC, expected.SessC) {
			divs = append(divs, Divergence{
				TraceFile: traceFile, StepIndex: i, Action: action, Variable: "sessC",
				Expected: fmtSess(expected.SessC), Got: fmtSess(s.SessC),
			})
		}

		// On divergence, resync Go state to expected to continue checking further steps
		if len(divs) > 0 && divs[len(divs)-1].StepIndex == i {
			s.StoreA = copyMessages(expected.StoreA)
			s.StoreB = copyMessages(expected.StoreB)
			s.StoreC = copyMessages(expected.StoreC)
			s.SessA = copySessions(expected.SessA)
			s.SessB = copySessions(expected.SessB)
			s.SessC = copySessions(expected.SessC)
		}
	}

	return divs
}

// specMergeFuncs returns the spec-aligned merge functions.
func specMergeFuncs() MergeFuncs {
	return MergeFuncs{
		MergeMessages: MergeMessages,
		MergeSessions: MergeSessions,
	}
}

// collectTraceFiles finds ITF trace files from testdata/ and optionally the full traces dir.
func collectTraceFiles(t *testing.T) []string {
	t.Helper()

	var files []string

	// Always include testdata/ files (checked in)
	testdataDir := filepath.Join("testdata")
	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		t.Fatalf("reading testdata dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			files = append(files, filepath.Join(testdataDir, e.Name()))
		}
	}

	// If the full traces directory exists, add those too
	fullTracesDir := "/home/vscode/src/auto-stack/.tmp/experiments/quint-sync-protocol/traces"
	if entries, err := os.ReadDir(fullTracesDir); err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".json" {
				files = append(files, filepath.Join(fullTracesDir, e.Name()))
			}
		}
	}

	sort.Strings(files)
	return files
}

func TestITFParse_SampleTrace(t *testing.T) {
	files, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no trace files in testdata/")
	}

	path := filepath.Join("testdata", files[0].Name())
	trace, err := ParseITFFile(path)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	if len(trace.States) == 0 {
		t.Fatal("no states in trace")
	}
	if trace.States[0].ActionTaken != "init" {
		t.Errorf("expected first action 'init', got %q", trace.States[0].ActionTaken)
	}
	t.Logf("parsed %s: %d states, first action=%s", path, len(trace.States), trace.States[0].ActionTaken)
}

func TestSpecMerge_ReplayTraces(t *testing.T) {
	files := collectTraceFiles(t)
	if len(files) == 0 {
		t.Fatal("no trace files found")
	}

	funcs := specMergeFuncs()
	var totalDivs []Divergence
	totalTraces := 0
	totalSteps := 0
	actionCounts := make(map[string]int)

	for _, f := range files {
		trace, err := ParseITFFile(f)
		if err != nil {
			t.Errorf("parsing %s: %v", f, err)
			continue
		}
		totalTraces++
		totalSteps += len(trace.States) - 1 // subtract init state

		// Count actions
		for _, s := range trace.States {
			actionCounts[s.ActionTaken]++
		}

		divs := replayTrace(trace, funcs, f)
		totalDivs = append(totalDivs, divs...)
	}

	t.Logf("=== Spec-aligned merge replay results ===")
	t.Logf("Traces parsed: %d", totalTraces)
	t.Logf("Steps replayed: %d", totalSteps)
	t.Logf("Action distribution: %v", actionCounts)
	t.Logf("Divergences: %d", len(totalDivs))

	if len(totalDivs) > 0 {
		t.Errorf("spec-aligned merge had %d divergences (should be 0)", len(totalDivs))
		for i, d := range totalDivs {
			if i >= 5 {
				t.Logf("... and %d more divergences", len(totalDivs)-5)
				break
			}
			t.Logf("  divergence %d: %s", i+1, d)
		}
	}
}
