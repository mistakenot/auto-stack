package events

import (
	"testing"
	"time"
)

func TestShardNameStablePerPath(t *testing.T) {
	day := time.Date(2026, 6, 9, 13, 30, 0, 0, time.UTC)
	a := ShardName("host1", day, "/home/vscode/src/auto-stack")
	b := ShardName("host1", day, "/home/vscode/src/auto-stack")
	if a != b {
		t.Fatalf("same host+day+path must be stable: %q != %q", a, b)
	}
}

func TestShardNameDistinctPerWorktree(t *testing.T) {
	day := time.Date(2026, 6, 9, 13, 30, 0, 0, time.UTC)
	a := ShardName("host1", day, "/home/vscode/src/auto-stack")
	b := ShardName("host1", day, "/home/vscode/src/auto-stack-task-019")
	if a == b {
		t.Fatalf("two distinct worktree roots on same host+day must differ, both %q", a)
	}
}

func TestShardNameUsesUTCDate(t *testing.T) {
	// 23:30 in a -05:00 zone is 04:30 the next UTC day; the shard must use UTC.
	zone := time.FixedZone("minus5", -5*3600)
	local := time.Date(2026, 6, 9, 23, 30, 0, 0, zone)
	name := ShardName("host1", local, "/some/root")
	want := ShardName("host1", local.UTC(), "/some/root")
	if name != want {
		t.Fatalf("shard name must be computed in UTC: got %q want %q", name, want)
	}
	if got := name; got[len("host1-"):len("host1-")+10] != "2026-06-10" {
		t.Fatalf("expected UTC date 2026-06-10 in %q", got)
	}
}
