// Package ownership is the pure, I/O-free home of the three-part deletion
// authority that gates every prune / adopt / doctor decision.
//
// Architecture: this package is a LEAF. It imports only internal/skill (for the
// *skill.Manifest value type) and stdlib. It MUST NOT import internal/sync —
// internal/sync's prune pass imports this package, so the reverse would create a
// cycle. Consequently receipts arrive as a plain map[string]map[string]string
// (target → name → digest) rather than the sync.Receipts type, and on-disk tree
// digests are computed by the caller (via render.ComputeSkillVersion) and passed
// in. Classify itself performs ZERO I/O, which keeps the guard-rail matrix a
// table-driven unit test.
package ownership

import (
	"sort"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// State is the ownership label for one (target, dir). Exactly one applies.
type State string

const (
	// StateManagedCurrent: managed, desired, and the on-disk dir matches the
	// receipt digest. Nothing to do.
	StateManagedCurrent State = "managed-current"
	// StateManagedOrphan: managed, NOT desired, and the on-disk dir matches the
	// receipt this machine wrote. The ONLY prune-eligible state.
	StateManagedOrphan State = "managed-orphan"
	// StateManagedUnestablished: a manifest entry exists but this machine has no
	// local receipt for it (e.g. a manifest row introduced by someone's commit).
	// Report only — never delete.
	StateManagedUnestablished State = "managed-unestablished"
	// StateModified: a receipt exists but the on-disk dir digest has drifted from
	// it (locally edited). Report only — never delete.
	StateModified State = "modified"
	// StateForeign: the name is not in the manifest's managed set at all. Not
	// managed by us → adoptable, never prune-eligible.
	StateForeign State = "foreign"
)

// ScannedDir is one directory found in a target, with its computed on-disk tree
// digest. Digest is the render.ComputeSkillVersion of the dir's canonical tree,
// supplied by the caller; it is "" when the dir was unreadable.
type ScannedDir struct {
	Name   string
	Digest string
	Exists bool
}

// TargetScan is one output target (keyed by its ABSOLUTE dir path — the receipts
// key) together with the dirs found in it.
type TargetScan struct {
	Target string // absolute target skills dir path (the receipts key)
	Dirs   []ScannedDir
}

// Inputs are the pure inputs to Classify.
type Inputs struct {
	Targets  []TargetScan                 // scanned targets keyed by absolute path
	Desired  map[string]bool              // names that SHOULD exist after sync
	Manifest *skill.Manifest              // declares which names are managed; may be nil
	Receipts map[string]map[string]string // target(abs path) → name → digest (deletion authority)
}

// DirStatus is the per-(target, dir) verdict, carrying the digests that justified
// the label so reporting (doctor) can explain it.
type DirStatus struct {
	Target          string
	Name            string
	State           State
	OnDiskDigest    string
	ReceiptDigest   string // "" when no receipt
	ExpectedVersion string // manifest's expected skill_version for this name, "" if none
}

// managedUnion returns the set of skill names managed by ANY target in the
// manifest.
//
// Seam note: the manifest's Targets map is keyed by target STYLE name (e.g.
// "claude"), but TargetScan.Target and Receipts are keyed by ABSOLUTE path.
// Phase 1 is I/O-free and cannot resolve style → path on its own, so it does NOT
// attempt to. Instead "managed" is the UNION of every Manifest.Targets[*].
// ManagedSkills. This is correct because sync writes the full managed union into
// every target (see internal/sync buildManifest: every target receives the same
// managed map), so a name managed for one target is managed for all.
func managedUnion(m *skill.Manifest) map[string]bool {
	union := map[string]bool{}
	if m == nil {
		return union
	}
	for _, mt := range m.Targets {
		for name := range mt.ManagedSkills {
			union[name] = true
		}
	}
	return union
}

// Classify applies the three-part deletion authority to every scanned dir and
// returns the verdicts sorted deterministically by (Target, Name).
//
// A dir is StateManagedOrphan (the only prune-eligible state) IFF all three hold:
//  1. the name is in the manifest's managed union but NOT in the desired set, AND
//  2. a local receipt records a digest for (target, name), AND
//  3. the on-disk dir digest == that receipt digest.
//
// Miss any one and the dir is reported (modified / unestablished / foreign),
// never deleted. A forged in-file metadata.auto_skill stamp authorizes nothing:
// the digest is computed over raw bytes including the stamp, so a forged stamp
// simply makes the on-disk digest match no receipt → modified/unestablished, and
// a name absent from the manifest union is foreign regardless of its stamp.
func Classify(in Inputs) []DirStatus {
	managed := managedUnion(in.Manifest)

	var out []DirStatus
	for _, ts := range in.Targets {
		receipts := in.Receipts[ts.Target]
		for _, dir := range ts.Dirs {
			ds := DirStatus{
				Target:          ts.Target,
				Name:            dir.Name,
				OnDiskDigest:    dir.Digest,
				ExpectedVersion: expectedVersion(in.Manifest, dir.Name),
			}

			receiptDigest, hasReceipt := "", false
			if receipts != nil {
				receiptDigest, hasReceipt = receipts[dir.Name]
			}
			ds.ReceiptDigest = receiptDigest

			ds.State = classifyOne(managed[dir.Name], in.Desired[dir.Name], hasReceipt, dir.Digest, receiptDigest)
			out = append(out, ds)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// classifyOne is the pure decision table for a single dir.
func classifyOne(managed, desired, hasReceipt bool, onDisk, receiptDigest string) State {
	if !managed {
		return StateForeign
	}
	if desired {
		switch {
		case hasReceipt && onDisk == receiptDigest:
			return StateManagedCurrent
		case hasReceipt: // digest drifted
			return StateModified
		default:
			return StateManagedUnestablished
		}
	}
	// managed AND NOT desired — orphan candidate.
	switch {
	case hasReceipt && onDisk == receiptDigest:
		return StateManagedOrphan
	case hasReceipt: // digest mismatch — locally modified, report only
		return StateModified
	default:
		return StateManagedUnestablished
	}
}

// expectedVersion returns the manifest's expected skill_version for name, or "".
func expectedVersion(m *skill.Manifest, name string) string {
	if m == nil {
		return ""
	}
	if ms, ok := m.Skills[name]; ok {
		return ms.SkillVersion
	}
	return ""
}

// PruneEligible filters verdicts to StateManagedOrphan only — exactly what the
// sync prune pass is permitted to delete.
func PruneEligible(verdicts []DirStatus) []DirStatus {
	return filterState(verdicts, StateManagedOrphan)
}

// Adoptable filters verdicts to StateForeign only — what doctor lists and adopt
// consumes.
func Adoptable(verdicts []DirStatus) []DirStatus {
	return filterState(verdicts, StateForeign)
}

func filterState(verdicts []DirStatus, want State) []DirStatus {
	var out []DirStatus
	for _, v := range verdicts {
		if v.State == want {
			out = append(out, v)
		}
	}
	return out
}
