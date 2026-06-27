package add

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-skill/internal/discovery"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/source"
)

// Import safety limits — matching cache.Extract predicates.
const (
	maxImportFiles    = 2000
	maxImportTotalMiB = 64
	maxImportFileMiB  = 8

	maxImportTotal = maxImportTotalMiB * 1024 * 1024
	maxImportFile  = maxImportFileMiB * 1024 * 1024
)

// handleLocal routes local sources to git-repo or plain-dir import paths.
func handleLocal(env skill.Env, src source.Source, opts Options) (Result, error) {
	absPath := src.URL
	if !filepath.IsAbs(absPath) {
		var err error
		absPath, err = filepath.Abs(absPath)
		if err != nil {
			return Result{Source: src.URL}, fmt.Errorf("resolve local path: %w", err)
		}
	}

	if isGitRepo(absPath) {
		return handleLocalGit(env, absPath, opts)
	}
	return handleLocalPlain(env, absPath, opts)
}

// isGitRepo returns true if path is inside a git work tree.
func isGitRepo(path string) bool {
	// Fast check: .git directory or file exists.
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	// Fallback: ask git.
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// handleLocalGit imports skills from a local git repository.
func handleLocalGit(env skill.Env, absPath string, opts Options) (Result, error) {
	// Resolve HEAD commit.
	cmd := exec.Command("git", "-C", absPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return Result{Source: absPath}, fmt.Errorf("resolve local HEAD: %w", err)
	}
	sha := strings.TrimSpace(string(out))

	// Discover.
	discOpts := discovery.Options{
		Paths:     opts.Paths,
		FullDepth: opts.FullDepth,
	}
	discovered, err := discovery.Discover(absPath, discOpts)
	if err != nil {
		return Result{Source: absPath}, fmt.Errorf("discover: %w", err)
	}

	// Selection.
	selected, err := applySelection(discovered, opts)
	if err != nil {
		return Result{Source: absPath}, err
	}

	// List mode.
	if opts.List {
		listed := make([]ListedSkill, len(selected))
		for i, d := range selected {
			listed[i] = ListedSkill{
				Name:      effectiveName(d, opts.As, len(selected)),
				Subpath:   d.Subpath,
				NameValid: d.NameValid,
				NeedsAs:   !d.NameValid,
				Container: d.Container,
			}
		}
		return Result{Listed: listed, Source: absPath}, nil
	}

	versionSpec := opts.Version
	if versionSpec == "" {
		versionSpec = "latest"
	}

	// Write lock + skills.yaml.
	lock, err := loadOrCreateLock(env)
	if err != nil {
		return Result{Source: absPath}, err
	}
	syaml, err := loadOrCreateSkillsYAML(env)
	if err != nil {
		return Result{Source: absPath}, err
	}

	var added []AddedSkill
	for _, d := range selected {
		name := effectiveName(d, opts.As, len(selected))

		if err := skill.ValidateSkillName(name); err != nil {
			return Result{Source: absPath}, &AddError{
				Code:    CodeInvalidSkillName,
				Message: fmt.Sprintf("skill %q has invalid name; use --as to provide a valid name: %s", d.Name, err),
			}
		}

		if existing, ok := lock.Skills[name]; ok {
			if existing.URL != absPath {
				return Result{Source: absPath}, &AddError{
					Code:    CodeNameCollision,
					Message: fmt.Sprintf("skill %q already exists from %s; use --as to rename", name, existing.URL),
				}
			}
		}

		lock.Skills[name] = skill.LockEntry{
			Source:      absPath,
			URL:         absPath,
			VersionSpec: versionSpec,
			Ref:         sha,
			Commit:      sha,
			Subpath:     d.Subpath,
			Local:       true,
			State:       "resolved",
		}

		if _, exists := syaml.Skills[name]; !exists {
			if syaml.Skills == nil {
				syaml.Skills = make(map[string]skill.SkillConfig)
			}
			syaml.Skills[name] = skill.SkillConfig{
				Version: versionSpec,
			}
		}

		added = append(added, AddedSkill{
			Name:        name,
			Subpath:     d.Subpath,
			Commit:      sha,
			VersionSpec: versionSpec,
			Local:       true,
		})
	}

	// Validate and write.
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		return Result{Source: absPath}, fmt.Errorf("create config dir: %w", err)
	}

	if lockErrs := skill.ValidateLock(lock); len(lockErrs) > 0 {
		return Result{Source: absPath}, fmt.Errorf("lock validation: %v", lockErrs)
	}
	if yamlErrs := skill.ValidateSkillsYAML(syaml); len(yamlErrs) > 0 {
		return Result{Source: absPath}, fmt.Errorf("skills.yaml validation: %v", yamlErrs)
	}

	if err := writeJSONLock(env.LockPath(), lock); err != nil {
		return Result{Source: absPath}, err
	}
	if err := writeSkillsYAML(env.SkillsYAMLPath(), syaml); err != nil {
		return Result{Source: absPath}, err
	}

	return Result{Added: added, Source: absPath}, nil
}

