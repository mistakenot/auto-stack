package sync

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/ownership"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// ── helpers ──────────────────────────────────────────────────────────────

// writeForeignDir creates an un-managed (foreign) skill dir directly in a target,
// bypassing sync — no manifest entry, no receipt. body lets a test detect whether
// it was later overwritten.
func writeForeignDir(t *testing.T, env skill.Env, style, name, body string) string {
	t.Helper()
	dir := filepath.Join(env.Root, "."+style, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing.\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func skillBody(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return string(data)
}

func targetSkillDirs(targets []Target, name string) []string {
	out := make([]string, 0, len(targets))
	for _, tg := range targets {
		out = append(out, filepath.Join(tg.Dir, name))
	}
	return out
}

// buildPruneCommit drives A→C against the current ./skills, classifies the
// on-disk targets, and returns a commitInput carrying the receipt-gated prunes
// (desiredComplete=true).
func buildPruneCommit(t *testing.T, env skill.Env) (commitInput, []journalPrune, *ProcessResult) {
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
	desired := desiredSetFromStaged(proc.Staged)
	inputs, err := ScanOwnership(env, desired)
	if err != nil {
		t.Fatalf("ScanOwnership: %v", err)
	}
	verdicts := ownership.Classify(inputs)
	prunes := planPrune(verdicts, proc.Targets, true)
	in := commitInput{
		env:             env,
		installs:        proc.Installs,
		staged:          stagedByName(proc.Staged),
		manifest:        proc.Manifest,
		prunes:          prunes,
		desiredComplete: true,
	}
	return in, prunes, proc
}

func manifestManages(t *testing.T, env skill.Env, name string) bool {
	t.Helper()
	m := loadManifestBestEffort(env)
	if m == nil {
		return false
	}
	if _, ok := m.Skills[name]; ok {
		return true
	}
	for _, mt := range m.Targets {
		if _, ok := mt.ManagedSkills[name]; ok {
			return true
		}
	}
	return false
}

func receiptHas(t *testing.T, env skill.Env, style, name string) bool {
	t.Helper()
	_, ok := loadReceipts(env).Targets[style][name]
	return ok
}

func contains(items []string, want string) bool {
	return slices.Contains(items, want)
}

// ── unit tests (pure) ─────────────────────────────────────────────────────

// TestPlanPruneEmptyWhenIncomplete: an otherwise-eligible orphan is never pruned
// when the desired set is incomplete (AC-3).
func TestPlanPruneEmptyWhenIncomplete(t *testing.T) {
	targets := []Target{{Name: "claude", Dir: "/repo/.claude/skills"}}
	verdicts := []ownership.DirStatus{
		{Target: "claude", Name: "orphan", State: ownership.StateManagedOrphan, OnDiskDigest: "d1"},
	}
	if got := planPrune(verdicts, targets, false); len(got) != 0 {
		t.Fatalf("desiredComplete=false must yield no prunes, got %v", got)
	}
	got := planPrune(verdicts, targets, true)
	if len(got) != 1 {
		t.Fatalf("expected 1 prune, got %d", len(got))
	}
	p := got[0]
	if p.Target != "claude" || p.Skill != "orphan" {
		t.Errorf("prune target/skill = %s/%s", p.Target, p.Skill)
	}
	wantDir := filepath.Join("/repo/.claude/skills", "orphan")
	if p.Dir != wantDir {
		t.Errorf("prune dir = %q, want %q", p.Dir, wantDir)
	}
	wantTrash := filepath.Join("/repo/.claude/skills", ".sync-trash-prune-orphan")
	if p.Trash != wantTrash {
		t.Errorf("prune trash = %q, want %q", p.Trash, wantTrash)
	}
	if p.Digest != "d1" {
		t.Errorf("prune digest = %q, want d1", p.Digest)
	}
}

// TestPlanPruneOnlyOrphans: only StateManagedOrphan verdicts are prunable; every
// other state is reported, never deleted (AC-2).
func TestPlanPruneOnlyOrphans(t *testing.T) {
	targets := []Target{{Name: "claude", Dir: "/repo/.claude/skills"}}
	verdicts := []ownership.DirStatus{
		{Target: "claude", Name: "orphan", State: ownership.StateManagedOrphan},
		{Target: "claude", Name: "unestablished", State: ownership.StateManagedUnestablished},
		{Target: "claude", Name: "modified", State: ownership.StateModified},
		{Target: "claude", Name: "foreign", State: ownership.StateForeign},
		{Target: "claude", Name: "current", State: ownership.StateManagedCurrent},
	}
	got := planPrune(verdicts, targets, true)
	if len(got) != 1 || got[0].Skill != "orphan" {
		t.Fatalf("only the orphan is prunable, got %v", got)
	}
}

// TestDetectForeignCollisions: a desired name over a foreign dir is a conflict; a
// foreign dir that is NOT desired is not.
func TestDetectForeignCollisions(t *testing.T) {
	verdicts := []ownership.DirStatus{
		{Target: "claude", Name: "foo", State: ownership.StateForeign, OnDiskDigest: "fd"},
		{Target: "claude", Name: "bar", State: ownership.StateForeign},
		{Target: "claude", Name: "baz", State: ownership.StateManagedCurrent},
	}
	desired := map[string]bool{"foo": true, "baz": true}
	got := detectForeignCollisions(desired, verdicts)
	if len(got) != 1 {
		t.Fatalf("expected 1 collision, got %v", got)
	}
	if got[0].Skill != "foo" || got[0].Target != "claude" || got[0].Digest != "fd" {
		t.Errorf("unexpected conflict %+v", got[0])
	}
}

// ── integration: AC-1 rename → prune ──────────────────────────────────────

func TestPruneRenamedOrphan(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "old-name", "v1 body")

	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "old-name") {
		if !strings.Contains(skillBody(t, dir), "v1 body") {
			t.Fatalf("old-name not installed at %s", dir)
		}
	}

	// Rename: drop old-name from ./skills, add new-name (the desired set changes).
	if err := os.RemoveAll(filepath.Join(env.Root, "skills", "old-name")); err != nil {
		t.Fatal(err)
	}
	writeAuthoredSkill(t, env, "new-name", "v1 body")

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("rename sync should exit zero, errors=%v", res.Errors)
	}

	for _, style := range []string{"claude", "agents"} {
		oldDir := filepath.Join(env.Root, "."+style, "skills", "old-name")
		if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
			t.Errorf("orphan %s should be pruned (err=%v)", oldDir, err)
		}
		newDir := filepath.Join(env.Root, "."+style, "skills", "new-name")
		if !strings.Contains(skillBody(t, newDir), "v1 body") {
			t.Errorf("new-name not written at %s", newDir)
		}
		if !contains(res.Pruned, style+"/old-name") {
			t.Errorf("Result.Pruned missing %s/old-name: %v", style, res.Pruned)
		}
		if receiptHas(t, env, style, "old-name") {
			t.Errorf("receipts still list pruned %s/old-name", style)
		}
	}
	if manifestManages(t, env, "old-name") {
		t.Error("manifest still manages the pruned old-name")
	}
}

