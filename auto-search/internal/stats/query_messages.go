package stats

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type messageBucketAgg struct {
	Key      string
	Count    int
	Sessions map[string]struct{}
	Messages map[string]struct{}
	Sample   messageSample
}

func queryMessageStats(req *normalizedRequest) (queryResult, error) {
	if req.GroupBy == "bash_command" {
		return queryMessageBashStats(req)
	}

	bucketExpr, err := normalizedBucketExpr(scopeMessages, req.GroupBy)
	if err != nil {
		return queryResult{}, err
	}

	matchedCTE, baseArgs := buildMessageMatchedCTE(req, bucketExpr)

	totalMatches, err := countFromMatched(req.DB, matchedCTE, baseArgs)
	if err != nil {
		return queryResult{}, fmt.Errorf("message stats total matches query: %w", err)
	}

	totalBucketsUnfiltered, err := countMessageBuckets(req.DB, matchedCTE, baseArgs, false, req.Measure, req.MinCount)
	if err != nil {
		return queryResult{}, fmt.Errorf("message stats total buckets query: %w", err)
	}

	totalBuckets, err := countMessageBuckets(req.DB, matchedCTE, baseArgs, true, req.Measure, req.MinCount)
	if err != nil {
		return queryResult{}, fmt.Errorf("message stats filtered buckets query: %w", err)
	}

	buckets, err := pageMessageBuckets(req.DB, matchedCTE, baseArgs, req)
	if err != nil {
		return queryResult{}, fmt.Errorf("message stats page query: %w", err)
	}
	if len(buckets) > 0 {
		if err := hydrateMessageSamples(req, matchedCTE, baseArgs, buckets); err != nil {
			return queryResult{}, fmt.Errorf("message stats samples query: %w", err)
		}
	}

	return queryResult{
		TotalMatches:           totalMatches,
		TotalBucketsUnfiltered: totalBucketsUnfiltered,
		TotalBuckets:           totalBuckets,
		Buckets:                buckets,
	}, nil
}

func queryMessageBashStats(req *normalizedRequest) (queryResult, error) {
	matchedCTE, baseArgs := buildMessageMatchedCTE(req, "m.bash_command")
	sqlText := matchedCTE + `
		SELECT bucket_key, message_id, session_id, timestamp, content_truncated, score
		FROM matched
	`
	rows, err := req.DB.Query(sqlText, baseArgs...)
	if err != nil {
		return queryResult{}, err
	}
	defer func() { _ = rows.Close() }()

	aggs := map[string]*messageBucketAgg{}
	totalMatches := 0

	for rows.Next() {
		var (
			rawKey   string
			message  string
			session  string
			ts       int64
			content  string
			scoreVal float64
		)
		if err := rows.Scan(&rawKey, &message, &session, &ts, &content, &scoreVal); err != nil {
			return queryResult{}, fmt.Errorf("scan message stats row: %w", err)
		}
		totalMatches++

		key := normalizeBashCommandFamily(rawKey)
		agg, ok := aggs[key]
		if !ok {
			agg = &messageBucketAgg{
				Key:      key,
				Sessions: map[string]struct{}{},
				Messages: map[string]struct{}{},
			}
			aggs[key] = agg
		}
		agg.Count++
		agg.Sessions[session] = struct{}{}
		agg.Messages[message] = struct{}{}

		candidate := messageSample{
			BucketKey: key,
			MessageID: message,
			SessionID: session,
			Content:   content,
			Timestamp: ts,
			Score:     scoreVal,
		}
		if betterMessageSample(&candidate, &agg.Sample, req.HasQuery) {
			agg.Sample = candidate
		}
	}
	if err := rows.Err(); err != nil {
		return queryResult{}, fmt.Errorf("iterate message stats rows: %w", err)
	}

	allBuckets := make([]Bucket, 0, len(aggs))
	for _, agg := range aggs {
		bucket := Bucket{
			Key:              normalizeBucketValue(agg.Key),
			Count:            agg.Count,
			DistinctSessions: len(agg.Sessions),
			DistinctMessages: len(agg.Messages),
			SampleMessageID:  agg.Sample.MessageID,
			SampleSessionID:  agg.Sample.SessionID,
		}
		bucket.SampleSnippet = snippetForQuery(agg.Sample.Content, req.Terms, req.HasQuery)
		allBuckets = append(allBuckets, bucket)
	}

	totalBucketsUnfiltered := len(allBuckets)
	filtered := filterBucketsByMinCount(allBuckets, req.Measure, req.MinCount)
	totalBuckets := len(filtered)
	sortBuckets(filtered, req.Measure)
	paged := applyOffsetLimit(filtered, req.Offset, req.PageSize)

	return queryResult{
		TotalMatches:           totalMatches,
		TotalBucketsUnfiltered: totalBucketsUnfiltered,
		TotalBuckets:           totalBuckets,
		Buckets:                paged,
	}, nil
}

