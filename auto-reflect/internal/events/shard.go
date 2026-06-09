package events

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// worktreeDiscriminator returns the first 8 hex chars of SHA-256 of the absolute
// worktree root path. It isolates parallel same-host worktrees so their event
// shards never collide on a path under git merge.
func worktreeDiscriminator(worktreeRoot string) string {
	sum := sha256.Sum256([]byte(worktreeRoot))
	return hex.EncodeToString(sum[:])[:8]
}

// ShardName builds the shard filename for a host, day, and worktree root:
// <host>-<YYYY-MM-DD>-<wt8>.jsonl. The day is computed in UTC so the same
// wall-clock day produces one shard regardless of local timezone.
func ShardName(host string, day time.Time, worktreeRoot string) string {
	date := day.UTC().Format("2006-01-02")
	return fmt.Sprintf("%s-%s-%s.jsonl", host, date, worktreeDiscriminator(worktreeRoot))
}
