package stats

import (
	"database/sql"
	"time"

	"github.com/mistakenot/auto-search/internal/search"
)

const (
	scopeMessages = "messages"
	scopeSessions = "sessions"

	measureCount            = "count"
	measureDistinctSessions = "distinct_sessions"
	measureDistinctMessages = "distinct_messages"

	defaultPageSize = 20
	emptyBucketKey  = "(none)"
)

var messageGroupKeys = []string{
	"session_id",
	"role",
	"tool_name",
	"skill_name",
	"tool_file_path",
	"bash_command",
	"workspace",
	"git_remote",
	"model",
	"day",
	"week",
}

var sessionGroupKeys = []string{
	"session_id",
	"workspace",
	"git_remote",
	"model",
	"host_id",
	"agent",
	"is_subagent",
	"day",
	"week",
}

var validMeasures = []string{
	measureCount,
	measureDistinctSessions,
	measureDistinctMessages,
}

// Request configures one stats query.
type Request struct {
	DB        *sql.DB
	Scope     string
	GroupBy   string
	Query     string
	Measure   string
	Since     string
	After     string
	Before    string
	CWD       string
	Remote    string
	Skill     string
	ToolName  string
	Role      string
	Field     string
	MinCount  int
	Offset    int
	PageSize  int
	RequestID string
	Now       time.Time
}

// Response is the top-level stats response shape.
type Response struct {
	Meta    Meta     `json:"_meta"`
	Buckets []Bucket `json:"buckets"`
}

// Meta captures response metadata for stats requests.
type Meta struct {
	RequestID              string `json:"request_id"`
	Scope                  string `json:"scope"`
	Query                  string `json:"query"`
	GroupBy                string `json:"group_by"`
	Measure                string `json:"measure"`
	ElapsedMs              int64  `json:"elapsed_ms"`
	TotalMatches           int    `json:"total_matches"`
	TotalBucketsUnfiltered int    `json:"total_buckets_unfiltered"`
	TotalBuckets           int    `json:"total_buckets"`
	ReturnedBuckets        int    `json:"returned_buckets"`
	PageSize               int    `json:"page_size"`
	Offset                 int    `json:"offset"`
	HasMore                bool   `json:"has_more"`
	NextOffset             *int   `json:"next_offset,omitempty"`
	IsCapped               bool   `json:"is_capped"`
}

// Bucket is one grouped aggregate row.
type Bucket struct {
	Key              string `json:"key"`
	Count            int    `json:"count"`
	DistinctSessions int    `json:"distinct_sessions"`
	DistinctMessages int    `json:"distinct_messages"`
	SampleMessageID  string `json:"sample_message_id,omitempty"`
	SampleSessionID  string `json:"sample_session_id,omitempty"`
	SampleSnippet    string `json:"sample_snippet,omitempty"`
}

type normalizedRequest struct {
	DB        *sql.DB
	Scope     string
	GroupBy   string
	Query     string
	HasQuery  bool
	FTS       string
	Terms     []string
	Measure   string
	Since     string
	After     string
	Before    string
	CWD       string
	Remote    string
	Skill     string
	ToolName  string
	Role      string
	Field     string
	MinCount  int
	Offset    int
	PageSize  int
	RequestID string
	Now       time.Time
	Time      search.TimeFilter
}

type queryResult struct {
	TotalMatches           int
	TotalBucketsUnfiltered int
	TotalBuckets           int
	Buckets                []Bucket
}
