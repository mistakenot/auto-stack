package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// journalVersion is the on-disk schema version of the write-ahead sync journal.
const journalVersion = 1

// journalName is the write-ahead journal file under .auto/skills/. Its presence
// (non-empty) is the recovery trigger: an interrupted commit always leaves it
// behind, and clearing it is the commit point.
const journalName = ".sync-journal"

// errInjectedFault aborts a commit at a named step so journal_test can simulate
// a crash between any two commit steps. The orchestrator never injects it.
var errInjectedFault = errors.New("sync: injected commit fault")

// errStageMissing means a journaled write can be neither rolled forward (its
// staged tree is gone) nor confirmed (the target does not match the digest);
// recovery falls back to rolling the whole transaction back.
var errStageMissing = errors.New("sync: staged tree missing and target does not match digest")

// faultPoint names a commit step boundary for fault injection (test-only).
type faultPoint string

const (
	faultNone           faultPoint = ""
	faultBeforeReceipts faultPoint = "before_receipts"
	faultBeforeManifest faultPoint = "before_manifest"
	faultBeforeLock     faultPoint = "before_lock"
	faultBeforeClear    faultPoint = "before_clear"
)

// journalWrite is one intended per-(target, skill) directory swap. Trash is the
// pre-assigned, same-filesystem sibling where an existing non-empty target dir
// is moved (never deleted in place, never cross-FS) before the staged tree is
// renamed into place.
type journalWrite struct {
	Target string `json:"target"`
	Skill  string `json:"skill"`
	Dir    string `json:"dir"`    // final skill dir: <targetDir>/<skill>
	Stage  string `json:"stage"`  // staged same-FS sibling temp dir
	Trash  string `json:"trash"`  // sibling trash path for the replaced dir
	Digest string `json:"digest"` // expected skill_version of the staged tree
}

// journalPrune is one receipt-gated orphan deletion. T5's prune pass fills this
// slot (T4 always wrote an empty slice, so adding fields is wire-safe); a failed
// fetch / incomplete desired set still yields an empty slice, so a partial sync
// deletes nothing. Target is the STYLE name (the receipts key); Dir is the
// absolute skill dir; Trash is the pre-assigned same-FS sibling where the orphan
// is moved (never deleted in place) so a crashed prune can roll back.
type journalPrune struct {
	Target string `json:"target"`
	Skill  string `json:"skill"`
	Dir    string `json:"dir"`
	Trash  string `json:"trash"`
	Digest string `json:"digest,omitempty"`
}

// journal is the self-contained write-ahead record of one commit. It embeds the
// typed receipts/manifest/lock to write so recovery can roll a half-finished
// commit forward without re-rendering anything. The embedded values are
// re-marshaled deterministically (skill.EncodeJSON) on both the live and the
// recovery write, so the on-disk files are byte-identical either way.
type journal struct {
	Version         int             `json:"version"`
	StartedAt       string          `json:"started_at"`
	Root            string          `json:"root"`
	ProjectID       string          `json:"project_id"`
	DesiredComplete bool            `json:"desired_complete"` // false → pruning suppressed (T5)
	Writes          []journalWrite  `json:"writes"`
	Prunes          []journalPrune  `json:"prunes"` // reserved for T5; always empty in T4
	Receipts        *Receipts       `json:"receipts,omitempty"`
	ReceiptsPath    string          `json:"receipts_path"`
	Manifest        *skill.Manifest `json:"manifest,omitempty"`
	ManifestPath    string          `json:"manifest_path"`
	Lock            *skill.Lock     `json:"lock,omitempty"` // present only when the lock is rewritten
	LockPath        string          `json:"lock_path"`
}

// ── commit ──────────────────────────────────────────────────────────────

// commitInput bundles everything the journaled commit needs from phase C.
type commitInput struct {
	env             skill.Env
	installs        []Install
	staged          map[string]*StagedSkill // by skill name
	manifest        *skill.Manifest
	lock            *skill.Lock    // non-nil only when the plan marks a lock rewrite
	prunes          []journalPrune // receipt-gated orphans to delete (empty unless desiredComplete)
	desiredComplete bool
}

// commitOutcome reports what the commit did, for the orchestrator's Result.
type commitOutcome struct {
	Written         []string
	Skipped         []string
	Pruned          []string // target/skill of each pruned orphan
	ReceiptsPath    string
	ManifestWritten bool
	LockRewritten   bool
}

