package stats

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAndValidateDefaults(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	req := &Request{
		DB:      db,
		GroupBy: "SESSION_ID",
	}

	got, err := normalizeAndValidate(req)
	if err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	if got.Scope != scopeMessages {
		t.Fatalf("scope = %q, want %q", got.Scope, scopeMessages)
	}
	if got.GroupBy != "session_id" {
		t.Fatalf("groupBy = %q, want session_id", got.GroupBy)
	}
	if got.Measure != measureCount {
		t.Fatalf("measure = %q, want %q", got.Measure, measureCount)
	}
	if got.Field != "all" {
		t.Fatalf("field = %q, want all", got.Field)
	}
	if got.PageSize != defaultPageSize {
		t.Fatalf("pageSize = %d, want %d", got.PageSize, defaultPageSize)
	}
}

func TestNormalizeAndValidateScopeKeyMismatch(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		Scope:   "sessions",
		GroupBy: "bash_command",
	})
	if err == nil {
		t.Fatal("expected error for scope/key mismatch")
	}
	if !strings.Contains(err.Error(), "invalid --group-by value") {
		t.Fatalf("error = %q, want invalid --group-by", err.Error())
	}
}

func TestNormalizeAndValidateInvalidMeasure(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Measure: "bad",
	})
	if err == nil {
		t.Fatal("expected error for invalid measure")
	}
	if !strings.Contains(err.Error(), "invalid --measure value") {
		t.Fatalf("error = %q, want invalid --measure", err.Error())
	}
}

func TestNormalizeAndValidateInvalidField(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Field:   "bad_field",
	})
	if err == nil {
		t.Fatal("expected error for invalid field")
	}
	if !strings.Contains(err.Error(), "invalid --field value") {
		t.Fatalf("error = %q, want invalid --field", err.Error())
	}
}

func TestNormalizeAndValidateInvalidRole(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Role:    "system",
	})
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
	if !strings.Contains(err.Error(), "invalid --role value") {
		t.Fatalf("error = %q, want invalid --role", err.Error())
	}
}

func TestNormalizeAndValidateThinkingRoleAccepted(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	got, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Role:    "thinking",
	})
	if err != nil {
		t.Fatalf("expected thinking role to be accepted, got error: %v", err)
	}
	if got.Role != "thinking" {
		t.Fatalf("role = %q, want thinking", got.Role)
	}
}

func TestNormalizeAndValidateConflictingFilters(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		CWD:     "/workspace",
		Remote:  "git@github.com:test/repo",
	})
	if err == nil {
		t.Fatal("expected --cwd/--remote conflict error")
	}
	if !strings.Contains(err.Error(), "--cwd and --remote") {
		t.Fatalf("error = %q, want --cwd/--remote conflict", err.Error())
	}
}

func TestNormalizeAndValidateMinCountNegative(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:       db,
		GroupBy:  "session_id",
		MinCount: -1,
	})
	if err == nil {
		t.Fatal("expected --min-count error")
	}
	if !strings.Contains(err.Error(), "--min-count") {
		t.Fatalf("error = %q, want --min-count mention", err.Error())
	}
}

func TestNormalizeAndValidateTimeModeConflict(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Since:   "7d",
		After:   "2026-03-01",
	})
	if err == nil {
		t.Fatal("expected time filter mode conflict")
	}
	if !strings.Contains(err.Error(), "--since") {
		t.Fatalf("error = %q, want --since mention", err.Error())
	}
}

func TestNormalizeAndValidateQueryParseError(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	_, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Query:   "NOT",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse query") {
		t.Fatalf("error = %q, want parse query prefix", err.Error())
	}
}

func TestNormalizeAndValidateSinceCanonical(t *testing.T) {
	t.Parallel()

	db := &sql.DB{}
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	got, err := normalizeAndValidate(&Request{
		DB:      db,
		GroupBy: "session_id",
		Since:   "1d",
		Now:     now,
	})
	if err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	if got.Time.Canonical == "" {
		t.Fatal("expected canonical time filter")
	}
}
