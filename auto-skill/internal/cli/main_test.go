package cli

import (
	"os"
	"testing"
)

// TestMain isolates every CLI test in this package from the real home dir.
//
// The `init --project` (and related) command paths register the project in the
// shared host-level registry at ~/.auto/projects.json, whose location resolves
// from $HOME — independent of the per-test `--root` sandbox used for project
// files. Without overriding HOME, each run appended a /tmp fixture entry (id
// "001") to the developer's real registry; those stale, duplicate entries then
// failed strict validation and crash-looped the autowatch daemon's startup
// doctor. Pointing HOME at a throwaway dir keeps the global registry writes
// inside the sandbox.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "auto-skill-cli-test-home-")
	if err != nil {
		panic(err)
	}
	// Override HOME for the whole package; individual tests may still set their
	// own temp HOME via t.Setenv, which takes precedence within that test.
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = os.RemoveAll(home)
	os.Exit(code)
}
