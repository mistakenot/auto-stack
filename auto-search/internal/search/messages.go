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
//
// ToolName, DurationMs, and Interrupted are surfaced so callers don't need a
// follow-up `message describe` to see the basic timing of a tool call. They
// are omitted from JSON when zero/empty so non-tool hits stay clean.
type MessageHit struct {
	ID                string  `json:"id"`
	SessionID         string  `json:"sessionId"`
	MessageID         string  `json:"messageId"`
	MessageType       string  `json:"messageType"`
	ToolName          string  `json:"toolName,omitempty"`
	DurationMs        int64   `json:"durationMs,omitempty"`
	Interrupted       bool    `json:"interrupted,omitempty"`
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
	TotalMatches     int    `json:"total_matches"`
	DistinctSessions int    `json:"distinct_sessions"`
	DistinctMessages int    `json:"distinct_messages"`
	ReturnedHits     int    `json:"returned_hits"`
	PageSize         int    `json:"page_size"`
	Offset           int    `json:"offset"`
	HasMore          bool   `json:"has_more"`
	NextOffset       *int   `json:"next_offset,omitempty"`
	IsCapped         bool   `json:"is_capped"`
	WildcardFallback bool   `json:"wildcard_fallback"`
}

type matchStats struct {
	TotalMatches     int
	DistinctSessions int
	DistinctMessages int
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
	Field     string
	ToolName  string
	SessionID string
	// MinToolDurationMs filters to messages with duration_ms >= this value.
	// Implies a tool_use/tool_result row because non-tool rows always have
	// duration_ms = 0. Combined with other filters with AND.
	MinToolDurationMs *int64
	// OnlyInterrupted, when true, restricts results to messages where
	// interrupted=true (Claude's per-tool-call cancel/stuck flag).
	OnlyInterrupted bool
	Offset          int
	PageSize        int
	RequestID       string
	Highlight       bool
	Now             time.Time
}

const (
	minHitsForFallback = 3
	defaultPageSize    = 20
	maxPageSize        = 1000
)

const (
	searchFieldAll        = "all"
	searchFieldContent    = "content"
	searchFieldToolInput  = "tool_input"
	searchFieldToolOutput = "tool_output"
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

	hasStructuredFilter := opts.ToolName != "" || opts.SessionID != "" ||
		opts.MinToolDurationMs != nil || opts.OnlyInterrupted

	queryText := strings.TrimSpace(opts.Query)
	emptyQuery := queryText == ""
	if emptyQuery && !hasStructuredFilter {
		return nil, errors.New("query must be non-empty unless at least one structured filter (--tool-name / --session-id / --min-tool-duration / --interrupted) is set")
	}

	filters := normalizeFilters(opts.CWD, opts.Remote, opts.Skill, role, field, timeFilter.Canonical)

	var hits []MessageHit
	var stats matchStats
	wildcard := false

	if emptyQuery {
		// Structured-only path: skip FTS entirely and run a direct scan of
		// the messages table. This lets callers ask "show me every
		// interrupted call" or "every Bash call >60s in this session"
		// without inventing a dummy FTS query.
		hits, stats, err = execMessageSearchNoFTS(opts.DB, opts.CWD, opts.Remote, opts.Skill, role, field,
			opts.ToolName, opts.SessionID, opts.MinToolDurationMs, opts.OnlyInterrupted,
			timeFilter, offset, pageSize)
		if err != nil {
			return nil, err
		}
	} else {
		ast, err := query.Parse(opts.Query)
		if err != nil {
			return nil, fmt.Errorf("parse query: %w", err)
		}

		fts := query.CompileFTS(ast)
		terms := ExtractTerms(ast)

		exec := func(ftsExpr string) ([]MessageHit, matchStats, error) {
			return execMessageSearch(opts.DB, ftsExpr, opts.CWD, opts.Remote, opts.Skill, role, field,
				opts.ToolName, opts.SessionID, opts.MinToolDurationMs, opts.OnlyInterrupted,
				timeFilter, terms, opts.Highlight, opts.Query, filters, offset, pageSize)
		}

		hits, stats, err = exec(fts)
		if err != nil {
			return nil, err
		}

		if stats.TotalMatches < minHitsForFallback {
			fallbackAST := query.PrefixFallback(ast)
			fallbackFTS := query.CompileFTS(fallbackAST)
			fallbackHits, fallbackStats, err := exec(fallbackFTS)
			if err == nil && fallbackStats.TotalMatches > stats.TotalMatches {
				hits = fallbackHits
				stats = fallbackStats
				wildcard = true
			}
		}
	}

	returnedHits := len(hits)
	hasMore := offset+returnedHits < stats.TotalMatches
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
			TotalHits:        stats.TotalMatches,
			TotalMatches:     stats.TotalMatches,
			DistinctSessions: stats.DistinctSessions,
			DistinctMessages: stats.DistinctMessages,
			ReturnedHits:     returnedHits,
			PageSize:         pageSize,
			Offset:           offset,
			HasMore:          hasMore,
			NextOffset:       nextOffset,
			IsCapped:         false,
			WildcardFallback: wildcard,
		},
		Hits: hits,
	}, nil
}

