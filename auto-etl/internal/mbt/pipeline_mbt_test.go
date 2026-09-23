// Package mbt holds a model-based conformance test for the sessions ETL.
//
// The model is deliberately trivial: a set of Message IDs that have ever been
// written into the raw input. The conformance relation is set equality against
// the Message IDs actually present in the parquet output after a run. That is
// the whole contract the pipeline owes its callers — "everything you ever gave
// me is still here" — and it is a great deal simpler than the implementation,
// which is what makes it worth testing against.
//
// The command alphabet is what makes this find the partition-freeze defect:
// a single call can never expose it, because it only appears in the ORDER
// append → run → append → run over the same closed week.
package mbt

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-etl/internal/parser"
	"github.com/mistakenot/auto-etl/internal/transform"
	"github.com/mistakenot/auto-etl/internal/writer"
	"github.com/parquet-go/parquet-go"

	sharedmodel "github.com/mistakenot/auto-shared/model"
)

// ── the command alphabet ───────────────────────────────────────────────

type cmdKind int

const (
	cmdAppend cmdKind = iota // add n Messages to the session for `weeksAgo`
	cmdRun                   // run parse → transform → write
)

type command struct {
	kind     cmdKind
	weeksAgo int // 0 = the current ISO week; >0 = a week that has already closed
	n        int
}

func (c command) String() string {
	if c.kind == cmdRun {
		return "RunETL()"
	}
	return fmt.Sprintf("Append(weeksAgo=%d, n=%d)", c.weeksAgo, c.n)
}

// ── the model ──────────────────────────────────────────────────────────

// model is the entire specification: every Message ID ever handed to the
// pipeline. No timestamps, no partitions, no files — the reader of the parquet
// does not care how the rows got there, only that they are all there.
type model struct{ ids map[string]bool }

func newModel() *model { return &model{ids: map[string]bool{}} }

// ── the system under test ──────────────────────────────────────────────

type world struct {
	inputDir  string
	outputDir string
	written   map[int]int // weeksAgo -> how many Messages that session already holds
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return &world{inputDir: t.TempDir(), outputDir: t.TempDir(), written: map[int]int{}}
}

// sessionID names one session per week bucket, so a Message's index within its
// file is stable under append — which is what makes its ID stable.
func sessionID(weeksAgo int) string { return fmt.Sprintf("sess-w%d", weeksAgo) }

// weekAnchor returns a timestamp safely inside the ISO week `weeksAgo` weeks
// back: the Tuesday of that week at noon, so adding a few minutes per Message
// cannot spill into a neighbouring week.
func weekAnchor(weeksAgo int) time.Time {
	t := time.Now().UTC().AddDate(0, 0, -7*weeksAgo)
	for t.Weekday() != time.Monday {
		t = t.AddDate(0, 0, -1)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
}

// apply runs one command against both the model and the real pipeline.
func (w *world) apply(t *testing.T, m *model, c command) {
	t.Helper()
	switch c.kind {
	case cmdAppend:
		sid := sessionID(c.weeksAgo)
		path := filepath.Join(w.inputDir, sid+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		base := w.written[c.weeksAgo]
		anchor := weekAnchor(c.weeksAgo)
		for i := range c.n {
			idx := base + i
			ts := anchor.Add(time.Duration(idx) * time.Minute).Format(time.RFC3339)
			line := fmt.Sprintf(
				`{"type":"user","sessionId":%q,"cwd":"/tmp/mbt","timestamp":%q,"message":{"role":"user","content":"msg-%d"}}`+"\n",
				sid, ts, idx)
			if _, err := f.WriteString(line); err != nil {
				t.Fatalf("append: %v", err)
			}
			// The pipeline derives Message IDs as "{sessionID}-{index}".
			m.ids[fmt.Sprintf("%s-%d", sid, idx)] = true
		}
		_ = f.Close()
		w.written[c.weeksAgo] = base + c.n

	case cmdRun:
		sessions, err := parser.ScanAndParse(w.inputDir)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		cfg := transform.DefaultConfig()
		cfg.HostID = "mbt"
		rows, err := transform.Transform(sessions, cfg, nil)
		if err != nil {
			t.Fatalf("transform: %v", err)
		}
		if err := writer.Write(w.outputDir, rows); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// observe projects the real system's state into the model's shape: the set of
// Message IDs readable from the parquet output.
func (w *world) observe(t *testing.T) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	root := filepath.Join(w.outputDir, "messages")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".parquet") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		rdr := parquet.NewGenericReader[sharedmodel.AgentMessage](f)
		defer func() { _ = rdr.Close() }()
		buf := make([]sharedmodel.AgentMessage, 512)
		for {
			n, rerr := rdr.Read(buf)
			for i := range n {
				got[buf[i].ID] = true
			}
			if n == 0 || rerr != nil {
				break
			}
		}
		return nil
	})
	return got
}

