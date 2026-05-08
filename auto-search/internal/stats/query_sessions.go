package stats

import (
	"database/sql"
	"fmt"
	"strings"
)

func querySessionStats(req *normalizedRequest) (queryResult, error) {
	bucketExpr, err := normalizedBucketExpr(scopeSessions, req.GroupBy)
	if err != nil {
		return queryResult{}, err
	}

	matchedCTE, baseArgs := buildSessionMatchedCTE(req, bucketExpr)
	groupedCTE := buildSessionGroupedCTE(matchedCTE)

	totalMatches, err := countFromMatched(req.DB, matchedCTE, baseArgs)
	if err != nil {
		return queryResult{}, fmt.Errorf("session stats total matches query: %w", err)
	}

	totalBucketsUnfiltered, err := countSessionBuckets(req.DB, groupedCTE, baseArgs, false, req.Measure, req.MinCount)
	if err != nil {
		return queryResult{}, fmt.Errorf("session stats total buckets query: %w", err)
	}

	totalBuckets, err := countSessionBuckets(req.DB, groupedCTE, baseArgs, true, req.Measure, req.MinCount)
	if err != nil {
		return queryResult{}, fmt.Errorf("session stats filtered buckets query: %w", err)
	}

	buckets, err := pageSessionBuckets(req.DB, groupedCTE, baseArgs, req)
	if err != nil {
		return queryResult{}, fmt.Errorf("session stats page query: %w", err)
	}
	if len(buckets) > 0 {
		if err := hydrateSessionSamples(req, matchedCTE, baseArgs, buckets); err != nil {
			return queryResult{}, fmt.Errorf("session stats samples query: %w", err)
		}
	}

	return queryResult{
		TotalMatches:           totalMatches,
		TotalBucketsUnfiltered: totalBucketsUnfiltered,
		TotalBuckets:           totalBuckets,
		Buckets:                buckets,
	}, nil
}

func buildSessionMatchedCTE(req *normalizedRequest, bucketExpr string) (string, []any) {
	fromClause := "FROM sessions s"
	where := []string{"1=1"}
	args := make([]any, 0, 8)

	scoreExpr := "0.0 AS score"
	if req.HasQuery {
		fromClause = "FROM sessions_fts JOIN sessions s ON s.doc_id = sessions_fts.rowid"
		where = append(where, "sessions_fts MATCH ?")
		args = append(args, req.FTS)
		scoreExpr = "bm25(sessions_fts) AS score"
	}

	if req.CWD != "" {
		where = append(where, "s.workspace = ?")
		args = append(args, req.CWD)
	}
	if req.Remote != "" {
		where = append(where, "s.git_remote = ?")
		args = append(args, req.Remote)
	}
	if req.SessionID != "" {
		where = append(where, "s.session_id = ?")
		args = append(args, req.SessionID)
	}
	if req.Skill != "" {
		where = append(where, "s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE skill_name = ?)")
		args = append(args, req.Skill)
	}
	if req.Role != "" {
		where = append(where, "s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE role = ?)")
		args = append(args, req.Role)
	}
	switch req.Field {
	case "all":
	case "content":
		where = append(where, "s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE tool_input = '' AND role != 'tool')")
	case "tool_input":
		where = append(where, "s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE tool_input != '')")
	case "tool_output":
		where = append(where, "s.session_id IN (SELECT DISTINCT session_id FROM messages WHERE role = 'tool')")
	}
	if req.Time.StartMs != nil {
		where = append(where, "s.first_message_at >= ?")
		args = append(args, *req.Time.StartMs)
	}
	if req.Time.EndMs != nil {
		where = append(where, "s.first_message_at < ?")
		args = append(args, *req.Time.EndMs)
	}

	sqlText := fmt.Sprintf(`
		WITH matched AS (
			SELECT
				s.session_id,
				s.first_message_at,
				s.transcript_truncated,
				%s AS bucket_key,
				%s
			%s
			WHERE %s
		)
	`, bucketExpr, scoreExpr, fromClause, strings.Join(where, " AND "))
	return sqlText, args
}

func buildSessionGroupedCTE(matchedCTE string) string {
	return matchedCTE + `
		, grouped AS (
			SELECT
				bucket_key,
				COUNT(*) AS count,
				COUNT(DISTINCT session_id) AS distinct_sessions
			FROM matched
			GROUP BY bucket_key
		),
		grouped_with_messages AS (
			SELECT
				g.bucket_key,
				g.count,
				g.distinct_sessions,
				(
					SELECT COUNT(DISTINCT m.message_id)
					FROM messages m
					JOIN matched ms ON ms.session_id = m.session_id
					WHERE ms.bucket_key = g.bucket_key
				) AS distinct_messages
			FROM grouped g
		)
	`
}

