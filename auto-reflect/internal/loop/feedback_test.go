package loop

import (
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/rules"
)

// setupTwoSelected seeds two rules, retrieves and selects both within a single
// session, and returns the service plus the two outstanding fb-ids in order.
func setupTwoSelected(t *testing.T, repo, sessionID string) (*Service, []string) {
	t.Helper()
	seedRule(t, repo, "first feedback topic", "content one", "go", rules.RuleTypeSoft)
	seedRule(t, repo, "second feedback topic", "content two", "go", rules.RuleTypeSoft)

	t.Setenv("AUTO_SESSION_ID", sessionID)
	svc := NewService(repo)
	retrieved, err := svc.Retrieve("first second feedback topic", nil, 0)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	ids := make([]string, 0, len(retrieved))
	for _, r := range retrieved {
		ids = append(ids, r.RetrievalID)
	}
	selected, err := svc.Select(ids)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	fbIDs := make([]string, 0, len(selected))
	for _, s := range selected {
		fbIDs = append(fbIDs, s.FeedbackID)
	}
	if len(fbIDs) != 2 {
		t.Fatalf("expected 2 outstanding fb-ids, got %d", len(fbIDs))
	}
	return svc, fbIDs
}

func TestSubmitFeedbackValidationMatrix(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(in *FeedbackInput, fb []string)
		wantField string
	}{
		{
			name: "missing id",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Rankings = []events.FeedbackRanking{{FeedbackID: fb[0], Rank: 1, Reason: "ok"}}
			},
			wantField: "rankings",
		},
		{
			name: "extra id",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Rankings = append(in.Rankings, events.FeedbackRanking{FeedbackID: "fb-deadbeef", Rank: 3, Reason: "extra"})
			},
			wantField: "feedback_id",
		},
		{
			name: "dup rank",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Rankings[0].Rank = 1
				in.Rankings[1].Rank = 1
			},
			wantField: "rank",
		},
		{
			name: "ungrounded gap",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Gap = &events.FeedbackGap{Report: "missing a rule", Moment: ""}
			},
			wantField: "gap.moment",
		},
		{
			name: "bad outcome",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Outcome = "kinda"
			},
			wantField: "outcome",
		},
		{
			name: "empty reason",
			mutate: func(in *FeedbackInput, fb []string) {
				in.Rankings[0].Reason = ""
			},
			wantField: "reason",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initLoopRepo(t)
			svc, fb := setupTwoSelected(t, repo, "session-validate")

			in := completeFeedback(fb)
			tc.mutate(&in, fb)

			errs, err := svc.SubmitFeedback(in, "")
			if err != nil {
				t.Fatalf("submit returned hard error: %v", err)
			}
			if len(errs) == 0 {
				t.Fatalf("expected validation errors for %s", tc.name)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Field, tc.wantField) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected an error on field containing %q, got %#v", tc.wantField, errs)
			}

			// No feedback event should have been appended on a rejected payload.
			all, _ := events.ReadAll(repo)
			for _, ev := range all {
				if ev.Type == events.TypeFeedback {
					t.Fatalf("rejected payload should not append a feedback event")
				}
			}
		})
	}
}

