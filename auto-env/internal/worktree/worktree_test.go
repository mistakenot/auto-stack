package worktree

import (
	"strings"
	"testing"
)

func TestSlotHash(t *testing.T) {
	s1 := hashSlot("feature-abc")
	s2 := hashSlot("feature-abc")
	if s1 != s2 {
		t.Errorf("same name produced different slots: %d vs %d", s1, s2)
	}
	if s1 < 1 || s1 > 99 {
		t.Errorf("slot %d out of range [1, 99]", s1)
	}

	s3 := hashSlot("feature-xyz")
	// Different names CAN produce same slot (collision), but usually don't
	_ = s3
}

func TestBranchSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"feature/FOO--bar", "feature-foo-bar"},
		{"main", "main"},
		{"UPPER_CASE", "upper-case"},
		{"--leading-trailing--", "leading-trailing"},
		{"a/b/c/d", "a-b-c-d"},
		{"special!@#$chars", "special-chars"},
	}
	for _, tt := range tests {
		got := Slugify(tt.input)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBranchSlugTruncation(t *testing.T) {
	long := strings.Repeat("a", 70)
	slug := Slugify(long)
	if len(slug) > 63 {
		t.Errorf("slug length %d exceeds 63", len(slug))
	}
}
