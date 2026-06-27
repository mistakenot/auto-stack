// Package sync implements the native skill sync engine's network-planning half:
// phase A planning (dedupe, cache-satisfied skip, locked materialization, intent
// reconciliation), phase B parallel fetch (bounded worker pool behind the trust
// gate, isolated per-repo failure), and the float-then-render update engine.
//
// It drives 009's cache/transport/trust substrate and 032's lock/skills.yaml
// schemas; it never re-implements their logic. This half is independent of the
// render *output* — it works purely on the resolved work-list. Phases 4 and 5
// build the process/manifest/journal layers on top of the types defined here.
package sync

import (
	stdsync "sync"

	"github.com/mistakenot/auto-skill/internal/transport"
)

// DefaultJobs is the fetch worker-pool size when Options.Jobs is unset.
const DefaultJobs = 8

// Options configures a sync/update run. Phases 4 and 5 extend this struct with
// their process/journal fields; keep field names stable for those consumers.
type Options struct {
	Check          bool     // offline CI gate (sync): plan only, no network, write nothing
	Locked         bool     // force locked materialization (no float, even if auto_update)
	NoUpdate       bool     // do not advance floating specs this run
	Targets        []string // restrict to these skill names (empty = all)
	Jobs           int      // fetch worker-pool size (default DefaultJobs)
	AutoUpdate     bool     // float floating specs (effective skills.yaml auto_update)
	TrustRequested bool     // pass-through to the trust gate
	IsTTY          bool     // trust-gate interactive context
}

func (o Options) jobs() int {
	if o.Jobs > 0 {
		return o.Jobs
	}
	return DefaultJobs
}

// Action classifies what phase A decided for a single skill.
type Action string

const (
	// ActionUpToDate: the target commit's objects are already cached; no work.
	ActionUpToDate Action = "up_to_date"
	// ActionMaterialize: a pinned target commit is missing and must be fetched
	// (its exact objects) in phase B — no ref re-resolution.
	ActionMaterialize Action = "materialize"
	// ActionResolve: a floating spec was re-resolved to a new commit.
	ActionResolve Action = "resolve"
	// ActionIntentChanged: skills.yaml's version differs from the lock and
	// auto_update is off — reported, lock left untouched.
	ActionIntentChanged Action = "intent_changed"
	// ActionUnavailable: a pinned commit is gone upstream / unfetchable.
	ActionUnavailable Action = "unavailable"
	// ActionError: planning failed for this skill (bad URL, trust, open).
	ActionError Action = "error"
)

// SkillPlan is the resolved per-skill work item produced by phase A.
type SkillPlan struct {
	Name         string `json:"name"`
	Repo         string `json:"repo"` // dedupe key (canonical URL)
	URL          string `json:"url"`
	Subpath      string `json:"subpath,omitempty"`
	VersionSpec  string `json:"version_spec"`  // effective spec acted on (intent or lock)
	LockSpec     string `json:"lock_spec"`     // the lock's recorded version_spec
	LockedCommit string `json:"locked_commit"` // commit currently pinned in the lock
	TargetCommit string `json:"target_commit"` // commit after planning
	Action       Action `json:"action"`
	LockRewrite  bool   `json:"lock_rewrite,omitempty"` // lock entry should be rewritten
	Cached       bool   `json:"cached"`                 // target objects present offline
	Warning      string `json:"warning,omitempty"`      // e.g. tag force-move
	Message      string `json:"message,omitempty"`      // human note
	Err          error  `json:"-"`                      // set for error/unavailable actions
}

// RepoTarget is one distinct repo plus the commits phase B must materialize.
type RepoTarget struct {
	Key          string                  `json:"key"`
	URL          string                  `json:"url"`
	CanonicalURL string                  `json:"canonical_url"`
	CacheID      transport.CacheIdentity `json:"-"`
	Endpoint     string                  `json:"endpoint"`
	Commits      []string                `json:"commits"` // distinct pinned commits to ensure present
}

// Plan is phase A's resolved work-list. Skills is the full per-skill decision
// set; Repos is the distinct set of repos needing a phase-B fetch.
type Plan struct {
	Skills []SkillPlan  `json:"skills"`
	Repos  []RepoTarget `json:"repos"`
	Errors []error      `json:"-"` // planning-time errors (unavailable, trust, bad URL)
}

// HasErrors reports whether any planning-time error was collected.
func (p *Plan) HasErrors() bool { return len(p.Errors) > 0 }

// planMode is the internal resolution mode shared by Plan and Update.
type planMode struct {
	offline   bool // no network: do not clone or fetch (sync --check)
	floatRefs bool // re-resolve latest/branch: to newest upstream
	floatTags bool // also re-resolve tag: specs (explicit update only)
	rewrite   bool // mark lock entries for rewrite when the commit/spec changes
}

// boundedRun runs fn over items with at most jobs concurrent workers. The
// returned slice is index-aligned with items; a nil entry means success.
func boundedRun[T any](jobs int, items []T, fn func(T) error) []error {
	if jobs < 1 {
		jobs = 1
	}
	errs := make([]error, len(items))
	sem := make(chan struct{}, jobs)
	var wg stdsync.WaitGroup
	for i := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			errs[idx] = fn(items[idx])
		}(i)
	}
	wg.Wait()
	return errs
}
