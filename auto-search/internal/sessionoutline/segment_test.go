package sessionoutline

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/mistakenot/auto-search/internal/sessionhtml"
)

// The segmenter is pure, so these tests drive it with synthetic in-memory
// events — no index, no fixtures, no ripple into the fixture-count assertions
// the rest of the module carries.

const testSession = "sess-1"

// tool builds a tool event at index idx, one minute after the previous one.
func tool(idx int, name, summary string) sessionhtml.Event {
	return sessionhtml.Event{
		Kind:    "tool",
		Idx:     idx,
		Ts:      baseTs + int64(idx)*60_000,
		Tool:    name,
		Summary: summary,
		MID:     mid(idx),
	}
}

const baseTs int64 = 1_711_000_000_000

func mid(idx int) string {
	return testSession + "-" + strconv.Itoa(idx)
}

func prose(idx int, kind, summary string) sessionhtml.Event {
	return sessionhtml.Event{
		Kind:    kind,
		Idx:     idx,
		Ts:      baseTs + int64(idx)*60_000,
		Summary: summary,
		MID:     mid(idx),
	}
}

// reasons is the boundary reason of each produced segment, in order.
func reasons(segs []Segment) []string {
	out := make([]string, len(segs))
	for i := range segs {
		out[i] = segs[i].Reason
	}
	return out
}

// ranges is the message_index span of each produced segment, in order.
func ranges(segs []Segment) [][2]int {
	out := make([][2]int, len(segs))
	for i := range segs {
		out[i] = segs[i].IndexRange
	}
	return out
}

func TestSegmentEmptyTimeline(t *testing.T) {
	if got := segment(testSession, nil); got != nil {
		t.Fatalf("segment(nil) = %v, want nil", got)
	}
}

func TestSegmentSingleRunStaysOneSegment(t *testing.T) {
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		prose(1, "assistant", "looks fine"),
		tool(2, "Read", "b.go"),
		tool(3, "Read", "c.go"),
	}
	segs := segment(testSession, events)
	if len(segs) != 1 {
		t.Fatalf("reasons = %v, want a single unbroken Read run", reasons(segs))
	}
	if segs[0].Reason != reasonStart {
		t.Fatalf("reason = %q, want %q", segs[0].Reason, reasonStart)
	}
	if segs[0].IndexRange != [2]int{0, 3} {
		t.Fatalf("index_range = %v, want [0 3]", segs[0].IndexRange)
	}
	// Interleaved prose must not split a tool burst.
	if got := segs[0].Counts.File; got != 3 {
		t.Fatalf("file count = %d, want 3", got)
	}
}

func TestSegmentCutsOnBashError(t *testing.T) {
	fail := tool(2, "Bash", "go test ./...")
	fail.Exit = 1
	fail.IsError = true
	events := []sessionhtml.Event{
		tool(0, "Bash", "go build ./..."),
		tool(1, "Bash", "gofmt -l ."),
		fail,
		tool(3, "Bash", "go build ./..."),
	}
	segs := segment(testSession, events)
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonBashError, reasonErrorClear}) {
		t.Fatalf("reasons = %v, want start/bash_error/error_cleared", got)
	}
	if got := ranges(segs); !reflect.DeepEqual(got, [][2]int{{0, 1}, {2, 2}, {3, 3}}) {
		t.Fatalf("ranges = %v", got)
	}
	if segs[1].Label != "Bash FAIL — go test ./..." {
		t.Fatalf("label = %q, want the failing command", segs[1].Label)
	}
}

func TestSegmentCutsOnNonBashToolError(t *testing.T) {
	fail := tool(1, "Read", "/missing.go")
	fail.IsError = true
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		fail,
	}
	segs := segment(testSession, events)
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonToolError}) {
		t.Fatalf("reasons = %v, want start/tool_error", got)
	}
}