// commit runs the journaled crash-consistent commit in the fixed order:
//
//	(1) stage each rendered tree same-FS + journal intended writes/digests
//	(2) swap per skill by rename (existing non-empty dir → journaled trash first)
//	(3) write machine-local receipts
//	(4) write manifest.json then lock.json (atomic; manifest before lock)
//	(5) clear the journal (the commit point), then drop trash
//
// fault (test-only) stops the commit at a named boundary, leaving the journal in
// place so a subsequent recover() can finish or revert it.
func commit(in commitInput, fault faultPoint) (commitOutcome, error) {
	var out commitOutcome
	env := in.env

	// The receipts/manifest/lock the commit will write are embedded (typed) in
	// the journal so recovery can roll a half-finished commit forward without
	// re-rendering; both paths re-marshal them deterministically.
	receipts := buildReceipts(env, in.installs)

	// (1) Stage every write-intent tree into a same-FS sibling temp dir.
	var writes []journalWrite
	for _, inst := range in.installs {
		if inst.Action != InstallWrite {
			out.Skipped = append(out.Skipped, inst.Target+"/"+inst.Skill)
			continue
		}
		st := in.staged[inst.Skill]
		if st == nil {
			cleanupStages(writes)
			return out, fmt.Errorf("no staged tree for %q (target %q)", inst.Skill, inst.Target)
		}
		stage, serr := StageSkillDir(inst.Dir, inst.Skill, st.Files)
		if serr != nil {
			cleanupStages(writes)
			return out, serr
		}
		writes = append(writes, journalWrite{
			Target: inst.Target,
			Skill:  inst.Skill,
			Dir:    filepath.Join(inst.Dir, inst.Skill),
			Stage:  stage,
			Trash:  filepath.Join(inst.Dir, ".sync-trash-"+inst.Skill),
			Digest: inst.Want,
		})
		out.Written = append(out.Written, inst.Target+"/"+inst.Skill)
	}

	prunes := in.prunes
	if prunes == nil {
		prunes = []journalPrune{}
	}
	// Drop each pruned orphan from the embedded receipts BEFORE the journal is
	// written, so the journaled (and later-written, and recovery-rewritten)
	// receipts are already correct — they must never still claim ownership of a
	// dir this commit is deleting. The new manifest (in.manifest) already excludes
	// orphans: they were never staged, so buildManifest's managed map omits them.
	for _, p := range prunes {
		if m := receipts.Targets[p.Target]; m != nil {
			delete(m, p.Skill)
		}
		out.Pruned = append(out.Pruned, p.Target+"/"+p.Skill)
	}

	j := &journal{
		Version:         journalVersion,
		StartedAt:       time.Now().UTC().Format(time.RFC3339),
		Root:            env.Root,
		ProjectID:       projectID(env),
		DesiredComplete: in.desiredComplete,
		Writes:          writes,
		Prunes:          prunes,
		Receipts:        receipts,
		ReceiptsPath:    receiptsPath(env),
		Manifest:        in.manifest,
		ManifestPath:    env.ManifestPath(),
		Lock:            in.lock,
		LockPath:        env.LockPath(),
	}
	out.ReceiptsPath = j.ReceiptsPath

	// The journal write is the write-ahead barrier: after it, a crash recovers;
	// before it, the only residue is orphan stage dirs (harmless, swept later).
	if err := writeJournal(env, j); err != nil {
		cleanupStages(writes)
		return out, fmt.Errorf("write sync journal: %w", err)
	}

	// (2) Swap per skill, then apply the receipt-gated prunes (orphan dir → its
	// journaled trash, never an in-place delete) — both ride the same pre-receipts
	// boundary so recovery treats them uniformly.
	for i := range j.Writes {
		if err := swapOne(j.Writes[i]); err != nil {
			return out, fmt.Errorf("swap %s/%s: %w", j.Writes[i].Target, j.Writes[i].Skill, err)
		}
	}
	if err := applyPrunes(j); err != nil {
		return out, fmt.Errorf("apply prunes: %w", err)
	}
	if fault == faultBeforeReceipts {
		return out, errInjectedFault
	}

	// (3) Receipts.
	if err := config.WriteJSONFileAtomic(j.ReceiptsPath, j.Receipts); err != nil {
		return out, fmt.Errorf("write receipts: %w", err)
	}
	if fault == faultBeforeManifest {
		return out, errInjectedFault
	}

	// (4) Manifest then lock (manifest before lock).
	if err := config.WriteJSONFileAtomic(j.ManifestPath, j.Manifest); err != nil {
		return out, fmt.Errorf("write manifest: %w", err)
	}
	out.ManifestWritten = true
	if fault == faultBeforeLock {
		return out, errInjectedFault
	}
	if j.Lock != nil {
		if err := config.WriteJSONFileAtomic(j.LockPath, j.Lock); err != nil {
			return out, fmt.Errorf("write lock: %w", err)
		}
		out.LockRewritten = true
	}
	if fault == faultBeforeClear {
		return out, errInjectedFault
	}

	// (5) Clear the journal — the commit point — then drop trash + stages.
	if err := removeJournal(env); err != nil {
		return out, fmt.Errorf("clear sync journal: %w", err)
	}
	j.dropResidue()
	return out, nil
}

