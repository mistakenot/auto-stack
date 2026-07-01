package ownership

import (
	"strconv"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
	"pgregory.net/rapid"
)

// inputsGen produces valid, internally-consistent Inputs for Classify.
//
// Construction rules (construct-don't-reject) guaranteeing validity:
//   - Targets: 1-3 targets keyed by DISTINCT absolute paths ("/wt-N/.claude/skills"),
//     so every (Target, Name) verdict key is unique.
//   - Dirs within a target: a DISTINCT subset drawn from a small skill-name
//     alphabet, so a name never repeats inside one target (again, unique keys).
//   - Digests: drawn from a tiny set {"d1","d2","d3"} so receipt/on-disk matches
//     and mismatches both happen with meaningful probability, exercising every
//     branch of the decision table.
//   - Receipts: keyed by the SAME absolute target path as the TargetScan (the
//     deletion-authority keying the real code relies on); each dir independently
//     may or may not have a receipt, and when present its digest is drawn from the
//     same tiny set (so ~1/3 match).
//   - Manifest: nil ~half the time (=> nothing managed => all foreign); otherwise
//     a manifest whose managed union is a distinct subset of the alphabet, matching
//     the single-target "claude" union idiom used by production sync + the existing
//     table test.
//   - Desired: a distinct subset of the alphabet.
//
// The name/manifest/desired/receipt sets are drawn independently over the SAME
// small alphabet, so overlaps and disjointness both occur, reaching all five
// states across a run.
func inputsGen(rt *rapid.T) Inputs {
	names := []string{"alpha", "beta", "gamma", "delta", "foo", "bar"}
	digests := []string{"d1", "d2", "d3"}
	id := func(s string) string { return s }

	nTargets := rapid.IntRange(1, 3).Draw(rt, "ntargets")
	targets := make([]TargetScan, nTargets)
	receipts := map[string]map[string]string{}

	for i := range nTargets {
		tp := "/wt-" + strconv.Itoa(i) + "/.claude/skills"
		dirNames := rapid.SliceOfNDistinct(rapid.SampledFrom(names), 0, len(names), id).
			Draw(rt, "dirs"+strconv.Itoa(i))

		dirs := make([]ScannedDir, len(dirNames))
		rmap := map[string]string{}
		for j, n := range dirNames {
			dirs[j] = ScannedDir{
				Name:   n,
				Digest: rapid.SampledFrom(digests).Draw(rt, "ondisk"+strconv.Itoa(i)+"_"+n),
				Exists: true,
			}
			if rapid.Bool().Draw(rt, "hasreceipt"+strconv.Itoa(i)+"_"+n) {
				rmap[n] = rapid.SampledFrom(digests).Draw(rt, "receipt"+strconv.Itoa(i)+"_"+n)
			}
		}
		targets[i] = TargetScan{Target: tp, Dirs: dirs}
		if len(rmap) > 0 {
			receipts[tp] = rmap
		}
	}

	var manifest *skill.Manifest
	if rapid.Bool().Draw(rt, "hasmanifest") {
		managedNames := rapid.SliceOfNDistinct(rapid.SampledFrom(names), 0, len(names), id).
			Draw(rt, "managed")
		managed := map[string]string{}
		skills := map[string]skill.ManifestSkill{}
		for _, n := range managedNames {
			v := "v-" + n
			managed[n] = v
			skills[n] = skill.ManifestSkill{SkillVersion: v}
		}
		manifest = &skill.Manifest{
			Skills:  skills,
			Targets: map[string]skill.ManifestTarget{"claude": {ManagedSkills: managed}},
		}
	}

	desiredNames := rapid.SliceOfNDistinct(rapid.SampledFrom(names), 0, len(names), id).
		Draw(rt, "desired")
	desired := map[string]bool{}
	for _, n := range desiredNames {
		desired[n] = true
	}

	return Inputs{Targets: targets, Desired: desired, Manifest: manifest, Receipts: receipts}
}

// validStates is the closed set of ownership labels Classify may emit.
var validStates = map[State]bool{
	StateManagedCurrent:       true,
	StateManagedOrphan:        true,
	StateManagedUnestablished: true,
	StateModified:             true,
	StateForeign:              true,
}

// hasReceipt reports whether the input carries a local receipt for (target, name)
// — the deletion authority. Derived from the raw input map so the test never
// depends on Classify's own bookkeeping.
func hasReceipt(in Inputs, target, name string) bool {
	m, ok := in.Receipts[target]
	if !ok {
		return false
	}
	_, ok = m[name]
	return ok
}

