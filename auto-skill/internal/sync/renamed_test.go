package sync

import (
	"errors"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/cache"
)

// TestRenamedUpstreamErrorMessage pins the structured remediation message so the
// CLI/doctor presentation stays stable.
func TestRenamedUpstreamErrorMessage(t *testing.T) {
	err := &RenamedUpstreamError{Name: "alpha", Subpath: "skills/alpha", Commit: "deadbeef"}
	msg := err.Error()
	for _, want := range []string{
		`alpha not found at its locked path "skills/alpha"`,
		"renamed or removed upstream?",
		"auto skill add <url>",
		"auto skill remove alpha --vendored",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

// TestExtractVendoredRenamedSubpath: a locked subpath that no longer resolves in
// the (present) commit yields a *RenamedUpstreamError, not a raw extract error.
func TestExtractVendoredRenamedSubpath(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "vendored alpha")

	env := newEnv(t)
	realizeCommit(t, env, f.url, head)
	c := cache.NewCache(env.UpstreamCacheDir())

	// The commit is present, but skills/renamed was never in it (rename sim).
	sp := SkillPlan{
		Name:         "renamed",
		Repo:         f.url,
		URL:          f.url,
		Subpath:      "skills/renamed",
		TargetCommit: head,
		Action:       ActionMaterialize,
	}

	_, err := extractVendored(c, sp)
	if err == nil {
		t.Fatal("expected error for missing subpath")
	}
	var rerr *RenamedUpstreamError
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %v, want *RenamedUpstreamError", err)
	}
	if rerr.Name != "renamed" || rerr.Subpath != "skills/renamed" {
		t.Errorf("RenamedUpstreamError = %+v", rerr)
	}
}
