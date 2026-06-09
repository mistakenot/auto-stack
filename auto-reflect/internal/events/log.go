package events

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mistakenot/auto-shared/config"

	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/store"
)

// AppendOptions carries the optional, caller-resolved context for an append.
// SessionID, when non-empty, overrides environment detection (e.g. a --session
// flag). Now, when non-zero, overrides the timestamp/day (used by tests).
type AppendOptions struct {
	SessionID string
	Now       time.Time
}

// AppendEvent appends a single event to the shard for the current host, day, and
// worktree under an exclusive file lock. It allocates a monotonic per-shard seq
// by reading the shard's last line for the current max seq and writing seq+1.
// Git provenance degrades gracefully on an unborn HEAD: the worktree root still
// resolves but hash/remote may be empty; event writing never hard-fails just
// because the repo has no commits.
//
// It returns the fully populated event that was persisted.
func AppendEvent(cwd, eventType string, payload any, opts AppendOptions) (Event, error) {
	repo, err := gitutil.DetectRepoLenient(cwd)
	if err != nil {
		return Event{}, err
	}

	_, host, _, err := config.EnsureHost()
	if err != nil {
		return Event{}, fmt.Errorf("resolve host: %w", err)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = DetectSessionID()
	}

	id, err := newEventID()
	if err != nil {
		return Event{}, err
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}

	event := Event{
		ID:            id,
		Type:          eventType,
		SchemaVersion: SchemaVersion,
		TS:            now.UTC().Format(time.RFC3339),
		Host:          host.HostID,
		SessionID:     sessionID,
		Agent:         DetectAgent(),
		Git: GitProvenance{
			Hash:   repo.Head,
			Remote: repo.Remote,
		},
		Payload: payloadBytes,
	}

	dir := store.EventsDir(repo.Root)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return Event{}, fmt.Errorf("create events directory: %w", err)
	}
	path := filepath.Join(dir, ShardName(host.HostID, now, repo.Root))

	seq, err := appendWithSeq(path, &event)
	if err != nil {
		return Event{}, err
	}
	event.Seq = seq

	return event, nil
}

const dirPermission = 0o755

// appendWithSeq opens the shard read-modify-write under flock(LOCK_EX), parses
// the current max seq from the last line, assigns event.Seq = max+1, appends the
// encoded event, and syncs. The pattern mirrors store.AppendJSONLine's locking
// but reads first to allocate the sequence (AppendJSONLine is O_APPEND-only and
// cannot allocate seq). Returns the assigned seq.
func appendWithSeq(path string, event *Event) (int, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open events shard: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return 0, fmt.Errorf("acquire file lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()

	maxSeq, err := maxSeqInFile(file)
	if err != nil {
		return 0, err
	}
	event.Seq = maxSeq + 1

	data, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("encode event: %w", err)
	}
	data = append(data, '\n')

	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return 0, fmt.Errorf("seek events shard: %w", err)
	}
	if err := writeAll(file, data); err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	if err := file.Sync(); err != nil {
		return 0, fmt.Errorf("sync events shard: %w", err)
	}
	return event.Seq, nil
}

// maxSeqInFile scans the open shard for the highest seq value. An empty file
// yields 0. Blank/garbage lines are skipped so a partially-written tail cannot
// crash the allocator.
func maxSeqInFile(file *os.File) (int, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek events shard: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return 0, fmt.Errorf("read events shard: %w", err)
	}
	maxSeq := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}
	return maxSeq, nil
}

// ShardedEvent pairs a decoded event with the shard filename it came from, so
// callers (e.g. the rules fold) can track per-shard high-water marks.
type ShardedEvent struct {
	Shard string
	Event Event
}

// shardedEvent pairs a decoded event with its shard filename for total ordering.
type shardedEvent struct {
	shard string
	event Event
}

// ReadAll walks the events directory under repoRoot, decodes every line of every
// shard, and returns the events in the deterministic total order (ts, shard
// name, seq). A missing events directory yields an empty slice, not an error.
func ReadAll(repoRoot string) ([]Event, error) {
	collected, err := readAllSharded(repoRoot)
	if err != nil {
		return nil, err
	}
	result := make([]Event, len(collected))
	for i := range collected {
		result[i] = collected[i].event
	}
	return result, nil
}

// ReadAllSharded is ReadAll but preserves the originating shard name for each
// event, in the same deterministic total order. Callers that fold rules use the
// shard to advance per-shard folded_through high-water marks.
func ReadAllSharded(repoRoot string) ([]ShardedEvent, error) {
	collected, err := readAllSharded(repoRoot)
	if err != nil {
		return nil, err
	}
	result := make([]ShardedEvent, len(collected))
	for i := range collected {
		result[i] = ShardedEvent{Shard: collected[i].shard, Event: collected[i].event}
	}
	return result, nil
}

func readAllSharded(repoRoot string) ([]shardedEvent, error) {
	dir := store.EventsDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read events directory: %w", err)
	}

	var collected []shardedEvent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		shard := entry.Name()
		readErr := store.ReadJSONLines(path, func(_ int, line []byte) error {
			var e Event
			if err := json.Unmarshal(line, &e); err != nil {
				return fmt.Errorf("decode event in %s: %w", shard, err)
			}
			collected = append(collected, shardedEvent{shard: shard, event: e})
			return nil
		})
		if readErr != nil {
			return nil, readErr
		}
	}

	sort.SliceStable(collected, func(i, j int) bool {
		a, b := collected[i], collected[j]
		if a.event.TS != b.event.TS {
			return a.event.TS < b.event.TS
		}
		if a.shard != b.shard {
			return a.shard < b.shard
		}
		return a.event.Seq < b.event.Seq
	})

	return collected, nil
}

func newEventID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}
	return fmt.Sprintf("ev-%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3]), nil
}

func writeAll(file *os.File, data []byte) error {
	written := 0
	for written < len(data) {
		n, err := file.Write(data[written:])
		if err != nil {
			return err
		}
		written += n
	}
	return nil
}