// ── integration: AC-2 refuse without the full three-part authority ─────────

// installTwo seeds two authored skills cleanly so each has a matching receipt +
// manifest entry, then returns the resolved targets.
func installTwo(t *testing.T, env skill.Env, a, b string) []Target {
	t.Helper()
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, a, "body a")
	writeAuthoredSkill(t, env, b, "body b")
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	return resolveTargets(env, nil)
}

// TestPruneRefusesWithoutReceipt: a manifest-managed orphan with NO local receipt
// is reported managed-unestablished, never deleted (AC-2a).
func TestPruneRefusesWithoutReceipt(t *testing.T) {
	env := newEnv(t)
	targets := installTwo(t, env, "keep", "orphan")

	// Drop this machine's receipts: the orphan is now manifest-managed but has no
	// receipt — the deletion authority is incomplete.
	if err := os.Remove(receiptsPath(env)); err != nil {
		t.Fatalf("remove receipts: %v", err)
	}

	desired := map[string]bool{"keep": true}
	inputs, err := ScanOwnership(env, desired)
	if err != nil {
		t.Fatalf("ScanOwnership: %v", err)
	}
	verdicts := ownership.Classify(inputs)
	assertState(t, verdicts, "orphan", ownership.StateManagedUnestablished)
	if got := planPrune(verdicts, targets, true); len(got) != 0 {
		t.Fatalf("no-receipt orphan must not be prunable, got %v", got)
	}
	for _, dir := range targetSkillDirs(targets, "orphan") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("orphan dir must be untouched at %s: %v", dir, err)
		}
	}
}

