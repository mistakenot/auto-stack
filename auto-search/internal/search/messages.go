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
	RequestID string
	Highlight bool
	Now       time.Time
}

const minHitsForFallback = 3

// SearchMessages performs a BM25 message-scope search.
func SearchMessages(opts *MessageSearchOpts) (*MessageSearchResult, error) {
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
	terms := ExtractTerms(ast)
	filters := normalizeFilters(opts.CWD, opts.Remote, opts.Skill, timeFilter.Canonical)

	hits, err := execMessageSearch(opts.DB, fts, opts.CWD, opts.Remote, opts.Skill, timeFilter, terms, opts.Highlight, opts.Query, filters)
	if err != nil {
		return nil, err
	}

	wildcard := false
	if len(hits) < minHitsForFallback {
		fallbackAST := query.PrefixFallback(ast)
		fallbackFTS := query.CompileFTS(fallbackAST)
		fallbackHits, err := execMessageSearch(opts.DB, fallbackFTS, opts.CWD, opts.Remote, opts.Skill, timeFilter, terms, opts.Highlight, opts.Query, filters)
		if err == nil && len(fallbackHits) > len(hits) {
			hits = fallbackHits
			wildcard = true
		}
	}

	elapsed := time.Since(start).Milliseconds()
	return &MessageSearchResult{
		Meta: Meta{
			RequestID:        opts.RequestID,
			Scope:            "messages",
			Mode:             "bm25",
			Query:            opts.Query,
			ElapsedMs:        elapsed,
			TotalHits:        len(hits),
			WildcardFallback: wildcard,
		},
		Hits: hits,
	}, nil
}

func execMessageSearch(db *sql.DB, fts, cwd, remote, skill string, timeFilter TimeFilter, terms []string, highlight bool, rawQuery, filters string) ([]MessageHit, error) {
	q := `
		SELECT m.message_id, m.session_id, m.role, m.content_truncated,
		       m.message_index, bm25(messages_fts) AS score
		FROM messages_fts
		JOIN messages m ON m.doc_id = messages_fts.rowid
		WHERE messages_fts MATCH ?
	`
	args := []any{fts}

	if cwd != "" {
		q += " AND m.workspace = ?"
		args = append(args, cwd)
	}
	if remote != "" {
		q += " AND m.git_remote = ?"
		args = append(args, remote)
	}
	if skill != "" {
		q += " AND m.skill_name = ?"
		args = append(args, skill)
	}
	if timeFilter.StartMs != nil {
		q += " AND m.timestamp >= ?"
		args = append(args, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		q += " AND m.timestamp < ?"
		args = append(args, *timeFilter.EndMs)
	}

	q += " ORDER BY score LIMIT 50"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("message search query: %w", err)
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
			return nil, fmt.Errorf("scan message hit: %w", err)
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
		return nil, fmt.Errorf("iterate message hits: %w", err)
	}
	if hits == nil {
		hits = []MessageHit{}
	}
	return hits, nil
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

func normalizeFilters(cwd, remote, skill, timeCanonical string) string {
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
	if timeCanonical != "" {
		parts = append(parts, timeCanonical)
	}
	return strings.Join(parts, ";")
}
