package cache

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
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
// leaves dest empty.
func (r *Repo) Extract(sha, subpath, dest string) error {
	treeish := sha
	if subpath != "" {
		treeish = sha + ":" + subpath
	}

	// Pass 1: validate all entries without writing anything.
	cmd := newGitCmd(r.Path, nil, "archive", "--format=tar", treeish)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe git archive: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start git archive: %w", err)
	}

	tr := tar.NewReader(stdout)
	var totalSize int64
	var fileCount int

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Wait()
			return fmt.Errorf("read archive: %w", err)
		}

		if err := validateEntry(hdr, dest, &fileCount, &totalSize); err != nil {
			_ = cmd.Wait()
			return err
		}
	}

	if err := cmd.Wait(); err != nil {
		// git emits "not a valid object name: <sha>:<subpath>" when the subpath
		// no longer exists in the (present) commit — surface a sentinel so callers
		// can report a rename/removal instead of a raw archive failure.
		if subpath != "" && strings.Contains(stderr.String(), "not a valid object name") {
			return fmt.Errorf("git archive %s: %w", treeish, ErrSubpathNotFound)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git archive: %s", msg)
		}
		return fmt.Errorf("git archive: %w", err)
	}

	// Pass 2: validation passed, re-stream and write.
	cmd2 := newGitCmd(r.Path, nil, "archive", "--format=tar", treeish)
	stdout2, err := cmd2.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe git archive (write pass): %w", err)
	}
	if err := cmd2.Start(); err != nil {
		return fmt.Errorf("start git archive (write pass): %w", err)
	}

	tr2 := tar.NewReader(stdout2)
	for {
		hdr, err := tr2.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd2.Wait()
			return fmt.Errorf("read archive (write pass): %w", err)
		}

		if hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader {
			continue
		}

		target := filepath.Join(dest, filepath.FromSlash(hdr.Name))
		cleanTarget := filepath.Clean(target)
		cleanDest := filepath.Clean(dest) + string(filepath.Separator)
		if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(dest) {
			_ = cmd2.Wait()
			return &ExtractError{Code: CodeSpecialEntry, Message: "path escapes dest: " + hdr.Name}
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := safeMkdirAll(target, dest); err != nil {
				_ = cmd2.Wait()
				return fmt.Errorf("mkdir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if err := safeMkdirAll(filepath.Dir(target), dest); err != nil {
				_ = cmd2.Wait()
				return fmt.Errorf("mkdir parent %s: %w", hdr.Name, err)
			}
			if err := writeFile(target, hdr, tr2); err != nil {
				_ = cmd2.Wait()
				return err
			}
		}
	}

	return cmd2.Wait()
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
