package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
)

func msg(id string, year, week int) model.AgentMessage {
	return model.AgentMessage{ID: id, SessionID: "s", Year: int32(year), Week: int32(week), Role: "user", Content: id}
}

func sess(id string, year, month int) model.AgentSession {
	return model.AgentSession{ID: id, Year: int32(year), Month: int32(month)}
}

func msgPath(dir string, year, week int) string {
	return filepath.Join(dir, "messages", fmt.Sprintf("year=%d", year), fmt.Sprintf("week=%02d", week), "messages.parquet")
}

func readIDs(t *testing.T, path string) map[string]bool {
	t.Helper()
	rows, err := readExistingParquet[model.AgentMessage](path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	ids := map[string]bool{}
	for i := range rows {
		ids[rows[i].ID] = true
	}
	return ids
}

// A partition for a period that has already closed must still accept new rows.
// The writer used to skip any existing partition that was not the current
// calendar period, which silently discarded every late-arriving Message.
func TestWriteMergesIntoAClosedPartition(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -30)
	year, week := past.ISOWeek()

	if err := Write(dir, &model.TransformedRows{Messages: []model.AgentMessage{msg("a", year, week)}}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := Write(dir, &model.TransformedRows{Messages: []model.AgentMessage{msg("b", year, week)}}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got := readIDs(t, msgPath(dir, year, week))
	for _, want := range []string{"a", "b"} {
		if !got[want] {
			t.Errorf("message %q missing after the second write — a closed partition must still accept rows (got %v)", want, got)
		}
	}
}

// The same guarantee for the monthly sessions partition.
func TestWriteMergesIntoAClosedSessionPartition(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, -2, 0)
	year, month := past.Year(), int(past.Month())

	if err := Write(dir, &model.TransformedRows{Sessions: []model.AgentSession{sess("s1", year, month)}}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := Write(dir, &model.TransformedRows{Sessions: []model.AgentSession{sess("s2", year, month)}}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	path := filepath.Join(dir, "sessions", fmt.Sprintf("year=%d", year), fmt.Sprintf("month=%02d", month), "sessions.parquet")
	rows, err := readExistingParquet[model.AgentSession](path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("session rows = %d, want 2 — got %+v", len(rows), rows)
	}
}

// Re-writing identical rows must not duplicate them: the merge is a union by ID,
// so the pipeline can be run as often as the cron fires without the record growing.
func TestWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -14)
	year, week := past.ISOWeek()
	rows := &model.TransformedRows{Messages: []model.AgentMessage{msg("a", year, week), msg("b", year, week)}}

	for i := range 3 {
		if err := Write(dir, rows); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	got := readIDs(t, msgPath(dir, year, week))
	if len(got) != 2 {
		t.Errorf("after 3 identical writes: %d rows, want 2 (%v)", len(got), got)
	}
}

// A partition is published by rename, so a reader never sees a partial file and
// an interrupted write leaves the previous partition readable. Assert no stray
// temp files survive a successful publish.
func TestWritePublishesAtomically(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -21)
	year, week := past.ISOWeek()

	if err := Write(dir, &model.TransformedRows{Messages: []model.AgentMessage{msg("a", year, week)}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	partDir := filepath.Dir(msgPath(dir, year, week))
	entries, err := os.ReadDir(partDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "messages.parquet" {
			t.Errorf("stray file %q left in the partition directory after publish", e.Name())
		}
	}
}

// A corrupt or unreadable partition must abort the write, not silently republish
// it with only the incoming rows — that would convert a transient read failure
// into permanent loss of everything already stored there.
func TestWriteRefusesToOverwriteAnUnreadablePartition(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -7)
	year, week := past.ISOWeek()
	path := msgPath(dir, year, week)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("this is not parquet"), 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}

	err := Write(dir, &model.TransformedRows{Messages: []model.AgentMessage{msg("a", year, week)}})
	if err == nil {
		t.Fatal("Write succeeded over an unreadable partition; it must fail loudly instead of discarding the existing rows")
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil || string(data) != "this is not parquet" {
		t.Errorf("the unreadable partition was modified; it must be left untouched for inspection")
	}
}

// Read-merge-write is only safe under one writer at a time. `auto etl run` is on
// a */10 cron and overlaps manual runs trivially, so concurrent writers must
// serialise rather than lose each other's rows.
func TestConcurrentWritesDoNotLoseRows(t *testing.T) {
	dir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -10)
	year, week := past.ISOWeek()

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Write(dir, &model.TransformedRows{
				Messages: []model.AgentMessage{msg(fmt.Sprintf("m%d", i), year, week)},
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	got := readIDs(t, msgPath(dir, year, week))
	for i := range writers {
		if id := fmt.Sprintf("m%d", i); !got[id] {
			t.Errorf("row %q lost to a concurrent write (have %d of %d)", id, len(got), writers)
		}
	}
}
