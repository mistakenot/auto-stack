// Package adopt pulls a foreign (un-managed) skill dir that was hand-dropped
// into an output target back into the project's authored source of truth
// (./skills/<name>/). It performs a filesystem-agnostic staged move — copy into
// a temp sibling of the destination, verify the copied tree (full-tree digest
// equal to the source AND a readable SKILL.md), atomically rename it into place,
// then remove the source — and finally `git add`s the new path. It never uses
// `git mv` (the source is normally untracked/gitignored, so `git mv` would
// fail) and a failure before the source removal leaves no half-written
// destination. adopt deliberately does NOT re-render; the next `sync` does.
package adopt

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/ownership"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
)

// Options controls an Adopt run.
type Options struct {
	// All adopts every unambiguous adoptable candidate (a divergent candidate
	// still errors and requires --from).
	All bool
	// Force overwrites an existing ./skills/<name>/ instead of refusing.
	Force bool
	// Yes is accepted for CLI symmetry; adopt is non-interactive so it has no
	// behavioral effect here.
	Yes bool
	// From selects which target's copy to adopt when copies diverge across
	// targets. It is a target STYLE name (e.g. "claude").
	From string
}

// Candidate is one adoptable foreign skill and the target copies that carry it.
type Candidate struct {
	Name   string `json:"name"`
	Copies []Copy `json:"copies"` // one per target containing a foreign dir of this name
}

// Copy is one foreign copy of a candidate in a single target.
type Copy struct {
	Target string `json:"target"` // target STYLE name
	Digest string `json:"digest"` // on-disk full-tree digest
}

// Adopted records one successful adoption.
type Adopted struct {
	Name string `json:"name"`
	From string `json:"from"` // target style the copy was taken from
	Dir  string `json:"dir"`  // project-relative destination, e.g. "skills/new-plan"
}

// Result is the outcome of an Adopt run.
type Result struct {
	// Candidates is populated only in list mode (no names and no --all): the
	// sorted set of adoptable foreign skills, with no filesystem change made.
	Candidates []Candidate `json:"candidates,omitempty"`
	Adopted    []Adopted   `json:"adopted,omitempty"`
	Errors     []string    `json:"errors,omitempty"`
}

