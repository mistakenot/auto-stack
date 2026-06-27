package sync

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestBoundedRunConcurrency: the worker pool never exceeds the job bound.
func TestBoundedRunConcurrency(t *testing.T) {
	const jobs = 2
	items := make([]int, 12)
	var cur, max int32
	errs := boundedRun(jobs, items, func(int) error {
		n := atomic.AddInt32(&cur, 1)
		for {
			old := atomic.LoadInt32(&max)
			if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		return nil
	})
	if len(errs) != len(items) {
		t.Fatalf("expected %d results, got %d", len(items), len(errs))
	}
	if max > jobs {
		t.Fatalf("concurrency %d exceeded job bound %d", max, jobs)
	}
	if max < 2 {
		t.Fatalf("expected real parallelism, max was %d", max)
	}
}

// TestBoundedRunIsolatesErrors: one failing item does not stop the others.
func TestBoundedRunIsolatesErrors(t *testing.T) {
	items := []int{0, 1, 2, 3}
	errs := boundedRun(3, items, func(i int) error {
		if i == 2 {
			return errors.New("boom")
		}
		return nil
	})
	failed := 0
	for _, e := range errs {
		if e != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected exactly 1 failure, got %d", failed)
	}
}

// TestFetchIsolatedFailure: one repo failing does not abort the run; the valid
// repo still processes and the run reports failure.
func TestFetchIsolatedFailure(t *testing.T) {
	good := newFixture(t)
	head := good.commitSkill("alpha", "v1")
	bad := newFixture(t)
	bad.commitSkill("beta", "v1")

	env := newEnv(t)
	approve(t, env, good.url)
	approve(t, env, bad.url)

	goodCanon, goodID, goodEP := mustCanonical(t, good.url)
	badCanon, badID, badEP := mustCanonical(t, bad.url)
	gone := "0123456789abcdef0123456789abcdef01234567"

	plan := &Plan{Repos: []RepoTarget{
		{Key: goodCanon, URL: good.url, CanonicalURL: goodCanon, CacheID: goodID, Endpoint: goodEP, Commits: []string{head}},
		{Key: badCanon, URL: bad.url, CanonicalURL: badCanon, CacheID: badID, Endpoint: badEP, Commits: []string{gone}},
	}}

	res, err := Fetch(env, plan, Options{Jobs: 4})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Fetched) != 1 || res.Fetched[0] != goodCanon {
		t.Fatalf("expected the good repo fetched, got %v", res.Fetched)
	}
	if len(res.Failed) != 1 || res.Failed[0].Key != badCanon {
		t.Fatalf("expected the bad repo failed, got %v", res.Failed)
	}
	if !res.HasErrors() || res.Err() == nil {
		t.Fatal("expected the run to report failure")
	}
}

// TestFetchCheckNoNetwork: --check skips phase B entirely (offline), so even a
// deleted upstream does not cause a network error.
func TestFetchCheckNoNetwork(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "v1")
	canon, id, ep := mustCanonical(t, f.url)
	f.remove() // upstream gone

	env := newEnv(t)
	approve(t, env, f.url)
	plan := &Plan{Repos: []RepoTarget{
		{Key: canon, URL: f.url, CanonicalURL: canon, CacheID: id, Endpoint: ep, Commits: []string{head}},
	}}

	res, err := Fetch(env, plan, Options{Check: true})
	if err != nil {
		t.Fatalf("Fetch --check: %v", err)
	}
	if res.HasErrors() || len(res.Fetched) != 0 {
		t.Fatalf("--check must do no network: %+v", res)
	}
}

// TestFetchTrustFailClosed: an unapproved endpoint fails closed (non-TTY).
func TestFetchTrustFailClosed(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "v1")
	canon, id, ep := mustCanonical(t, f.url)

	env := newEnv(t) // no approval
	plan := &Plan{Repos: []RepoTarget{
		{Key: canon, URL: f.url, CanonicalURL: canon, CacheID: id, Endpoint: ep, Commits: []string{head}},
	}}

	res, err := Fetch(env, plan, Options{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected a trust failure for an unapproved endpoint")
	}
}
