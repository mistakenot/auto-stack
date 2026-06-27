package ownership

import (
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// manifestWith builds a manifest whose named skills are managed by a single
// "claude" target (the union is all we depend on) and carry the given expected
// skill_versions.
func manifestWith(versions map[string]string) *skill.Manifest {
	managed := map[string]string{}
	skills := map[string]skill.ManifestSkill{}
	for name, ver := range versions {
		managed[name] = ver
		skills[name] = skill.ManifestSkill{SkillVersion: ver}
	}
	return &skill.Manifest{
		Skills:  skills,
		Targets: map[string]skill.ManifestTarget{"claude": {ManagedSkills: managed}},
	}
}

const tgt = "/abs/.claude/skills"

func TestClassify_StateMatrix(t *testing.T) {
	tests := []struct {
		name string
		// dir under test
		dirName   string
		onDisk    string
		desired   bool
		managed   bool   // include dirName in manifest union
		receipt   string // "" => no receipt
		hasRcpt   bool
		wantState State
	}{
		{
			name:      "managed+receipt+match+desired => current",
			dirName:   "alpha",
			onDisk:    "d1",
			desired:   true,
			managed:   true,
			receipt:   "d1",
			hasRcpt:   true,
			wantState: StateManagedCurrent,
		},
		{
			name:      "managed+receipt+match+NOT desired => orphan",
			dirName:   "alpha",
			onDisk:    "d1",
			desired:   false,
			managed:   true,
			receipt:   "d1",
			hasRcpt:   true,
			wantState: StateManagedOrphan,
		},
		{
			name:      "managed+receipt+mismatch+NOT desired => modified",
			dirName:   "alpha",
			onDisk:    "d2",
			desired:   false,
			managed:   true,
			receipt:   "d1",
			hasRcpt:   true,
			wantState: StateModified,
		},
		{
			name:      "managed+NO receipt+NOT desired => unestablished",
			dirName:   "alpha",
			onDisk:    "d1",
			desired:   false,
			managed:   true,
			hasRcpt:   false,
			wantState: StateManagedUnestablished,
		},
		{
			name:      "managed+receipt+mismatch+desired => modified",
			dirName:   "alpha",
			onDisk:    "d2",
			desired:   true,
			managed:   true,
			receipt:   "d1",
			hasRcpt:   true,
			wantState: StateModified,
		},
		{
			name:      "managed+NO receipt+desired => unestablished",
			dirName:   "alpha",
			onDisk:    "d1",
			desired:   true,
			managed:   true,
			hasRcpt:   false,
			wantState: StateManagedUnestablished,
		},
		{
			name:      "NOT in manifest => foreign",
			dirName:   "stranger",
			onDisk:    "d9",
			desired:   false,
			managed:   false,
			hasRcpt:   false,
			wantState: StateForeign,
		},
		{
			// forged in-file stamp simulation: the dir is in the manifest union
			// but its on-disk digest matches NO receipt (the stamp the forger
			// added perturbs the raw bytes -> digest). Must never be orphan.
			name:      "forged stamp, managed, receipt mismatch => modified never orphan",
			dirName:   "alpha",
			onDisk:    "forged-digest",
			desired:   false,
			managed:   true,
			receipt:   "real-digest",
			hasRcpt:   true,
			wantState: StateModified,
		},
		{
			// forged stamp where there is no receipt at all -> unestablished.
			name:      "forged stamp, managed, no receipt => unestablished never orphan",
			dirName:   "alpha",
			onDisk:    "forged-digest",
			desired:   false,
			managed:   true,
			hasRcpt:   false,
			wantState: StateManagedUnestablished,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var manifest *skill.Manifest
			if tc.managed {
				manifest = manifestWith(map[string]string{tc.dirName: "v-" + tc.dirName})
			} else {
				// a non-empty manifest that does NOT contain dirName.
				manifest = manifestWith(map[string]string{"other": "v-other"})
			}

			receipts := map[string]map[string]string{}
			if tc.hasRcpt {
				receipts[tgt] = map[string]string{tc.dirName: tc.receipt}
			}

			in := Inputs{
				Targets: []TargetScan{{
					Target: tgt,
					Dirs:   []ScannedDir{{Name: tc.dirName, Digest: tc.onDisk, Exists: true}},
				}},
				Desired:  map[string]bool{},
				Manifest: manifest,
				Receipts: receipts,
			}
			if tc.desired {
				in.Desired[tc.dirName] = true
			}

			got := Classify(in)
			if len(got) != 1 {
				t.Fatalf("expected 1 verdict, got %d", len(got))
			}
			v := got[0]
			if v.State != tc.wantState {
				t.Fatalf("state = %q, want %q", v.State, tc.wantState)
			}
			if v.Target != tgt || v.Name != tc.dirName {
				t.Fatalf("unexpected target/name: %q/%q", v.Target, v.Name)
			}
			if v.OnDiskDigest != tc.onDisk {
				t.Fatalf("OnDiskDigest = %q, want %q", v.OnDiskDigest, tc.onDisk)
			}
			wantReceipt := ""
			if tc.hasRcpt {
				wantReceipt = tc.receipt
			}
			if v.ReceiptDigest != wantReceipt {
				t.Fatalf("ReceiptDigest = %q, want %q", v.ReceiptDigest, wantReceipt)
			}
			// ExpectedVersion populated only when the manifest declares the name.
			if tc.managed && v.ExpectedVersion != "v-"+tc.dirName {
				t.Fatalf("ExpectedVersion = %q, want %q", v.ExpectedVersion, "v-"+tc.dirName)
			}
			if !tc.managed && v.ExpectedVersion != "" {
				t.Fatalf("ExpectedVersion = %q, want empty for foreign", v.ExpectedVersion)
			}
		})
	}
}