func countSessionBuckets(db *sql.DB, groupedCTE string, baseArgs []any, withMin bool, measure string, minCount int) (int, error) {
	sqlText := groupedCTE + ` SELECT COUNT(*) FROM grouped_with_messages`
	args := append([]any{}, baseArgs...)
	if withMin {
		sqlText += fmt.Sprintf(" WHERE %s >= ?", measure)
		args = append(args, minCount)
	}

	var total int
	if err := db.QueryRow(sqlText, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func pageSessionBuckets(db *sql.DB, groupedCTE string, baseArgs []any, req *normalizedRequest) ([]Bucket, error) {
	sqlText := groupedCTE + fmt.Sprintf(`
		SELECT bucket_key, count, distinct_sessions, distinct_messages
		FROM grouped_with_messages
		WHERE %s >= ?
		ORDER BY %s DESC, bucket_key ASC
		LIMIT ? OFFSET ?
	`, req.Measure, req.Measure)

	args := append([]any{}, baseArgs...)
	args = append(args, req.MinCount, req.PageSize, req.Offset)
	rows, err := db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Key, &b.Count, &b.DistinctSessions, &b.DistinctMessages); err != nil {
			return nil, fmt.Errorf("scan session bucket row: %w", err)
		}
		b.Key = normalizeBucketValue(b.Key)
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session bucket rows: %w", err)
	}
	if buckets == nil {
		return []Bucket{}, nil
	}
	return buckets, nil
}

func hydrateSessionSamples(req *normalizedRequest, matchedCTE string, baseArgs []any, buckets []Bucket) error {
	keys := make([]string, 0, len(buckets))
	for _, b := range buckets {
		keys = append(keys, b.Key)
	}

	orderBy := "first_message_at DESC, session_id ASC"
	if req.HasQuery {
		orderBy = "score ASC, first_message_at DESC, session_id ASC"
	}
	placeholders := makePlaceholders(len(keys))

	sampleSQL := matchedCTE + fmt.Sprintf(`
		, ranked AS (
			SELECT
				bucket_key,
				session_id,
				transcript_truncated,
				first_message_at,
				score,
				ROW_NUMBER() OVER (PARTITION BY bucket_key ORDER BY %s) AS rn
			FROM matched
			WHERE bucket_key IN (%s)
		)
		SELECT bucket_key, session_id, transcript_truncated, first_message_at, score
		FROM ranked
		WHERE rn = 1
	`, orderBy, placeholders)

	args := append([]any{}, baseArgs...)
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := req.DB.Query(sampleSQL, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	sampleByKey := map[string]sessionSample{}
	sessions := make([]string, 0, len(keys))
	for rows.Next() {
		var s sessionSample
		if err := rows.Scan(&s.BucketKey, &s.SessionID, &s.Transcript, &s.FirstMessageAt, &s.Score); err != nil {
			return fmt.Errorf("scan session sample row: %w", err)
		}
		sampleByKey[s.BucketKey] = s
		sessions = append(sessions, s.SessionID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate session sample rows: %w", err)
	}

	messageBySession, err := newestMessageBySession(req.DB, sessions)
	if err != nil {
		return err
	}

	for i := range buckets {
		s, ok := sampleByKey[buckets[i].Key]
		if !ok {
			continue
		}
		buckets[i].SampleSessionID = s.SessionID
		buckets[i].SampleMessageID = messageBySession[s.SessionID]
		buckets[i].SampleSnippet = snippetForQuery(s.Transcript, req.Terms, req.HasQuery)
	}
	return nil
}

func newestMessageBySession(db *sql.DB, sessionIDs []string) (map[string]string, error) {
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}
	placeholders := makePlaceholders(len(sessionIDs))
	sqlText := fmt.Sprintf(`
		WITH ranked AS (
			SELECT
				session_id,
				message_id,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY timestamp DESC, message_id ASC) AS rn
			FROM messages
			WHERE session_id IN (%s)
		)
		SELECT session_id, message_id
		FROM ranked
		WHERE rn = 1
	`, placeholders)

	args := make([]any, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		args = append(args, id)
	}
	rows, err := db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var sid string
		var mid string
		if err := rows.Scan(&sid, &mid); err != nil {
			return nil, fmt.Errorf("scan ranked message row: %w", err)
		}
		out[sid] = mid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ranked message rows: %w", err)
	}
	return out, nil
}
