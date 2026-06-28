package cli_test

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-reflect/internal/events"
)

// gapRowResp mirrors one element of the `gap list` top-level array.
type gapRowResp struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	TS        string `json:"ts"`
	Report    string `json:"report"`
	Moment    string `json:"moment"`
}

var evIDRe = regexp.MustCompile(`^ev-[0-9a-f]{8}$`)

// appendFeedback writes a feedback event directly to the repo's event log,
// giving the test precise control over the timestamp, session, and whether a gap
// is present — without standing up the full retrieve/select/feedback loop.
func appendFeedback(t *testing.T, repo, session string, now time.Time, gap *events.FeedbackGap) events.Event {
	t.Helper()
	payload := events.FeedbackPayload{
		Outcome: "success",
		Summary: "did the task",
		Gap:     gap,
	}
	stored, err := events.AppendEvent(repo, events.TypeFeedback, payload, events.AppendOptions{
		SessionID: session,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("append feedback event: %v", err)
	}
	return stored
}

func listGaps(t *testing.T, repo string, args ...string) (rows []gapRowResp, rawStdout string) {
	t.Helper()
	full := append([]string{"gap", "list"}, args...)
	stdout, stderr, code := runCLIAt(t, repo, full...)
	if code != 0 {
		t.Fatalf("gap list failed: code=%d\nstderr:\n%s", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("decode gap list: %v\nraw:\n%s", err, stdout)
	}
	return rows, stdout
}

// TestGapListSurfacesGaps asserts a feedback event carrying a gap surfaces as one
// row with the source event id (ev-), session id, ts, report, and moment.
func TestGapListSurfacesGaps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	stored := appendFeedback(t, repo, "gap-session", time.Now(), &events.FeedbackGap{
		Report: "needed a rule about retry backoff",
		Moment: "while wiring the http client",
	})

	rows, _ := listGaps(t, repo)
	if len(rows) != 1 {
		t.Fatalf("expected 1 gap row, got %d: %#v", len(rows), rows)
	}
	row := rows[0]
	if !evIDRe.MatchString(row.ID) {
		t.Fatalf("expected row id matching ^ev-[0-9a-f]{8}$, got %q", row.ID)
	}
	if row.ID != stored.ID {
		t.Fatalf("row id %q != source feedback event id %q", row.ID, stored.ID)
	}
	if row.SessionID != "gap-session" {
		t.Fatalf("expected session_id gap-session, got %q", row.SessionID)
	}
	if strings.TrimSpace(row.TS) == "" {
		t.Fatalf("expected non-empty ts, got %q", row.TS)
	}
	if row.Report != "needed a rule about retry backoff" {
		t.Fatalf("unexpected report: %q", row.Report)
	}
	if row.Moment != "while wiring the http client" {
		t.Fatalf("unexpected moment: %q", row.Moment)
	}
}

// TestGapListOmitsGaplessFeedback asserts feedback events without a gap are
// skipped (gap == nil), so only gap-bearing feedback surfaces.
func TestGapListOmitsGaplessFeedback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	appendFeedback(t, repo, "no-gap", time.Now(), nil)
	withGap := appendFeedback(t, repo, "with-gap", time.Now(), &events.FeedbackGap{
		Report: "missing rule",
		Moment: "during review",
	})

	rows, _ := listGaps(t, repo)
	if len(rows) != 1 {
		t.Fatalf("expected only the gap-bearing feedback, got %d rows: %#v", len(rows), rows)
	}
	if rows[0].ID != withGap.ID {
		t.Fatalf("surfaced the wrong feedback: got %q want %q", rows[0].ID, withGap.ID)
	}
}

// TestGapListSinceFilter asserts --since drops gaps whose feedback event predates
// the window while keeping recent ones.
func TestGapListSinceFilter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	appendFeedback(t, repo, "old-session", time.Now().Add(-48*time.Hour), &events.FeedbackGap{
		Report: "old gap", Moment: "two days ago",
	})
	fresh := appendFeedback(t, repo, "fresh-session", time.Now(), &events.FeedbackGap{
		Report: "fresh gap", Moment: "just now",
	})

	// Unfiltered returns both.
	all, _ := listGaps(t, repo)
	if len(all) != 2 {
		t.Fatalf("expected 2 gaps unfiltered, got %d", len(all))
	}

	// --since 1h keeps only the fresh one.
	recent, _ := listGaps(t, repo, "--since", "1h")
	if len(recent) != 1 {
		t.Fatalf("expected 1 gap within --since 1h, got %d: %#v", len(recent), recent)
	}
	if recent[0].ID != fresh.ID {
		t.Fatalf("--since kept the wrong row: got %q want %q", recent[0].ID, fresh.ID)
	}
}

// TestGapListEmptyReturnsArray asserts the empty result marshals as [] (never
// null), so consumers can always iterate the array.
func TestGapListEmptyReturnsArray(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	rows, raw := listGaps(t, repo)
	if len(rows) != 0 {
		t.Fatalf("expected no gaps, got %d", len(rows))
	}
	if strings.TrimSpace(raw) != "[]" {
		t.Fatalf("expected empty list to marshal as [], got %q", strings.TrimSpace(raw))
	}
}

// TestGapListTextFormat asserts --format text renders the gap fields on a line.
func TestGapListTextFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	appendFeedback(t, repo, "text-session", time.Now(), &events.FeedbackGap{
		Report: "needed a doc on flags",
		Moment: "while reading help output",
	})

	stdout, stderr, code := runCLIAt(t, repo, "gap", "list", "--format", "text")
	if code != 0 {
		t.Fatalf("gap list --format text failed: code=%d\nstderr:\n%s", code, stderr)
	}
	for _, needle := range []string{"needed a doc on flags", "while reading help output", "text-session", "ev-"} {
		if !strings.Contains(stdout, needle) {
			t.Fatalf("text output missing %q\noutput:\n%s", needle, stdout)
		}
	}
}

// TestGapListDomainUnsupported documents the chosen --domain semantics: feedback
// events carry no domain, so the flag fails fast with a remediation hint rather
// than silently returning an empty list.
func TestGapListDomainUnsupported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	appendFeedback(t, repo, "s1", time.Now(), &events.FeedbackGap{Report: "r", Moment: "m"})

	stdout, stderr, code := runCLIAt(t, repo, "gap", "list", "--domain", "docs")
	if code != 1 {
		t.Fatalf("expected exit 1 for --domain, got %d\nstdout:\n%s", code, stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected empty stdout on error, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--domain") || !strings.Contains(stderr, "no domain") {
		t.Fatalf("expected stderr to explain the --domain limitation, got:\n%s", stderr)
	}
}
