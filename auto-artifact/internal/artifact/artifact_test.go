package artifact

import (
	"strings"
	"testing"
)

func TestNewUUIDv4Format(t *testing.T) {
	id, err := NewUUIDv4()
	if err != nil {
		t.Fatalf("NewUUIDv4: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("uuid %q length = %d, want 36", id, len(id))
	}
	if id[14] != '4' {
		t.Errorf("uuid %q version nibble = %c, want 4", id, id[14])
	}
	if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("uuid %q variant nibble = %c, want 8/9/a/b", id, v)
	}
	other, _ := NewUUIDv4()
	if id == other {
		t.Errorf("two UUIDs collided: %s", id)
	}
}

func TestBuildKey(t *testing.T) {
	got := BuildKey("30d", "abc-uuid", "/some/dir/screenshot.png")
	want := "30d/abc-uuid/screenshot.png"
	if got != want {
		t.Errorf("BuildKey = %q, want %q", got, want)
	}
}

func TestResolveRetention(t *testing.T) {
	cases := []struct {
		flag, cfg, want string
		wantErr         bool
	}{
		{"", "", "90d", false},      // nothing set → default
		{"", "30d", "30d", false},   // config default honored
		{"7d", "30d", "7d", false},  // flag overrides config
		{"365d", "", "365d", false}, // valid tier
		{" 90d ", "", "90d", false}, // trimmed
		{"60d", "", "", true},       // invalid flag
		{"", "weird", "", true},     // invalid config default
	}
	for _, tc := range cases {
		got, err := ResolveRetention(tc.flag, tc.cfg)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ResolveRetention(%q,%q) = %q, want error", tc.flag, tc.cfg, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ResolveRetention(%q,%q): unexpected error %v", tc.flag, tc.cfg, err)
		}
		if got != tc.want {
			t.Errorf("ResolveRetention(%q,%q) = %q, want %q", tc.flag, tc.cfg, got, tc.want)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	if ct := DetectContentType("shot.png"); ct != "image/png" {
		t.Errorf("png content type = %q, want image/png", ct)
	}
	if ct := DetectContentType("clip.mp4"); !strings.HasPrefix(ct, "video/mp4") {
		t.Errorf("mp4 content type = %q, want video/mp4*", ct)
	}
	if ct := DetectContentType("data.unknownext"); ct != DefaultContentType {
		t.Errorf("unknown ext content type = %q, want %q", ct, DefaultContentType)
	}
}