// TestClassify_WorktreeKeyingNoCrossContamination asserts that a receipt written
// for one absolute target path is never applied to a different path that happens
// to hold a dir of the same name (the worktree rule).
func TestClassify_WorktreeKeyingNoCrossContamination(t *testing.T) {
	const pathA = "/wt-a/.claude/skills"
	const pathB = "/wt-b/.claude/skills"

	manifest := manifestWith(map[string]string{"shared": "v1"})

	in := Inputs{
		Targets: []TargetScan{
			{Target: pathA, Dirs: []ScannedDir{{Name: "shared", Digest: "dA", Exists: true}}},
			{Target: pathB, Dirs: []ScannedDir{{Name: "shared", Digest: "dA", Exists: true}}},
		},
		Desired:  map[string]bool{}, // not desired => orphan candidate
		Manifest: manifest,
		Receipts: map[string]map[string]string{
			// Only path A has a receipt that matches its on-disk digest.
			pathA: {"shared": "dA"},
			// path B's receipt is for a DIFFERENT digest (or could be absent).
			pathB: {"shared": "dZ"},
		},
	}

	got := Classify(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 verdicts, got %d", len(got))
	}

	byTarget := map[string]DirStatus{}
	for _, v := range got {
		byTarget[v.Target] = v
	}

	// Path A: receipt matches on-disk -> orphan (prune-eligible).
	if byTarget[pathA].State != StateManagedOrphan {
		t.Fatalf("path A state = %q, want %q", byTarget[pathA].State, StateManagedOrphan)
	}
	// Path B: receipt is for a different digest -> modified, NOT orphan. This
	// proves A's receipt did not leak into B.
	if byTarget[pathB].State != StateModified {
		t.Fatalf("path B state = %q, want %q (cross-contamination!)", byTarget[pathB].State, StateModified)
	}

	// Deterministic ordering: A sorts before B.
	if got[0].Target != pathA || got[1].Target != pathB {
		t.Fatalf("verdicts not sorted by target: %q, %q", got[0].Target, got[1].Target)
	}
}

// TestPruneEligible_OnlyOrphans asserts PruneEligible returns ONLY orphans and
// Adoptable returns ONLY foreign across a mixed verdict set.
func TestFilters_PruneEligibleAndAdoptable(t *testing.T) {
	manifest := manifestWith(map[string]string{
		"current": "v1", "orphan": "v1", "drifted": "v1", "noreceipt": "v1",
	})

	in := Inputs{
		Targets: []TargetScan{{
			Target: tgt,
			Dirs: []ScannedDir{
				{Name: "current", Digest: "c", Exists: true},
				{Name: "orphan", Digest: "o", Exists: true},
				{Name: "drifted", Digest: "x", Exists: true}, // receipt says "d" -> modified
				{Name: "noreceipt", Digest: "n", Exists: true},
				{Name: "stranger", Digest: "s", Exists: true}, // not managed -> foreign
			},
		}},
		Desired:  map[string]bool{"current": true}, // only "current" is desired
		Manifest: manifest,
		Receipts: map[string]map[string]string{
			tgt: {"current": "c", "orphan": "o", "drifted": "d"},
		},
	}

	verdicts := Classify(in)

	prunes := PruneEligible(verdicts)
	if len(prunes) != 1 || prunes[0].Name != "orphan" {
		t.Fatalf("PruneEligible = %+v, want only [orphan]", prunes)
	}

	adopt := Adoptable(verdicts)
	if len(adopt) != 1 || adopt[0].Name != "stranger" {
		t.Fatalf("Adoptable = %+v, want only [stranger]", adopt)
	}

	// Sanity: the modified and unestablished dirs appear in neither filter.
	for _, v := range prunes {
		if v.State != StateManagedOrphan {
			t.Fatalf("PruneEligible leaked non-orphan: %+v", v)
		}
	}
	for _, v := range adopt {
		if v.State != StateForeign {
			t.Fatalf("Adoptable leaked non-foreign: %+v", v)
		}
	}
}

// TestClassify_NilManifestAllForeign: with no manifest, nothing is managed.
func TestClassify_NilManifestAllForeign(t *testing.T) {
	in := Inputs{
		Targets: []TargetScan{{
			Target: tgt,
			Dirs:   []ScannedDir{{Name: "anything", Digest: "d", Exists: true}},
		}},
		Desired:  map[string]bool{},
		Manifest: nil,
		Receipts: map[string]map[string]string{tgt: {"anything": "d"}},
	}
	got := Classify(in)
	if len(got) != 1 || got[0].State != StateForeign {
		t.Fatalf("nil manifest: got %+v, want single foreign", got)
	}
}