// TestPruneRefusesWhenModified: a receipt exists but the on-disk dir digest has
// drifted (locally edited) → modified, never deleted (AC-2b).
func TestPruneRefusesWhenModified(t *testing.T) {
	env := newEnv(t)
	targets := installTwo(t, env, "keep", "orphan")

	// Edit the installed orphan so its on-disk digest no longer matches the
	// receipt.
	for _, dir := range targetSkillDirs(targets, "orphan") {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: orphan\ndescription: Use when testing.\n---\n\nLOCALLY EDITED\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	desired := map[string]bool{"keep": true}
	inputs, err := ScanOwnership(env, desired)
	if err != nil {
		t.Fatalf("ScanOwnership: %v", err)
	}
	verdicts := ownership.Classify(inputs)
	assertState(t, verdicts, "orphan", ownership.StateModified)
	if got := planPrune(verdicts, targets, true); len(got) != 0 {
		t.Fatalf("modified orphan must not be prunable, got %v", got)
	}
	for _, dir := range targetSkillDirs(targets, "orphan") {
		if !strings.Contains(skillBody(t, dir), "LOCALLY EDITED") {
			t.Errorf("modified orphan must be untouched at %s", dir)
		}
	}
}

// TestPruneRefusesForgedStamp: a dir carrying a forged in-file managed stamp but
// no matching receipt is foreign (digest over raw bytes includes the stamp) and
// never deleted (AC-2c).
func TestPruneRefusesForgedStamp(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "keep", "body keep")
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)

	// Plant a dir with a forged "managed" stamp directly in a target.
	forged := "---\nname: ghost\ndescription: Use when testing.\nmetadata:\n  auto_skill:\n    managed: true\n---\n\nFORGED\n"
	ghost := filepath.Join(targets[0].Dir, "ghost")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghost, "SKILL.md"), []byte(forged), 0o644); err != nil {
		t.Fatal(err)
	}

	desired := map[string]bool{"keep": true}
	inputs, err := ScanOwnership(env, desired)
	if err != nil {
		t.Fatalf("ScanOwnership: %v", err)
	}
	verdicts := ownership.Classify(inputs)
	assertState(t, verdicts, "ghost", ownership.StateForeign)
	if got := planPrune(verdicts, targets, true); len(got) != 0 {
		t.Fatalf("forged-stamp dir must not be prunable, got %v", got)
	}
	if !strings.Contains(skillBody(t, ghost), "FORGED") {
		t.Error("forged dir must be untouched")
	}
}

func assertState(t *testing.T, verdicts []ownership.DirStatus, name string, want ownership.State) {
	t.Helper()
	for _, v := range verdicts {
		if v.Name == name {
			if v.State != want {
				t.Errorf("%s state = %q, want %q", name, v.State, want)
			}
			return
		}
	}
	t.Errorf("no verdict for %q", name)
}

// ── integration: AC-3 incomplete set suppresses pruning ────────────────────