func TestSegmentCutsOnToolBurstChange(t *testing.T) {
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		tool(1, "Read", "b.go"),
		tool(2, "Bash", "go build ./..."),
		tool(3, "Edit", "a.go"),
	}
	segs := segment(testSession, events)
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonToolBurst, reasonToolBurst}) {
		t.Fatalf("reasons = %v, want start/tool_burst/tool_burst", got)
	}
	if got := ranges(segs); !reflect.DeepEqual(got, [][2]int{{0, 1}, {2, 2}, {3, 3}}) {
		t.Fatalf("ranges = %v", got)
	}
	if segs[0].Label != "Read ×2 — a.go" {
		t.Fatalf("label = %q, want a Read burst label", segs[0].Label)
	}
}

func TestSegmentCutsOnTimeGap(t *testing.T) {
	late := tool(1, "Read", "b.go")
	late.Ts = baseTs + timeGapThresholdMs + 1
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		late,
	}
	segs := segment(testSession, events)
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonTimeGap}) {
		t.Fatalf("reasons = %v, want start/time_gap", got)
	}
}

func TestSegmentDoesNotCutBelowTimeGapThreshold(t *testing.T) {
	near := tool(1, "Read", "b.go")
	near.Ts = baseTs + timeGapThresholdMs
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		near,
	}
	if segs := segment(testSession, events); len(segs) != 1 {
		t.Fatalf("reasons = %v, want no cut exactly at the threshold", reasons(segs))
	}
}

func TestSegmentIgnoresMissingTimestamps(t *testing.T) {
	// Rows indexed before Event.Ts existed carry a zero timestamp; a zero must
	// never read as a decades-long gap.
	a, b := tool(0, "Read", "a.go"), tool(1, "Read", "b.go")
	a.Ts, b.Ts = 0, 0
	if segs := segment(testSession, []sessionhtml.Event{a, b}); len(segs) != 1 {
		t.Fatalf("reasons = %v, want no cut when timestamps are absent", reasons(segs))
	}
}

func TestSegmentCutsOnTodoMarker(t *testing.T) {
	events := []sessionhtml.Event{
		tool(0, "Bash", "go build ./..."),
		tool(1, "Bash", "br update auto-1 --status=in_progress"),
		tool(2, "Bash", "go test ./..."),
		tool(3, "TodoWrite", "3 items"),
	}
	segs := segment(testSession, events)
	// The marker opens a segment that then carries the work following it,
	// until the next boundary — here the second marker.
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonTodoMarker, reasonTodoMarker}) {
		t.Fatalf("reasons = %v", got)
	}
	if got := ranges(segs); !reflect.DeepEqual(got, [][2]int{{0, 0}, {1, 2}, {3, 3}}) {
		t.Fatalf("ranges = %v", got)
	}
	if segs[1].Label != "todo · br update auto-1 --status=in_progress" {
		t.Fatalf("label = %q, want the todo marker command", segs[1].Label)
	}
}

func TestSegmentDoesNotTreatUnrelatedBrCallAsMarker(t *testing.T) {
	events := []sessionhtml.Event{
		tool(0, "Bash", "br ready"),
		tool(1, "Bash", "br list --status=open"),
	}
	if segs := segment(testSession, events); len(segs) != 1 {
		t.Fatalf("reasons = %v, want read-only br calls to stay in one segment", reasons(segs))
	}
}

func TestSegmentIsolatesSubagentDispatch(t *testing.T) {
	dispatch := sessionhtml.Event{
		Kind: "agent", Idx: 2, Ts: baseTs + 120_000,
		Summary: "explore the auth module", SubagentType: "Explore", MID: mid(2),
	}
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		tool(1, "Read", "b.go"),
		dispatch,
		tool(3, "Read", "c.go"),
	}
	segs := segment(testSession, events)
	if got := reasons(segs); !reflect.DeepEqual(got, []string{reasonStart, reasonDispatch, reasonResume}) {
		t.Fatalf("reasons = %v, want start/subagent_dispatch/resume", got)
	}
	if got := ranges(segs); !reflect.DeepEqual(got, [][2]int{{0, 1}, {2, 2}, {3, 3}}) {
		t.Fatalf("ranges = %v, want the dispatch alone in its own segment", got)
	}
	if segs[1].Label != "Explore · explore the auth module" {
		t.Fatalf("label = %q, want the dispatch label", segs[1].Label)
	}
	if segs[1].Counts.Agent != 1 {
		t.Fatalf("agent count = %d, want 1", segs[1].Counts.Agent)
	}
}

