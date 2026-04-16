package stats

import (
	"strings"

	"github.com/mistakenot/auto-search/internal/search"
)

type messageSample struct {
	BucketKey string
	MessageID string
	SessionID string
	Content   string
	Timestamp int64
	Score     float64
}

type sessionSample struct {
	BucketKey       string
	SessionID       string
	Transcript      string
	FirstMessageAt  int64
	Score           float64
	SampleMessageID string
}

func betterMessageSample(candidate, current *messageSample, hasQuery bool) bool {
	if current.MessageID == "" {
		return true
	}
	if hasQuery {
		if candidate.Score < current.Score {
			return true
		}
		if candidate.Score > current.Score {
			return false
		}
	}
	if candidate.Timestamp > current.Timestamp {
		return true
	}
	if candidate.Timestamp < current.Timestamp {
		return false
	}
	return candidate.MessageID < current.MessageID
}

func snippetForQuery(text string, terms []string, hasQuery bool) string {
	if !hasQuery || len(terms) == 0 || strings.TrimSpace(text) == "" {
		return ""
	}
	snippet, _, _ := search.Snippet(text, terms, false)
	snippet = strings.TrimSpace(snippet)
	const maxLen = 160
	if len(snippet) <= maxLen {
		return snippet
	}
	return strings.TrimSpace(snippet[:maxLen-3]) + "..."
}