// classifyWith mirrors Classify's traversal but delegates the per-dir decision to
// oneFn, so kill tests can inject a single defect into the pure decision table
// without touching production code. Unsorted (kill tests key by target+name).
func classifyWith(in Inputs, oneFn func(managed, desired, has bool, onDisk, receipt string) State) []DirStatus {
	managed := managedUnion(in.Manifest)
	var out []DirStatus
	for _, ts := range in.Targets {
		receipts := in.Receipts[ts.Target]
		for _, dir := range ts.Dirs {
			rd, has := "", false
			if receipts != nil {
				rd, has = receipts[dir.Name]
			}
			out = append(out, DirStatus{
				Target:          ts.Target,
				Name:            dir.Name,
				OnDiskDigest:    dir.Digest,
				ReceiptDigest:   rd,
				ExpectedVersion: expectedVersion(in.Manifest, dir.Name),
				State:           oneFn(managed[dir.Name], in.Desired[dir.Name], has, dir.Digest, rd),
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Property 1: Totality — Classify emits exactly one verdict per scanned (target,
// dir), with no duplicate (target, name) key and only valid state labels.
// ---------------------------------------------------------------------------

func TestClassifyProp_Totality(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)

		total := 0
		for _, ts := range in.Targets {
			total += len(ts.Dirs)
		}

		got := Classify(in)
		if len(got) != total {
			t.Fatalf("verdict count = %d, want %d (one per scanned dir)", len(got), total)
		}

		seen := map[string]bool{}
		for _, v := range got {
			key := v.Target + "\x00" + v.Name
			if seen[key] {
				t.Fatalf("duplicate verdict for key %q", key)
			}
			seen[key] = true
			if !validStates[v.State] {
				t.Fatalf("invalid state %q for %q", v.State, key)
			}
		}
	})
}

// TestClassifyProp_Totality_Kill: a classify that duplicates a verdict must be
// caught by the "no duplicate key" invariant.
func TestClassifyProp_Totality_Kill(t *testing.T) {
	buggy := func(in Inputs) []DirStatus {
		v := Classify(in)
		if len(v) > 0 {
			v = append(v, v[0]) // BUG: emits a duplicate (target, name) verdict.
		}
		return v
	}

	found := false
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		seen := map[string]bool{}
		for _, v := range buggy(in) {
			key := v.Target + "\x00" + v.Name
			if seen[key] {
				found = true
				return
			}
			seen[key] = true
		}
	})
	if !found {
		t.Fatal("kill test failed: duplicate-verdict bug never triggered the totality invariant")
	}
}

// ---------------------------------------------------------------------------
// Property 2: PruneSafety — everything the code treats as prune-eligible
// (PruneEligible) is StateManagedOrphan and nothing else.
// ---------------------------------------------------------------------------

func TestClassifyProp_PruneSafety(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range PruneEligible(Classify(in)) {
			if v.State != StateManagedOrphan {
				t.Fatalf("prune-eligible verdict has state %q, want %q (target=%q name=%q)",
					v.State, StateManagedOrphan, v.Target, v.Name)
			}
			// Deeper guarantee: an orphan is managed, NOT desired, and its on-disk
			// digest equals the local receipt digest.
			if in.Desired[v.Name] {
				t.Fatalf("prune-eligible verdict is in the desired set: %q", v.Name)
			}
			if !hasReceipt(in, v.Target, v.Name) {
				t.Fatalf("prune-eligible verdict lacks a receipt: target=%q name=%q", v.Target, v.Name)
			}
			if v.OnDiskDigest != v.ReceiptDigest {
				t.Fatalf("prune-eligible verdict on-disk %q != receipt %q", v.OnDiskDigest, v.ReceiptDigest)
			}
		}
	})
}

// TestClassifyProp_PruneSafety_Kill: a prune filter that also admits StateModified
// must be caught by the "only orphans are prune-eligible" invariant.
func TestClassifyProp_PruneSafety_Kill(t *testing.T) {
	buggyPrune := func(verdicts []DirStatus) []DirStatus {
		var out []DirStatus
		for _, v := range verdicts {
			if v.State == StateManagedOrphan || v.State == StateModified { // BUG: modified is not deletable.
				out = append(out, v)
			}
		}
		return out
	}

	found := false
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range buggyPrune(Classify(in)) {
			if v.State != StateManagedOrphan {
				found = true
				return
			}
		}
	})
	if !found {
		t.Fatal("kill test failed: modified-in-prune bug never surfaced a non-orphan verdict")
	}
}

