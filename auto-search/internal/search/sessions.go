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
	Role      string
	Field     string
	Offset    int
	PageSize  int
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
	offset, pageSize, err := normalizePagination(opts.Offset, opts.PageSize)
	if err != nil {
		return nil, err
	}
	field, err := normalizeField(opts.Field)
	if err != nil {
		return nil, err
	}
	role, err := normalizeRole(opts.Role)
	if err != nil {
		return nil, err
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
	filters := normalizeFilters(opts.CWD, opts.Remote, opts.Skill, role, field, timeFilter.Canonical)

	hits, stats, err := execSessionSearch(opts.DB, fts, opts.CWD, opts.Remote, opts.Skill, role, field, timeFilter, opts.Query, filters, offset, pageSize)
	if err != nil {
		return nil, err
	}

	returnedHits := len(hits)
	hasMore := offset+returnedHits < stats.TotalMatches
	var nextOffset *int
	if hasMore {
		next := offset + returnedHits
		nextOffset = &next
	}

	elapsed := time.Since(start).Milliseconds()
	return &SessionSearchResult{
		Meta: Meta{
			RequestID:    opts.RequestID,
			Scope:        "sessions",
			Mode:         "bm25",
			Query:        opts.Query,
			ElapsedMs:    elapsed,
			TotalHits:    stats.TotalMatches,
			TotalMatches: stats.TotalMatches,
			DistinctSessions: stats.DistinctSessions,
			DistinctMessages: stats.DistinctMessages,
			ReturnedHits: returnedHits,
			PageSize:     pageSize,
			Offset:       offset,
			HasMore:      hasMore,
			NextOffset:   nextOffset,
			IsCapped:     false,
		},
		Hits: hits,
	}, nil
}

func execSessionSearch(db *sql.DB, fts, cwd, remote, skill, role, field string, timeFilter TimeFilter, rawQuery, filters string, offset, pageSize int) ([]SessionHit, matchStats, error) {
	zeroStats := matchStats{}

	baseQuery := `
		FROM sessions_fts
		JOIN sessions s ON s.doc_id = sessions_fts.rowid
		WHERE sessions_fts MATCH ?
	`
	args := []any{fts}

	if cwd != "" {
		baseQuery += " AND s.workspace = ?"
		args = append(args, cwd)
	}
	if remote != "" {
		baseQuery += " AND s.git_remote = ?"
		args = append(args, remote)
	}
	if skill != "" {
		baseQuery += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE skill_name = ?)"
		args = append(args, skill)
	}
	if role != "" {
		baseQuery += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE role = ?)"
		args = append(args, role)
	}
	switch field {
	case searchFieldAll:
		// no-op
	case searchFieldContent:
		baseQuery += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE tool_input = '' AND role != 'tool')"
	case searchFieldToolInput:
		baseQuery += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE tool_input != '')"
	case searchFieldToolOutput:
		baseQuery += " AND s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE role = 'tool')"
	default:
		return nil, zeroStats, fmt.Errorf("invalid --field value %q (use all, content, tool_input, tool_output)", field)
	}
	if timeFilter.StartMs != nil {
		baseQuery += " AND s.first_message_at >= ?"
		args = append(args, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		baseQuery += " AND s.first_message_at < ?"
		args = append(args, *timeFilter.EndMs)
	}

	countQuery := "SELECT COUNT(*) " + baseQuery
	var totalHits int
	if err := db.QueryRow(countQuery, args...).Scan(&totalHits); err != nil {
		return nil, zeroStats, fmt.Errorf("session search count query: %w", err)
	}

	distinctMessagesQuery := `
		SELECT COUNT(DISTINCT m.message_id)
		FROM messages m
		JOIN (
			SELECT DISTINCT s.session_id
	` + baseQuery + `
		) matched ON matched.session_id = m.session_id
	`
	var distinctMessages int
	if err := db.QueryRow(distinctMessagesQuery, args...).Scan(&distinctMessages); err != nil {
		return nil, zeroStats, fmt.Errorf("session search distinct messages query: %w", err)
	}

	q := `
		SELECT s.session_id, s.workspace, s.first_message_at, s.last_message_at,
		       bm25(sessions_fts) AS score
	` + baseQuery + `
		ORDER BY score, s.session_id
		LIMIT ? OFFSET ?
	`
	hitArgs := append(append([]any{}, args...), pageSize, offset)

	rows, err := db.Query(q, hitArgs...)
	if err != nil {
		return nil, zeroStats, fmt.Errorf("session search query: %w", err)
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
			return nil, zeroStats, fmt.Errorf("scan session hit: %w", err)
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
		return nil, zeroStats, fmt.Errorf("iterate session hits: %w", err)
	}
	if hits == nil {
		hits = []SessionHit{}
	}
	return hits, matchStats{
		TotalMatches:     totalHits,
		DistinctSessions: totalHits,
		DistinctMessages: distinctMessages,
	}, nil
}
