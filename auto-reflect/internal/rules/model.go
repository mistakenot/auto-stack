package rules

import "github.com/mistakenot/auto-shared/config"

var (
	idPattern   = `^r-[0-9a-f]{8}$`
	tagPattern  = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
	catPattern  = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
	timePattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`
)

type Rule struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

type Playbook struct {
	SchemaVersion int    `json:"schema_version"`
	Rules         []Rule `json:"rules"`
}

type CreateInput struct {
	Content  string
	Category string
	Tags     []string
}

type CreateResult struct {
	Path    string
	Created Rule
}

type Match struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	MatchScore float64  `json:"match_score"`
}

type LookupResult struct {
	Query    string   `json:"query"`
	Keywords []string `json:"keywords"`
	Rules    []Match  `json:"rules"`
}

type ValidationError = config.ValidationError