func TestSubmitFeedbackCompleteAppendsExactlyOnce(t *testing.T) {
	repo := initLoopRepo(t)
	svc, fb := setupTwoSelected(t, repo, "session-complete")

	in := completeFeedback(fb)
	errs, err := svc.SubmitFeedback(in, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got %#v", errs)
	}

	all, _ := events.ReadAll(repo)
	count := 0
	for _, ev := range all {
		if ev.Type == events.TypeFeedback {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one feedback event, got %d", count)
	}

	// Gate should now be clean for the session.
	res, err := svc.GateCheck("session-complete", "")
	if err != nil {
		t.Fatalf("gate check: %v", err)
	}
	if !res.Clean() {
		t.Fatalf("gate should be clean after complete feedback, outstanding: %v", res.Outstanding)
	}
}

func TestGateOpenBeforeFeedback(t *testing.T) {
	repo := initLoopRepo(t)
	svc, _ := setupTwoSelected(t, repo, "session-open")

	res, err := svc.GateCheck("session-open", "")
	if err != nil {
		t.Fatalf("gate check: %v", err)
	}
	if res.Clean() {
		t.Fatalf("gate should be open before feedback")
	}
	if len(res.Outstanding) != 2 {
		t.Fatalf("expected 2 outstanding, got %d", len(res.Outstanding))
	}
}

func TestGateNoRulesSessionPasses(t *testing.T) {
	repo := initLoopRepo(t)
	t.Setenv("AUTO_SESSION_ID", "empty-session")
	svc := NewService(repo)

	res, err := svc.GateCheck("empty-session", "")
	if err != nil {
		t.Fatalf("gate check: %v", err)
	}
	if !res.Clean() {
		t.Fatalf("a session that consumed no rules must pass, outstanding: %v", res.Outstanding)
	}
}

func TestGateOrphansOlderThanWindowDoNotBlock(t *testing.T) {
	repo := initLoopRepo(t)

	// An old selection with NO session id, far outside the lookback window.
	old := time.Now().UTC().Add(-72 * time.Hour)
	payload := events.SelectionPayload{Items: []events.SelectionItem{
		{FeedbackID: "fb-aaaaaaaa", RetrievalID: "rt-aaaaaaaa", RuleID: "r-aaaaaaaa"},
	}}
	if _, err := events.AppendEvent(repo, events.TypeSelection, payload, events.AppendOptions{Now: old}); err != nil {
		t.Fatalf("append old selection: %v", err)
	}

	// No session detectable: fall back to host+worktree within 24h window.
	t.Setenv("AUTO_SESSION_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	svc := NewService(repo)

	res, err := svc.GateCheck("", "")
	if err != nil {
		t.Fatalf("gate check: %v", err)
	}
	if !res.Clean() {
		t.Fatalf("orphans older than the lookback window must not block, outstanding: %v", res.Outstanding)
	}
}

func TestFeedbackSessionOverrideClosesOrphanWithAbandoned(t *testing.T) {
	repo := initLoopRepo(t)

	// Orphan selection from a long-dead session.
	payload := events.SelectionPayload{Items: []events.SelectionItem{
		{FeedbackID: "fb-bbbbbbbb", RetrievalID: "rt-bbbbbbbb", RuleID: "r-bbbbbbbb"},
	}}
	if _, err := events.AppendEvent(repo, events.TypeSelection, payload, events.AppendOptions{SessionID: "dead-session"}); err != nil {
		t.Fatalf("append orphan selection: %v", err)
	}

	svc := NewService(repo)
	in := FeedbackInput{
		Outcome:  OutcomeAbandoned,
		Summary:  "closing a dead session",
		Rankings: []events.FeedbackRanking{{FeedbackID: "fb-bbbbbbbb", Rank: 1, Reason: "abandoned"}},
	}
	errs, err := svc.SubmitFeedback(in, "dead-session")
	if err != nil {
		t.Fatalf("submit with override: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors closing orphan, got %#v", errs)
	}

	res, err := svc.GateCheck("dead-session", "")
	if err != nil {
		t.Fatalf("gate check: %v", err)
	}
	if !res.Clean() {
		t.Fatalf("orphan should be closed via --session override, outstanding: %v", res.Outstanding)
	}
}

// completeFeedback builds a valid feedback payload ranking the given fb-ids.
func completeFeedback(fb []string) FeedbackInput {
	rankings := make([]events.FeedbackRanking, 0, len(fb))
	for i, id := range fb {
		rankings = append(rankings, events.FeedbackRanking{FeedbackID: id, Rank: i + 1, Reason: "reason"})
	}
	return FeedbackInput{
		Outcome:  OutcomeSuccess,
		Summary:  "did the task",
		Rankings: rankings,
	}
}
