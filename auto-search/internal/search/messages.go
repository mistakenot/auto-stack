package search

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mistakenot/auto-search/internal/query"
)

// MessageHit is a single message-scope search result.
type MessageHit struct {
	ID                string  `json:"id"`
	SessionID         string  `json:"sessionId"`
	MessageID         string  `json:"messageId"`
	MessageType       string  `json:"messageType"`
	Score             float64 `json:"score"`
	SnippetStartIndex int     `json:"snippetStartIndex"`
	SnippetEndIndex   int     `json:"snippetEndIndex"`
	Snippet           string  `json:"snippet"`
	PreviousMessageID string  `json:"previousMessageId"`
	NextMessageID     string  `json:"nextMessageId"`
}

// MessageSearchResult is the full response for a message-scope search.
type MessageSearchResult struct {
	Meta Meta         `json:"_meta"`
	Hits []MessageHit `json:"hits"`
}

// Meta contains response metadata.
type Meta struct {
	RequestID        string `json:"request_id"`
	Scope            string `json:"scope"`
	Mode             string `json:"mode"`
	Query            string `json:"query"`
	ElapsedMs        int64  `json:"elapsed_ms"`
	TotalHits        int    `json:"total_hits"`
	ReturnedHits     int    `json:"returned_hits"`
	PageSize         int    `json:"page_size"`
	Offset           int    `json:"offset"`
	HasMore          bool   `json:"has_more"`
	NextOffset       *int   `json:"next_offset,omitempty"`
	WildcardFallback bool   `json:"wildcard_fallback"`
}

// MessageSearchOpts holds the parameters for a message-scope search.
type MessageSearchOpts struct {
	DB        *sql.DB
	Query     string
	Since     string
	After     string
	Before    string
	CWD       string
	Remote    string
	Skill     string
	Role      string
	Offset    int
	PageSize  int
	RequestID string
	Highlight bool
	Now       time.Time
}

const (
	minHitsForFallback = 3
	defaultPageSize    = 20
)

// SearchMessages performs a BM25 message-scope search.
func SearchMessages(opts *MessageSearchOpts) (*MessageSearchResult, error) {
	start := time.Now()

	if opts.CWD != "" && opts.Remote != "" {
		return nil, errors.New("--cwd and --remote are mutually exclusive")
	}
	offset, pageSize, err := normalizePagination(opts.Offset, opts.PageSize)
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
	terms := ExtractTerms(ast)
	filters := normalizeFilters(opts.CWD, opts.Remote, opts.Skill, opts.Role, timeFilter.Canonical)

	hits, totalHits, err := execMessageSearch(opts.DB, fts, opts.CWD, opts.Remote, opts.Skill, opts.Role, timeFilter, terms, opts.Highlight, opts.Query, filters, offset, pageSize)
	if err != nil {
		return nil, err
	}

	wildcard := false
	if totalHits < minHitsForFallback {
		fallbackAST := query.PrefixFallback(ast)
		fallbackFTS := query.CompileFTS(fallbackAST)
		fallbackHits, fallbackTotal, err := execMessageSearch(opts.DB, fallbackFTS, opts.CWD, opts.Remote, opts.Skill, opts.Role, timeFilter, terms, opts.Highlight, opts.Query, filters, offset, pageSize)
		if err == nil && fallbackTotal > totalHits {
			hits = fallbackHits
			totalHits = fallbackTotal
			wildcard = true
		}
	}

	returnedHits := len(hits)
	hasMore := offset+returnedHits < totalHits
	var nextOffset *int
	if hasMore {
		next := offset + returnedHits
		nextOffset = &next
	}

	elapsed := time.Since(start).Milliseconds()
	return &MessageSearchResult{
		Meta: Meta{
			RequestID:        opts.RequestID,
			Scope:            "messages",
			Mode:             "bm25",
			Query:            opts.Query,
			ElapsedMs:        elapsed,
			TotalHits:        totalHits,
			ReturnedHits:     returnedHits,
			PageSize:         pageSize,
			Offset:           offset,
			HasMore:          hasMore,
			NextOffset:       nextOffset,
			WildcardFallback: wildcard,
		},
		Hits: hits,
	}, nil
}

