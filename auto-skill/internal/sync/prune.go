package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-skill/internal/ownership"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// ── ownership scanning (reusable by doctor / adopt, offline) ──────────────
//
// SEAM NOTE (differs from the T5 brief): the brief said TargetScan.Target should
// be the ABSOLUTE target dir path. But the merged T4 keys its receipts, installs
// and journal writes by the target STYLE name (Install.Target = Target.Name; see
// process.go / receipts.go). loadReceipts().Targets is therefore style-keyed, so
// keying TargetScan.Target by the absolute path would make ownership.Classify
// look receipts up under the wrong key and find none — every dir would fall to
// managed-unestablished and NOTHING would ever be prune-eligible (AC-1 would
// fail). To keep the deletion authority (receipts) consistent end-to-end we key
// the scan by the STYLE name, exactly matching loadReceipts().Targets. planPrune
// then takes the resolved targets to map a style name back to its absolute dir
// for the filesystem rename.

// DesiredSet returns the names sync would manage offline: authored skills (dirs
// under env.SkillsDir()) ∪ vendored skills (lock.Skills keys). doctor/adopt use
// it without running a full plan; the sync prune pass passes its own staged-set
// desired instead (the precise set sync is about to realize this run).
func DesiredSet(env skill.Env) (map[string]bool, error) {
	desired := map[string]bool{}

	// Authored: each child dir under ./skills is a managed authored skill.
	entries, err := os.ReadDir(env.SkillsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read authored skills dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && !isTempDirName(e.Name()) {
			desired[e.Name()] = true
		}
	}

	// Vendored: every lock entry name.
	lock, err := loadLock(env)
	if err != nil {
		return nil, err
	}
	for name := range lock.Skills {
		desired[name] = true
	}
	return desired, nil
}

// ScanOwnership builds the pure ownership.Inputs for env: it resolves the output
// targets, scans each target dir for child skill dirs (computing each one's
// on-disk tree digest with the same canonicalization sync uses), loads the
// on-disk manifest (the previously-managed set; nil if absent) and the
// machine-local receipts map (the deletion authority). TargetScan.Target is the
// target STYLE name — the key under which receipts/installs/journal are recorded.
func ScanOwnership(env skill.Env, desired map[string]bool) (ownership.Inputs, error) {
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		// best-effort: a missing/garbled skills.yaml just means default targets.
		syaml = nil
	}
	targets := resolveTargets(env, syaml)

	scans := make([]ownership.TargetScan, 0, len(targets))
	for _, t := range targets {
		ts := ownership.TargetScan{Target: t.Name}
		dents, derr := os.ReadDir(t.Dir)
		if derr != nil {
			if os.IsNotExist(derr) {
				scans = append(scans, ts)
				continue
			}
			return ownership.Inputs{}, fmt.Errorf("scan target %s (%s): %w", t.Name, t.Dir, derr)
		}
		for _, de := range dents {
			if !de.IsDir() || isTempDirName(de.Name()) {
				continue
			}
			child := filepath.Join(t.Dir, de.Name())
			digest, _, err := onDiskDigest(child)
			if err != nil {
				// Unreadable tree → empty digest matches no receipt → reported,
				// never deleted. Keep it visible rather than dropping it.
				digest = ""
			}
			ts.Dirs = append(ts.Dirs, ownership.ScannedDir{Name: de.Name(), Digest: digest, Exists: true})
		}
		scans = append(scans, ts)
	}

	return ownership.Inputs{
		Targets:  scans,
		Desired:  desired,
		Manifest: loadManifestBestEffort(env),
		Receipts: loadReceipts(env).Targets,
	}, nil
}

// isTempDirName reports whether a directory entry is one of the engine's own
// transient working dirs (stage / trash / journal) that must never be classified
// as a skill.
func isTempDirName(name string) bool {
	return strings.HasPrefix(name, ".sync-trash-") ||
		strings.HasPrefix(name, ".stage-") ||
		strings.HasPrefix(name, ".sync-journal")
}

// ── prune planning ────────────────────────────────────────────────────────

// planPrune returns the journalPrune entries for the receipt-gated orphans the
// commit may delete. It is EMPTY when desiredComplete is false — a failed fetch
// or any per-skill error never deletes anything (AC-3). Each orphan's absolute
// dir is resolved from the style name via the resolved targets.
//
// scope restricts which orphans may be pruned. On a full sync it is nil and
// every receipt-gated orphan is eligible. On a SCOPED (`--target X`) run it is
// the set of targeted skill names: only an orphan that was explicitly named may
// be reaped. A scoped plan stages only the targeted skills, so every OTHER
// managed skill classifies as a (false) orphan — without this gate a scoped
// `sync --target X` would delete every non-targeted skill's render (the full
// sync prune contract wrongly leaking into a partial run).
//
// (Deviates from the brief's planPrune(verdicts, desiredComplete) signature: it
// takes the resolved targets too, because ownership is keyed by style name and
// the filesystem rename needs the absolute dir — see the SEAM NOTE above.)
func planPrune(verdicts []ownership.DirStatus, targets []Target, desiredComplete bool, scope map[string]bool) []journalPrune {
	if !desiredComplete {
		return []journalPrune{}
	}
	dirOf := make(map[string]string, len(targets))
	for _, t := range targets {
		dirOf[t.Name] = t.Dir
	}
	var out []journalPrune
	for _, v := range ownership.PruneEligible(verdicts) {
		if scope != nil && !scope[v.Name] {
			// Scoped run: never reap a skill outside the --target set.
			continue
		}
		base, ok := dirOf[v.Target]
		if !ok {
			// A verdict for an unknown target style (should not happen, the scan
			// and the targets share resolveTargets) — skip rather than guess a path.
			continue
		}
		dir := filepath.Join(base, v.Name)
		out = append(out, journalPrune{
			Target: v.Target, // style name — the receipts key
			Skill:  v.Name,
			Dir:    dir, // absolute skill dir
			Trash:  filepath.Join(filepath.Dir(dir), ".sync-trash-prune-"+v.Name),
			Digest: v.OnDiskDigest,
		})
	}
	return out
}

// Conflict is a desired skill name landing on a foreign (un-manifested) dir of
// the same name in a target. It is reported with remediation and NEVER
// overwritten or pruned without --force (AC-4).
type Conflict struct {
	Target string `json:"target"` // target style name
	Skill  string `json:"skill"`
	Digest string `json:"digest,omitempty"` // on-disk digest of the foreign dir
}

// detectForeignCollisions returns a Conflict for every desired name that lands on
// a foreign dir of the same name in a target.
func detectForeignCollisions(desired map[string]bool, verdicts []ownership.DirStatus) []Conflict {
	var out []Conflict
	for _, v := range verdicts {
		if v.State == ownership.StateForeign && desired[v.Name] {
			out = append(out, Conflict{Target: v.Target, Skill: v.Name, Digest: v.OnDiskDigest})
		}
	}
	return out
}

// conflictMessage renders the remediation hint for a foreign collision.
func conflictMessage(c Conflict) string {
	return fmt.Sprintf(
		"conflict: desired skill %q collides with a foreign (un-managed) dir in target %q — "+
			"run `auto skill adopt %s`, rename the incoming skill with `--as`, or re-run with `--force` to overwrite",
		c.Skill, c.Target, c.Skill)
}
