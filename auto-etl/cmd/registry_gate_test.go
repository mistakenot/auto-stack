package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// regFixture builds a small project registry used across the filter cases.
// "main" is registered by both an HTTPS remote and its on-disk path; "ssh" is
// registered by an SSH remote (to exercise SSH↔HTTPS normalization).
func regFixture() sharedconfig.ProjectsConfig {
	return sharedconfig.ProjectsConfig{Projects: []sharedconfig.ProjectRef{
		{ID: "main", Path: "/repos/main", Remote: "https://github.com/me/main.git"},
		{ID: "ssh", Path: "/repos/ssh", Remote: "git@github.com:me/sshrepo.git"},
	}}
}

func TestFilterRemotesByRegistry(t *testing.T) {
	cfg := regFixture()

	cases := []struct {
		name        string
		remotes     map[string]string
		wantKept    []string // workspace paths expected to survive
		wantSkipped int
	}{
		{
			name:        "exact path match",
			remotes:     map[string]string{"/repos/main": "https://github.com/me/main.git"},
			wantKept:    []string{"/repos/main"},
			wantSkipped: 0,
		},
		{
			name:        "nested sub-path matches via longest-prefix",
			remotes:     map[string]string{"/repos/main/cmd/tool": "https://github.com/other/unrelated.git"},
			wantKept:    []string{"/repos/main/cmd/tool"},
			wantSkipped: 0,
		},
		{
			name:        "remote fallback when path differs",
			remotes:     map[string]string{"/elsewhere/worktree": "https://github.com/me/main.git"},
			wantKept:    []string{"/elsewhere/worktree"},
			wantSkipped: 0,
		},
		{
			name: "ssh remote matches https-normalized registry entry",
			// Registered as git@github.com:me/sshrepo.git; candidate uses HTTPS.
			remotes:     map[string]string{"/some/other/path": "https://github.com/me/sshrepo.git"},
			wantKept:    []string{"/some/other/path"},
			wantSkipped: 0,
		},
		{
			name: "credential-normalized https remote matches",
			remotes: map[string]string{
				"/cred/path": "https://user:token@github.com/me/main.git",
			},
			wantKept:    []string{"/cred/path"},
			wantSkipped: 0,
		},
		{
			name:        "unregistered repo dropped",
			remotes:     map[string]string{"/random/clone": "https://github.com/stranger/thing.git"},
			wantKept:    nil,
			wantSkipped: 1,
		},
		{
			name: "mixed kept and skipped",
			remotes: map[string]string{
				"/repos/main":   "https://github.com/me/main.git",
				"/random/clone": "https://github.com/stranger/thing.git",
			},
			wantKept:    []string{"/repos/main"},
			wantSkipped: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, stats := filterRemotesByRegistry(tc.remotes, cfg)

			if stats.total != len(tc.remotes) {
				t.Errorf("total = %d, want %d", stats.total, len(tc.remotes))
			}
			if stats.kept != len(tc.wantKept) {
				t.Errorf("kept = %d, want %d (got map %v)", stats.kept, len(tc.wantKept), got)
			}
			if stats.skipped != tc.wantSkipped {
				t.Errorf("skipped = %d, want %d", stats.skipped, tc.wantSkipped)
			}
			if len(got) != len(tc.wantKept) {
				t.Fatalf("kept map size = %d, want %d (%v)", len(got), len(tc.wantKept), got)
			}
			for _, p := range tc.wantKept {
				if _, ok := got[p]; !ok {
					t.Errorf("expected kept path %q in result, got %v", p, got)
				}
			}
		})
	}
}

func TestFilterRemotesEmptyRegistry(t *testing.T) {
	remotes := map[string]string{
		"/repos/main":   "https://github.com/me/main.git",
		"/random/clone": "https://github.com/stranger/thing.git",
	}
	got, stats := filterRemotesByRegistry(remotes, sharedconfig.ProjectsConfig{})
	if len(got) != 0 {
		t.Errorf("expected empty result for empty registry, got %v", got)
	}
	if stats.kept != 0 {
		t.Errorf("kept = %d, want 0", stats.kept)
	}
	if stats.skipped != 2 {
		t.Errorf("skipped = %d, want 2", stats.skipped)
	}
}

func TestFilterRemotesDoesNotMutateInput(t *testing.T) {
	remotes := map[string]string{
		"/repos/main":   "https://github.com/me/main.git",
		"/random/clone": "https://github.com/stranger/thing.git",
	}
	before := len(remotes)
	got, _ := filterRemotesByRegistry(remotes, regFixture())

	if len(remotes) != before {
		t.Fatalf("input map mutated: size changed from %d to %d", before, len(remotes))
	}
	if _, ok := remotes["/random/clone"]; !ok {
		t.Errorf("input map lost an entry — filter mutated the input")
	}
	// Mutating the returned map must not bleed into the input.
	got["/repos/main"] = "tampered"
	if remotes["/repos/main"] == "tampered" {
		t.Errorf("returned map shares backing storage with input")
	}
}

func TestGateSummaryHint(t *testing.T) {
	const hint = "auto init --project"

	// Empty registry → hint present even when nothing was discovered.
	if s := gateSummary(gateStats{}, true); !strings.Contains(s, hint) {
		t.Errorf("empty registry summary missing hint: %q", s)
	}
	// Non-empty registry but nothing kept → hint present.
	if s := gateSummary(gateStats{total: 2, kept: 0, skipped: 2}, false); !strings.Contains(s, hint) {
		t.Errorf("kept==0 summary missing hint: %q", s)
	}
	// Something kept → no hint, but still reports the counts.
	s := gateSummary(gateStats{total: 2, kept: 1, skipped: 1}, false)
	if strings.Contains(s, hint) {
		t.Errorf("summary should omit hint when repos were kept: %q", s)
	}
	if !strings.Contains(s, "kept 1") || !strings.Contains(s, "skipped 1") {
		t.Errorf("summary missing counts: %q", s)
	}
}

func TestLoadRegistryQuietly(t *testing.T) {
	t.Run("missing file yields empty registry", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		cfg := loadRegistryQuietly()
		if len(cfg.Projects) != 0 {
			t.Errorf("expected empty registry for missing file, got %+v", cfg.Projects)
		}
	})

	t.Run("seeded file is loaded", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		path, err := sharedconfig.ProjectsConfigPath()
		if err != nil {
			t.Fatalf("ProjectsConfigPath: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		seed := sharedconfig.ProjectsConfig{Projects: []sharedconfig.ProjectRef{
			{ID: "main", Path: "/repos/main", Remote: "https://github.com/me/main.git"},
		}}
		if err := sharedconfig.SaveProjects(path, seed); err != nil {
			t.Fatalf("SaveProjects: %v", err)
		}

		cfg := loadRegistryQuietly()
		if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "main" {
			t.Fatalf("expected seeded registry to load, got %+v", cfg.Projects)
		}
	})
}