// Adopt classifies the project's output targets, groups the foreign (adoptable)
// dirs by name, and — unless in list mode — stage-moves the chosen copy of each
// requested name into ./skills/<name>/ and git-adds it.
//
// List mode (len(names)==0 && !opts.All) makes no filesystem change and returns
// the sorted candidates. Otherwise the set to adopt is every candidate (--all)
// or the requested names. Divergent multi-target copies are a hard error that
// requires --from <target>; adopt never silently picks one and discards the rest.
func Adopt(env skill.Env, names []string, opts Options) (Result, error) {
	desired, err := sync.DesiredSet(env)
	if err != nil {
		return Result{}, err
	}
	inputs, err := sync.ScanOwnership(env, desired)
	if err != nil {
		return Result{}, err
	}
	verdicts := ownership.Classify(inputs)
	adoptable := ownership.Adoptable(verdicts)

	// style → absolute skills dir, so a copy's source path is
	// filepath.Join(absDir[style], name).
	targets, err := sync.ResolveTargets(env)
	if err != nil {
		return Result{}, err
	}
	absDir := make(map[string]string, len(targets))
	for _, t := range targets {
		absDir[t.Name] = t.Dir
	}

	// Group adoptable verdicts by name into candidates.
	byName := map[string]*Candidate{}
	for _, v := range adoptable {
		c, ok := byName[v.Name]
		if !ok {
			c = &Candidate{Name: v.Name}
			byName[v.Name] = c
		}
		c.Copies = append(c.Copies, Copy{Target: v.Target, Digest: v.OnDiskDigest})
	}
	for _, c := range byName {
		sort.Slice(c.Copies, func(i, j int) bool { return c.Copies[i].Target < c.Copies[j].Target })
	}

	// List mode: no names and not --all → report candidates, change nothing.
	if len(names) == 0 && !opts.All {
		cands := make([]Candidate, 0, len(byName))
		for _, c := range byName {
			cands = append(cands, *c)
		}
		sort.Slice(cands, func(i, j int) bool { return cands[i].Name < cands[j].Name })
		return Result{Candidates: cands}, nil
	}

	// Determine which names to adopt.
	var want []string
	if opts.All {
		for name := range byName {
			want = append(want, name)
		}
		sort.Strings(want)
	} else {
		want = names
	}

	var result Result
	for _, name := range want {
		cand := byName[name]
		var copies []Copy
		if cand != nil {
			copies = cand.Copies
		}
		if len(copies) == 0 {
			result.Errors = append(result.Errors,
				fmt.Sprintf("no adoptable (foreign) skill named %q in any target", name))
			continue
		}

		chosen, chooseErr := chooseCopy(name, copies, opts.From)
		if chooseErr != nil {
			result.Errors = append(result.Errors, chooseErr.Error())
			continue
		}

		dst := filepath.Join(env.SkillsDir(), name)
		if dirExists(dst) && !opts.Force {
			result.Errors = append(result.Errors,
				fmt.Sprintf("./skills/%s already exists; pass --force to overwrite", name))
			continue
		}

		base, ok := absDir[chosen.Target]
		if !ok {
			result.Errors = append(result.Errors,
				fmt.Sprintf("internal: target %q has no resolved directory", chosen.Target))
			continue
		}
		src := filepath.Join(base, name)

		cleanup, mvErr := stageMove(src, dst, opts.Force)
		if mvErr != nil {
			// stageMove self-cleans before the source removal — no half-written dst.
			result.Errors = append(result.Errors,
				fmt.Sprintf("adopt %q: %v", name, mvErr))
			continue
		}

		rel := filepath.ToSlash(filepath.Join("skills", name))
		gitErr := runGit(env.Root, "add", rel)
		// The move already committed (source removed); a git add failure must NOT
		// roll it back — the file is in place. Report it as a warning-style error.
		cleanup()
		if gitErr != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("adopt %q: moved into %s but `git add` failed (add it manually): %v", name, rel, gitErr))
			continue
		}
		result.Adopted = append(result.Adopted, Adopted{Name: name, From: chosen.Target, Dir: rel})
	}

	return result, nil
}

// chooseCopy picks the copy to adopt. A single copy is taken as-is. Multiple
// copies with identical digests collapse to one (the target sorting first, for
// determinism). Divergent digests are a hard error: --from <target> selects the
// source, and without it the error lists each target=digest and demands --from.
func chooseCopy(name string, copies []Copy, from string) (Copy, error) {
	if len(copies) == 1 {
		if from != "" && copies[0].Target != from {
			return Copy{}, fmt.Errorf("adopt %q: no copy in target %q (available: %s)", name, from, copies[0].Target)
		}
		return copies[0], nil
	}

	// copies are pre-sorted by Target.
	allSame := true
	for _, c := range copies[1:] {
		if c.Digest != copies[0].Digest {
			allSame = false
			break
		}
	}
	if allSame {
		if from != "" {
			for _, c := range copies {
				if c.Target == from {
					return c, nil
				}
			}
			return Copy{}, fmt.Errorf("adopt %q: no copy in target %q", name, from)
		}
		return copies[0], nil
	}

	// Divergent.
	if from != "" {
		for _, c := range copies {
			if c.Target == from {
				return c, nil
			}
		}
		return Copy{}, fmt.Errorf("adopt %q: no copy in target %q (divergent copies: %s)", name, from, describeCopies(copies))
	}
	return Copy{}, fmt.Errorf(
		"adopt %q: copies differ across targets (%s) — pick the source with --from <target>",
		name, describeCopies(copies))
}

