package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// githubOnlyLock is a vercel skills-lock.json with two github entries and no
// unsupported types — a clean migration (exit 0).
const githubOnlyLock = `{
  "version": 1,
  "skills": {
    "alpha-skill": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/alpha-skill/SKILL.md",
      "computedHash": "1111111111111111111111111111111111111111111111111111111111111111"
    },
    "beta-skill": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/beta-skill/SKILL.md",
      "computedHash": "2222222222222222222222222222222222222222222222222222222222222222",
      "ref": "v1.2.3"
    }
  }
}
`

// withUnsupportedLock has one github entry and one unsupported node_modules entry.
const withUnsupportedLock = `{
  "version": 1,
  "skills": {
    "alpha-skill": {
      "source": "acme/skills",
      "sourceType": "github",
      "skillPath": "skills/alpha-skill/SKILL.md",
      "computedHash": "1111111111111111111111111111111111111111111111111111111111111111"
    },
    "nm-skill": {
      "source": "some-package",
      "sourceType": "node_modules",
      "computedHash": "3333333333333333333333333333333333333333333333333333333333333333"
    }
  }
}
`

func lockPaths(root string) (lock, yaml string) {
	return filepath.Join(root, ".auto", "skills", "lock.json"),
		filepath.Join(root, ".auto", "skills", "skills.yaml")
}

func TestMigrateVercelJSONDefault(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills-lock.json"), githubOnlyLock)

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSONMap(t, stdout)
	migrated, ok := out["migrated"].([]any)
	if !ok || len(migrated) != 2 {
		t.Fatalf("expected 2 migrated, got %v", out["migrated"])
	}
	counts, ok := out["counts"].(map[string]any)
	if !ok {
		t.Fatalf("expected counts object, got %v", out["counts"])
	}
	if counts["migrated"].(float64) != 2 {
		t.Errorf("counts.migrated = %v, want 2", counts["migrated"])
	}
	if out["failed"].(bool) {
		t.Error("failed = true, want false (no skips)")
	}

	// Clean run: no diagnostics on stderr.
	if stderr != "" {
		t.Errorf("expected empty stderr, got:\n%s", stderr)
	}

	// Additive files written.
	lockPath, yamlPath := lockPaths(root)
	assertExists(t, lockPath)
	assertExists(t, yamlPath)
}

func TestMigrateVercelTextSummary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills-lock.json"), githubOnlyLock)

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel", "--format", "text")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr:\n%s", code, stderr)
	}

	want := "migrated 2 deps, skipped 0 (unsupported); run `auto skill sync` to resolve commits and render.\n"
	if stdout != want {
		t.Errorf("text summary mismatch\n got: %q\nwant: %q", stdout, want)
	}
}

func TestMigrateVercelFromOverride(t *testing.T) {
	root := t.TempDir()
	// Place the lock at a non-default custom path; no ./skills-lock.json exists.
	writeFile(t, filepath.Join(root, "custom", "my-lock.json"), githubOnlyLock)

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel", "--from", "custom/my-lock.json")
	if code != 0 {
		t.Fatalf("expected exit 0 with --from override, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	out := decodeJSONMap(t, stdout)
	if migrated, _ := out["migrated"].([]any); len(migrated) != 2 {
		t.Fatalf("expected 2 migrated from custom path, got %v", out["migrated"])
	}
	lockPath, _ := lockPaths(root)
	assertExists(t, lockPath)
}

func TestMigrateVercelDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills-lock.json"), githubOnlyLock)

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel", "--dry-run")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr:\n%s", code, stderr)
	}

	out := decodeJSONMap(t, stdout)
	if out["dry_run"] != true {
		t.Errorf("dry_run = %v, want true", out["dry_run"])
	}
	if migrated, _ := out["migrated"].([]any); len(migrated) != 2 {
		t.Errorf("expected 2 planned migrations, got %v", out["migrated"])
	}

	// Nothing written.
	if _, err := os.Stat(filepath.Join(root, ".auto")); !os.IsNotExist(err) {
		t.Errorf(".auto exists after dry-run (err=%v)", err)
	}
}

func TestMigrateVercelMissingFrom(t *testing.T) {
	root := t.TempDir() // no skills-lock.json

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel")
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing --from, got 0\nstdout:\n%s", stdout)
	}

	diags := decodeDiagnostics(t, stderr)
	if len(diags) == 0 {
		t.Fatal("expected structured diagnostics on stderr")
	}
	assertHasDiag(t, diags, "parse_error")
	if diags[0]["field"] != "from" {
		t.Errorf("expected field=from, got %v", diags[0]["field"])
	}

	// Stdout must carry no payload for a fatal error.
	if stdout != "" {
		t.Errorf("expected empty stdout on fatal error, got:\n%s", stdout)
	}
	// Nothing written.
	if _, err := os.Stat(filepath.Join(root, ".auto")); !os.IsNotExist(err) {
		t.Errorf(".auto exists after error (err=%v)", err)
	}
}

func TestMigrateVercelGarbledJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills-lock.json"), "this is not valid json {")

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel")
	if code == 0 {
		t.Fatalf("expected non-zero exit for garbled JSON, got 0\nstdout:\n%s", stdout)
	}

	diags := decodeDiagnostics(t, stderr)
	assertHasDiag(t, diags, "parse_error")
	if _, err := os.Stat(filepath.Join(root, ".auto")); !os.IsNotExist(err) {
		t.Errorf(".auto exists after error (err=%v)", err)
	}
}

func TestMigrateVercelUnsupportedExitsNonZero(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills-lock.json"), withUnsupportedLock)

	stdout, stderr, code := runCLI(t, "--root", root, "migrate", "vercel")
	if code != 1 {
		t.Fatalf("expected exit 1 (unsupported skipped), got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// Valid results still printed first.
	out := decodeJSONMap(t, stdout)
	migrated, _ := out["migrated"].([]any)
	if len(migrated) != 1 {
		t.Fatalf("expected 1 migrated (the github entry), got %v", out["migrated"])
	}
	if out["failed"] != true {
		t.Error("failed = false, want true")
	}

	// Skipped list names the unsupported entry with its reason.
	skipped, _ := out["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %v", out["skipped"])
	}
	s := skipped[0].(map[string]any)
	if s["name"] != "nm-skill" {
		t.Errorf("skipped name = %v, want nm-skill", s["name"])
	}
	if s["reason"] != "unsupported_source_type" {
		t.Errorf("skipped reason = %v, want unsupported_source_type", s["reason"])
	}

	// Warning echoed on stderr (readable diagnostic), naming the skipped skill.
	if !strings.Contains(stderr, "nm-skill") {
		t.Errorf("expected stderr to mention nm-skill, got:\n%s", stderr)
	}
}
