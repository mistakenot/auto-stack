package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-watch/internal/daemon"
)

// TestTailOutputByteCapAndCarriageReturns (AC-12): a 400 KB single-"line" log
// full of \r progress redraws must be capped at 8 KiB, keep only the final text
// after each line's last \r, and end in a breadcrumb naming the full output path.
func TestTailOutputByteCapAndCarriageReturns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")

	// One long newline-free line of progress redraws, then a final segment after
	// the last \r. ~400 KB of redraw noise before the text that matters.
	var sb strings.Builder
	for range 40000 {
		sb.WriteString("downloading 12% [####......]\r")
	}
	const finalText = "done: build succeeded"
	sb.WriteString(finalText)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := len(sb.String()); got < 400*1024 {
		t.Fatalf("fixture should exceed 400 KB, got %d bytes", got)
	}

	tail := daemon.TailOutput(path, daemon.OutputTailMaxBytes)

	if len(tail) > daemon.OutputTailMaxBytes {
		t.Fatalf("tail must be <= %d bytes, got %d", daemon.OutputTailMaxBytes, len(tail))
	}
	if !strings.Contains(tail, finalText) {
		t.Fatalf("tail must keep the final text after the last \\r; got:\n%s", tail)
	}
	breadcrumb := "…[truncated — full output: " + path + "]"
	if !strings.HasSuffix(tail, breadcrumb) {
		t.Fatalf("tail must end with the path breadcrumb %q; got:\n%s", breadcrumb, tail)
	}
}

// TestTailOutputSmallFileUntouched: a small log under the cap is returned
// verbatim (trimmed), with no breadcrumb.
func TestTailOutputSmallFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tail := daemon.TailOutput(path, daemon.OutputTailMaxBytes)
	if tail != "line one\nline two" {
		t.Fatalf("unexpected tail for a small file: %q", tail)
	}
	if strings.Contains(tail, "truncated") {
		t.Fatalf("a small file must not be marked truncated: %q", tail)
	}
}

// TestTailOutputMissingFile: a missing or empty path yields an empty tail, never
// an error or a breadcrumb.
func TestTailOutputMissingFile(t *testing.T) {
	if got := daemon.TailOutput(filepath.Join(t.TempDir(), "nope.log"), daemon.OutputTailMaxBytes); got != "" {
		t.Fatalf("missing file should tail to empty string, got %q", got)
	}
	if got := daemon.TailOutput("", daemon.OutputTailMaxBytes); got != "" {
		t.Fatalf("empty path should tail to empty string, got %q", got)
	}
}