// ── recovery ────────────────────────────────────────────────────────────

// recoverJournal completes or reverts a pending commit found at startup. It
// returns whether a journal was present. A non-empty journal rolls forward when
// every write can reach its target digest (its stage survives or the target
// already matches); otherwise the whole transaction rolls back to the prior
// trees (restoring journaled trash) and writes no manifest/lock/receipts —
// output never looks foreign and the manifest never claims un-written bytes.
func recoverJournal(env skill.Env) (bool, error) {
	j, ok, err := readJournal(env)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// Only the WRITES gate the forward/back decision. Prunes never block forward:
	// an unapplied prune (dir present) is applyable, and an applied one (dir gone,
	// trash present) is already done — applyPrunes tolerates both. On a rollback
	// (forced by an unrecoverable write) restorePrunes brings any moved orphan
	// back, so prunes are reconciled either way.
	canForward := true
	for i := range j.Writes {
		w := j.Writes[i]
		if dirMatchesDigest(w.Dir, w.Digest) {
			continue // already swapped
		}
		if pathExists(w.Stage) {
			continue // stage survives → can complete the swap
		}
		canForward = false
		break
	}

	if canForward {
		return true, j.rollForward(env)
	}
	return true, j.rollBack(env)
}

// rollForward completes every swap, then re-applies the embedded receipts /
// manifest / lock bytes (idempotent — identical bytes if already written) and
// clears the journal.
func (j *journal) rollForward(env skill.Env) error {
	for i := range j.Writes {
		if err := swapOne(j.Writes[i]); err != nil {
			return fmt.Errorf("recover swap %s/%s: %w", j.Writes[i].Target, j.Writes[i].Skill, err)
		}
	}
	// Idempotently ensure every prune is applied (the embedded receipts already
	// dropped these entries, so re-writing them below is correct).
	if err := applyPrunes(j); err != nil {
		return fmt.Errorf("recover apply prunes: %w", err)
	}
	if j.Receipts != nil && j.ReceiptsPath != "" {
		if err := config.WriteJSONFileAtomic(j.ReceiptsPath, j.Receipts); err != nil {
			return fmt.Errorf("recover receipts: %w", err)
		}
	}
	if j.Manifest != nil && j.ManifestPath != "" {
		if err := config.WriteJSONFileAtomic(j.ManifestPath, j.Manifest); err != nil {
			return fmt.Errorf("recover manifest: %w", err)
		}
	}
	if j.Lock != nil && j.LockPath != "" {
		if err := config.WriteJSONFileAtomic(j.LockPath, j.Lock); err != nil {
			return fmt.Errorf("recover lock: %w", err)
		}
	}
	if err := removeJournal(env); err != nil {
		return fmt.Errorf("recover clear journal: %w", err)
	}
	j.dropResidue()
	return nil
}

// rollBack restores every replaced tree from journaled trash and discards
// surviving stages, then clears the journal without advancing receipts/
// manifest/lock. The repo is left exactly as it was before the failed commit.
func (j *journal) rollBack(env skill.Env) error {
	for i := range j.Writes {
		w := j.Writes[i]
		if pathExists(w.Trash) {
			if err := os.RemoveAll(w.Dir); err != nil {
				return fmt.Errorf("recover rollback clear %s: %w", w.Dir, err)
			}
			if err := os.Rename(w.Trash, w.Dir); err != nil {
				return fmt.Errorf("recover rollback restore %s: %w", w.Dir, err)
			}
		}
		if pathExists(w.Stage) {
			_ = os.RemoveAll(w.Stage)
		}
	}
	// Restore any prune that was already moved to trash — receipts/manifest are
	// NOT advanced on a rollback, so the on-disk receipts still claim these dirs
	// and the dirs must come back to match (the repo is left exactly as before).
	if err := j.restorePrunes(); err != nil {
		return err
	}
	if err := removeJournal(env); err != nil {
		return fmt.Errorf("recover clear journal: %w", err)
	}
	return nil
}

// ── swap primitive ──────────────────────────────────────────────────────

