package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	envDir := filepath.Join(dir, EnvDir)
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"up_command": "npm start", "down_command": "npm stop"}`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpCommand != "npm start" {
		t.Errorf("UpCommand = %q, want %q", cfg.UpCommand, "npm start")
	}
	if cfg.DownCommand != "npm stop" {
		t.Errorf("DownCommand = %q, want %q", cfg.DownCommand, "npm stop")
	}
	if cfg.PortBase != DefaultPortBase {
		t.Errorf("PortBase = %d, want %d", cfg.PortBase, DefaultPortBase)
	}
	if cfg.PortStride != DefaultPortStride {
		t.Errorf("PortStride = %d, want %d", cfg.PortStride, DefaultPortStride)
	}
	if cfg.Delimiters != [2]string{"{{", "}}"} {
		t.Errorf("Delimiters = %v, want default", cfg.Delimiters)
	}
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{
		"up_command": "pm2 start",
		"down_command": "pm2 delete all",
		"port_base": 4000,
		"port_stride": 50,
		"delimiters": ["[[", "]]"]
	}`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PortBase != 4000 {
		t.Errorf("PortBase = %d, want 4000", cfg.PortBase)
	}
	if cfg.PortStride != 50 {
		t.Errorf("PortStride = %d, want 50", cfg.PortStride)
	}
	if cfg.Delimiters != [2]string{"[[", "]]"} {
		t.Errorf("Delimiters = %v, want [[, ]]", cfg.Delimiters)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"up_command": "npm start"}`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing down_command")
	}
}

func TestLoadBadDelimiters(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{
		"up_command": "x",
		"down_command": "y",
		"delimiters": ["only-one"]
	}`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for bad delimiters")
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}
