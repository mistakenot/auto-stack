package writer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/parquet-go/parquet-go"
)

// Write outputs transformed rows as partitioned parquet files.
//
// Partition scheme:
//   - messages: year=YYYY/week=WW/messages.parquet (weekly)
//   - sessions: year=YYYY/month=MM/sessions.parquet (monthly)
//
// Current period is always regenerated. Past periods are skipped if file exists.
func Write(outputDir string, rows *model.TransformedRows) error {
	now := time.Now()
	curYear, curWeek := now.ISOWeek()
	curMonth := int(now.Month())

	// Write messages (weekly partitions)
	msgPartitions := groupMessagesByWeek(rows.Messages)
	for key, msgs := range msgPartitions {
		isCurrent := key.Year == curYear && key.Week == curWeek
		dir := filepath.Join(outputDir, "messages", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("week=%02d", key.Week))
		path := filepath.Join(dir, "messages.parquet")

		if !isCurrent && fileExists(path) {
			continue
		}

		if err := writeParquet(path, msgs); err != nil {
			return fmt.Errorf("write messages year=%d/week=%02d: %w", key.Year, key.Week, err)
		}
		log.Printf("wrote %s (%d rows)", path, len(msgs))
	}

	// Write sessions (monthly partitions)
	sessPartitions := groupSessionsByMonth(rows.Sessions)
	for key, sessions := range sessPartitions {
		isCurrent := key.Year == curYear && key.Month == curMonth
		dir := filepath.Join(outputDir, "sessions", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
		path := filepath.Join(dir, "sessions.parquet")

		if !isCurrent && fileExists(path) {
			continue
		}

		if err := writeParquet(path, sessions); err != nil {
			return fmt.Errorf("write sessions year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		log.Printf("wrote %s (%d rows)", path, len(sessions))
	}

	return nil
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

func writeParquet[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	w := parquet.NewGenericWriter[T](f)

	if _, err := w.Write(rows); err != nil {
		return err
	}

	return w.Close()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