// ---------------------------------------------------------------------------
// Property 3: ForeignExclusivity — a dir is StateForeign iff its name is not in
// the manifest's managed union.
// ---------------------------------------------------------------------------

func TestClassifyProp_ForeignExclusivity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		managed := managedUnion(in.Manifest)
		for _, v := range Classify(in) {
			isForeign := v.State == StateForeign
			if isForeign == managed[v.Name] {
				t.Fatalf("foreign/managed mismatch: name=%q managed=%v state=%q",
					v.Name, managed[v.Name], v.State)
			}
		}
	})
}

// TestClassifyProp_ForeignExclusivity_Kill: a classifier that labels unmanaged
// dirs StateModified (instead of foreign) must violate the iff invariant.
func TestClassifyProp_ForeignExclusivity_Kill(t *testing.T) {
	buggyOne := func(managed, desired, has bool, onDisk, receipt string) State {
		if !managed {
			return StateModified // BUG: unmanaged must be foreign.
		}
		return classifyOne(managed, desired, has, onDisk, receipt)
	}

	found := false
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		managed := managedUnion(in.Manifest)
		for _, v := range classifyWith(in, buggyOne) {
			if (v.State == StateForeign) == managed[v.Name] {
				found = true
				return
			}
		}
	})
	if !found {
		t.Fatal("kill test failed: unmanaged-as-modified bug never broke foreign exclusivity")
	}
}

// ---------------------------------------------------------------------------
// Property 4: DesiredNeverOrphaned — a dir in the desired set is never
// StateManagedOrphan (prune must never touch something we still want).
// ---------------------------------------------------------------------------

func TestClassifyProp_DesiredNeverOrphaned(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range Classify(in) {
			if in.Desired[v.Name] && v.State == StateManagedOrphan {
				t.Fatalf("desired dir %q classified as orphan", v.Name)
			}
		}
	})
}

// TestClassifyProp_DesiredNeverOrphaned_Kill: a classifier that ignores the
// desired flag when deciding orphan status must orphan a desired dir.
func TestClassifyProp_DesiredNeverOrphaned_Kill(t *testing.T) {
	buggyOne := func(managed, desired, has bool, onDisk, receipt string) State {
		return classifyOne(managed, false, has, onDisk, receipt) // BUG: desired forced false.
	}

	found := false
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range classifyWith(in, buggyOne) {
			if in.Desired[v.Name] && v.State == StateManagedOrphan {
				found = true
				return
			}
		}
	})
	if !found {
		t.Fatal("kill test failed: desired-ignored bug never orphaned a desired dir")
	}
}

// ---------------------------------------------------------------------------
// Property 5: ReceiptNecessity — without a local receipt, a dir is never
// StateManagedOrphan and never StateModified (both require the receipt authority).
// ---------------------------------------------------------------------------

func TestClassifyProp_ReceiptNecessity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range Classify(in) {
			if hasReceipt(in, v.Target, v.Name) {
				continue
			}
			if v.State == StateManagedOrphan || v.State == StateModified {
				t.Fatalf("receipt-less dir %q got state %q (needs a receipt)", v.Name, v.State)
			}
		}
	})
}

// TestClassifyProp_ReceiptNecessity_Kill: a classifier that treats a missing
// receipt as StateModified must violate the invariant.
func TestClassifyProp_ReceiptNecessity_Kill(t *testing.T) {
	buggyOne := func(managed, desired, has bool, onDisk, receipt string) State {
		if !managed {
			return StateForeign
		}
		if !has {
			return StateModified // BUG: no receipt must be unestablished, never modified.
		}
		return classifyOne(managed, desired, has, onDisk, receipt)
	}

	found := false
	rapid.Check(t, func(rt *rapid.T) {
		in := inputsGen(rt)
		for _, v := range classifyWith(in, buggyOne) {
			if hasReceipt(in, v.Target, v.Name) {
				continue
			}
			if v.State == StateManagedOrphan || v.State == StateModified {
				found = true
				return
			}
		}
	})
	if !found {
		t.Fatal("kill test failed: no-receipt-as-modified bug never violated receipt necessity")
	}
}
