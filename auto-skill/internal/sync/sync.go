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
	"fmt"
	stdsync "sync"

	"github.com/mistakenot/auto-skill/internal/ownership"
	"github.com/mistakenot/auto-skill/internal/skill"
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
	Force          bool     // overwrite a foreign-dir collision instead of refusing (AC-4)
	As             string   // TODO(phase 6): rename the incoming skill on a collision (--as); unused in phase 2
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

// ── orchestrator ────────────────────────────────────────────────────────

// StaleItem is one target/skill that `sync --check` found out of date. Reason
// is "stale_by_render" (on-disk tree digest differs from the expected
// skill_version, or the dir is absent) or "stale_by_intent" (skills.yaml's
// declared version differs from the lock and auto_update is off).
type StaleItem struct {
	Target string `json:"target,omitempty"`
	Skill  string `json:"skill"`
	Reason string `json:"reason"`
	OnDisk string `json:"on_disk,omitempty"`
	Want   string `json:"want,omitempty"`
}

// Result is the JSON-serializable outcome of a sync/check run. Phase 6's CLI
// emits it on stdout and maps it to an exit code via ExitCode(): any Errors, or
// (under --check) any Stale entry, exits non-zero; a token-budget overflow is a
// Warning only and exits zero.
type Result struct {
	Mode            string      `json:"mode"`  // "sync" | "check"
	Check           bool        `json:"check"` // offline dry-run gate
	Locked          bool        `json:"locked"`
	Recovered       bool        `json:"recovered"`        // a pending journal was recovered at startup
	DesiredComplete bool        `json:"desired_complete"` // false → pruning suppressed (failed fetch / errors)
	ScopedSkills    []string    `json:"scoped_skills,omitempty"`
	Plan            []SkillPlan `json:"plan"`
	ReposFetched    []string    `json:"repos_fetched,omitempty"`
	Installs        []Install   `json:"installs"`
	Written         []string    `json:"written,omitempty"`
	Skipped         []string    `json:"skipped,omitempty"`
	Pruned          []string    `json:"pruned,omitempty"`    // target/skill of each deleted orphan
	Conflicts       []Conflict  `json:"conflicts,omitempty"` // desired names colliding with foreign dirs
	Stale           []StaleItem `json:"stale,omitempty"`
	ManifestWritten bool        `json:"manifest_written"`
	LockRewritten   bool        `json:"lock_rewritten"`
	ReceiptsPath    string      `json:"receipts_path,omitempty"`
	Warnings        []string    `json:"warnings,omitempty"`
	Errors          []string    `json:"errors,omitempty"`
}

// ExitCode is the process exit code phase 6 applies: non-zero when any error
// was collected, or when --check found a stale target. A clean run (including
// one with only advisory warnings) returns 0.
func (r *Result) ExitCode() int {
	if len(r.Errors) > 0 {
		return 1
	}
	if r.Check && len(r.Stale) > 0 {
		return 1
	}
	return 0
}

