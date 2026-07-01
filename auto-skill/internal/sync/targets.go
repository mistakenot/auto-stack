package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/render"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// Target is one resolved output target: a style name plus the absolute skills
// directory `sync` writes that style's union of authored + vendored skills into.
type Target struct {
	Name string // style name as written in skills.yaml (e.g. "claude", "agents")
	Dir  string // absolute path to the target's skills directory
}

// defaultTargets are the two output styles `init` seeds and `sync` falls back to
// when skills.yaml declares none (see remote-skills-design.md output-targets).
var defaultTargets = []string{"claude", "agents"}

// resolveTargets resolves the configured output targets for this project. It
// reads skills.yaml's `targets:` list (falling back to the defaults when unset),
// optionally restricts to opts.Targets-as-styles is NOT applied here — opts
// scopes skills, not target styles — and maps each style name to its on-disk
// skills directory. Order is deterministic (sorted by style name).
func resolveTargets(env skill.Env, syaml *skill.SkillsYAML) []Target {
	names := defaultTargets
	if syaml != nil && len(syaml.Targets) > 0 {
		names = syaml.Targets
	}

	seen := map[string]bool{}
	out := make([]Target, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, Target{Name: n, Dir: targetDir(env.Root, n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveTargets resolves the configured output targets (style name → absolute
// skills dir) for env. It reads skills.yaml best-effort (a missing/garbled file
// just means the default styles) and is the exported seam the adopt package uses
// to map a target style name back to its on-disk directory.
func ResolveTargets(env skill.Env) ([]Target, error) {
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		// best-effort: a missing/garbled skills.yaml just means default targets.
		syaml = nil
	}
	return resolveTargets(env, syaml), nil
}

// OnDiskDigest exposes the on-disk tree digest helper for the adopt package: it
// returns the render-canonical full-tree digest of the skill dir at dir, with
// exists=false when dir is absent. It is the same oracle sync and ownership use,
// so a copy verified against it hashes identically end to end.
func OnDiskDigest(dir string) (digest string, exists bool, err error) {
	return onDiskDigest(dir)
}

// guardTargetsWithinRoot fails the sync if any resolved target directory lies
// outside the project root. It is the render-time enforcement of the H3
// path-traversal defense: even a hand-edited skills.yaml that slips past
// skill.ValidateSkillsYAML can never make the engine write outside root.
func guardTargetsWithinRoot(env skill.Env, targets []Target) error {
	root := filepath.Clean(env.Root)
	for _, t := range targets {
		rel, err := filepath.Rel(root, filepath.Clean(t.Dir))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("refusing to render: target %q resolves to %q, outside the project root %q — remove the \"..\" or absolute path from skills.yaml targets", t.Name, t.Dir, root)
		}
	}
	return nil
}

// targetDir maps a target style name to its skills directory under root. The two
// canonical styles are "claude" → .claude/skills and "agents" → .agents/skills;
// a bare token maps to `.<name>/skills` (a leading dot is tolerated so ".codex"
// and "codex" coincide), and a name already containing a path separator is taken
// as a literal directory relative to root.
func targetDir(root, name string) string {
	n := strings.TrimSpace(name)
	if strings.ContainsAny(n, "/\\") {
		return filepath.Join(root, filepath.FromSlash(n))
	}
	n = strings.TrimPrefix(n, ".")
	return filepath.Join(root, "."+n, "skills")
}

// onDiskDigest reads the skill directory at dir into canonical TreeFiles and
// returns the full-tree digest computed by the SAME canonicalization as
// skill_version (render.CanonicalTreeFile + render.ComputeSkillVersion). The
// returned exists is false when dir is absent. This is the incremental-skip
// oracle: it hashes the raw on-disk bytes (no stamp stripping), so a user edit,
// a truncated side file, or a forged in-file stamp all change the digest and
// force a re-render — presence and the embedded stamp are never trusted alone.
func onDiskDigest(dir string) (digest string, exists bool, err error) {
	info, statErr := os.Stat(dir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", false, nil
		}
		return "", false, statErr
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("target skill path %q is not a directory", dir)
	}

	files, err := readTree(dir)
	if err != nil {
		return "", false, err
	}
	return render.ComputeSkillVersion(files), true, nil
}

// readTree reads every regular file under dir into a canonical TreeFile, keyed
// by its slash-separated path relative to dir. Symlinks are skipped (skill trees
// never contain them; the extractor rejects them on the way in).
func readTree(dir string) ([]render.TreeFile, error) {
	var files []render.TreeFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		files = append(files, render.CanonicalTreeFile(filepath.ToSlash(rel), modeForFileInfo(info), data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// modeForFileInfo maps an os.FileInfo to the git file mode render uses, deriving
// the executable bit from the owner-execute permission.
func modeForFileInfo(info os.FileInfo) string {
	if info.Mode().Perm()&0o100 != 0 {
		return render.ModeExecutable
	}
	return render.ModeFile
}

// WriteSkillDir writes a rendered skill tree into dir, creating parent
// directories and applying the executable bit from each TreeFile's mode. It is
// the staging primitive phase 5 uses to materialize a skill onto the same
// filesystem as its target before the atomic rename swap; it is also used to
// install directly when no journaling is required. dir is created fresh — the
// caller is responsible for removing any prior contents (the swap does this by
// renaming the old tree to the journal trash).
func WriteSkillDir(dir string, files []render.TreeFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skill dir %q: %w", dir, err)
	}
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create parent for %q: %w", f.Path, err)
		}
		perm := os.FileMode(0o644)
		if f.Mode == render.ModeExecutable {
			perm = 0o755
		}
		if err := os.WriteFile(target, f.Data, perm); err != nil {
			return fmt.Errorf("write %q: %w", f.Path, err)
		}
	}
	return nil
}

// StageSkillDir writes a rendered skill tree into a fresh staging directory that
// is a sibling of its final location <targetDir>/<name>, so it lives on the same
// filesystem and phase 5 can swap it into place with a single atomic rename. It
// returns the staging directory path; the caller renames it onto the final path
// (after moving any existing tree to the journal trash) and is responsible for
// cleaning the staging dir on abort.
func StageSkillDir(targetDir, name string, files []render.TreeFile) (string, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create target dir %q: %w", targetDir, err)
	}
	stage, err := os.MkdirTemp(targetDir, ".stage-"+name+"-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir for %q: %w", name, err)
	}
	if err := WriteSkillDir(stage, files); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}