// swapOne renames a staged tree into its final location, moving an existing
// non-empty target dir to journaled trash first (same-FS rename, never an
// in-place delete). It is idempotent so both the live commit and recovery can
// call it: if the stage is already consumed and the target matches the digest
// the swap is complete; if the stage is gone and the target does not match it
// signals errStageMissing so recovery can roll back instead.
func swapOne(w journalWrite) error {
	if !pathExists(w.Stage) {
		if dirMatchesDigest(w.Dir, w.Digest) {
			return nil
		}
		return errStageMissing
	}
	if pathExists(w.Dir) {
		if isNonEmptyDir(w.Dir) && !pathExists(w.Trash) {
			if err := os.MkdirAll(filepath.Dir(w.Trash), 0o755); err != nil {
				return err
			}
			if err := os.Rename(w.Dir, w.Trash); err != nil {
				return err
			}
		} else if err := os.RemoveAll(w.Dir); err != nil {
			return err
		}
	}
	return os.Rename(w.Stage, w.Dir)
}

// applyPrunes moves each receipt-gated orphan dir to its journaled trash (a
// same-FS rename, never an in-place delete) so a crashed prune can roll back. It
// is idempotent: an orphan whose dir is already gone is skipped; an orphan whose
// dir AND trash both survive (a re-entry after a partial rename) is converged by
// dropping the dir, with the trash preserved as the rollback copy.
func applyPrunes(j *journal) error {
	for i := range j.Prunes {
		p := j.Prunes[i]
		if !pathExists(p.Dir) {
			continue // already pruned
		}
		if pathExists(p.Trash) {
			if err := os.RemoveAll(p.Dir); err != nil {
				return fmt.Errorf("prune drop %s: %w", p.Dir, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p.Trash), 0o755); err != nil {
			return err
		}
		if err := os.Rename(p.Dir, p.Trash); err != nil {
			return fmt.Errorf("prune move %s: %w", p.Dir, err)
		}
	}
	return nil
}

// restorePrunes undoes applied prunes during a rollback: each orphan moved to
// trash is restored to its original dir. Mirrors the write rollback.
func (j *journal) restorePrunes() error {
	for i := range j.Prunes {
		p := j.Prunes[i]
		if pathExists(p.Trash) {
			if err := os.RemoveAll(p.Dir); err != nil {
				return fmt.Errorf("prune rollback clear %s: %w", p.Dir, err)
			}
			if err := os.Rename(p.Trash, p.Dir); err != nil {
				return fmt.Errorf("prune rollback restore %s: %w", p.Dir, err)
			}
		}
	}
	return nil
}

// dropResidue removes journaled trash and any surviving stage dirs (best effort
// — they are unreferenced once the journal is cleared).
func (j *journal) dropResidue() {
	for i := range j.Writes {
		if j.Writes[i].Trash != "" {
			_ = os.RemoveAll(j.Writes[i].Trash)
		}
		if j.Writes[i].Stage != "" {
			_ = os.RemoveAll(j.Writes[i].Stage)
		}
	}
	for i := range j.Prunes {
		if j.Prunes[i].Trash != "" {
			_ = os.RemoveAll(j.Prunes[i].Trash)
		}
	}
}

// ── journal file io ─────────────────────────────────────────────────────

func journalPath(env skill.Env) string {
	return filepath.Join(env.SkillsConfigDir(), journalName)
}

// journalPending reports whether a non-empty journal awaits recovery.
func journalPending(env skill.Env) bool {
	_, ok, err := readJournal(env)
	return err == nil && ok
}

func writeJournal(env skill.Env, j *journal) error {
	return config.WriteJSONFileAtomic(journalPath(env), j)
}

// readJournal loads the journal. ok is false when the file is absent or empty.
func readJournal(env skill.Env) (*journal, bool, error) {
	data, err := os.ReadFile(journalPath(env))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, false, nil
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, false, fmt.Errorf("parse sync journal %s: %w", journalPath(env), err)
	}
	return &j, true, nil
}

func removeJournal(env skill.Env) error {
	if err := os.Remove(journalPath(env)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ── small helpers ───────────────────────────────────────────────────────

// dirMatchesDigest reports whether the on-disk tree at dir hashes to digest
// (the same canonicalization as skill_version).
func dirMatchesDigest(dir, digest string) bool {
	d, exists, err := onDiskDigest(dir)
	return err == nil && exists && d == digest
}

func isNonEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func cleanupStages(writes []journalWrite) {
	for i := range writes {
		if writes[i].Stage != "" {
			_ = os.RemoveAll(writes[i].Stage)
		}
	}
}
