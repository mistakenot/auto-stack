package hooks

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
	sharedhooks "github.com/mistakenot/auto-shared/hooks"
)

// Ingest reads new hook events from JSONL log files in rawDir, starting from
// the watermark offsets in state. Returns rows and updates state offsets.
func Ingest(rawDir string, state *HooksSyncState, fallbackHostID string) ([]model.HookEventRow, error) {
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read hooks raw dir: %w", err)
	}

	// Filter and sort event files.
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "events-") && strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var rows []model.HookEventRow
	for _, name := range names {
		path := filepath.Join(rawDir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := info.Size()

		// Get or create file state.
		fs := state.Files[name]
		if fs == nil {
			fs = &FileState{}
			state.Files[name] = fs
		}

		if fs.Offset >= size {
			continue // fully consumed
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}

		if fs.Offset > 0 {
			if _, err := f.Seek(fs.Offset, io.SeekStart); err != nil {
				_ = f.Close()
				continue
			}
		}

		offset := fs.Offset
		r := bufio.NewReader(f)
		for {
			lineBytes, err := r.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					// Partial line (no \n) — don't advance offset past it.
					break
				}
				break
			}

			// Complete line ending in \n.
			lineStart := offset
			offset += int64(len(lineBytes))
			line := strings.TrimSpace(string(lineBytes))
			if line == "" {
				continue
			}

			row := parseLine(line, name, lineStart, fallbackHostID)
			rows = append(rows, row)
		}
		fs.Offset = offset
		_ = f.Close()
	}

	return rows, nil
}

func parseLine(line string, sourceFile string, lineOffset int64, fallbackHostID string) model.HookEventRow {
	var env sharedhooks.Envelope
	_ = json.Unmarshal([]byte(line), &env)

	host := env.HostID
	if host == "" {
		host = fallbackHostID
	}

	// Parse payload into map for extraction.
	var payload map[string]any
	if len(env.Payload) > 0 {
		_ = json.Unmarshal(env.Payload, &payload)
	}

	// Derive normalized fields from the payload via shared extractors.
	event := sharedhooks.ExtractEventName(payload)
	session := sharedhooks.ExtractSessionID(payload)
	tool := sharedhooks.ExtractTool(payload)
	paths := sharedhooks.ExtractPaths(payload)

	var pathsJSON string
	if len(paths) > 0 {
		if b, err := json.Marshal(paths); err == nil {
			pathsJSON = string(b)
		}
	}

	// Parse captured_at from envelope.
	var capturedAtMs int64
	if env.CapturedAt != "" {
		if t, err := time.Parse(time.RFC3339, env.CapturedAt); err == nil {
			capturedAtMs = t.UnixMilli()
		}
	}

	// Year/Month from captured_at UTC.
	var year, month int32
	if capturedAtMs > 0 {
		t := time.UnixMilli(capturedAtMs).UTC()
		year = int32(t.Year())
		month = int32(t.Month())
	}

	// Raw JSON is the verbatim payload, or the whole line if no payload.
	rawJSON := string(env.Payload)
	if rawJSON == "" || rawJSON == "null" {
		rawJSON = line
	}

	// Host-stable ID: sha256(hostID \x00 file \x00 offset).
	idInput := fmt.Sprintf("%s\x00%s\x00%d", host, sourceFile, lineOffset)
	hash := sha256.Sum256([]byte(idInput))
	id := hex.EncodeToString(hash[:16]) // 32 hex chars

	return model.HookEventRow{
		ID:            id,
		HostID:        host,
		Agent:         env.Agent,
		Event:         event,
		SessionID:     session,
		Cwd:           env.Cwd,
		Project:       env.Project,
		Tool:          tool,
		PathsJSON:     pathsJSON,
		CapturedAt:    capturedAtMs,
		RawJSON:       rawJSON,
		SourceFile:    sourceFile,
		Year:          year,
		Month:         month,
		SchemaVersion: model.HookSchemaVersion,
	}
}
