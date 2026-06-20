package config

import (
	"strings"
	"testing"
)

func TestValidateHostID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"dev-box", true},
		{"host.charlie", true},
		{"a1-b2.c3_d4", true},
		{"0abc", true},
		{"", false},
		{"UPPER", false},
		{"has space", false},
		{"-leading", false},
		{".leading", false},
		{"_leading", false},
		{"special!char", false},
	}
	for _, tt := range tests {
		err := ValidateHostID(tt.id)
		if tt.valid && err != nil {
			t.Errorf("ValidateHostID(%q) = %v, want nil", tt.id, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateHostID(%q) = nil, want error", tt.id)
		}
	}
}

func TestHostIDQuietly(t *testing.T) {
	if got := HostIDQuietly(); got == "" {
		t.Error("HostIDQuietly() = \"\", want non-empty")
	}
}

func TestEnsureHostCreatesValidDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, cfg, _, err := EnsureHost()
	if err != nil {
		t.Fatalf("EnsureHost() error = %v", err)
	}
	if err := ValidateHostID(cfg.HostID); err != nil {
		t.Errorf("EnsureHost() hostId %q invalid: %v", cfg.HostID, err)
	}
	if !strings.Contains(cfg.HostID, ".") {
		t.Errorf("EnsureHost() hostId %q missing hostname.username separator", cfg.HostID)
	}
}