// describeCopies renders "target=digest12chars" for each copy, space-separated.
func describeCopies(copies []Copy) string {
	parts := make([]string, 0, len(copies))
	for _, c := range copies {
		parts = append(parts, fmt.Sprintf("%s=%s", c.Target, shortDigest(c.Digest)))
	}
	return strings.Join(parts, " ")
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

// stageMove performs a filesystem-agnostic move of src into dst: it copies src
// into a temp sibling of dst, verifies the copy (tree digest == src digest AND a
// readable SKILL.md), moves any existing dst aside (only reached with force),
// atomically renames the staged copy into dst, then removes src. Any failure
// before the src removal leaves NO half-written dst (it cleans the stage and
// restores any moved-aside dst). The returned cleanup drops the moved-aside
// trash on success; it is safe to call once the move is committed.
func stageMove(src, dst string, force bool) (cleanup func(), err error) {
	// Defensive guard: the caller already refuses a non-force overwrite, but a
	// move that would clobber an existing dst without force must never proceed.
	if dirExists(dst) && !force {
		return func() {}, fmt.Errorf("%s already exists (use --force to overwrite)", filepath.Base(dst))
	}
	parent := filepath.Dir(dst)
	if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
		return func() {}, fmt.Errorf("create ./skills parent: %w", mkErr)
	}

	stage, stErr := os.MkdirTemp(parent, ".adopt-stage-*")
	if stErr != nil {
		return func() {}, fmt.Errorf("create staging dir: %w", stErr)
	}
	if cpErr := copyTree(src, stage); cpErr != nil {
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("copy source tree: %w", cpErr)
	}

	// Verify: the staged tree hashes identically to the source AND has a readable
	// SKILL.md. A mismatch means a partial/corrupt copy — never proceed.
	stageDigest, _, sdErr := sync.OnDiskDigest(stage)
	if sdErr != nil {
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("digest staged copy: %w", sdErr)
	}
	srcDigest, _, srcErr := sync.OnDiskDigest(src)
	if srcErr != nil {
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("digest source: %w", srcErr)
	}
	if stageDigest != srcDigest {
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("staged copy digest %s != source digest %s — aborting move", shortDigest(stageDigest), shortDigest(srcDigest))
	}
	if _, mdErr := os.Stat(filepath.Join(stage, "SKILL.md")); mdErr != nil {
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("staged copy missing readable SKILL.md: %w", mdErr)
	}

	// Move any existing dst aside (only reachable with force; the caller already
	// refused a non-force overwrite).
	trash := ""
	if dirExists(dst) {
		tmp, trErr := os.MkdirTemp(parent, ".adopt-trash-*")
		if trErr != nil {
			_ = os.RemoveAll(stage)
			return func() {}, fmt.Errorf("create trash dir: %w", trErr)
		}
		_ = os.Remove(tmp) // free the unique name so Rename can claim it
		if rnErr := os.Rename(dst, tmp); rnErr != nil {
			_ = os.RemoveAll(stage)
			return func() {}, fmt.Errorf("move existing %s aside: %w", dst, rnErr)
		}
		trash = tmp
	}

	if rnErr := os.Rename(stage, dst); rnErr != nil {
		// Restore the moved-aside dst, drop the stage.
		if trash != "" {
			_ = os.Rename(trash, dst)
		}
		_ = os.RemoveAll(stage)
		return func() {}, fmt.Errorf("rename staged copy into place: %w", rnErr)
	}

	// Commit the move: remove the source.
	if rmErr := os.RemoveAll(src); rmErr != nil {
		// dst is already in place and valid; surface the failure to remove src but
		// keep the (committed) move. Drop trash via cleanup.
		return func() { _ = os.RemoveAll(trash) }, fmt.Errorf("remove source after move: %w", rmErr)
	}

	return func() {
		if trash != "" {
			_ = os.RemoveAll(trash)
		}
	}, nil
}

// copyTree recursively copies the regular files under src into dst, creating
// directories and preserving the executable bit. Symlinks are skipped (skill
// trees never contain them).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // skip symlinks
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single regular file, preserving the owner-execute bit.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	perm := os.FileMode(0o644)
	if info.Mode().Perm()&0o100 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// runGit runs `git -C root <args...>`, returning a descriptive error (including
// stderr) on failure.
func runGit(root string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return nil
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
