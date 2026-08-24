package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// multibyteBlob returns a string of `count` copies of a 4-byte rune (😀,
// U+1F600), giving a string whose length in bytes is never a multiple of the
// naive cut offsets used below — so a byte-slice truncation would split a rune.
func multibyteBlob(count int) string {
	return strings.Repeat("😀", count)
}

func TestRuneSafePrefixNeverSplitsRune(t *testing.T) {
	s := multibyteBlob(50) // 200 bytes
	for n := 0; n <= len(s)+4; n++ {
		got := runeSafePrefix(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("runeSafePrefix(blob, %d) = invalid UTF-8: %q", n, got)
		}
		if len(got) > n && n < len(s) {
			t.Fatalf("runeSafePrefix(blob, %d) grew to %d bytes", n, len(got))
		}
	}
}

func TestRuneSafeSuffixNeverSplitsRune(t *testing.T) {
	s := multibyteBlob(50) // 200 bytes
	for n := 0; n <= len(s)+4; n++ {
		got := runeSafeSuffix(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("runeSafeSuffix(blob, %d) = invalid UTF-8: %q", n, got)
		}
		if len(got) > n && n < len(s) {
			t.Fatalf("runeSafeSuffix(blob, %d) grew to %d bytes", n, len(got))
		}
	}
}

func TestMidTruncateShortInputUnchanged(t *testing.T) {
	s := "short enough"
	if got := midTruncate(s, 100, "msg-1"); got != s {
		t.Fatalf("midTruncate(short) = %q, want %q", got, s)
	}
}

func TestMidTruncateKeepsValidUTF8(t *testing.T) {
	s := multibyteBlob(500)
	got := midTruncate(s, 100, "msg-123")
	if !utf8.ValidString(got) {
		t.Fatalf("midTruncate produced invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "msg-123") {
		t.Fatalf("midTruncate dropped the retrieval marker: %q", got)
	}
}

func TestTranscriptSummaryShortInputUnchanged(t *testing.T) {
	s := "a short transcript that stays whole"
	if got := transcriptSummary(s); got != s {
		t.Fatalf("transcriptSummary(short) = %q, want %q", got, s)
	}
}

func TestTranscriptSummaryKeepsValidUTF8(t *testing.T) {
	// Length must exceed n*2+10 = 610 bytes to trigger truncation; 500 runes
	// = 2000 bytes clears that.
	s := multibyteBlob(500)
	got := transcriptSummary(s)
	if !utf8.ValidString(got) {
		t.Fatalf("transcriptSummary produced invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "\n...\n") {
		t.Fatalf("transcriptSummary dropped the elision marker: %q", got)
	}
}
