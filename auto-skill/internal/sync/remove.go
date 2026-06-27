package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
)

// Selector chooses which source(s) of truth `Remove` drops for a skill name. A
// skill may exist as authored (./skills/<name>/), vendored (a lock entry), or
// both; the selector disambiguates when both are present.
type Selector int

const (
	// SelUnset infers the selector — valid only when exactly one of the
	// authored/vendored sources exists for the name (ambiguous otherwise).
	SelUnset Selector = iota
	// SelLocal removes the authored ./skills/<name>/ source.
	SelLocal
	// SelVendored removes the vendored source (the lock + skills.yaml entry).
	SelVendored
)

// RemoveResult is the JSON-serializable outcome of a `remove` run. Removed lists
// which sources of truth were dropped ("local" / "vendored"); Pruned/Reported
// reflect the receipt-gated reconcile that converges the targets afterwards.
type RemoveResult struct {
	Name     string   `json:"name"`
	Removed  []string `json:"removed"`            // sources dropped: "local" and/or "vendored"
	Pruned   []string `json:"pruned,omitempty"`   // target/skill entries pruned by the reconcile
	Reported []string `json:"reported,omitempty"` // target copies NOT deleted (no receipt / modified)
	Errors   []string `json:"errors,omitempty"`   // reconcile-time errors (post-mutation)
}

// Remove drops a skill's source of truth and then reconciles every output target
// by RE-RUNNING the sync engine (Options{Locked:true}). Because the removed skill
// is no longer in the desired set, phase 2's receipt-gated journaled prune deletes
// its now-orphaned target copies automatically — and only those a local receipt
// authorizes; a modified or unestablished copy is reported, never deleted. This
// reuses the existing prune path rather than re-implementing deletion.
//
// A returned error is reserved for fail-fast USAGE problems (unknown name,
// ambiguous selector, mismatched selector) — these mutate nothing so the CLI can
// map them to a usage exit. Post-mutation reconcile failures land in
// RemoveResult.Errors (the source of truth is already dropped at that point).
func Remove(env skill.Env, name string, sel Selector) (RemoveResult, error) {
	res := RemoveResult{Name: name}

	if err := skill.ValidateSkillName(name); err != nil {
		return res, err
	}

	// Detect existence of each source.
	local := dirExists(filepath.Join(env.SkillsDir(), name))

	lock, err := loadLock(env)
	if err != nil {
		return res, fmt.Errorf("load lock: %w", err)
	}
	_, vendored := lock.Skills[name]

	// Selector validation — fail-fast, no mutation on error.
	switch {
	case !local && !vendored:
		return res, fmt.Errorf("no skill named %q found (not in ./skills/ or the lock); nothing to remove", name)
	case local && vendored && sel == SelUnset:
		return res, fmt.Errorf("%q exists as both a local and a vendored skill; pass --local or --vendored to choose", name)
	case sel == SelLocal && !local:
		return res, fmt.Errorf("no local (authored ./skills/%s/) skill named %q to remove; it exists only as a vendored skill — use --vendored", name, name)
	case sel == SelVendored && !vendored:
		return res, fmt.Errorf("no vendored (locked) skill named %q to remove; it exists only as a local skill — use --local", name)
	}

	// Resolve SelUnset to the single present source.
	if sel == SelUnset {
		if local {
			sel = SelLocal
		} else {
			sel = SelVendored
		}
	}

	// Apply the drop.
	switch sel {
	case SelLocal:
		if err := os.RemoveAll(filepath.Join(env.SkillsDir(), name)); err != nil {
			return res, fmt.Errorf("remove authored skill dir: %w", err)
		}
		res.Removed = append(res.Removed, "local")
	case SelVendored:
		delete(lock.Skills, name)
		if err := config.WriteJSONFileAtomic(env.LockPath(), lock); err != nil {
			return res, fmt.Errorf("rewrite lock without %q: %w", name, err)
		}
		if err := removeSkillsYAMLEntry(env.SkillsYAMLPath(), name); err != nil {
			return res, fmt.Errorf("drop skills.yaml entry for %q: %w", name, err)
		}
		res.Removed = append(res.Removed, "vendored")
	}

	// Reconcile: the removed skill is no longer desired, so the receipt-gated
	// journaled prune converges every target (deleting only authorized copies).
	run, runErr := Run(env, Options{Locked: true})
	if runErr != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("reconcile after removing %q: %v", name, runErr))
		return res, nil
	}
	res.Errors = append(res.Errors, run.Errors...)

	// Map the reconcile's prunes for this name into Pruned.
	suffix := "/" + name
	for _, p := range run.Pruned {
		if strings.HasSuffix(p, suffix) {
			res.Pruned = append(res.Pruned, p)
		}
	}

	// Any target copy of the removed name that SURVIVED the reconcile was held
	// back by the deletion authority (no receipt / modified / foreign) — surface
	// it as reported-not-deleted so the user knows a copy remains.
	res.Reported = append(res.Reported, survivingTargetCopies(env, name, res.Pruned)...)

	return res, nil
}

// survivingTargetCopies returns "style/name" for every output target that still
// holds a dir for name after the reconcile but did NOT prune it — i.e. copies the
// receipt-gated prune reported rather than deleted (G-no-foreign-delete).
func survivingTargetCopies(env skill.Env, name string, pruned []string) []string {
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		syaml = nil
	}
	prunedSet := make(map[string]bool, len(pruned))
	for _, p := range pruned {
		prunedSet[p] = true
	}
	var out []string
	for _, t := range resolveTargets(env, syaml) {
		key := t.Name + "/" + name
		if prunedSet[key] {
			continue
		}
		if dirExists(filepath.Join(t.Dir, name)) {
			out = append(out, key)
		}
	}
	return out
}

// removeSkillsYAMLEntry deletes the skills.<name> entry from skills.yaml using a
// node-level edit so surrounding comments, key order and formatting are
// preserved (a typed re-marshal would discard them). The key and its value are
// two consecutive entries in the mapping node's Content slice. A missing file or
// a missing key is not an error — the skill may be authored-only or declared
// elsewhere.
func removeSkillsYAMLEntry(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skills.yaml: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse skills.yaml: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	changed := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "skills" {
			continue
		}
		skillsNode := root.Content[i+1]
		if skillsNode.Kind != yaml.MappingNode {
			break
		}
		for j := 0; j+1 < len(skillsNode.Content); j += 2 {
			if skillsNode.Content[j].Value == name {
				// Drop the key node and its value node (two consecutive entries).
				skillsNode.Content = append(skillsNode.Content[:j], skillsNode.Content[j+2:]...)
				changed = true
				break
			}
		}
		break
	}
	if !changed {
		return nil
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("re-encode skills.yaml: %w", err)
	}
	return writeFileAtomic(path, out)
}

// writeFileAtomic writes raw bytes via a temp file in the same directory followed
// by an atomic rename (mirrors config.WriteJSONFileAtomic for non-JSON payloads).
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp into %s: %w", path, err)
	}
	return nil
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
