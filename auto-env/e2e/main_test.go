package e2e

import (
	"os"
	"testing"
)

// TestMain isolates every e2e test in this package from the real home dir.
//
// The env registry (auto-env/internal/registry) resolves its location from
// config.AutoDir() → $HOME/.auto/env, independent of the per-test repo dir that
// `initGitRepo` creates via t.TempDir(). Without overriding HOME, each `up`/`down`
// exercised through cli.Execute appended a /tmp fixture entry to the developer's
// real ~/.auto/env/environments.json; those stale entries then accumulated and
// polluted `auto env`/`auto watch` host state. Pointing HOME at a throwaway dir
// keeps the global registry writes inside the sandbox.
//
// This mirrors the equivalent isolation in auto-skill/internal/cli/main_test.go.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "auto-env-e2e-test-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
