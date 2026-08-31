package conformance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is AC-12 / G11: **nothing outside the mail package reads the mail
// store.**
//
// The compiler already guarantees the important half — `auto-mail/internal/...`
// is unimportable from any other module (D-062-8) — so what follows is a
// regression guard over that guarantee plus the half the compiler cannot see:
// a path string handed to a SQL driver or to a `sqlite3` shell-out.
//
// **What this guard is scoped to, and why.** It targets *the mail store*, not
// SQLite. Three things make a file a violation:
//
//  1. It names the mail store — the `alpha-store.db` filename, the
//     `alpha-flags` directory, or an `.auto/mail` path — **and** in the same
//     file reaches for a database: `modernc.org/sqlite`, `database/sql`,
//     `sql.Open`, or a `sqlite3` invocation.
//  2. It imports `auto-mail/internal/...` from outside the auto-mail module.
//
// Naming the store *without* opening it is deliberately allowed, because that
// is what a correct caller already does: `auto-cli/cmd/auto/hooksmail_test.go`
// asserts the hook leaves `alpha-store.db` non-existent, and the harness
// scenario and its tests reference the path to gate readiness and to assert the
// same property from outside the process. Those are observations *of* the seam,
// not routes around it. A repo-wide ban on the string would fail all of them,
// and a repo-wide ban on `modernc.org/sqlite` would fail
// `auto-watch/internal/store`, `auto-search/internal/indexdb` and
// `auto-search/internal/cochange`, which legitimately keep their own stores —
// it would have failed on day zero, before this task began.

// mailStoreNames are the ways the mail store gets named on disk.
var mailStoreNames = []string{"alpha-store.db", "alpha-flags", ".auto/mail"}

// dbOpeners are the ways a file reaches for a database. A file that names the
// mail store and holds one of these is opening the mail store.
var dbOpeners = []string{"modernc.org/sqlite", "database/sql", "sql.Open", "sqlite3 ", "sqlite3."}

// storeOwner is the one package allowed to do both. `auto-mail/internal/config`
// resolves the paths and `auto-mail/mail` passes them to the store, but neither
// opens a database itself, so neither needs an exemption here.
const storeOwner = "auto-mail/internal/store"

// internalImport is the import path no package outside the module may name.
const internalImport = "github.com/mistakenot/auto-mail/internal/"

// exempt is this file itself. It has to hold both halves of the pattern it
// looks for — the store's names and the ways a database is opened — so it
// matches its own rule. Naming the exemption is better than weakening the rule
// to something that happens not to describe the guard.
const exempt = "auto-mail/conformance/seam_test.go"

// repoRoot walks up from this test file looking for go.work.
//
// It returns "" rather than failing when there is none: auto-mail is its own
// Go module and must stay testable when checked out on its own, and a guard
// that fails in that checkout would be asserting something about the
// filesystem rather than about the code.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// scanned reports whether a repo-relative path is in the guard's remit: every
// module's Go source, plus the harness and skills trees. Docs are out — prose
// naming the store is documentation of it, not access to it.
func scanned(rel string) bool {
	for _, skip := range []string{".git/", "node_modules/", "vendor/", "build/", "dist/", ".venv/", ".tmp/", "docs/", "testdata/"} {
		if strings.HasPrefix(rel, skip) || strings.Contains(rel, "/"+skip) {
			return false
		}
	}
	if strings.HasSuffix(rel, ".go") {
		return true
	}
	if strings.HasPrefix(rel, "harness/") {
		ext := filepath.Ext(rel)
		base := filepath.Base(rel)
		return ext == ".py" || ext == ".sh" || ext == ".yaml" || ext == ".yml" ||
			strings.HasPrefix(base, "Dockerfile")
	}
	return strings.HasPrefix(rel, "skills/")
}

func containsAny(haystack string, needles []string) (string, bool) {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return needle, true
		}
	}
	return "", false
}

// TestNothingOutsideTheMailPackageOpensTheStore is G11 as an executable rail.
func TestNothingOutsideTheMailPackageOpensTheStore(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("no go.work above this module — standalone checkout, nothing repo-wide to guard")
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && !scannedDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !scanned(rel) {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(source)

		if !strings.HasPrefix(rel, storeOwner) && rel != exempt {
			if name, named := containsAny(text, mailStoreNames); named {
				if opener, opens := containsAny(text, dbOpeners); opens {
					t.Errorf("%s names the mail store (%q) and opens a database (%q) — "+
						"nothing outside %s may read the mail store; go through mail.Client",
						rel, name, opener, storeOwner)
				}
			}
		}

		if !strings.HasPrefix(rel, "auto-mail/") && strings.Contains(text, internalImport) {
			t.Errorf("%s imports %s… from outside the auto-mail module — "+
				"the store is internal on purpose; go through mail.Client",
				rel, internalImport)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
}

// scannedDir keeps the walk off trees that hold no first-party source. Pruning
// at the directory level matters: `harness/.venv` and `node_modules` are large
// enough that walking them makes this guard slow instead of cheap.
func scannedDir(rel string) bool {
	base := filepath.Base(rel)
	switch base {
	case ".git", "node_modules", "vendor", "build", "dist", ".venv", ".tmp", ".mypy_cache",
		"__pycache__", ".pytest_cache", ".ruff_cache", "testdata":
		return false
	}
	return true
}
