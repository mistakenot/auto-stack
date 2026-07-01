package sync

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
	"github.com/mistakenot/auto-skill/internal/trust"
)

// RepoError records a single repo's phase-B failure (isolated; other repos in
// the same run still proceed).
type RepoError struct {
	Key string
	URL string
	Err error
}

func (e RepoError) Error() string { return fmt.Sprintf("%s: %v", e.URL, e.Err) }
func (e RepoError) Unwrap() error { return e.Err }

// FetchResult reports phase-B outcomes. A non-empty Failed makes `sync` exit
// non-zero while every successfully fetched repo still proceeds to phase C.
type FetchResult struct {
	Fetched []string    `json:"fetched"` // repo keys materialized successfully
	Failed  []RepoError `json:"failed,omitempty"`
}

// HasErrors reports whether any repo failed to fetch.
func (r *FetchResult) HasErrors() bool { return len(r.Failed) > 0 }

// Err aggregates the per-repo failures into one error (nil when all succeeded).
func (r *FetchResult) Err() error {
	if len(r.Failed) == 0 {
		return nil
	}
	msgs := make([]string, len(r.Failed))
	for i, f := range r.Failed {
		msgs[i] = f.Error()
	}
	return fmt.Errorf("%d repo(s) failed to fetch: %s", len(r.Failed), strings.Join(msgs, "; "))
}

// Fetch runs phase B: a bounded worker pool over the plan's distinct repos, each
// materialized through 009's cache behind the trust gate. Per-repo failures are
// isolated — valid repos still proceed and are reported in Fetched. Phase B is
// skipped entirely under --check (the caller plans offline and never calls this).
func Fetch(env skill.Env, plan *Plan, opts Options) (*FetchResult, error) {
	result := &FetchResult{}
	if plan == nil || len(plan.Repos) == 0 {
		return result, nil
	}
	if opts.Check {
		// --check is offline: phase B must never touch the network.
		return result, nil
	}

	c := cache.NewCache(env.UpstreamCacheDir()).WithTrace(opts.Trace)
	store := trust.NewStore(env.TrustPath())
	gate := &trust.Gate{Store: store}
	gio := trust.GateIO{IsTTY: opts.IsTTY, TrustRequested: opts.TrustRequested}

	// Load skills.yaml for the advisory trusted_hosts set (best effort).
	var trustedHosts []string
	if syaml, err := loadSkillsYAML(env); err == nil {
		trustedHosts = syaml.TrustedHosts
	}
	trace.Logf(opts.Trace, "sync fetch jobs=%d repos=%d trusted_hosts=%d", opts.jobs(), len(plan.Repos), len(trustedHosts))

	errs := boundedRun(opts.jobs(), plan.Repos, func(rt RepoTarget) error {
		done := trace.Spanf(opts.Trace, "sync fetch repo %s commits=%d", rt.Key, len(rt.Commits))
		err := fetchRepo(c, gate, gio, trustedHosts, rt, opts.Trace)
		if err != nil {
			done("error=%v", err)
		} else {
			done("")
		}
		return err
	})

	for i := range plan.Repos {
		rt := &plan.Repos[i]
		if errs[i] != nil {
			result.Failed = append(result.Failed, RepoError{Key: rt.Key, URL: rt.URL, Err: errs[i]})
		} else {
			result.Fetched = append(result.Fetched, rt.Key)
		}
	}
	sort.Strings(result.Fetched)
	sort.Slice(result.Failed, func(i, j int) bool { return result.Failed[i].Key < result.Failed[j].Key })
	return result, nil
}

// errPinnedUnavailable signals a pinned commit that is gone upstream.
var errPinnedUnavailable = errors.New("pinned commit unavailable upstream")

// fetchRepo materializes every pinned commit for one repo, taking the per-repo
// cache lock the cache already provides via Realize.
func fetchRepo(c *cache.Cache, gate *trust.Gate, gio trust.GateIO, trustedHosts []string, rt RepoTarget, tr *trace.Logger) error {
	done := trace.Spanf(tr, "sync fetch authorize %s", rt.Endpoint)
	if err := gate.Authorize(rt.Endpoint, trustedHosts, gio); err != nil {
		done("error=%v", err)
		return err
	}
	done("")
	repo, err := c.Open(rt.CacheID, rt.URL)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	for _, commit := range rt.Commits {
		// Realize fetches the exact pinned commit's objects (no ref
		// re-resolution). A fallback fetch may still leave the object absent if
		// it was GC'd / history-rewritten upstream — verify and fail clearly.
		if err := repo.Realize(commit); err != nil {
			return fmt.Errorf("%w: %s: %w", errPinnedUnavailable, short(commit), err)
		}
		if present, err := repo.CommitPresent(commit); err != nil || !present {
			return fmt.Errorf("%w: %s", errPinnedUnavailable, short(commit))
		}
		trace.Logf(tr, "sync fetch commit present repo=%s commit=%s", rt.Key, short(commit))
	}
	return nil
}
