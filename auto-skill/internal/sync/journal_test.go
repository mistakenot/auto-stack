package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// prepareCommit drives phases A→C against a single authored skill and returns a
// commitInput ready for the journaled commit, plus the resolved targets.
func prepareCommit(t *testing.T, env skill.Env, desiredComplete bool) (commitInput, *ProcessResult) {
	t.Helper()
	opts := Options{Locked: true}
	plan, err := BuildPlan(env, opts)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	fetch, err := Fetch(env, plan, opts)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	proc, err := Process(env, plan, fetch, opts)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	in := commitInput{
		env:             env,
		installs:        proc.Installs,
		staged:          stagedByName(proc.Staged),
		manifest:        proc.Manifest,
		desiredComplete: desiredComplete,
	}
	return in, proc
}

func readSkillBody(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return string(data)
}

// TestJournalRecoverBeforeManifest: a crash after the swaps but before the
// manifest write leaves a non-empty journal; the next run rolls forward to a
// consistent tree (manifest written, journal cleared, receipts present).
func TestJournalRecoverBeforeManifest(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	in, proc := prepareCommit(t, env, true)
	if _, err := commit(in, faultBeforeManifest); err == nil {
		t.Fatal("expected injected fault before manifest")
	}

	// Journal still present; manifest not yet written.
	if !journalPending(env) {
		t.Fatal("expected a pending journal after the injected fault")
	}
	if _, err := os.Stat(env.ManifestPath()); !os.IsNotExist(err) {
		t.Fatalf("manifest must not exist before recovery (err=%v)", err)
	}

	recovered, err := recoverJournal(env)
	if err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if !recovered {
		t.Fatal("expected recovery to report a journal")
	}
	if journalPending(env) {
		t.Fatal("journal must be cleared after recovery")
	}

	// Tree consistent: every target holds the rendered skill_version.
	alpha, _ := findStaged(proc, "alpha")
	for _, tg := range proc.Targets {
		if !dirMatchesDigest(filepath.Join(tg.Dir, "alpha"), alpha.SkillVersion) {
			t.Errorf("target %q tree does not match skill_version after recovery", tg.Name)
		}
	}
	// Manifest and receipts now exist.
	if _, err := os.Stat(env.ManifestPath()); err != nil {
		t.Fatalf("manifest missing after recovery: %v", err)
	}
	assertReceiptDigest(t, env, proc.Targets[0].Name, "alpha", alpha.SkillVersion)
}

// TestJournalRecoverBeforeLock: a crash after the manifest but before the lock
// rewrite recovers forward, writing the embedded lock.
func TestJournalRecoverBeforeLock(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	in, proc := prepareCommit(t, env, true)
	// Provide a lock to rewrite so the before-lock boundary is meaningful.
	in.lock = &skill.Lock{Version: 1, Skills: map[string]skill.LockEntry{}}

	if _, err := commit(in, faultBeforeLock); err == nil {
		t.Fatal("expected injected fault before lock")
	}
	if !journalPending(env) {
		t.Fatal("expected a pending journal")
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if journalPending(env) {
		t.Fatal("journal must be cleared after recovery")
	}
	if _, err := os.Stat(env.LockPath()); err != nil {
		t.Fatalf("lock missing after recovery: %v", err)
	}
	alpha, _ := findStaged(proc, "alpha")
	for _, tg := range proc.Targets {
		if !dirMatchesDigest(filepath.Join(tg.Dir, "alpha"), alpha.SkillVersion) {
			t.Errorf("target %q not consistent after recovery", tg.Name)
		}
	}
}

// TestJournalRecoverBeforeClear: a crash after writing receipts/manifest/lock
// but before clearing the journal recovers idempotently (same final tree, same
// manifest bytes) and clears the journal.
func TestJournalRecoverBeforeClear(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	in, proc := prepareCommit(t, env, true)
	if _, err := commit(in, faultBeforeClear); err == nil {
		t.Fatal("expected injected fault before clear")
	}
	manifestBefore, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		t.Fatalf("manifest should already exist before clear: %v", err)
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if journalPending(env) {
		t.Fatal("journal must be cleared after recovery")
	}
	manifestAfter, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		t.Fatalf("manifest missing after recovery: %v", err)
	}
	if !bytes.Equal(manifestBefore, manifestAfter) {
		t.Error("recovery rewrote a byte-different manifest (must be idempotent)")
	}
	alpha, _ := findStaged(proc, "alpha")
	if !dirMatchesDigest(filepath.Join(proc.Targets[0].Dir, "alpha"), alpha.SkillVersion) {
		t.Error("tree not consistent after recovery")
	}
}

