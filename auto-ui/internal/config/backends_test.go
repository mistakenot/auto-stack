package config

import (
	"errors"
	"testing"
)

func TestLoadBackendsMissingFileIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	cfg, err := LoadBackends(path)
	if err != nil {
		t.Fatalf("LoadBackends on missing file: %v", err)
	}
	if len(cfg.Backends) != 0 {
		t.Fatalf("expected empty config, got %d backends", len(cfg.Backends))
	}
}

func TestSaveLoadBackendsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	want := BackendsConfig{Backends: []Backend{
		{URI: "unix:///tmp/aw.sock", Name: "local", HostID: "host-a"},
		{URI: "tcp://127.0.0.1:9001", HostID: "host-b"},
	}}
	if err := SaveBackends(path, want); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}
	got, err := LoadBackends(path)
	if err != nil {
		t.Fatalf("LoadBackends: %v", err)
	}
	if len(got.Backends) != len(want.Backends) {
		t.Fatalf("backend count = %d, want %d", len(got.Backends), len(want.Backends))
	}
	for i := range want.Backends {
		if got.Backends[i] != want.Backends[i] {
			t.Errorf("backend[%d] = %+v, want %+v", i, got.Backends[i], want.Backends[i])
		}
	}
}

func TestSaveBackendsRejectsDuplicateURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	cfg := BackendsConfig{Backends: []Backend{
		{URI: "tcp://127.0.0.1:9001"},
		{URI: "tcp://127.0.0.1:9001"},
	}}
	err = SaveBackends(path, cfg)
	if err == nil {
		t.Fatal("expected duplicate-URI rejection, got nil")
	}
	var verrs *ValidationErrorsError
	if !errors.As(err, &verrs) {
		t.Fatalf("expected ValidationErrorsError, got %T: %v", err, err)
	}
	if !hasCode(verrs.Errors, "duplicate") {
		t.Fatalf("expected a duplicate error, got %+v", verrs.Errors)
	}
}

func TestSaveBackendsRejectsDuplicateHostID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	cfg := BackendsConfig{Backends: []Backend{
		{URI: "tcp://127.0.0.1:9001", HostID: "dup"},
		{URI: "tcp://127.0.0.1:9002", HostID: "dup"},
	}}
	err = SaveBackends(path, cfg)
	if err == nil {
		t.Fatal("expected duplicate-hostId rejection, got nil")
	}
	var verrs *ValidationErrorsError
	if !errors.As(err, &verrs) {
		t.Fatalf("expected ValidationErrorsError, got %T: %v", err, err)
	}
}

func TestSaveBackendsRejectsInvalidURI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	cases := []string{"", "http://example.com", "no-scheme", "tcp://"}
	for _, uri := range cases {
		cfg := BackendsConfig{Backends: []Backend{{URI: uri}}}
		if err := SaveBackends(path, cfg); err == nil {
			t.Errorf("expected rejection for uri %q, got nil", uri)
		}
	}
}

func TestSaveBackendsAcceptsValidSchemes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	cfg := BackendsConfig{Backends: []Backend{
		{URI: "unix:///tmp/a.sock"},
		{URI: "tcp://127.0.0.1:9001"},
	}}
	if err := SaveBackends(path, cfg); err != nil {
		t.Fatalf("SaveBackends with valid schemes: %v", err)
	}
}

func hasCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