func buildMessageMatchedCTE(req *normalizedRequest, bucketExpr string) (string, []any) {
	fromClause := "FROM messages m"
	where := []string{"1=1"}
	args := make([]any, 0, 8)

	scoreExpr := "0.0 AS score"
	if req.HasQuery {
		fromClause = "FROM messages_fts JOIN messages m ON m.doc_id = messages_fts.rowid"
		where = append(where, "messages_fts MATCH ?")
		args = append(args, req.FTS)
		scoreExpr = "bm25(messages_fts) AS score"
	}

	if req.CWD != "" {
		where = append(where, "m.workspace = ?")
		args = append(args, req.CWD)
	}
	if req.Remote != "" {
		where = append(where, "m.git_remote = ?")
		args = append(args, req.Remote)
	}
	if req.Skill != "" {
		where = append(where, "m.skill_name = ?")
		args = append(args, req.Skill)
	}
	if req.Role != "" {
		where = append(where, "m.role = ?")
		args = append(args, req.Role)
	}
	if len(req.Tools) > 0 {
		where = append(where, "m.tool_name IN ("+makePlaceholders(len(req.Tools))+")")
		for _, t := range req.Tools {
			args = append(args, t)
		}
	}
	switch req.Field {
	case "all":
	case "content":
		where = append(where, "m.tool_input = ''", "m.role != 'tool'")
	case "tool_input":
		where = append(where, "m.tool_input != ''")
	case "tool_output":
		where = append(where, "m.role = 'tool'")
	}
	if req.Time.StartMs != nil {
		where = append(where, "m.timestamp >= ?")
		args = append(args, *req.Time.StartMs)
	}
	if req.Time.EndMs != nil {
		where = append(where, "m.timestamp < ?")
		args = append(args, *req.Time.EndMs)
	}

	sqlText := fmt.Sprintf(`
		WITH matched AS (
			SELECT
				m.message_id,
				m.session_id,
				m.timestamp,
				m.content_truncated,
				%s AS bucket_key,
				%s
			%s
			WHERE %s
		)
	`, bucketExpr, scoreExpr, fromClause, strings.Join(where, " AND "))
	return sqlText, args
}