func execMessageSearch(db *sql.DB, fts, cwd, remote, skill, role string, timeFilter TimeFilter, terms []string, highlight bool, rawQuery, filters string, offset, pageSize int) ([]MessageHit, int, error) {
	baseQuery := `
		FROM messages_fts
		JOIN messages m ON m.doc_id = messages_fts.rowid
		WHERE messages_fts MATCH ?
	`
	args := []any{fts}

	if cwd != "" {
		baseQuery += " AND m.workspace = ?"
		args = append(args, cwd)
	}
	if remote != "" {
		baseQuery += " AND m.git_remote = ?"
		args = append(args, remote)
	}
	if skill != "" {
		baseQuery += " AND m.skill_name = ?"
		args = append(args, skill)
	}
	if role != "" {
		baseQuery += " AND m.role = ?"
		args = append(args, role)
	}
	if timeFilter.StartMs != nil {
		baseQuery += " AND m.timestamp >= ?"
		args = append(args, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		baseQuery += " AND m.timestamp < ?"
		args = append(args, *timeFilter.EndMs)
	}

	countQuery := "SELECT COUNT(*) " + baseQuery
	var totalHits int
	if err := db.QueryRow(countQuery, args...).Scan(&totalHits); err != nil {
		return nil, 0, fmt.Errorf("message search count query: %w", err)
	}

	q := `
		SELECT m.message_id, m.session_id, m.role, m.content_truncated,
		       m.message_index, bm25(messages_fts) AS score
	` + baseQuery + `
		ORDER BY score, m.message_id
		LIMIT ? OFFSET ?
	`
	hitArgs := append(append([]any{}, args...), pageSize, offset)

	rows, err := db.Query(q, hitArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("message search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []MessageHit
	for rows.Next() {
		var (
			messageID        string
			sessionID        string
			role             string
			contentTruncated string
			messageIndex     int
			score            float64
		)
		if err := rows.Scan(&messageID, &sessionID, &role, &contentTruncated, &messageIndex, &score); err != nil {
			return nil, 0, fmt.Errorf("scan message hit: %w", err)
		}

		snippet, startIdx, endIdx := Snippet(contentTruncated, terms, highlight)

		prev, next := neighborMessageIDs(db, sessionID, messageIndex)

		hits = append(hits, MessageHit{
			ID:                HitID("messages", "bm25", rawQuery, filters, messageID),
			SessionID:         sessionID,
			MessageID:         messageID,
			MessageType:       role,
			Score:             score,
			SnippetStartIndex: startIdx,
			SnippetEndIndex:   endIdx,
			Snippet:           snippet,
			PreviousMessageID: prev,
			NextMessageID:     next,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate message hits: %w", err)
	}
	if hits == nil {
		hits = []MessageHit{}
	}
	return hits, totalHits, nil
}

func neighborMessageIDs(db *sql.DB, sessionID string, messageIndex int) (prev, next string) {
	_ = db.QueryRow(
		"SELECT message_id FROM messages WHERE session_id = ? AND message_index = ?",
		sessionID, messageIndex-1,
	).Scan(&prev)
	_ = db.QueryRow(
		"SELECT message_id FROM messages WHERE session_id = ? AND message_index = ?",
		sessionID, messageIndex+1,
	).Scan(&next)
	return
}

func normalizeFilters(cwd, remote, skill, role, timeCanonical string) string {
	var parts []string
	if cwd != "" {
		parts = append(parts, "cwd="+cwd)
	}
	if remote != "" {
		parts = append(parts, "remote="+remote)
	}
	if skill != "" {
		parts = append(parts, "skill="+skill)
	}
	if role != "" {
		parts = append(parts, "role="+role)
	}
	if timeCanonical != "" {
		parts = append(parts, timeCanonical)
	}
	return strings.Join(parts, ";")
}

func normalizePagination(offset, pageSize int) (int, int, error) {
	if offset < 0 {
		return 0, 0, errors.New("--offset must be >= 0")
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	return offset, pageSize, nil
}
