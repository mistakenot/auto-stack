package search

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mistakenot/auto-search/internal/query"
)

// SessionHit is a single session-scope search result.
type SessionHit struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"sessionId"`
	Score          float64 `json:"score"`
	Workspace      string  `json:"workspace"`
	FirstMessageAt int64   `json:"firstMessageAt"`
	LastMessageAt  int64   `json:"lastMessageAt"`
	TotalMessages  int     `json:"totalMessages"`
}

// SessionSearchResult is the full response for a session-scope search.
type SessionSearchResult struct {
	Meta Meta         `json:"_meta"`
	Hits []SessionHit `json:"hits"`
}

// SessionSearchOpts holds the parameters for a session-scope search.
type SessionSearchOpts struct {
	DB        *sql.DB
	Query     string
	Since     string
	After     string
	Before    string
	CWD       string
	Remote    string
	Skill     string
	RequestID string
	Now       time.Time
}

// SearchSessions performs a BM25 session-scope search.
func SearchSessions(opts *SessionSearchOpts) (*SessionSearchResult, error) {
	if opts == nil {
		return nil, errors.New("search options are required")
	}

	start := time.Now()

	if opts.CWD != "" && opts.Remote != "" {
		return nil, errors.New("--cwd and --remote are mutually exclusive")
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	timeFilter, err := ParseTimeFilter(now, opts.Since, opts.After, opts.Before)
	if err != nil {
		return nil, err
	}

	ast, err := query.Parse(opts.Query)
	if err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}

	fts := query.CompileFTS(ast)
	filters := normalizeFilters(opts.CWD, opts.Remote, opts.Skill, timeFilter.Canonical)

	hits, err := execSessionSearch(opts.DB, fts, opts.CWD, opts.Remote, opts.Skill, timeFilter, opts.Query, filters)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start).Milliseconds()
	return &SessionSearchResult{
		Meta: Meta{
			RequestID: opts.RequestID,
			Scope:     "sessions",
			Mode:      "bm25",
			Query:     opts.Query,
			ElapsedMs: elapsed,
			TotalHits: len(hits),
		},
		Hits: hits,
	}, nil
}

func execSessionSearch(db *sql.DB, fts, cwd, remote, skill string, timeFilter TimeFilter, rawQuery, filters string) ([]SessionHit, error) {
	q := `
		SELECT s.session_id, s.workspace, s.first_message_at, s.last_message_at,
		       bm25(sessions_fts) AS score
		FROM sessions_fts
		JOIN sessions s ON s.doc_id = sessions_fts.rowid
		WHERE sessions_fts MATCH ?
	`
	args := []any{fts}

	if cwd != "" {
		q += " AND s.workspace = ?"
		args = append(args, cwd)
	}
	if remote != "" {
		q += " AND s.git_remote = ?"
		args = append(args, remote)
	}
	if skill != "" {
		q += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE skill_name = ?)"
		args = append(args, skill)
	}
	if timeFilter.StartMs != nil {
		q += " AND s.first_message_at >= ?"
		args = append(args, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		q += " AND s.first_message_at < ?"
		args = append(args, *timeFilter.EndMs)
	}

	q += " ORDER BY score LIMIT 50"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("session search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SessionHit
	for rows.Next() {
		var (
			sessionID      string
			workspace      string
			firstMessageAt int64
			lastMessageAt  int64
			score          float64
		)
		if err := rows.Scan(&sessionID, &workspace, &firstMessageAt, &lastMessageAt, &score); err != nil {
			return nil, fmt.Errorf("scan session hit: %w", err)
		}

		// Count messages for this session.
		var totalMessages int
		_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&totalMessages)

		hits = append(hits, SessionHit{
			ID:             HitID("sessions", "bm25", rawQuery, filters, sessionID),
			SessionID:      sessionID,
			Score:          score,
			Workspace:      workspace,
			FirstMessageAt: firstMessageAt,
			LastMessageAt:  lastMessageAt,
			TotalMessages:  totalMessages,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session hits: %w", err)
	}
	if hits == nil {
		hits = []SessionHit{}
	}
	return hits, nil
}
