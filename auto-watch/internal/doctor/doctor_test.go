package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func writeUnit(t *testing.T, dir, execStart string) string {
	t.Helper()
	unitPath := filepath.Join(dir, "autowatch.service")
	content := "[Unit]\nDescription=autowatch daemon\n\n[Service]\nExecStart=" + execStart + "\n\n[Install]\nWantedBy=default.target\n"
	if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	return unitPath
}

func writeBinary(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	binPath := filepath.Join(dir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	return binPath
}

func TestCheckDaemonUnit_ValidUnit(t *testing.T) {
	dir := t.TempDir()
	bin := writeBinary(t, dir, "auto", 0o755)
	unitPath := writeUnit(t, dir, bin+" watch start")

	check := checkDaemonUnitAt([]string{unitPath}, os.ReadFile, os.Stat)
	if check.Status != "ok" {
		t.Fatalf("expected ok, got %q (message=%q)", check.Status, check.Message)
	}
	if check.Message != unitPath {
		t.Fatalf("expected message to be unit path %q, got %q", unitPath, check.Message)
	}
}

func TestCheckDaemonUnit_MissingBinary(t *testing.T) {
	dir := t.TempDir()
	// ExecStart references a binary that does not exist on disk.
	missing := filepath.Join(dir, "auto")
	unitPath := writeUnit(t, dir, missing+" watch start")

	check := checkDaemonUnitAt([]string{unitPath}, os.ReadFile, os.Stat)
	if check.Status != "fail" {
		t.Fatalf("expected fail, got %q", check.Status)
	}
	if check.Remediation != "auto watch daemon install" {
		t.Fatalf("expected install remediation, got %q", check.Remediation)
	}
}

func TestCheckDaemonUnit_StaleAutowatchBinary(t *testing.T) {
	dir := t.TempDir()
	// Even if the old binary exists, the stale `autowatch` form must fail so the
	// migration to `auto watch start` is surfaced.
	bin := writeBinary(t, dir, "autowatch", 0o755)
	unitPath := writeUnit(t, dir, bin+" start")

	check := checkDaemonUnitAt([]string{unitPath}, os.ReadFile, os.Stat)
	if check.Status != "fail" {
		t.Fatalf("expected fail for stale autowatch binary, got %q", check.Status)
	}
	if check.Remediation != "auto watch daemon install" {
		t.Fatalf("expected install remediation, got %q", check.Remediation)
	}
}

func TestCheckDaemonUnit_NoUnitInstalled(t *testing.T) {
	dir := t.TempDir()
	// Point only at a path that does not exist.
	check := checkDaemonUnitAt([]string{filepath.Join(dir, "autowatch.service")}, os.ReadFile, os.Stat)
	if check.Status != "ok" {
		t.Fatalf("expected ok when no unit is installed, got %q", check.Status)
	}
	if check.Message != "no daemon unit installed" {
		t.Fatalf("expected explanatory message, got %q", check.Message)
	}
}

func TestCheckHostConfig_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	check := checkHostConfig()
	if check.Name != "host_config" {
		t.Errorf("name = %q, want host_config", check.Name)
	}
	if check.Status != "fail" {
		t.Errorf("expected fail for missing host config, got %q", check.Status)
	}
	if check.Remediation != "run auto watch init" {
		t.Errorf("remediation = %q, want 'run auto watch init'", check.Remediation)
	}
}

func TestCheckHostConfig_InvalidFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create ~/.auto/host.json with an invalid hostId.
	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "host.json"), []byte(`{"hostId":"UPPER-case"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	check := checkHostConfig()
	if check.Status != "fail" {
		t.Errorf("expected fail for invalid hostId, got %q", check.Status)
	}
	if check.Details != "UPPER-case" {
		t.Errorf("details = %q, want UPPER-case", check.Details)
	}
}

func TestCheckHostConfig_Valid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "host.json"), []byte(`{"hostId":"dev-box.charlie"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	check := checkHostConfig()
	if check.Status != "ok" {
		t.Errorf("expected ok for valid host config, got %q (message=%q)", check.Status, check.Message)
	}
}

func TestCheckDaemonUnit_UserScopePreferredOverSystem(t *testing.T) {
	userDir := t.TempDir()
	systemDir := t.TempDir()
	userBin := writeBinary(t, userDir, "auto", 0o755)
	userUnit := writeUnit(t, userDir, userBin+" watch start")
	// A system unit also exists but references a missing binary; the user unit
	// must win because it appears first in the candidate list.
	systemUnit := writeUnit(t, systemDir, filepath.Join(systemDir, "auto")+" watch start")

	check := checkDaemonUnitAt([]string{userUnit, systemUnit}, os.ReadFile, os.Stat)
	if check.Status != "ok" {
		t.Fatalf("expected ok from user-scope unit, got %q (message=%q)", check.Status, check.Message)
	}
	if check.Message != userUnit {
		t.Fatalf("expected user unit %q to be selected, got %q", userUnit, check.Message)
	}
}

func TestCheckSettings_WarnsOnSkippableEntriesNotFail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	autoDir := filepath.Join(home, ".auto")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One real project + one stale entry pointing at a deleted dir. The stale
	// entry must downgrade the check to "warn" (skipped, not fatal) so the
	// daemon still starts — the bug that crash-looped it on test pollution.
	real := t.TempDir()
	reg := `{"projects":[` +
		`{"id":"good","path":"` + real + `"},` +
		`{"id":"001","path":"/tmp/deleted-by-a-test-1618033988/001"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(autoDir, "projects.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	check := checkSettings()
	if check.Status != "warn" {
		t.Fatalf("expected warn (daemon should still start), got %q: %s", check.Status, check.Message)
	}
}
