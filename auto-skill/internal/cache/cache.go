package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mistakenot/auto-shared/git"
	"github.com/mistakenot/auto-skill/internal/transport"
)

const CodePathEscapesCacheRoot = "path_escapes_cache_root"

var platformReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Cache manages a content-addressed bare blobless git cache.
type Cache struct {
	Root string
}

// Repo represents a single cached bare git repository.
type Repo struct {
	Path         string
	CanonicalURL string
	cache        *Cache
}

// RepoInfo holds metadata about a cached repo for listing.
type RepoInfo struct {
	Identity  string    `json:"identity"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	LastFetch time.Time `json:"last_fetch"`
}

// PruneOptions controls cache eviction behavior.
type PruneOptions struct {
	MaxAge        time.Duration
	MaxSize       int64
	DryRun        bool
	Unreferenced  bool
	ReferencedIDs map[string]bool
}

// PruneResult reports what was evicted and what was skipped.
type PruneResult struct {
	Evicted []RepoInfo `json:"evicted"`
	Skipped []RepoInfo `json:"skipped"`
	Errors  []string   `json:"errors,omitempty"`
}

func NewCache(root string) *Cache {
	return &Cache{Root: root}
}

// Open returns a Repo handle for the given identity and canonical URL.
// If absent, clones bare+blobless. If present, verifies origin.
func (c *Cache) Open(id transport.CacheIdentity, canonicalURL string) (*Repo, error) {
	repoPath, err := c.repoPath(id)
	if err != nil {
		return nil, err
	}

	repo := &Repo{
		Path:         repoPath,
		CanonicalURL: canonicalURL,
		cache:        c,
	}

	return repo, repo.withLock(func() error {
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			return repo.cloneBare(canonicalURL)
		}

		origin, err := runGit(repoPath, nil, "remote", "get-url", "--", "origin")
		if err != nil {
			return fmt.Errorf("verify origin: %w", err)
		}
		origin = strings.TrimSpace(origin)
		normalizedOrigin := git.NormalizeRemoteURL(origin)
		normalizedExpected := git.NormalizeRemoteURL(canonicalURL)

		if normalizedOrigin != normalizedExpected {
			suffix := id.HashSuffix(canonicalURL)
			suffixedPath := repoPath + "-" + suffix
			repo.Path = suffixedPath

			if _, err := os.Stat(suffixedPath); os.IsNotExist(err) {
				return repo.cloneBare(canonicalURL)
			}
		}
		return nil
	})
}

func (r *Repo) cloneBare(url string) error {
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o755); err != nil {
		return fmt.Errorf("create cache parent: %w", err)
	}
	_, err := runGit(filepath.Dir(r.Path), nil,
		"clone", "--bare", "--filter=blob:none", "--", url, r.Path)
	return err
}

// ResolveRef resolves a ref to a commit SHA.
func (r *Repo) ResolveRef(ref string) (string, error) {
	out, err := runGit(r.Path, nil, "rev-parse", "--verify", ref)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// Realize fetches reachable objects for a commit so it is fully present.
func (r *Repo) Realize(sha string) error {
	return r.withLock(func() error {
		_, err := runGit(r.Path, nil, "fetch", "origin", sha)
		if err != nil {
			_, err = runGit(r.Path, nil, "fetch", "origin")
		}
		return err
	})
}

// CommitPresent checks whether a commit's objects are fully present
// without making any network calls (GIT_NO_LAZY_FETCH=1).
func (r *Repo) CommitPresent(sha string) (bool, error) {
	_, err := runGitOffline(r.Path, "cat-file", "-t", sha)
	if err != nil {
		return false, fmt.Errorf("incomplete cache for commit %s; run: auto skill sync", sha)
	}

	_, err = runGitOffline(r.Path, "rev-list", "--objects", "--quiet", sha)
	if err != nil {
		return false, fmt.Errorf("incomplete cache for commit %s (missing objects); run: auto skill sync", sha)
	}
	return true, nil
}

// List returns metadata for all cached repos.
func (c *Cache) List() ([]RepoInfo, error) {
	var repos []RepoInfo

	if _, err := os.Stat(c.Root); os.IsNotExist(err) {
		return repos, nil
	}

	err := filepath.Walk(c.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}
		if !info.IsDir() {
			return nil
		}
		headPath := filepath.Join(path, "HEAD")
		if _, err := os.Stat(headPath); err != nil {
			return nil //nolint:nilerr // not a bare repo
		}
		if _, err := os.Stat(filepath.Join(path, "objects")); err != nil {
			return nil //nolint:nilerr // not a bare repo
		}

		rel, err := filepath.Rel(c.Root, path)
		if err != nil {
			return nil //nolint:nilerr // skip unresolvable paths
		}

		size, _ := dirSize(path)
		lastFetch := repoLastFetch(path)

		repos = append(repos, RepoInfo{
			Identity:  strings.TrimSuffix(rel, ".git"),
			Path:      path,
			SizeBytes: size,
			LastFetch: lastFetch,
		})

		return filepath.SkipDir
	})
	return repos, err
}

// Prune evicts cache repos based on the provided options.
func (c *Cache) Prune(opts PruneOptions) (PruneResult, error) {
	var result PruneResult

	repos, err := c.List()
	if err != nil {
		return result, err
	}

	now := time.Now()
	for _, repo := range repos {
		evict := false

		if opts.MaxAge > 0 && now.Sub(repo.LastFetch) > opts.MaxAge {
			evict = true
		}

		if opts.Unreferenced && opts.ReferencedIDs != nil {
			if !opts.ReferencedIDs[repo.Identity] {
				evict = true
			}
		}

		if !evict {
			result.Skipped = append(result.Skipped, repo)
			continue
		}

		if opts.DryRun {
			result.Evicted = append(result.Evicted, repo)
			continue
		}

		if err := os.RemoveAll(repo.Path); err != nil {
			result.Errors = append(result.Errors, "remove "+repo.Path+": "+err.Error())
			result.Skipped = append(result.Skipped, repo)
		} else {
			result.Evicted = append(result.Evicted, repo)
		}
	}

	return result, nil
}

// RepoPath returns the on-disk path for a given cache identity without
// cloning or opening the repo.
func (c *Cache) RepoPath(id transport.CacheIdentity) (string, error) {
	return c.repoPath(id)
}

func (c *Cache) repoPath(id transport.CacheIdentity) (string, error) {
	parts := make([]string, 0, 1+len(id.Path))

	hostEnc, err := safeComponent(id.Host)
	if err != nil {
		return "", err
	}
	parts = append(parts, hostEnc)

	for _, p := range id.Path {
		enc, err := safeComponent(p)
		if err != nil {
			return "", err
		}
		parts = append(parts, enc)
	}

	if len(parts) > 0 {
		parts[len(parts)-1] += ".git"
	}

	repoPath := filepath.Join(c.Root, filepath.Join(parts...))

	if !isSubpathSafe(c.Root, repoPath) {
		return "", &transport.TransportError{
			Code:    CodePathEscapesCacheRoot,
			Message: "derived cache path escapes root " + c.Root,
			Value:   repoPath,
		}
	}

	return repoPath, nil
}

func safeComponent(s string) (string, error) {
	if s == "" || s == "." || s == ".." {
		return "", fmt.Errorf("invalid path component: %q", s)
	}

	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '/' || r == '\\':
			fmt.Fprintf(&b, "%%%02X", r)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
		i += size
	}

	result := b.String()

	if strings.HasSuffix(result, ".") || strings.HasSuffix(result, " ") {
		result = result[:len(result)-1] + fmt.Sprintf("%%%02X", result[len(result)-1])
	}

	lower := strings.ToLower(result)
	base := lower
	if dotIdx := strings.Index(base, "."); dotIdx > 0 {
		base = base[:dotIdx]
	}
	if platformReserved[base] {
		result = fmt.Sprintf("%%%02X", result[0]) + result[1:]
	}

	return result, nil
}

func isSubpathSafe(root, path string) bool {
	cleanRoot := filepath.Clean(root) + string(filepath.Separator)
	cleanPath := filepath.Clean(path)
	return strings.HasPrefix(cleanPath, cleanRoot)
}

func (r *Repo) withLock(fn func() error) error {
	lockPath := r.Path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("create lock parent: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible files
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func repoLastFetch(repoPath string) time.Time {
	for _, name := range []string{"FETCH_HEAD", "packed-refs", "HEAD"} {
		info, err := os.Stat(filepath.Join(repoPath, name))
		if err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}
