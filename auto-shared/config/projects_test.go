package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertProjectReplacesByPath(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	UpsertProject(&cfg, ProjectRef{ID: "alpha-renamed", Path: "/repos/alpha", Remote: "git@x:alpha.git"})

	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project after upsert by same path, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].ID != "alpha-renamed" || cfg.Projects[0].Remote != "git@x:alpha.git" {
		t.Fatalf("expected entry replaced in place, got %+v", cfg.Projects[0])
	}
}

func TestUpsertProjectAppendsDistinctPaths(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	UpsertProject(&cfg, ProjectRef{ID: "beta", Path: "/repos/beta"})
	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(cfg.Projects))
	}
}

func TestFindProjectByPathLongestPrefix(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "outer", Path: "/repos"},
		{ID: "inner", Path: "/repos/alpha"},
	}}
	got := cfg.FindProjectByPath("/repos/alpha/cmd/main")
	if got == nil || got.ID != "inner" {
		t.Fatalf("expected longest-prefix match 'inner', got %+v", got)
	}
	if got := cfg.FindProjectByPath("/elsewhere"); got != nil {
		t.Fatalf("expected no match for unrelated dir, got %+v", got)
	}
	if got := cfg.FindProjectByPath("/repos"); got == nil || got.ID != "outer" {
		t.Fatalf("expected exact root match 'outer', got %+v", got)
	}
}

func TestFindProjectByExactPathIgnoresParent(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "outer", Path: "/repos"},
		{ID: "inner", Path: "/repos/alpha"},
	}}
	// A nested path with no exact entry must NOT match the parent.
	if got := cfg.FindProjectByExactPath("/repos/alpha/cmd"); got != nil {
		t.Fatalf("expected no exact match for nested path, got %+v", got)
	}
	if got := cfg.FindProjectByExactPath("/repos/alpha"); got == nil || got.ID != "inner" {
		t.Fatalf("expected exact match 'inner', got %+v", got)
	}
}

func TestValidateProjectsCatchesBadIDAndDupes(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "Bad_ID", Path: "/repos/a"},
		{ID: "dup", Path: "/repos/b"},
		{ID: "dup", Path: "/repos/c"},
	}}
	errs := ValidateProjects(cfg)
	codes := map[string]bool{}
	for _, e := range errs {
		codes[e.Code] = true
	}
	if !codes["invalid_project_id"] {
		t.Errorf("expected invalid_project_id error, got %+v", errs)
	}
	if !codes["duplicate_project_id"] {
		t.Errorf("expected duplicate_project_id error, got %+v", errs)
	}
}

func TestSlugifyID(t *testing.T) {
	cases := map[string]string{
		"auto-stack":      "auto-stack",
		"My_Repo.Name":    "my-repo-name",
		"tmp.Jd3XQJCWKW":  "tmp-jd3xqjcwkw",
		"  Trailing--  ":  "trailing",
		"___":             "",
		"Already-Valid-1": "already-valid-1",
	}
	for in, want := range cases {
		if got := SlugifyID(in); got != want {
			t.Errorf("SlugifyID(%q) = %q, want %q", in, got, want)
		}
		if want != "" && len(ValidateProjects(ProjectsConfig{Projects: []ProjectRef{{ID: SlugifyID(in), Path: "/x"}}})) != 0 {
			t.Errorf("SlugifyID(%q)=%q did not produce a valid id", in, SlugifyID(in))
		}
	}
}

func TestSaveLoadProjectsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	want := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "git@x:alpha.git", Name: "Alpha", Tools: []string{"watch", "ui"}},
	}}
	if err := SaveProjects(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0].ID != "alpha" || got.Projects[0].Name != "Alpha" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Projects[0].Tools) != 2 {
		t.Fatalf("expected tools preserved, got %+v", got.Projects[0].Tools)
	}
}

func TestEnsureProjectsMigratesLegacyRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed the legacy watch-owned registry, as an un-migrated host would have.
	legacy := filepath.Join(home, ".auto", "watch", "settings.json")
	if err := WriteJSONFile(legacy, ProjectsConfig{Projects: []ProjectRef{
		{ID: "auto-stack", Path: "/repos/auto-stack", Remote: "git@x:auto-stack.git"},
	}}); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	// EnsureProjects is what `auto init` calls — it must migrate, not start empty.
	path, cfg, created, err := EnsureProjects()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatalf("expected registry created on first ensure")
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "auto-stack" {
		t.Fatalf("expected legacy project migrated, got %#v", cfg.Projects)
	}
	if want := filepath.Join(home, ".auto", "projects.json"); path != want {
		t.Fatalf("registry path = %s, want %s", path, want)
	}
	// Legacy file must be retired.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("expected legacy file renamed away, stat err = %v", err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Errorf("expected legacy file renamed to .migrated, got %v", err)
	}
}

func TestEnsureProjectsNoLegacyStartsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, cfg, created, err := EnsureProjects()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created || len(cfg.Projects) != 0 {
		t.Fatalf("expected empty created registry, got created=%v projects=%#v", created, cfg.Projects)
	}
}

func TestUpsertProjectDefaultsRegisteredAt(t *testing.T) {
	cfg := ProjectsConfig{}
	UpsertProject(&cfg, ProjectRef{ID: "alpha", Path: "/repos/alpha"})
	if cfg.Projects[0].RegisteredAt == "" {
		t.Fatalf("expected RegisteredAt to be defaulted when blank")
	}
	// An explicit value must be preserved, not overwritten.
	UpsertProject(&cfg, ProjectRef{ID: "beta", Path: "/repos/beta", RegisteredAt: "2020-01-01T00:00:00Z"})
	if got := cfg.Projects[1].RegisteredAt; got != "2020-01-01T00:00:00Z" {
		t.Fatalf("expected explicit RegisteredAt preserved, got %q", got)
	}
}

func TestSaveProjectsIsAtomicAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	if err := SaveProjects(path, ProjectsConfig{Projects: []ProjectRef{{ID: "alpha", Path: "/a"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "projects.json" {
		t.Fatalf("expected only projects.json (no temp left behind), got %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestFindProjectByRemoteHit(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "git@github.com:org/alpha.git"},
		{ID: "beta", Path: "/repos/beta", Remote: "https://github.com/org/beta.git"},
	}}
	// SSH remote should match via normalization.
	got := cfg.FindProjectByRemote("ssh://git@github.com/org/alpha.git")
	if got == nil || got.ID != "alpha" {
		t.Fatalf("expected alpha, got %+v", got)
	}
	// HTTPS remote should match.
	got = cfg.FindProjectByRemote("https://github.com/org/beta")
	if got == nil || got.ID != "beta" {
		t.Fatalf("expected beta, got %+v", got)
	}
}

func TestFindProjectByRemoteMiss(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "https://github.com/org/alpha"},
	}}
	got := cfg.FindProjectByRemote("https://github.com/org/other-repo")
	if got != nil {
		t.Fatalf("expected nil for miss, got %+v", got)
	}
}

func TestFindProjectByRemoteEmptyInput(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "https://github.com/org/alpha"},
	}}
	got := cfg.FindProjectByRemote("")
	if got != nil {
		t.Fatalf("expected nil for empty remote, got %+v", got)
	}
}

func TestFindProjectByRemoteNormalizesTokenBearingRemote(t *testing.T) {
	// The stored remote may contain a PAT; the lookup remote may also contain one.
	// Both sides should normalize, so a token-bearing stored remote matches a
	// clean lookup remote and vice versa.
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: "/repos/alpha", Remote: "https://x-access-token:ghp_SECRET@github.com/org/alpha.git"},
	}}
	// Clean remote should match token-bearing stored remote.
	got := cfg.FindProjectByRemote("https://github.com/org/alpha")
	if got == nil || got.ID != "alpha" {
		t.Fatalf("expected alpha via normalized match, got %+v", got)
	}
	// Token-bearing remote should also match.
	got = cfg.FindProjectByRemote("https://x-access-token:ghp_OTHER@github.com/org/alpha.git")
	if got == nil || got.ID != "alpha" {
		t.Fatalf("expected alpha via token-bearing match, got %+v", got)
	}
}

func TestFindProjectByRemoteSkipsEmptyRemote(t *testing.T) {
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "local-only", Path: "/repos/local"},
		{ID: "alpha", Path: "/repos/alpha", Remote: "https://github.com/org/alpha"},
	}}
	// Should not crash or match the project with empty remote.
	got := cfg.FindProjectByRemote("https://github.com/org/alpha")
	if got == nil || got.ID != "alpha" {
		t.Fatalf("expected alpha, got %+v", got)
	}
}

func TestLoadProjectsLenientUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	// A future field the current schema doesn't know about must not break loading.
	if err := WriteJSONFile(path, map[string]any{
		"projects":  []map[string]any{{"id": "alpha", "path": "/repos/alpha", "futureField": 1}},
		"topLevelX": true,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadProjects(path)
	if err != nil {
		t.Fatalf("expected lenient load to succeed, got %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0].ID != "alpha" {
		t.Fatalf("expected alpha loaded, got %+v", got)
	}
}

func TestUsableSkipsStaleAndInvalidKeepsRealProjects(t *testing.T) {
	// Two real directories on disk; everything else should be skipped.
	realA := t.TempDir()
	realB := t.TempDir()
	realC := t.TempDir()
	cfg := ProjectsConfig{Projects: []ProjectRef{
		{ID: "alpha", Path: realA},                            // kept
		{ID: "Bad_ID", Path: realB},                           // skipped: invalid id
		{ID: "beta", Path: ""},                                // skipped: missing path
		{ID: "gamma", Path: "/tmp/does-not-exist-2718281828"}, // skipped: path not real
		{ID: "beta", Path: realB},                             // kept: distinct valid entry
		{ID: "beta", Path: realC},                             // skipped: duplicate id (real path)
	}}
	usable, skipped := cfg.Usable()
	if len(usable.Projects) != 2 {
		t.Fatalf("expected 2 usable, got %d: %+v", len(usable.Projects), usable.Projects)
	}
	if usable.Projects[0].ID != "alpha" || usable.Projects[1].ID != "beta" {
		t.Fatalf("unexpected usable set: %+v", usable.Projects)
	}
	if len(skipped) != 4 {
		t.Fatalf("expected 4 skips, got %d: %+v", len(skipped), skipped)
	}
	codes := map[string]bool{}
	for _, s := range skipped {
		codes[s.Code] = true
	}
	for _, want := range []string{"invalid_project_id", "missing_project_path", "project_path_missing", "duplicate_project_id"} {
		if !codes[want] {
			t.Errorf("expected a %q skip, got codes %v", want, codes)
		}
	}
}

func TestUsableEmptyRegistry(t *testing.T) {
	usable, skipped := ProjectsConfig{}.Usable()
	if len(usable.Projects) != 0 || len(skipped) != 0 {
		t.Fatalf("expected empty usable + no skips, got %d/%d", len(usable.Projects), len(skipped))
	}
}