func execMessageSearch(db *sql.DB, fts, cwd, remote, skill, role, field, toolName, sessionID string, minToolDurationMs *int64, onlyInterrupted bool, timeFilter TimeFilter, terms []string, highlight bool, rawQuery, filters string, offset, pageSize int) ([]MessageHit, matchStats, error) {
	zeroStats := matchStats{}

	var preFilterConds []string
	var preFilterArgs []any

	if cwd != "" {
		preFilterConds = append(preFilterConds, "workspace = ?")
		preFilterArgs = append(preFilterArgs, cwd)
	}
	if remote != "" {
		preFilterConds = append(preFilterConds, "git_remote = ?")
		preFilterArgs = append(preFilterArgs, remote)
	}
	if skill != "" {
		preFilterConds = append(preFilterConds, "skill_name = ?")
		preFilterArgs = append(preFilterArgs, skill)
	}
	if role != "" {
		preFilterConds = append(preFilterConds, "role = ?")
		preFilterArgs = append(preFilterArgs, role)
	}
	if toolName != "" {
		preFilterConds = append(preFilterConds, "tool_name = ?")
		preFilterArgs = append(preFilterArgs, toolName)
	}
	if sessionID != "" {
		preFilterConds = append(preFilterConds, "session_id = ?")
		preFilterArgs = append(preFilterArgs, sessionID)
	}
	if minToolDurationMs != nil {
		// Hits the idx_messages_duration_ms index (added in PR 2).
		preFilterConds = append(preFilterConds, "duration_ms >= ?")
		preFilterArgs = append(preFilterArgs, *minToolDurationMs)
	}
	if onlyInterrupted {
		// Hits the idx_messages_interrupted index (added in PR 2).
		preFilterConds = append(preFilterConds, "interrupted = 1")
	}
	switch field {
	case searchFieldAll:
		// no-op
	case searchFieldContent:
		preFilterConds = append(preFilterConds, "tool_input = ''", "role != 'tool'")
	case searchFieldToolInput:
		preFilterConds = append(preFilterConds, "tool_input != ''")
	case searchFieldToolOutput:
		preFilterConds = append(preFilterConds, "role = 'tool'")
	default:
		return nil, zeroStats, fmt.Errorf("invalid --field value %q (use all, content, tool_input, tool_output)", field)
	}
	if timeFilter.StartMs != nil {
		preFilterConds = append(preFilterConds, "timestamp >= ?")
		preFilterArgs = append(preFilterArgs, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		preFilterConds = append(preFilterConds, "timestamp < ?")
		preFilterArgs = append(preFilterArgs, *timeFilter.EndMs)
	}

	var args []any

	baseQuery := `
		FROM messages_fts
		JOIN messages m ON m.doc_id = messages_fts.rowid
		WHERE messages_fts MATCH ?
	`
	args = append(args, fts)
	if len(preFilterConds) > 0 {
		var buf strings.Builder
		for _, cond := range preFilterConds {
			buf.WriteString(" AND +m.")
			buf.WriteString(cond)
		}
		baseQuery += buf.String()
	}
	args = append(args, preFilterArgs...)

	countQuery := "SELECT COUNT(*), COUNT(DISTINCT m.session_id), COUNT(DISTINCT m.message_id) " + baseQuery
	var totalHits, distinctSessions, distinctMessages int
	if err := db.QueryRow(countQuery, args...).Scan(&totalHits, &distinctSessions, &distinctMessages); err != nil {
		return nil, zeroStats, fmt.Errorf("message search count query: %w", err)
	}

	q := `
		SELECT m.message_id, m.session_id, m.role, m.content_truncated,
		       m.message_index, m.tool_name, m.duration_ms, m.interrupted,
		       bm25(messages_fts) AS score
	` + baseQuery + `
		ORDER BY score, m.message_id
		LIMIT ? OFFSET ?
	`
	hitArgs := append(append([]any{}, args...), pageSize, offset)

	rows, err := db.Query(q, hitArgs...)
	if err != nil {
		return nil, zeroStats, fmt.Errorf("message search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type scannedHit struct {
		messageID, sessionID, role string
		contentTruncated           string
		messageIndex               int
		toolName                   string
		durationMs                 int64
		interrupted                bool
		score                      float64
	}
	var scanned []scannedHit
	for rows.Next() {
		var h scannedHit
		var interruptedInt int
		if err := rows.Scan(&h.messageID, &h.sessionID, &h.role, &h.contentTruncated, &h.messageIndex, &h.toolName, &h.durationMs, &interruptedInt, &h.score); err != nil {
			return nil, zeroStats, fmt.Errorf("scan message hit: %w", err)
		}
		h.interrupted = interruptedInt != 0
		scanned = append(scanned, h)
	}
	if err := rows.Err(); err != nil {
		return nil, zeroStats, fmt.Errorf("iterate message hits: %w", err)
	}

	lookups := make([]neighborLookup, len(scanned))
	for i, h := range scanned {
		lookups[i] = neighborLookup{h.sessionID, h.messageIndex}
	}
	neighbors := batchNeighborMessageIDs(db, lookups)
	hits := make([]MessageHit, 0, len(scanned))
	for i, h := range scanned {
		snippet, startIdx, endIdx := Snippet(h.contentTruncated, terms, highlight)
		hits = append(hits, MessageHit{
			ID:                HitID("messages", "bm25", rawQuery, filters, h.messageID),
			SessionID:         h.sessionID,
			MessageID:         h.messageID,
			MessageType:       h.role,
			ToolName:          h.toolName,
			DurationMs:        h.durationMs,
			Interrupted:       h.interrupted,
			Score:             h.score,
			SnippetStartIndex: startIdx,
			SnippetEndIndex:   endIdx,
			Snippet:           snippet,
			PreviousMessageID: neighbors[i].prev,
			NextMessageID:     neighbors[i].next,
		})
	}
	return hits, matchStats{
		TotalMatches:     totalHits,
		DistinctSessions: distinctSessions,
		DistinctMessages: distinctMessages,
	}, nil
}

// execMessageSearchNoFTS runs the message search against the base messages
// table when the user supplied no FTS query — they're filtering by
// structured columns only (tool_name, session_id, duration_ms, interrupted,
// time window, workspace, role, etc.). Results are ordered by descending
// duration_ms when --min-tool-duration is set, otherwise by descending
// timestamp ("most recent first"), so the most-interesting rows surface
// first in --text output. No snippet highlighting is meaningful here
// (there's no matched term), so snippet is just a truncated content view.
func execMessageSearchNoFTS(db *sql.DB, cwd, remote, skill, role, field, toolName, sessionID string, minToolDurationMs *int64, onlyInterrupted bool, timeFilter TimeFilter, offset, pageSize int) ([]MessageHit, matchStats, error) {
	zeroStats := matchStats{}

	var conds []string
	var args []any

	if cwd != "" {
		conds = append(conds, "m.workspace = ?")
		args = append(args, cwd)
	}
	if remote != "" {
		conds = append(conds, "m.git_remote = ?")
		args = append(args, remote)
	}
	if skill != "" {
		conds = append(conds, "m.skill_name = ?")
		args = append(args, skill)
	}
	if role != "" {
		conds = append(conds, "m.role = ?")
		args = append(args, role)
	}
	if toolName != "" {
		conds = append(conds, "m.tool_name = ?")
		args = append(args, toolName)
	}
	if sessionID != "" {
		conds = append(conds, "m.session_id = ?")
		args = append(args, sessionID)
	}
	if minToolDurationMs != nil {
		conds = append(conds, "m.duration_ms >= ?")
		args = append(args, *minToolDurationMs)
	}
	if onlyInterrupted {
		conds = append(conds, "m.interrupted = 1")
	}
	switch field {
	case searchFieldAll, "":
		// no-op
	case searchFieldContent:
		conds = append(conds, "m.tool_input = ''", "m.role != 'tool'")
	case searchFieldToolInput:
		conds = append(conds, "m.tool_input != ''")
	case searchFieldToolOutput:
		conds = append(conds, "m.role = 'tool'")
	default:
		return nil, zeroStats, fmt.Errorf("invalid --field value %q (use all, content, tool_input, tool_output)", field)
	}
	if timeFilter.StartMs != nil {
		conds = append(conds, "m.timestamp >= ?")
		args = append(args, *timeFilter.StartMs)
	}
	if timeFilter.EndMs != nil {
		conds = append(conds, "m.timestamp < ?")
		args = append(args, *timeFilter.EndMs)
	}

	whereClause := ""
	if len(conds) > 0 {
		whereClause = "WHERE " + strings.Join(conds, " AND ")
	}

	var totalHits int
	countQuery := "SELECT COUNT(*) FROM messages m " + whereClause
	if err := db.QueryRow(countQuery, args...).Scan(&totalHits); err != nil {
		return nil, zeroStats, fmt.Errorf("structured count query: %w", err)
	}

	orderBy := "m.timestamp DESC, m.message_id"
	if minToolDurationMs != nil {
		// User asked "show me the slowest" — order accordingly.
		orderBy = "m.duration_ms DESC, m.timestamp DESC"
	}

	q := `
		SELECT m.message_id, m.session_id, m.role, m.content_truncated,
		       m.message_index, m.tool_name, m.duration_ms, m.interrupted
		FROM messages m
	` + whereClause + `
		ORDER BY ` + orderBy + `
		LIMIT ? OFFSET ?
	`
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	rows, err := db.Query(q, queryArgs...)
	if err != nil {
		return nil, zeroStats, fmt.Errorf("structured message query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type scanned struct {
		messageID, sessionID, role, contentTruncated, toolName string
		messageIndex                                           int
		durationMs                                             int64
		interrupted                                            bool
	}
	var scannedRows []scanned
	distinctSessions := map[string]struct{}{}
	distinctMessages := map[string]struct{}{}
	for rows.Next() {
		var s scanned
		var interruptedInt int
		if err := rows.Scan(&s.messageID, &s.sessionID, &s.role, &s.contentTruncated, &s.messageIndex, &s.toolName, &s.durationMs, &interruptedInt); err != nil {
			return nil, zeroStats, fmt.Errorf("scan structured hit: %w", err)
		}
		s.interrupted = interruptedInt != 0
		scannedRows = append(scannedRows, s)
		distinctSessions[s.sessionID] = struct{}{}
		distinctMessages[s.messageID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, zeroStats, fmt.Errorf("iterate structured hits: %w", err)
	}

	lookups := make([]neighborLookup, len(scannedRows))
	for i, s := range scannedRows {
		lookups[i] = neighborLookup{s.sessionID, s.messageIndex}
	}
	neighbors := batchNeighborMessageIDs(db, lookups)

	hits := make([]MessageHit, 0, len(scannedRows))
	for i, s := range scannedRows {
		// No query terms → no highlight; just return a trimmed snippet so
		// the row is identifiable in --text output without an extra
		// `message get` round-trip.
		snippet := TruncateAtRune(strings.TrimSpace(s.contentTruncated), 320)
		hits = append(hits, MessageHit{
			ID:                HitID("messages", "structured", "", "", s.messageID),
			SessionID:         s.sessionID,
			MessageID:         s.messageID,
			MessageType:       s.role,
			ToolName:          s.toolName,
			DurationMs:        s.durationMs,
			Interrupted:       s.interrupted,
			Score:             0,
			Snippet:           snippet,
			PreviousMessageID: neighbors[i].prev,
			NextMessageID:     neighbors[i].next,
		})
	}

	return hits, matchStats{
		TotalMatches:     totalHits,
		DistinctSessions: len(distinctSessions),
		DistinctMessages: len(distinctMessages),
	}, nil
}

type neighborLookup struct {
	sessionID    string
	messageIndex int
}

type neighborPair struct {
	prev, next string
}

func batchNeighborMessageIDs(db *sql.DB, hits []neighborLookup) []neighborPair {
	result := make([]neighborPair, len(hits))
	if len(hits) == 0 {
		return result
	}

	type lookupKey struct {
		sessionID    string
		messageIndex int
	}

	var entries []lookupKey
	seen := make(map[lookupKey]struct{})
	for _, h := range hits {
		prev := lookupKey{h.sessionID, h.messageIndex - 1}
		next := lookupKey{h.sessionID, h.messageIndex + 1}
		if _, ok := seen[prev]; !ok {
			entries = append(entries, prev)
			seen[prev] = struct{}{}
		}
		if _, ok := seen[next]; !ok {
			entries = append(entries, next)
			seen[next] = struct{}{}
		}
	}

	valueParts := make([]string, len(entries))
	args := make([]any, 0, len(entries)*2)
	for i, e := range entries {
		valueParts[i] = "(?, ?)"
		args = append(args, e.sessionID, e.messageIndex)
	}

	q := `WITH lookups(sid, midx) AS (VALUES ` + strings.Join(valueParts, ", ") + `)
		SELECT l.sid, l.midx, m.message_id
		FROM lookups l
		JOIN messages m ON m.session_id = l.sid AND m.message_index = l.midx`

	resolved := make(map[lookupKey]string)
	rows, err := db.Query(q, args...)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var sid string
			var midx int
			var mid string
			if err := rows.Scan(&sid, &midx, &mid); err != nil {
				break
			}
			resolved[lookupKey{sid, midx}] = mid
		}
		_ = rows.Err()
	}

	for i, h := range hits {
		result[i] = neighborPair{
			prev: resolved[lookupKey{h.sessionID, h.messageIndex - 1}],
			next: resolved[lookupKey{h.sessionID, h.messageIndex + 1}],
		}
	}
	return result
}

func normalizeFilters(cwd, remote, skill, role, field, timeCanonical string) string {
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
	if field != "" && field != searchFieldAll {
		parts = append(parts, "field="+field)
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
	if pageSize < 0 {
		return 0, 0, fmt.Errorf("--limit must be >= 0 (got %d)", pageSize)
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return 0, 0, fmt.Errorf("--limit must be <= %d (got %d)", maxPageSize, pageSize)
	}
	return offset, pageSize, nil
}

func normalizeRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	if normalized == "" {
		return "", nil
	}
	switch normalized {
	case "user", "assistant", "tool":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid --role value %q (use user, assistant, or tool)", role)
	}
}

func normalizeField(field string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(field))
	if normalized == "" {
		return searchFieldAll, nil
	}
	switch normalized {
	case searchFieldAll, searchFieldContent, searchFieldToolInput, searchFieldToolOutput:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid --field value %q (use all, content, tool_input, tool_output)", field)
	}
}