// TestJournalExistingDirToTrash: an existing non-empty target dir is moved to
// journaled trash (never deleted in place) before the new tree is swapped in;
// recovery then drops the trash.
func TestJournalExistingDirToTrash(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	// First sync installs v1 everywhere.
	writeAuthoredSkill(t, env, "alpha", "v1 body")
	in1, proc1 := prepareCommit(t, env, true)
	if _, err := commit(in1, faultNone); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	target := proc1.Targets[0]
	finalDir := filepath.Join(target.Dir, "alpha")
	if !strings.Contains(readSkillBody(t, finalDir), "v1 body") {
		t.Fatal("v1 not installed")
	}

	// Edit the skill, commit v2 but crash before clearing the journal.
	writeAuthoredSkill(t, env, "alpha", "v2 body")
	in2, proc2 := prepareCommit(t, env, true)
	if _, err := commit(in2, faultBeforeClear); err == nil {
		t.Fatal("expected fault")
	}

	// The OLD tree must be in journaled trash (not deleted in place), and the
	// final dir must hold the NEW tree.
	trash := filepath.Join(target.Dir, ".sync-trash-alpha")
	if !strings.Contains(readSkillBody(t, trash), "v1 body") {
		t.Errorf("old tree not moved to journaled trash; trash=%q", trash)
	}
	if !strings.Contains(readSkillBody(t, finalDir), "v2 body") {
		t.Errorf("new tree not swapped into place")
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Errorf("journaled trash should be dropped after recovery (err=%v)", err)
	}
	alpha, _ := findStaged(proc2, "alpha")
	if !dirMatchesDigest(finalDir, alpha.SkillVersion) {
		t.Error("final tree inconsistent after recovery")
	}
}

// TestJournalPruningSuppressedOnIncompleteSet: with the desired set incomplete
// (a failed fetch), the journal records desired_complete=false and reserves an
// empty prunes slot — T4 deletes nothing and never half-advances.
func TestJournalPruningSuppressedOnIncompleteSet(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	// desiredComplete=false simulates an upstream fetch failure for some skill.
	in, _ := prepareCommit(t, env, false)
	if _, err := commit(in, faultBeforeClear); err == nil {
		t.Fatal("expected fault before clear")
	}

	j, ok, err := readJournal(env)
	if err != nil || !ok {
		t.Fatalf("expected a readable pending journal (ok=%v err=%v)", ok, err)
	}
	if j.DesiredComplete {
		t.Error("journal desired_complete must be false on an incomplete set (pruning suppressed)")
	}
	if len(j.Prunes) != 0 {
		t.Errorf("prunes slot must stay empty in T4, got %d entries", len(j.Prunes))
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
}

// TestJournalRollBackOnMissingStage: when a write can be neither completed (its
// stage is gone) nor confirmed (the target does not match), recovery rolls the
// whole transaction back to the prior tree and writes no manifest.
func TestJournalRollBackOnMissingStage(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	// Install v1 cleanly.
	writeAuthoredSkill(t, env, "alpha", "v1 body")
	in1, proc1 := prepareCommit(t, env, true)
	if _, err := commit(in1, faultNone); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	target := proc1.Targets[0]
	finalDir := filepath.Join(target.Dir, "alpha")

	// Begin a v2 commit, fault after the journal write but before swaps by
	// hand-crafting the crash state: write the journal, move old → trash, then
	// destroy the stage so recovery cannot roll forward.
	writeAuthoredSkill(t, env, "alpha", "v2 body")
	in2, _ := prepareCommit(t, env, true)
	staged := in2.staged["alpha"]
	stage, err := StageSkillDir(target.Dir, "alpha", staged.Files)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	trash := filepath.Join(target.Dir, ".sync-trash-alpha")
	if err := os.Rename(finalDir, trash); err != nil {
		t.Fatalf("move to trash: %v", err)
	}
	j := &journal{
		Version: journalVersion,
		Root:    env.Root,
		Writes: []journalWrite{{
			Target: target.Name, Skill: "alpha", Dir: finalDir,
			Stage: stage, Trash: trash, Digest: staged.SkillVersion,
		}},
		Prunes:       []journalPrune{},
		ManifestPath: env.ManifestPath(),
	}
	if err := writeJournal(env, j); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	// Destroy the stage: forward is now impossible → recovery must roll back.
	if err := os.RemoveAll(stage); err != nil {
		t.Fatal(err)
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if journalPending(env) {
		t.Fatal("journal must be cleared after rollback")
	}
	// The prior v1 tree is restored from trash.
	if !strings.Contains(readSkillBody(t, finalDir), "v1 body") {
		t.Errorf("rollback did not restore the prior tree: %q", readSkillBody(t, finalDir))
	}
	if _, err := os.Stat(trash); !os.IsNotExist(err) {
		t.Errorf("trash should be consumed by rollback (err=%v)", err)
	}
}

func assertReceiptDigest(t *testing.T, env skill.Env, target, name, want string) {
	t.Helper()
	r := loadReceipts(env)
	got, ok := r.Targets[target][name]
	if !ok {
		t.Fatalf("receipts missing %s/%s", target, name)
	}
	if got != want {
		t.Errorf("receipt digest for %s/%s = %q, want %q", target, name, got, want)
	}
}
