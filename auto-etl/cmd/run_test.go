package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostConfigPathUsesHomeDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got := hostConfigPath()
	want := filepath.Join(tmpHome, ".auto", "host.json")
	if got != want {
		t.Fatalf("hostConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadHostIDReadsHostIDField(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostPath := filepath.Join(tmpHome, ".auto", "host.json")
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		t.Fatalf("create host dir: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"hostId":"etl-host"}`), 0o644); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	if got := loadHostID(); got != "etl-host" {
		t.Fatalf("loadHostID() = %q, want %q", got, "etl-host")
	}
}

func TestLoadHostIDFallsBackWhenHostIDMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostPath := filepath.Join(tmpHome, ".auto", "host.json")
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		t.Fatalf("create host dir: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"host":"legacy"}`), 0o644); err != nil {
		t.Fatalf("write host config: %v", err)
	}

	want, err := os.Hostname()
	if err != nil {
		want = "unknown"
	}
	if got := loadHostID(); got != want {
		t.Fatalf("loadHostID() = %q, want %q", got, want)
	}
}
