package main

import (
	"io"
	"os/exec"
	"path/filepath"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
)

func gitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestInitProjectRegistersWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	gitInTest(t, repo, "init")
	gitInTest(t, repo, "remote", "add", "origin", "https://user:github_pat_SECRET@github.com/acme/widgets.git")
	gitInTest(t, repo, "config", "user.email", "t@example.com")
	if err := exec.Command("mkdir", "-p", filepath.Join(repo, ".auto", "watch")).Run(); err != nil {
		t.Fatalf("mkdir tool dir: %v", err)
	}
	t.Chdir(repo)

	cmd := newInitCmd(io.Discard, io.Discard)
	cmd.SetArgs([]string{"--project", "--id", "widgets", "--name", "Widgets"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --project: %v", err)
	}

	cfg, err := sharedconfig.LoadProjects(filepath.Join(home, ".auto", "projects.json"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project, got %#v", cfg.Projects)
	}
	p := cfg.Projects[0]
	if p.ID != "widgets" || p.Name != "Widgets" {
		t.Errorf("unexpected id/name: %+v", p)
	}
	if want := sharedgit.NormalizeRemoteURL("https://user:github_pat_SECRET@github.com/acme/widgets.git"); p.Remote != want {
		t.Errorf("remote = %q, want %q", p.Remote, want)
	}
	if p.Remote == "" || p.Remote != "https://github.com/acme/widgets" {
		t.Errorf("remote not credential-stripped/normalized: %q", p.Remote)
	}
	found := false
	for _, tool := range p.Tools {
		if tool == "watch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected detected tool 'watch', got %v", p.Tools)
	}
	if p.RegisteredAt == "" {
		t.Errorf("expected registeredAt to be set")
	}
}

func TestInitProjectRejectsBadExplicitID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	gitInTest(t, repo, "init")
	t.Chdir(repo)

	cmd := newInitCmd(io.Discard, io.Discard)
	cmd.SetArgs([]string{"--project", "--id", "Bad_ID"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid explicit --id, got nil")
	}
}