// ── the conformance check ──────────────────────────────────────────────

// missing returns the Message IDs the model says exist but the output lacks.
func missing(m *model, got map[string]bool) []string {
	var out []string
	for id := range m.ids {
		if !got[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// runSequence replays a command sequence and reports the first divergence.
func runSequence(t *testing.T, seq []command) (step int, lost []string) {
	t.Helper()
	w := newWorld(t)
	m := newModel()
	for i, c := range seq {
		w.apply(t, m, c)
		if c.kind != cmdRun {
			continue
		}
		if lost := missing(m, w.observe(t)); len(lost) > 0 {
			return i, lost
		}
	}
	return -1, nil
}

// ── generation + shrinking ─────────────────────────────────────────────

func genSequence(rng *rand.Rand) []command {
	n := 6 + rng.Intn(8)
	seq := make([]command, 0, n)
	for range n {
		if rng.Intn(3) == 0 {
			seq = append(seq, command{kind: cmdRun})
			continue
		}
		seq = append(seq, command{kind: cmdAppend, weeksAgo: rng.Intn(4), n: 1 + rng.Intn(3)})
	}
	return append(seq, command{kind: cmdRun})
}

// shrink does delta debugging: drop any command that is not needed to keep the
// sequence failing, until nothing more can go.
func shrink(t *testing.T, seq []command) []command {
	t.Helper()
	changed := true
	for changed {
		changed = false
		for i := range seq {
			cand := make([]command, 0, len(seq)-1)
			cand = append(cand, seq[:i]...)
			cand = append(cand, seq[i+1:]...)
			if len(cand) == 0 {
				continue
			}
			if step, _ := runSequence(t, cand); step >= 0 {
				seq = cand
				changed = true
				break
			}
		}
	}
	return seq
}

// ── the test ───────────────────────────────────────────────────────────

// TestPipelineConformance asserts the pipeline's whole contract: every Message
// ever written to the input is readable from the output, no matter how many
// runs it took or when they happened.
func TestPipelineConformance(t *testing.T) {
	const seed = 1
	rng := rand.New(rand.NewSource(seed))

	for i := range 25 {
		seq := genSequence(rng)
		step, _ := runSequence(t, seq)
		if step < 0 {
			continue
		}
		min := shrink(t, seq)
		minStep, minLost := runSequence(t, min)

		var b strings.Builder
		fmt.Fprintf(&b, "\nCONFORMANCE DIVERGENCE (seed=%d, sequence %d)\n\n", seed, i)
		fmt.Fprintf(&b, "  shrunk counterexample (%d commands):\n", len(min))
		for j, c := range min {
			marker := "   "
			if j == minStep {
				marker = " ->"
			}
			fmt.Fprintf(&b, "  %s %d. %s\n", marker, j+1, c)
		}
		fmt.Fprintf(&b, "\n  at step %d the model holds Messages the output does not:\n", minStep+1)
		fmt.Fprintf(&b, "    missing: %v\n", minLost)
		fmt.Fprintf(&b, "\n  the model says every Message ever written to the input is readable\n")
		fmt.Fprintf(&b, "  from the parquet. The pipeline disagrees.\n")
		t.Fatal(b.String())
	}
}

// TestConformanceHoldsForTheCurrentWeek is the control. The same append → run →
// append → run shape that fails against a closed week passes against the open
// one, because the current partition is always regenerated. Without this, a
// permanently-red conformance test would prove nothing.
func TestConformanceHoldsForTheCurrentWeek(t *testing.T) {
	seq := []command{
		{kind: cmdAppend, weeksAgo: 0, n: 2},
		{kind: cmdRun},
		{kind: cmdAppend, weeksAgo: 0, n: 1},
		{kind: cmdRun},
	}
	if step, lost := runSequence(t, seq); step >= 0 {
		t.Fatalf("current-week sequence diverged at step %d, missing %v", step+1, lost)
	}
}

// TestConformanceHoldsForAClosedWeek pins the regression as a directed case, so
// the counterexample survives a change of generator seed.
//
// This is the sequence the generative test shrank to when the writer skipped
// closed partitions: append to a week, run, append to that same week again, run.
// Before the fix, step 4 silently dropped sess-w3-2.
func TestConformanceHoldsForAClosedWeek(t *testing.T) {
	seq := []command{
		{kind: cmdAppend, weeksAgo: 3, n: 2},
		{kind: cmdRun},
		{kind: cmdAppend, weeksAgo: 3, n: 1},
		{kind: cmdRun},
	}
	if step, lost := runSequence(t, seq); step >= 0 {
		t.Fatalf("closed-week sequence diverged at step %d: %v missing from the output. "+
			"A Message written into a week whose partition already exists must still be persisted.",
			step+1, lost)
	}
}