// handleLocalPlain imports a non-git directory by copying into ./skills/<name>.
func handleLocalPlain(env skill.Env, absPath string, opts Options) (Result, error) {
	// Discover.
	discOpts := discovery.Options{
		Paths:     opts.Paths,
		FullDepth: opts.FullDepth,
	}
	discovered, err := discovery.Discover(absPath, discOpts)
	if err != nil {
		return Result{Source: absPath}, fmt.Errorf("discover: %w", err)
	}

	selected, err := applySelection(discovered, opts)
	if err != nil {
		return Result{Source: absPath}, err
	}

	if opts.List {
		listed := make([]ListedSkill, len(selected))
		for i, d := range selected {
			listed[i] = ListedSkill{
				Name:      effectiveName(d, opts.As, len(selected)),
				Subpath:   d.Subpath,
				NameValid: d.NameValid,
				NeedsAs:   !d.NameValid,
				Container: d.Container,
			}
		}
		return Result{Listed: listed, Source: absPath}, nil
	}

	skillsDir := env.SkillsDir()
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return Result{Source: absPath}, fmt.Errorf("create skills dir: %w", err)
	}

	var added []AddedSkill
	for _, d := range selected {
		name := effectiveName(d, opts.As, len(selected))

		if err := skill.ValidateSkillName(name); err != nil {
			return Result{Source: absPath}, &AddError{
				Code:    CodeInvalidSkillName,
				Message: fmt.Sprintf("skill %q has invalid name; use --as to provide a valid name: %s", d.Name, err),
			}
		}

		destDir := filepath.Join(skillsDir, name)

		// Collision check.
		if _, err := os.Stat(destDir); err == nil {
			if !opts.Force {
				return Result{Source: absPath}, &AddError{
					Code:    CodeImportCollision,
					Message: fmt.Sprintf("skills/%s already exists; use --force to overwrite", name),
				}
			}
			if err := os.RemoveAll(destDir); err != nil {
				return Result{Source: absPath}, fmt.Errorf("remove existing %s: %w", destDir, err)
			}
		}

		// Copy source skill directory into ./skills/<name>.
		srcDir := filepath.Join(absPath, d.Subpath)
		if d.Subpath == "." {
			srcDir = absPath
		}
		if err := safeCopyDir(srcDir, destDir); err != nil {
			return Result{Source: absPath}, fmt.Errorf("copy skill %q: %w", name, err)
		}

		added = append(added, AddedSkill{
			Name:    name,
			Subpath: d.Subpath,
			Local:   true,
		})
	}

	// No lock entry or skills.yaml stub for plain imports — they are authored.
	return Result{Added: added, Source: absPath}, nil
}

// safeCopyDir copies src to dest with archive-safety predicates:
// no symlinks, no special files, file count limit, size limits, containment.
func safeCopyDir(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	var fileCount int
	var totalSize int64

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dest, rel)

		// Containment: target must stay within dest.
		cleanTarget := filepath.Clean(target)
		cleanDest := filepath.Clean(dest) + string(filepath.Separator)
		if cleanTarget != filepath.Clean(dest) && !strings.HasPrefix(cleanTarget, cleanDest) {
			return fmt.Errorf("path escapes destination: %s", rel)
		}

		info, lstatErr := os.Lstat(path)
		if lstatErr != nil {
			return lstatErr
		}

		// Reject symlinks.
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed in skill trees: %s", rel)
		}

		// Reject special files.
		if !info.Mode().IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("special file not allowed in skill trees: %s", rel)
		}

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		// Regular file checks.
		fileCount++
		if fileCount > maxImportFiles {
			return fmt.Errorf("source exceeds %d file limit", maxImportFiles)
		}
		if info.Size() > maxImportFile {
			return fmt.Errorf("file %s is %d bytes, exceeding %d MiB limit", rel, info.Size(), maxImportFileMiB)
		}
		totalSize += info.Size()
		if totalSize > maxImportTotal {
			return fmt.Errorf("source total size exceeds %d MiB limit", maxImportTotalMiB)
		}

		return copyFile(path, target)
	})
}

// copyFile copies a single regular file.
func copyFile(src, dest string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sf.Close() }()

	info, err := sf.Stat()
	if err != nil {
		return err
	}

	df, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()&0o755|0o644)
	if err != nil {
		return err
	}
	defer func() { _ = df.Close() }()

	_, err = io.Copy(df, sf)
	return err
}

// writeJSONLock writes lock.json via atomic write.
func writeJSONLock(path string, lock *skill.Lock) error {
	data, err := skill.EncodeJSON(lock)
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
