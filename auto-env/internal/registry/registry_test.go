package registry

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAddAndList(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}
	entry := Entry{
		RepoRoot:   "/home/user/project",
		Branch:     "main",
		BranchSlug: "main",
		Slot:       0,
		Ports:      map[string]int{"api": 3000, "web": 3001},
		Files:      []string{"docker-compose.yml"},
		CreatedAt:  time.Now().Truncate(time.Second),
	}

	if err := reg.Add(&entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	got := entries[0]
	if got.RepoRoot != entry.RepoRoot {
		t.Errorf("RepoRoot = %q, want %q", got.RepoRoot, entry.RepoRoot)
	}
	if got.Branch != entry.Branch {
		t.Errorf("Branch = %q, want %q", got.Branch, entry.Branch)
	}
	if got.Slot != entry.Slot {
		t.Errorf("Slot = %d, want %d", got.Slot, entry.Slot)
	}
	if len(got.Ports) != 2 || got.Ports["api"] != 3000 {
		t.Errorf("Ports = %v, want %v", got.Ports, entry.Ports)
	}
	if len(got.Files) != 1 || got.Files[0] != "docker-compose.yml" {
		t.Errorf("Files = %v, want %v", got.Files, entry.Files)
	}
}

func TestAddUpsert(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}
	entry := Entry{
		RepoRoot:   "/home/user/project",
		Branch:     "main",
		BranchSlug: "main",
		Slot:       0,
		CreatedAt:  time.Now(),
	}

	if err := reg.Add(&entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entry.Branch = "feat/new"
	entry.BranchSlug = "feat-new"
	entry.Slot = 42
	if err := reg.Add(&entry); err != nil {
		t.Fatalf("Add upsert: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", len(entries))
	}
	if entries[0].Branch != "feat/new" {
		t.Errorf("Branch = %q, want %q", entries[0].Branch, "feat/new")
	}
	if entries[0].Slot != 42 {
		t.Errorf("Slot = %d, want 42", entries[0].Slot)
	}
}

func TestRemove(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}

	e1 := Entry{RepoRoot: "/project-a", Branch: "main", CreatedAt: time.Now()}
	e2 := Entry{RepoRoot: "/project-b", Branch: "dev", CreatedAt: time.Now()}

	if err := reg.Add(&e1); err != nil {
		t.Fatalf("Add e1: %v", err)
	}
	if err := reg.Add(&e2); err != nil {
		t.Fatalf("Add e2: %v", err)
	}

	if err := reg.Remove("/project-a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].RepoRoot != "/project-b" {
		t.Errorf("remaining entry RepoRoot = %q, want /project-b", entries[0].RepoRoot)
	}
}

func TestRemoveNoOp(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}

	if err := reg.Remove("/nonexistent"); err != nil {
		t.Fatalf("Remove nonexistent: %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestConcurrentAdd(t *testing.T) {
	reg := &Registry{Dir: t.TempDir()}
	n := 20

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entry := Entry{
				RepoRoot:  fmt.Sprintf("/project-%d", i),
				Branch:    "main",
				CreatedAt: time.Now(),
			}
			if err := reg.Add(&entry); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent Add error: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != n {
		t.Errorf("expected %d entries, got %d", n, len(entries))
	}
}