func TestSegmentIDsAreStableOrdinals(t *testing.T) {
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		tool(1, "Bash", "go build ./..."),
		tool(2, "Edit", "a.go"),
	}
	segs := segment(testSession, events)
	want := []string{"sess-1#s0", "sess-1#s1", "sess-1#s2"}
	for i := range segs {
		if segs[i].ID != want[i] {
			t.Fatalf("segment %d id = %q, want %q", i, segs[i].ID, want[i])
		}
	}
}

func TestSegmentIsDeterministic(t *testing.T) {
	fail := tool(3, "Bash", "go test ./...")
	fail.Exit = 2
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		prose(1, "assistant", "now building"),
		tool(2, "Bash", "go build ./..."),
		fail,
		tool(4, "Bash", "br close auto-1"),
	}
	first, err := json.Marshal(segment(testSession, events))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(segment(testSession, events))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("segmentation is not byte-identical across runs:\n%s\n%s", first, second)
	}
}

// TestCountEventsErrorDefinition pins decision D-2: an outline error is
// `is_error || bash_exit_code != 0`, not `session list`'s bash-only count.
func TestCountEventsErrorDefinition(t *testing.T) {
	bashExit := tool(0, "Bash", "go test ./...")
	bashExit.Exit = 1

	toolFlagged := tool(1, "Grep", "pattern")
	toolFlagged.IsError = true

	clean := tool(2, "Bash", "go build ./...")

	agentEv := sessionhtml.Event{Kind: "agent", Idx: 3, Summary: "dispatch"}

	got := CountEvents([]sessionhtml.Event{bashExit, toolFlagged, clean, agentEv})
	want := Counts{Bash: 2, Tool: 1, Agent: 1, Error: 2}
	if got != want {
		t.Fatalf("CountEvents = %+v, want %+v", got, want)
	}
}

// TestSegmentCountsSumToNodeCounts guards the invariant that lets a reader
// trust a collapsed outline: the counts on a node equal the sum of its
// segments'.
func TestSegmentCountsSumToNodeCounts(t *testing.T) {
	fail := tool(2, "Bash", "go test ./...")
	fail.IsError = true
	events := []sessionhtml.Event{
		tool(0, "Read", "a.go"),
		tool(1, "Bash", "go build ./..."),
		fail,
		{Kind: "agent", Idx: 3, Ts: baseTs + 180_000, Summary: "dispatch"},
		tool(4, "Edit", "a.go"),
	}
	node := CountEvents(events)

	var sum Counts
	for _, s := range segment(testSession, events) {
		sum.Bash += s.Counts.Bash
		sum.File += s.Counts.File
		sum.Tool += s.Counts.Tool
		sum.Agent += s.Counts.Agent
		sum.Skill += s.Counts.Skill
		sum.Error += s.Counts.Error
	}
	if sum != node {
		t.Fatalf("segment counts %+v do not sum to node counts %+v", sum, node)
	}
}

// TestLeafBreadcrumbTargetsTheBodyRow pins the two-tier breadcrumb contract at
// the leaf tier: a tool leaf points at result_mid (the row holding the output),
// prose at its own mid.
func TestLeafBreadcrumbTargetsTheBodyRow(t *testing.T) {
	toolUse := tool(0, "Bash", "go build ./...")
	toolUse.ResultMID = "sess-1-1"

	segs := segment(testSession, []sessionhtml.Event{toolUse, prose(2, "assistant", "done")})
	var leaves []Leaf
	for _, s := range segs {
		leaves = append(leaves, s.Leaves...)
	}
	if len(leaves) != 2 {
		t.Fatalf("got %d leaves, want 2", len(leaves))
	}
	if leaves[0].Get != "auto search message get sess-1-1" {
		t.Fatalf("tool leaf get = %q, want the result row", leaves[0].Get)
	}
	if leaves[1].Get != "auto search message get sess-1-2" {
		t.Fatalf("prose leaf get = %q, want its own message id", leaves[1].Get)
	}
}
