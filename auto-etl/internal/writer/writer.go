package writer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/parquet-go/parquet-go"
)

// Write outputs transformed rows as partitioned parquet files.
//
// Partition scheme:
//   - messages: year=YYYY/week=WW/messages.parquet (weekly)
//   - sessions: year=YYYY/month=MM/sessions.parquet (monthly)
//
// Every partition a run touches is read back, merged with the incoming rows by
// stable ID, and rewritten — the same read-merge-write contract the git, github
// and hooks writers already use.
//
// This deliberately does NOT treat a closed calendar period as immutable. A week
// stops receiving data when the source stops producing it, not when the week
// ends: a Session appended to after a run, a corpus copied from another Host, or
// a backup restored all deliver Messages whose timestamps fall inside a period
// that has already been written. Skipping those partitions silently discarded
// 17.8% of the canonical record over five months.
//
// Writes are published atomically (temp file + rename) so a crash or a full disk
// cannot destroy a partition that was already good, and the whole output tree is
// held under an exclusive lock for the duration so two concurrent runs cannot
// lose each other's rows in a read-merge-write race.
func Write(outputDir string, rows *model.TransformedRows) error {
	unlock, err := lockOutput(outputDir)
	if err != nil {
		return fmt.Errorf("lock output dir: %w", err)
	}
	defer unlock()

	// Write messages (weekly partitions)
	for key, msgs := range groupMessagesByWeek(rows.Messages) {
		path := filepath.Join(outputDir, "messages",
			fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("week=%02d", key.Week), "messages.parquet")

		merged, err := mergePartition(path, msgs, func(m *model.AgentMessage) string { return m.ID })
		if err != nil {
			return fmt.Errorf("merge messages year=%d/week=%02d: %w", key.Year, key.Week, err)
		}
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write messages year=%d/week=%02d: %w", key.Year, key.Week, err)
		}
		log.Printf("wrote %s (%d rows, %d incoming)", path, len(merged), len(msgs))
	}

	// Write sessions (monthly partitions)
	for key, sessions := range groupSessionsByMonth(rows.Sessions) {
		path := filepath.Join(outputDir, "sessions",
			fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month), "sessions.parquet")

		merged, err := mergePartition(path, sessions, func(s *model.AgentSession) string { return s.ID })
		if err != nil {
			return fmt.Errorf("merge sessions year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write sessions year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		log.Printf("wrote %s (%d rows, %d incoming)", path, len(merged), len(sessions))
	}

	return nil
}

// mergePartition reads a partition back and unions it with the incoming rows by
// stable ID, incoming winning on a collision.
//
// A read failure is fatal rather than a warning. The alternative — logging and
// carrying on with only the incoming rows — turns a transient read error into
// permanent loss of every row already in that partition, which is the failure
// this whole change exists to remove.
func mergePartition[T any](path string, incoming []T, id func(*T) string) ([]T, error) {
	existing, err := readExistingParquet[T](path)
	if err != nil {
		return nil, fmt.Errorf("read existing %s: %w (refusing to overwrite it)", path, err)
	}
	return mergeByID(existing, incoming, id), nil
}

type partKey struct {
	Year  int
	Week  int
	Month int
}

func groupMessagesByWeek(msgs []model.AgentMessage) map[partKey][]model.AgentMessage {
	m := make(map[partKey][]model.AgentMessage)
	for i := range msgs {
		k := partKey{Year: int(msgs[i].Year), Week: int(msgs[i].Week)}
		m[k] = append(m[k], msgs[i])
	}
	return m
}

func groupSessionsByMonth(sessions []model.AgentSession) map[partKey][]model.AgentSession {
	m := make(map[partKey][]model.AgentSession)
	for i := range sessions {
		k := partKey{Year: int(sessions[i].Year), Month: int(sessions[i].Month)}
		m[k] = append(m[k], sessions[i])
	}
	return m
}

// writeParquet publishes rows to path atomically: the parquet is built in a temp
// file alongside the target, fsynced, and only then renamed over it. A reader
// therefore sees either the previous complete partition or the new one, never a
// half-written file, and an interrupted write leaves the old partition intact.
func writeParquet[T any](path string, rows []T) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: a no-op once the rename below has succeeded.
	defer func() { _ = os.Remove(tmpName) }()

	w := parquet.NewGenericWriter[T](tmp)
	if _, err := w.Write(rows); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := w.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir fsyncs a directory so a rename survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