// Run is the sync/update orchestrator. It recovers any pending journal, plans
// (phase A), fetches (phase B, skipped under --check), processes (phase C), then
// runs the journaled crash-consistent commit (skipped under --check). The error
// return is reserved for orchestration-level failures (unreadable config,
// recovery failure, manifest validation, a failed commit); per-skill / per-repo
// issues are collected in Result.Errors and drive the exit code, not the error.
func Run(env skill.Env, opts Options) (*Result, error) {
	// --target is a scoped partial/repair op: it implies --locked so it never
	// floats or advances the project-wide lock.
	if len(opts.Targets) > 0 {
		opts.Locked = true
	}

	result := &Result{
		Mode:         modeString(opts.Check),
		Check:        opts.Check,
		Locked:       opts.Locked,
		ScopedSkills: append([]string(nil), opts.Targets...),
	}

	// Startup recovery. Under --check (offline, writes nothing) we never run the
	// writing recovery; instead a pending journal is reported as an error so the
	// gate fails until a real `sync` reconciles it.
	if opts.Check {
		if journalPending(env) {
			result.Errors = append(result.Errors,
				"pending sync journal detected; run `auto skill sync` to recover before checking")
		}
	} else {
		recovered, err := recoverJournal(env)
		if err != nil {
			return result, fmt.Errorf("recover pending sync journal: %w", err)
		}
		result.Recovered = recovered
	}

	// Phase A — plan.
	plan, err := BuildPlan(env, opts)
	if err != nil {
		return result, err
	}
	result.Plan = plan.Skills
	for _, e := range plan.Errors {
		result.Errors = append(result.Errors, e.Error())
	}

	// Phase B — fetch (skipped under --check, which is offline).
	fetch := &FetchResult{}
	if !opts.Check {
		fetch, err = Fetch(env, plan, opts)
		if err != nil {
			return result, err
		}
		result.ReposFetched = append(result.ReposFetched, fetch.Fetched...)
		for _, f := range fetch.Failed {
			result.Errors = append(result.Errors, f.Error())
		}
	}

	// The desired set is complete only when nothing errored or failed to fetch;
	// an incomplete set suppresses pruning (recorded in the journal for T5).
	desiredComplete := !plan.HasErrors() && !fetch.HasErrors()

	// Phase C — process (pure: render + on-disk-digest compare, no writes).
	proc, err := Process(env, plan, fetch, opts)
	if err != nil {
		if proc != nil {
			for _, e := range proc.Errors {
				result.Errors = append(result.Errors, e.Error())
			}
		}
		return result, err
	}
	for _, e := range proc.Errors {
		result.Errors = append(result.Errors, e.Error())
		desiredComplete = false
	}
	result.Warnings = append(result.Warnings, proc.Warnings...)
	result.Installs = proc.Installs
	result.DesiredComplete = desiredComplete

	// ── ownership pass (T5): classify the on-disk dirs against the previously
	// managed set (the OLD on-disk manifest) + this machine's receipts, detect
	// foreign-dir collisions, and plan the receipt-gated orphan prunes. This is
	// read-only; the deletions ride the journaled commit below (or, under --check,
	// are merely reported). The desired set is exactly what sync WILL manage this
	// run — the staged skill names.
	desired := desiredSetFromStaged(proc.Staged)
	inputs, err := ScanOwnership(env, desired)
	if err != nil {
		return result, fmt.Errorf("scan target ownership: %w", err)
	}
	verdicts := ownership.Classify(inputs)
	conflicts := detectForeignCollisions(desired, verdicts)
	result.Conflicts = conflicts

	// AC-4: a desired name landing on a foreign dir is a hard refusal unless
	// --force overwrites it. Without --force we report the conflict, drop the
	// colliding install so the swap never touches the foreign dir, and treat the
	// desired set as unrealized → pruning is suppressed for this run.
	if len(conflicts) > 0 && !opts.Force {
		for _, c := range conflicts {
			result.Errors = append(result.Errors, conflictMessage(c))
			if !opts.Check {
				proc.Installs = removeInstall(proc.Installs, c.Target, c.Skill)
			}
		}
		desiredComplete = false
		result.DesiredComplete = desiredComplete
		result.Installs = proc.Installs
	}

	prunes := planPrune(verdicts, proc.Targets, desiredComplete)

	// --check — offline dry-run: report stale + would-be prunes, write nothing.
	if opts.Check {
		result.Pruned = prunedNames(prunes)
		result.Stale = computeStale(plan, proc)
		return result, nil
	}

	// Build the lock only when the plan marks a rewrite (a locked / --target run
	// never does — the "lock unchanged" contract).
	var lock *skill.Lock
	if planWantsLockRewrite(plan) {
		lock, err = buildUpdatedLock(env, plan)
		if err != nil {
			return result, fmt.Errorf("rebuild lock: %w", err)
		}
	}

	// Journaled commit.
	out, err := commit(commitInput{
		env:             env,
		installs:        proc.Installs,
		staged:          stagedByName(proc.Staged),
		manifest:        proc.Manifest,
		lock:            lock,
		prunes:          prunes,
		desiredComplete: desiredComplete,
	}, faultNone)
	if err != nil {
		return result, fmt.Errorf("journaled commit failed: %w", err)
	}
	result.Written = out.Written
	result.Skipped = out.Skipped
	result.Pruned = out.Pruned
	result.ManifestWritten = out.ManifestWritten
	result.LockRewritten = out.LockRewritten
	result.ReceiptsPath = out.ReceiptsPath
	return result, nil
}

