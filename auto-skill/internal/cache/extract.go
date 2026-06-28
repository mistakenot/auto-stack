package cache

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"path/filepath"
	"strings"
)

// ErrSubpathNotFound is returned (wrapped) by Extract when the requested
// sha:subpath does not resolve in the cached repo even though the commit is
// present — i.e. the subpath was renamed or removed upstream. Callers detect it
// with errors.Is so they can surface a remediation hint instead of a raw git
// failure.
var ErrSubpathNotFound = errors.New("subpath not found in commit")

const (
	MaxExtractFiles    = 2000
	MaxExtractTotalMiB = 64
	MaxExtractFileMiB  = 8

	maxExtractTotal = MaxExtractTotalMiB * 1024 * 1024
	maxExtractFile  = MaxExtractFileMiB * 1024 * 1024

	CodeSymlinkEntry   = "symlink_entry"
	CodeSpecialEntry   = "special_entry"
	CodeTooManyFiles   = "too_many_files"
	CodeSizeLimitFile  = "size_limit_file"
	CodeSizeLimitTotal = "size_limit_total"
)

// Test-overridable limits (production code uses the constants above).
var (
	maxExtractFilesForTest int64 = MaxExtractFiles
	maxExtractFileForTest  int64 = maxExtractFile
	maxExtractTotalForTest int64 = maxExtractTotal
)

type ExtractError struct {
	Code    string
	Message string
}

func (e *ExtractError) Error() string { return e.Message }

// Extract streams `git archive` for sha:subpath and writes files to dest.
// Every archive entry is validated before the first write; a rejected extract
// leaves dest empty. A non-empty subpath scopes the archive to that subtree
// (entries are written relative to dest, stripped of the subpath prefix).
func (r *Repo) Extract(sha, subpath, dest string) error {
	treeish := sha
	if subpath != "" {
		treeish = sha + ":" + subpath
	}
	return r.extractArchive([]string{"archive", "--format=tar", treeish}, subpath, dest)
}

// ExtractPaths archives only the given repo-relative paths in a single
// `git archive <sha> -- <paths...>` invocation and writes them under dest,
// preserving each path's full repo-relative location. Unlike Extract it never
// archives the whole tree, so a caller materializing a few skill subtrees from a
// large repo avoids both the size and the unrelated symlinks elsewhere in the
// tree (which the safe extractor would reject). With no paths it is a no-op.
func (r *Repo) ExtractPaths(sha string, paths []string, dest string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"archive", "--format=tar", sha, "--"}, paths...)
	return r.extractArchive(args, "", dest)
}

// ListSkillDirs lists the repo-relative directories that directly contain a
// SKILL.md at commit sha, derived from a single `git ls-tree -r` listing.
// Because ls-tree does not descend through symlinks, paths behind symlinked
// directories are not reported, so the result is symlink-free and safe to pass
// to ExtractPaths. A SKILL.md at the repo root yields "" (the whole tree).
func (r *Repo) ListSkillDirs(sha string) ([]string, error) {
	out, err := runGitOffline(r.Path, "ls-tree", "-r", "--name-only", sha)
	if err != nil {
		return nil, err
	}
	var dirs []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || gopath.Base(line) != "SKILL.md" {
			continue
		}
		dir := gopath.Dir(line)
		if dir == "." {
			dir = ""
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// extractArchive runs the validate pass and then the write pass for a
// `git archive` invocation described by args. Splitting the passes keeps the
// "a rejected extract leaves dest empty" contract: nothing is written until
// every entry has passed validation.
func (r *Repo) extractArchive(args []string, subpath, dest string) error {
	var totalSize int64
	var fileCount int
	if err := r.runArchive(args, subpath, validateConsumer(dest, &fileCount, &totalSize)); err != nil {
		return err
	}
	return r.runArchive(args, subpath, writeConsumer(dest))
}

// validateConsumer returns a tar consumer that validates every entry against
// the safety rules (size, count, symlink/special, path containment) without
// writing anything.
func validateConsumer(dest string, fileCount *int, totalSize *int64) func(*tar.Reader) error {
	return func(tr *tar.Reader) error {
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("read archive: %w", err)
			}
			if err := validateEntry(hdr, dest, fileCount, totalSize); err != nil {
				return err
			}
		}
	}
}

// writeConsumer returns a tar consumer that writes each entry under dest. It is
// only run after validateConsumer has accepted the whole stream.
func writeConsumer(dest string) func(*tar.Reader) error {
	return func(tr *tar.Reader) error {
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("read archive (write pass): %w", err)
			}

			if hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader {
				continue
			}

			target := filepath.Join(dest, filepath.FromSlash(hdr.Name))
			cleanTarget := filepath.Clean(target)
			cleanDest := filepath.Clean(dest) + string(filepath.Separator)
			if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(dest) {
				return &ExtractError{Code: CodeSpecialEntry, Message: "path escapes dest: " + hdr.Name}
			}

			switch hdr.Typeflag {
			case tar.TypeDir:
				if err := safeMkdirAll(target, dest); err != nil {
					return fmt.Errorf("mkdir %s: %w", hdr.Name, err)
				}
			case tar.TypeReg:
				if err := safeMkdirAll(filepath.Dir(target), dest); err != nil {
					return fmt.Errorf("mkdir parent %s: %w", hdr.Name, err)
				}
				if err := writeFile(target, hdr, tr); err != nil {
					return err
				}
			}
		}
	}
}