func TestPruneSuppressedOnFailedFetch(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("vend", "v1")

	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "keep", "body keep")
	writeAuthoredSkill(t, env, "orphan", "body orphan")
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)

	// Drop orphan from the desired set, and pin a vendored skill at a commit the
	// cache cannot satisfy so phase B fails → desiredComplete=false.
	if err := os.RemoveAll(filepath.Join(env.Root, "skills", "orphan")); err != nil {
		t.Fatal(err)
	}
	gone := "0123456789abcdef0123456789abcdef01234567"
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"vend": lockEntry(f.url, "vend", "commit:"+gone, gone),
	})

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.DesiredComplete {
		t.Fatal("desired set must be incomplete after a failed fetch")
	}
	if res.ExitCode() == 0 {
		t.Error("a failed fetch must exit non-zero")
	}
	if len(res.Pruned) != 0 {
		t.Errorf("no prune may occur on an incomplete set, got %v", res.Pruned)
	}
	for _, dir := range targetSkillDirs(targets, "orphan") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("orphan must survive an incomplete-set run at %s: %v", dir, err)
		}
	}
	// Valid writes still proceed: keep is present.
	for _, dir := range targetSkillDirs(targets, "keep") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("valid write keep missing at %s: %v", dir, err)
		}
	}
}

// ── integration: AC-4 foreign collision ────────────────────────────────────

func TestForeignCollisionRefusedThenForced(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "foo", "AUTHORED body")

	// A foreign dir named foo already lives in each target (no manifest/receipt).
	var foreignDirs []string
	for _, style := range []string{"claude", "agents"} {
		foreignDirs = append(foreignDirs, writeForeignDir(t, env, style, "foo", "FOREIGN body"))
	}

	// Without --force: reported, not overwritten, not pruned, exit non-zero.
	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Conflicts) == 0 {
		t.Fatal("expected a reported conflict")
	}
	if res.ExitCode() == 0 {
		t.Error("a foreign collision without --force must exit non-zero")
	}
	for _, dir := range foreignDirs {
		if !strings.Contains(skillBody(t, dir), "FOREIGN body") {
			t.Errorf("foreign dir %s must NOT be overwritten without --force", dir)
		}
	}

	// With --force: the incoming authored skill overwrites the foreign dir.
	res, err = Run(env, Options{Locked: true, Force: true})
	if err != nil {
		t.Fatalf("Run --force: %v", err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("--force overwrite should exit zero, errors=%v", res.Errors)
	}
	for _, dir := range foreignDirs {
		if !strings.Contains(skillBody(t, dir), "AUTHORED body") {
			t.Errorf("--force did not overwrite %s with the authored skill", dir)
		}
	}
}

// ── integration: AC-10 crash injection across the prune commit ─────────────

func TestPruneCrashRecovery(t *testing.T) {
	faults := []faultPoint{faultBeforeReceipts, faultBeforeManifest, faultBeforeLock}
	for _, fault := range faults {
		t.Run(string(fault), func(t *testing.T) {
			env := newEnv(t)
			targets := installTwo(t, env, "keep", "orphan")

			// A foreign dir must never be touched by recovery.
			ghost := writeForeignDir(t, env, targets[0].Name, "ghost", "FOREIGN body")

			// Drop orphan from ./skills so it becomes a prune-eligible orphan.
			if err := os.RemoveAll(filepath.Join(env.Root, "skills", "orphan")); err != nil {
				t.Fatal(err)
			}
			in, prunes, proc := buildPruneCommit(t, env)
			if len(prunes) == 0 {
				t.Fatal("expected the orphan to be prune-eligible")
			}

			if _, err := commit(in, fault); err == nil {
				t.Fatalf("expected injected fault %s", fault)
			}
			if !journalPending(env) {
				t.Fatalf("expected a pending journal after fault %s", fault)
			}

			recovered, err := recoverJournal(env)
			if err != nil {
				t.Fatalf("recoverJournal: %v", err)
			}
			if !recovered || journalPending(env) {
				t.Fatalf("journal must be recovered and cleared (recovered=%v pending=%v)", recovered, journalPending(env))
			}

			// Convergence: orphan pruned everywhere, keep consistent, foreign
			// untouched, receipts/manifest never claim the orphan.
			keep, _ := findStaged(proc, "keep")
			for _, tg := range proc.Targets {
				orphanDir := filepath.Join(tg.Dir, "orphan")
				if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
					t.Errorf("orphan not pruned at %s (err=%v)", orphanDir, err)
				}
				if !dirMatchesDigest(filepath.Join(tg.Dir, "keep"), keep.SkillVersion) {
					t.Errorf("keep inconsistent in %s after recovery", tg.Name)
				}
				if receiptHas(t, env, tg.Name, "orphan") {
					t.Errorf("receipts still claim pruned %s/orphan", tg.Name)
				}
			}
			if manifestManages(t, env, "orphan") {
				t.Error("manifest still manages the pruned orphan")
			}
			if !strings.Contains(skillBody(t, ghost), "FOREIGN body") {
				t.Error("recovery touched the foreign dir")
			}

			// Re-running sync converges and is idempotent (no further prune).
			res, err := Run(env, Options{Locked: true})
			if err != nil {
				t.Fatalf("post-recovery Run: %v", err)
			}
			if res.ExitCode() != 0 {
				t.Errorf("post-recovery sync should be clean, errors=%v", res.Errors)
			}
			if len(res.Pruned) != 0 {
				t.Errorf("idempotent re-run must prune nothing, got %v", res.Pruned)
			}
		})
	}
}