func countFromMatched(db *sql.DB, matchedCTE string, args []any) (int, error) {
	sqlText := matchedCTE + ` SELECT COUNT(*) FROM matched`
	var total int
	if err := db.QueryRow(sqlText, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func countMessageBuckets(db *sql.DB, matchedCTE string, baseArgs []any, withMin bool, measure string, minCount int) (int, error) {
	sqlText := matchedCTE + `
		SELECT COUNT(*)
		FROM (
			SELECT
				bucket_key,
				COUNT(*) AS count,
				COUNT(DISTINCT session_id) AS distinct_sessions,
				COUNT(DISTINCT message_id) AS distinct_messages
			FROM matched
			GROUP BY bucket_key
	`
	args := append([]any{}, baseArgs...)
	if withMin {
		sqlText += fmt.Sprintf(" HAVING %s >= ?", measure)
		args = append(args, minCount)
	}
	sqlText += ")"

	var total int
	if err := db.QueryRow(sqlText, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func pageMessageBuckets(db *sql.DB, matchedCTE string, baseArgs []any, req *normalizedRequest) ([]Bucket, error) {
	sqlText := matchedCTE + fmt.Sprintf(`
		SELECT
			bucket_key,
			COUNT(*) AS count,
			COUNT(DISTINCT session_id) AS distinct_sessions,
			COUNT(DISTINCT message_id) AS distinct_messages
		FROM matched
		GROUP BY bucket_key
		HAVING %s >= ?
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
			return nil, fmt.Errorf("scan bucket row: %w", err)
		}
		b.Key = normalizeBucketValue(b.Key)
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bucket rows: %w", err)
	}
	if buckets == nil {
		return []Bucket{}, nil
	}
	return buckets, nil
}

func hydrateMessageSamples(req *normalizedRequest, matchedCTE string, baseArgs []any, buckets []Bucket) error {
	keys := make([]string, 0, len(buckets))
	for _, b := range buckets {
		keys = append(keys, b.Key)
	}

	orderBy := "timestamp DESC, message_id ASC"
	if req.HasQuery {
		orderBy = "score ASC, timestamp DESC, message_id ASC"
	}

	placeholders := makePlaceholders(len(keys))
	sqlText := matchedCTE + fmt.Sprintf(`
		, ranked AS (
			SELECT
				bucket_key,
				message_id,
				session_id,
				content_truncated,
				timestamp,
				score,
				ROW_NUMBER() OVER (PARTITION BY bucket_key ORDER BY %s) AS rn
			FROM matched
			WHERE bucket_key IN (%s)
		)
		SELECT bucket_key, message_id, session_id, content_truncated, timestamp, score
		FROM ranked
		WHERE rn = 1
	`, orderBy, placeholders)

	args := append([]any{}, baseArgs...)
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := req.DB.Query(sqlText, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	sampleByKey := map[string]messageSample{}
	for rows.Next() {
		var s messageSample
		if err := rows.Scan(&s.BucketKey, &s.MessageID, &s.SessionID, &s.Content, &s.Timestamp, &s.Score); err != nil {
			return fmt.Errorf("scan sample row: %w", err)
		}
		sampleByKey[s.BucketKey] = s
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sample rows: %w", err)
	}

	for i := range buckets {
		s, ok := sampleByKey[buckets[i].Key]
		if !ok {
			continue
		}
		buckets[i].SampleMessageID = s.MessageID
		buckets[i].SampleSessionID = s.SessionID
		buckets[i].SampleSnippet = snippetForQuery(s.Content, req.Terms, req.HasQuery)
	}
	return nil
}

func makePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "?"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func filterBucketsByMinCount(buckets []Bucket, measure string, minCount int) []Bucket {
	if minCount <= 0 {
		out := make([]Bucket, len(buckets))
		copy(out, buckets)
		return out
	}
	out := make([]Bucket, 0, len(buckets))
	for i := range buckets {
		if bucketMeasureValue(&buckets[i], measure) >= minCount {
			out = append(out, buckets[i])
		}
	}
	return out
}

func sortBuckets(buckets []Bucket, measure string) {
	sort.SliceStable(buckets, func(i, j int) bool {
		mi := bucketMeasureValue(&buckets[i], measure)
		mj := bucketMeasureValue(&buckets[j], measure)
		if mi != mj {
			return mi > mj
		}
		return buckets[i].Key < buckets[j].Key
	})
}

func applyOffsetLimit(buckets []Bucket, offset, limit int) []Bucket {
	if offset >= len(buckets) {
		return []Bucket{}
	}
	end := min(offset+limit, len(buckets))
	return buckets[offset:end]
}

func bucketMeasureValue(b *Bucket, measure string) int {
	switch measure {
	case measureDistinctSessions:
		return b.DistinctSessions
	case measureDistinctMessages:
		return b.DistinctMessages
	default:
		return b.Count
	}
}
