package stats

import (
	"fmt"
	"time"
)

// Run executes a grouped stats request over the indexed dataset.
func Run(req *Request) (*Response, error) {
	start := time.Now()

	normalized, err := normalizeAndValidate(req)
	if err != nil {
		return nil, err
	}

	var result queryResult
	switch normalized.Scope {
	case scopeMessages:
		result, err = queryMessageStats(normalized)
	case scopeSessions:
		result, err = querySessionStats(normalized)
	default:
		err = fmt.Errorf("unsupported scope: %s", normalized.Scope)
	}
	if err != nil {
		return nil, err
	}

	returned := len(result.Buckets)
	hasMore := normalized.Offset+returned < result.TotalBuckets
	var nextOffset *int
	if hasMore {
		next := normalized.Offset + returned
		nextOffset = &next
	}

	elapsed := time.Since(start).Milliseconds()
	resp := &Response{
		Meta: Meta{
			RequestID:              normalized.RequestID,
			Scope:                  normalized.Scope,
			Query:                  normalized.Query,
			GroupBy:                normalized.GroupBy,
			Measure:                normalized.Measure,
			ElapsedMs:              elapsed,
			TotalMatches:           result.TotalMatches,
			TotalBucketsUnfiltered: result.TotalBucketsUnfiltered,
			TotalBuckets:           result.TotalBuckets,
			ReturnedBuckets:        returned,
			PageSize:               normalized.PageSize,
			Offset:                 normalized.Offset,
			HasMore:                hasMore,
			NextOffset:             nextOffset,
			IsCapped:               false,
		},
		Buckets: result.Buckets,
	}
	if resp.Buckets == nil {
		resp.Buckets = []Bucket{}
	}
	return resp, nil
}
