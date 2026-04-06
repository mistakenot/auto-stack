package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-watch/internal/config"
)

func TestHostPathUsesHomeAutoHostJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got, err := config.HostPath()
	if err != nil {
		t.Fatalf("HostPath() error = %v", err)
	}
	want := filepath.Join(tmpHome, ".auto", "host.json")
	if got != want {
		t.Fatalf("HostPath() = %q, want %q", got, want)
	}
}

func TestEnsureHostFileReadsExistingHostID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostPath := filepath.Join(tmpHome, ".auto", "host.json")
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		t.Fatalf("create host dir: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"hostId":"watch-host"}`), 0o644); err != nil {
		t.Fatalf("write host file: %v", err)
	}

	gotPath, info, created, err := config.EnsureHostFile()
	if err != nil {
		t.Fatalf("EnsureHostFile() error = %v", err)
	}
	if gotPath != hostPath {
		t.Fatalf("path = %q, want %q", gotPath, hostPath)
	}
	if created {
		t.Fatal("created = true, want false")
	}
	if info.HostID != "watch-host" {
		t.Fatalf("HostID = %q, want %q", info.HostID, "watch-host")
	}
}

func TestEnsureHostFileCreatesHostIDField(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostPath := filepath.Join(tmpHome, ".auto", "host.json")
	gotPath, info, created, err := config.EnsureHostFile()
	if err != nil {
		t.Fatalf("EnsureHostFile() error = %v", err)
	}
	if gotPath != hostPath {
		t.Fatalf("path = %q, want %q", gotPath, hostPath)
	}
	if !created {
		t.Fatal("created = false, want true")
	}
	if info.HostID == "" {
		t.Fatal("HostID is empty")
	}

	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("read host file: %v", err)
	}
	var got struct {
		HostID string `json:"hostId"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal host file: %v", err)
	}
	if got.HostID == "" {
		t.Fatal("host file hostId is empty")
	}
}
