package inspect

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

// loaders.go is the single seam over the 032/033/034 typed readers (lock,
// manifest, skills.yaml) and 034's on-disk-tree-digest computation. The triad and
// the sub-resources all load project state through here, so a merged-schema name
// change is absorbed in one place.

// defaultTargets mirrors sync.defaultTargets — the two output styles `init` seeds
// and inspect falls back to when skills.yaml declares none. Replicated (not
// imported) because the sync resolver is unexported and inspect must not depend on
// the write engine.
var defaultTargets = []string{"claude", "agents"}

// targetRef is a resolved output target: its style name and absolute skills dir.
type targetRef struct {
	Name string
	Dir  string
}

// loadLock reads .auto/skills/lock.json. A nil result (no error) means the file is
// absent — an un-initialised or authored-only project.
func loadLock(env skill.Env) (*skill.Lock, error) {
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read lock: %w", err)
	}
	lock, err := skill.ParseLock(data)
	if err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	return lock, nil
}

// loadManifest reads .auto/skills/manifest.json. A nil result (no error) means the
// file is absent — the stale flag then degrades to null (unknown).
func loadManifest(env skill.Env) (*skill.Manifest, error) {
	data, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := skill.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

// loadProjectConfig reads .auto/skills/skills.yaml. A nil result (no error) means
// the file is absent; callers fall back to defaultTargets.
func loadProjectConfig(env skill.Env) (*skill.SkillsYAML, error) {
	data, err := os.ReadFile(env.SkillsYAMLPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills.yaml: %w", err)
	}
	cfg, err := skill.ParseSkillsYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse skills.yaml: %w", err)
	}
	return cfg, nil
}

// resolveTargets resolves the configured output targets, deduped and sorted by
// style name. It mirrors sync.resolveTargets/targetDir (which are unexported) so
// inspect can compute target paths without depending on the write engine.
func resolveTargets(env skill.Env, cfg *skill.SkillsYAML) []targetRef {
	names := defaultTargets
	if cfg != nil && len(cfg.Targets) > 0 {
		names = cfg.Targets
	}
	seen := map[string]bool{}
	out := make([]targetRef, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, targetRef{Name: n, Dir: targetDir(env.Root, n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// targetDir maps a target style name to its skills directory under root, matching
// sync.targetDir: "claude" → .claude/skills, "agents" → .agents/skills, a bare
// token → .<name>/skills (a leading dot tolerated), and a name with a path
// separator → a literal relative directory.
func targetDir(root, name string) string {
	n := strings.TrimSpace(name)
	if strings.ContainsAny(n, "/\\") {
		return filepath.Join(root, filepath.FromSlash(n))
	}
	n = strings.TrimPrefix(n, ".")
	return filepath.Join(root, "."+n, "skills")
}

// treeDigest reads the rendered skill tree at dir and returns its full-tree digest
// computed by the SAME canonicalization as skill_version. exists is false (no
// error) when dir is absent. This is the offline stale oracle — it hashes the raw
// on-disk bytes and never fetches. It mirrors sync.onDiskDigest, replicated here
// over the exported render primitives so inspect stays free of the write engine.
func treeDigest(dir string) (digest string, exists bool, err error) {
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

// readTree reads every regular file under dir into a canonical render.TreeFile,
// keyed by its slash-separated path relative to dir; symlinks are skipped. It
// mirrors sync.readTree so a tree read back off disk hashes identically to the
// tree render produced.
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
// the executable bit from the owner-execute permission (mirrors sync).
func modeForFileInfo(info os.FileInfo) string {
	if info.Mode().Perm()&0o100 != 0 {
		return render.ModeExecutable
	}
	return render.ModeFile
}
