package events

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/config"

	"github.com/mistakenot/auto-reflect/internal/store"
)

// newGitRepo creates a freshly-initialized git repo with a seed commit and an
// origin remote, returning its root. HOME is redirected to a temp dir so
// EnsureHost writes ~/.auto/host.json under the sandbox.
func newGitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
	runGit(t, root, "commit", "--allow-empty", "-m", "seed")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func samplePayload() RuleCreatedPayload {
	return RuleCreatedPayload{
		RuleID:     "r-00000001",
		Domain:     []string{"go"},
		UseWhen:    "writing go",
		Content:    "build after each file",
		CausalNote: "catches errors early",
		RuleType:   "soft",
		Lifecycle:  "draft",
	}
}

func TestAppendSequentialSeq(t *testing.T) {
	root := newGitRepo(t)
	for i := 1; i <= 50; i++ {
		ev, err := AppendEvent(root, TypeRuleCreated, samplePayload(), AppendOptions{})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if ev.Seq != i {
			t.Fatalf("append %d: got seq %d, want %d", i, ev.Seq, i)
		}
	}

	all, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all) != 50 {
		t.Fatalf("expected 50 events, got %d", len(all))
	}
	for i, ev := range all {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d", i, ev.Seq)
		}
	}
}

func TestAppendConcurrentNoGapsOrDuplicates(t *testing.T) {
	root := newGitRepo(t)
	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AppendEvent(root, TypeRuleCreated, samplePayload(), AppendOptions{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent append: %v", err)
	}

	all, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(all) != n {
		t.Fatalf("expected %d events, got %d", n, len(all))
	}
	seqs := make([]int, len(all))
	for i, ev := range all {
		seqs[i] = ev.Seq
	}
	sort.Ints(seqs)
	for i := 0; i < n; i++ {
		if seqs[i] != i+1 {
			t.Fatalf("seqs have gap/dup: %v", seqs)
		}
	}
}

func TestReadAllDeterministic(t *testing.T) {
	root := newGitRepo(t)
	base := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_, err := AppendEvent(root, TypeRuleCreated, samplePayload(), AppendOptions{Now: base.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	first, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll first: %v", err)
	}
	second, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll second: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("length mismatch %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID || first[i].Seq != second[i].Seq || first[i].TS != second[i].TS {
			t.Fatalf("order differs at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestAppendSanitizesRemoteCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "remote", "add", "origin", "https://user:supersecrettoken@github.com/example/repo.git")
	runGit(t, root, "commit", "--allow-empty", "-m", "seed")

	ev, err := AppendEvent(root, TypeRuleCreated, samplePayload(), AppendOptions{})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if ev.Git.Remote == "" {
		t.Fatalf("expected a remote, got empty")
	}
	if strings.Contains(ev.Git.Remote, "supersecrettoken") || strings.Contains(ev.Git.Remote, "user:") {
		t.Fatalf("remote leaked credentials: %q", ev.Git.Remote)
	}
}

func TestAppendInRepoWithoutCommits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	runGit(t, root, "init")
	// No commit: HEAD is unborn.

	ev, err := AppendEvent(root, TypeRuleCreated, samplePayload(), AppendOptions{Now: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("append in commitless repo must succeed: %v", err)
	}
	if ev.Git.Hash != "" {
		t.Fatalf("expected empty git hash on unborn HEAD, got %q", ev.Git.Hash)
	}
	if ev.Seq != 1 {
		t.Fatalf("expected seq 1, got %d", ev.Seq)
	}

	// Shard must be correctly named even without commits.
	_, host, _, err := config.EnsureHost()
	if err != nil {
		t.Fatalf("ensure host: %v", err)
	}
	wantName := ShardName(host.HostID, time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC), root)
	wantPath := filepath.Join(store.EventsDir(root), wantName)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected shard %q to exist: %v", wantPath, err)
	}
}