// runArchive runs `git <args...>` (a `git archive`) and hands the tar stream to
// consume. It ALWAYS drains git's stdout before Wait — even when consume returns
// early (e.g. a rejected entry) — so git can never block writing into a full
// 64KB pipe buffer while we wait for it to exit (a classic pipe deadlock).
func (r *Repo) runArchive(args []string, subpath string, consume func(*tar.Reader) error) error {
	cmd := newGitCmd(r.Path, nil, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe git archive: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start git archive: %w", err)
	}

	consumeErr := consume(tar.NewReader(stdout))
	// Drain whatever git still wants to write so it can finish and exit; without
	// this an early return from consume leaves git blocked on a full pipe and the
	// Wait below deadlocks.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if consumeErr != nil {
		return consumeErr
	}
	if waitErr != nil {
		// git emits "not a valid object name: <sha>:<subpath>" when the subpath
		// no longer exists in the (present) commit — surface a sentinel so callers
		// can report a rename/removal instead of a raw archive failure.
		if subpath != "" && strings.Contains(stderr.String(), "not a valid object name") {
			return fmt.Errorf("git archive %s: %w", archiveTreeish(args), ErrSubpathNotFound)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git archive: %s", msg)
		}
		return fmt.Errorf("git archive: %w", waitErr)
	}
	return nil
}

// archiveTreeish returns the <treeish> argument of a
// `git archive --format=tar <treeish>` invocation for use in error messages.
func archiveTreeish(args []string) string {
	if len(args) >= 3 {
		return args[2]
	}
	return ""
}

func writeFile(target string, hdr *tar.Header, r io.Reader) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755|0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", hdr.Name, err)
	}
	defer func() { _ = f.Close() }()
	lr := &io.LimitedReader{R: r, N: maxExtractFileForTest + 1}
	if _, err := io.Copy(f, lr); err != nil {
		return fmt.Errorf("write %s: %w", hdr.Name, err)
	}
	return nil
}

// safeMkdirAll creates directories like os.MkdirAll but Lstats each component
// under dest to reject pre-existing symlinks that would escape the extract root.
func safeMkdirAll(target, dest string) error {
	cleanDest := filepath.Clean(dest)
	rel, err := filepath.Rel(cleanDest, filepath.Clean(target))
	if err != nil {
		return err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	cur := cleanDest
	for _, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			if mkErr := os.Mkdir(cur, 0o755); mkErr != nil && !os.IsExist(mkErr) {
				return mkErr
			}
			continue
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return &ExtractError{
				Code:    CodeSymlinkEntry,
				Message: fmt.Sprintf("pre-existing symlink at %q would escape extract root", cur),
			}
		}
		if !fi.IsDir() {
			return fmt.Errorf("path component %q is not a directory", cur)
		}
	}
	return nil
}

// validateEntry checks a single tar header against safety rules.
func validateEntry(hdr *tar.Header, dest string, fileCount *int, totalSize *int64) error {
	switch hdr.Typeflag {
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil // pax headers are metadata, skip
	case tar.TypeSymlink, tar.TypeLink:
		return &ExtractError{
			Code:    CodeSymlinkEntry,
			Message: fmt.Sprintf("archive contains symlink %q; symlinks are not allowed in skill trees", hdr.Name),
		}
	case tar.TypeDir, tar.TypeReg:
		// ok
	default:
		return &ExtractError{
			Code:    CodeSpecialEntry,
			Message: fmt.Sprintf("archive contains special entry %q (type %d); only regular files and directories are allowed", hdr.Name, hdr.Typeflag),
		}
	}

	if hdr.Typeflag == tar.TypeReg {
		*fileCount++
		if int64(*fileCount) > maxExtractFilesForTest {
			return &ExtractError{
				Code:    CodeTooManyFiles,
				Message: fmt.Sprintf("archive exceeds %d file limit", maxExtractFilesForTest),
			}
		}
		if hdr.Size > maxExtractFileForTest {
			return &ExtractError{
				Code:    CodeSizeLimitFile,
				Message: fmt.Sprintf("file %q is %d bytes, exceeding limit", hdr.Name, hdr.Size),
			}
		}
		*totalSize += hdr.Size
		if *totalSize > maxExtractTotalForTest {
			return &ExtractError{
				Code:    CodeSizeLimitTotal,
				Message: "archive total size exceeds limit",
			}
		}
	}

	target := filepath.Join(dest, filepath.FromSlash(hdr.Name))
	cleanTarget := filepath.Clean(target)
	cleanDest := filepath.Clean(dest) + string(filepath.Separator)
	if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(dest) {
		return &ExtractError{
			Code:    CodeSpecialEntry,
			Message: "path escapes destination: " + hdr.Name,
		}
	}

	return nil
}