func modeString(check bool) string {
	if check {
		return "check"
	}
	return "sync"
}

// computeStale derives the --check stale set: any would-be write is stale by
// render; any intent-changed plan entry is stale by intent.
func computeStale(plan *Plan, proc *ProcessResult) []StaleItem {
	var stale []StaleItem
	for _, inst := range proc.Installs {
		if inst.Action == InstallWrite {
			stale = append(stale, StaleItem{
				Target: inst.Target,
				Skill:  inst.Skill,
				Reason: "stale_by_render",
				OnDisk: inst.OnDisk,
				Want:   inst.Want,
			})
		}
	}
	for i := range plan.Skills {
		if plan.Skills[i].Action == ActionIntentChanged {
			stale = append(stale, StaleItem{Skill: plan.Skills[i].Name, Reason: "stale_by_intent"})
		}
	}
	return stale
}

// desiredSetFromStaged builds the set of skill names sync will manage this run —
// exactly the staged (rendered) skills. The prune pass classifies every other
// managed-but-not-desired dir as an orphan candidate.
func desiredSetFromStaged(staged []*StagedSkill) map[string]bool {
	out := make(map[string]bool, len(staged))
	for _, s := range staged {
		out[s.Name] = true
	}
	return out
}

// removeInstall returns installs with the (target style, skill) entry dropped, so
// a refused foreign-dir collision never reaches the swap.
func removeInstall(installs []Install, target, name string) []Install {
	out := installs[:0:0]
	for _, in := range installs {
		if in.Target == target && in.Skill == name {
			continue
		}
		out = append(out, in)
	}
	return out
}

// prunedNames renders the planned prunes as "target/skill" strings (used for the
// --check would-be-prune report; the committed run reports the commit outcome).
func prunedNames(prunes []journalPrune) []string {
	if len(prunes) == 0 {
		return nil
	}
	out := make([]string, 0, len(prunes))
	for _, p := range prunes {
		out = append(out, p.Target+"/"+p.Skill)
	}
	return out
}

func stagedByName(staged []*StagedSkill) map[string]*StagedSkill {
	out := make(map[string]*StagedSkill, len(staged))
	for _, s := range staged {
		out[s.Name] = s
	}
	return out
}

// planWantsLockRewrite reports whether any planned skill floated to a new commit
// the plan marked for a lock rewrite.
func planWantsLockRewrite(plan *Plan) bool {
	for i := range plan.Skills {
		if plan.Skills[i].LockRewrite {
			return true
		}
	}
	return false
}

// buildUpdatedLock applies the plan's resolved commits onto the existing lock
// for the entries marked LockRewrite, leaving every other entry byte-stable.
func buildUpdatedLock(env skill.Env, plan *Plan) (*skill.Lock, error) {
	lock, err := loadLock(env)
	if err != nil {
		return nil, err
	}
	for i := range plan.Skills {
		sp := plan.Skills[i]
		if !sp.LockRewrite {
			continue
		}
		entry, ok := lock.Skills[sp.Name]
		if !ok {
			continue
		}
		entry.Commit = sp.TargetCommit
		entry.VersionSpec = sp.VersionSpec
		lock.Skills[sp.Name] = entry
	}
	return lock, nil
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