// TestPruneCrashRollback: when a write cannot roll forward (its stage is gone) the
// whole transaction rolls back — a moved orphan is restored and no foreign dir is
// touched.
func TestPruneCrashRollback(t *testing.T) {
	env := newEnv(t)
	targets := installTwo(t, env, "keep", "orphan")
	ghost := writeForeignDir(t, env, targets[0].Name, "ghost", "FOREIGN body")

	// Edit keep so the next commit has a real write-swap, and drop orphan to make
	// it prune-eligible.
	writeAuthoredSkill(t, env, "keep", "body keep v2")
	if err := os.RemoveAll(filepath.Join(env.Root, "skills", "orphan")); err != nil {
		t.Fatal(err)
	}
	in, prunes, _ := buildPruneCommit(t, env)
	if len(prunes) == 0 {
		t.Fatal("expected a prune-eligible orphan")
	}

	// Hand-craft a crash state: stage keep, write the journal, apply the prune
	// (orphan → trash), then destroy keep's stage so forward is impossible.
	staged := in.staged["keep"]
	var writes []journalWrite
	for _, inst := range in.installs {
		if inst.Skill != "keep" || inst.Action != InstallWrite {
			continue
		}
		stage, err := StageSkillDir(inst.Dir, "keep", staged.Files)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		writes = append(writes, journalWrite{
			Target: inst.Target, Skill: "keep", Dir: filepath.Join(inst.Dir, "keep"),
			Stage: stage, Trash: filepath.Join(inst.Dir, ".sync-trash-keep"), Digest: inst.Want,
		})
	}
	if len(writes) == 0 {
		t.Fatal("expected a keep write")
	}
	j := &journal{
		Version:      journalVersion,
		Root:         env.Root,
		Writes:       writes,
		Prunes:       prunes,
		ManifestPath: env.ManifestPath(),
	}
	if err := writeJournal(env, j); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	if err := applyPrunes(j); err != nil {
		t.Fatalf("applyPrunes: %v", err)
	}
	// Orphan was moved to trash; now destroy keep's stage → must roll back.
	for _, w := range writes {
		if err := os.RemoveAll(w.Stage); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := recoverJournal(env); err != nil {
		t.Fatalf("recoverJournal: %v", err)
	}
	if journalPending(env) {
		t.Fatal("journal must be cleared after rollback")
	}
	// The orphan is restored (rollback never advances receipts, so the dir must
	// come back to match them).
	for _, dir := range targetSkillDirs(targets, "orphan") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("rollback did not restore orphan at %s: %v", dir, err)
		}
	}
	if !strings.Contains(skillBody(t, ghost), "FOREIGN body") {
		t.Error("rollback touched the foreign dir")
	}
}
