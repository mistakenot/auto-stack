package stats

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mistakenot/auto-search/internal/query"
	"github.com/mistakenot/auto-search/internal/search"
)

func normalizeAndValidate(req *Request) (normalizedRequest, error) {
	if req == nil {
		return normalizedRequest{}, errors.New("stats request is required")
	}
	if req.DB == nil {
		return normalizedRequest{}, errors.New("stats request db is required")
	}
	if strings.TrimSpace(req.CWD) != "" && strings.TrimSpace(req.Remote) != "" {
		return normalizedRequest{}, errors.New("--cwd and --remote are mutually exclusive")
	}

	scope, err := normalizeScope(req.Scope)
	if err != nil {
		return normalizedRequest{}, err
	}
	groupBy, err := normalizeGroupBy(scope, req.GroupBy)
	if err != nil {
		return normalizedRequest{}, err
	}
	measure, err := normalizeMeasure(req.Measure)
	if err != nil {
		return normalizedRequest{}, err
	}
	role, err := normalizeRole(req.Role)
	if err != nil {
		return normalizedRequest{}, err
	}
	field, err := normalizeField(req.Field)
	if err != nil {
		return normalizedRequest{}, err
	}
	offset, pageSize, err := normalizePagination(req.Offset, req.PageSize)
	if err != nil {
		return normalizedRequest{}, err
	}
	if req.MinCount < 0 {
		return normalizedRequest{}, errors.New("--min-count must be >= 0")
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	timeFilter, err := search.ParseTimeFilter(now, req.Since, req.After, req.Before)
	if err != nil {
		return normalizedRequest{}, err
	}

	queryText := strings.TrimSpace(req.Query)
	hasQuery := queryText != ""
	var fts string
	var terms []string
	if hasQuery {
		ast, err := query.Parse(queryText)
		if err != nil {
			return normalizedRequest{}, fmt.Errorf("parse query: %w", err)
		}
		fts = query.CompileFTS(ast)
		terms = search.ExtractTerms(ast)
	}

	return normalizedRequest{
		DB:        req.DB,
		Scope:     scope,
		GroupBy:   groupBy,
		Query:     queryText,
		HasQuery:  hasQuery,
		FTS:       fts,
		Terms:     terms,
		Measure:   measure,
		Since:     strings.TrimSpace(req.Since),
		After:     strings.TrimSpace(req.After),
		Before:    strings.TrimSpace(req.Before),
		CWD:       strings.TrimSpace(req.CWD),
		Remote:    strings.TrimSpace(req.Remote),
		Skill:     strings.TrimSpace(req.Skill),
		ToolName:  strings.TrimSpace(req.ToolName),
		Role:      role,
		Field:     field,
		MinCount:  req.MinCount,
		Offset:    offset,
		PageSize:  pageSize,
		RequestID: strings.TrimSpace(req.RequestID),
		Now:       now,
		Time:      timeFilter,
	}, nil
}

func normalizeScope(raw string) (string, error) {
	scope := strings.ToLower(strings.TrimSpace(raw))
	if scope == "" {
		scope = scopeMessages
	}
	switch scope {
	case scopeMessages, scopeSessions:
		return scope, nil
	default:
		return "", fmt.Errorf("invalid --scope value %q; valid values: messages, sessions", raw)
	}
}

func normalizeGroupBy(scope, raw string) (string, error) {
	groupBy := strings.ToLower(strings.TrimSpace(raw))
	if groupBy == "" {
		return "", errors.New("--group-by is required; run: auto search stats --help")
	}

	if slices.Contains(validKeysForScope(scope), groupBy) {
		return groupBy, nil
	}
	return "", fmt.Errorf(
		"invalid --group-by value %q for --scope %s; valid values: %s",
		raw,
		scope,
		strings.Join(validKeysForScope(scope), ", "),
	)
}

func validKeysForScope(scope string) []string {
	if scope == scopeSessions {
		return sessionGroupKeys
	}
	return messageGroupKeys
}

func normalizeMeasure(raw string) (string, error) {
	measure := strings.ToLower(strings.TrimSpace(raw))
	if measure == "" {
		measure = measureCount
	}
	if slices.Contains(validMeasures, measure) {
		return measure, nil
	}
	return "", fmt.Errorf(
		"invalid --measure value %q; valid values: %s",
		raw,
		strings.Join(validMeasures, ", "),
	)
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

func normalizeField(field string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(field))
	if normalized == "" {
		return "all", nil
	}
	switch normalized {
	case "all", "content", "tool_input", "tool_output":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid --field value %q (use all, content, tool_input, tool_output)", field)
	}
}

func normalizeRole(raw string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(raw))
	if role == "" {
		return "", nil
	}
	switch role {
	case "user", "assistant", "tool", "thinking":
		return role, nil
	default:
		return "", fmt.Errorf("invalid --role value %q; valid values: user, assistant, tool, thinking", raw)
	}
}
