package search

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateAtRune(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{
			name:     "short input returns unchanged",
			input:    "hello",
			maxBytes: 100,
			want:     "hello",
		},
		{
			name:     "ascii truncation at exact boundary",
			input:    "abcdefghij",
			maxBytes: 5,
			want:     "abcde…",
		},
		{
			name:     "boundary splits multi-byte rune — advances forward",
			input:    "abc" + "€" + "def", // € is 3 bytes (E2 82 AC)
			maxBytes: 4,                   // lands inside €
			want:     "abc€…",
		},
		{
			name:     "boundary at exact rune start",
			input:    "abc" + "€" + "def",
			maxBytes: 3,
			want:     "abc…",
		},
		{
			name:     "emoji (4-byte rune) preserved when boundary inside it",
			input:    "x" + "😀" + "y", // 😀 is 4 bytes (F0 9F 98 80)
			maxBytes: 3,               // inside emoji
			want:     "x😀…",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateAtRune(tc.input, tc.maxBytes)
			if got != tc.want {
				t.Errorf("TruncateAtRune(%q, %d) = %q, want %q", tc.input, tc.maxBytes, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateAtRune(%q, %d) returned invalid UTF-8: %q", tc.input, tc.maxBytes, got)
			}
		})
	}
}

func TestTruncateAtRuneNeverProducesInvalidUTF8(t *testing.T) {
	// Property: for every byte boundary inside a string that contains
	// multi-byte UTF-8 runes, the output is always valid UTF-8.
	s := strings.Repeat("a"+"€"+"b"+"😀"+"c", 20)
	for n := range len(s) {
		got := TruncateAtRune(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at maxBytes=%d: %q", n, got)
		}
	}
}
