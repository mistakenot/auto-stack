package stats

import (
	"fmt"
	"regexp"
	"strings"
)

var envAssignPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)

func normalizedBucketExpr(scope, groupBy string) (string, error) {
	var inner string
	switch scope {
	case scopeMessages:
		switch groupBy {
		case "session_id":
			inner = "m.session_id"
		case "role":
			inner = "m.role"
		case "tool_name":
			inner = "m.tool_name"
		case "skill_name":
			inner = "m.skill_name"
		case "tool_file_path":
			inner = "m.tool_file_path"
		case "bash_command":
			inner = "m.bash_command"
		case "workspace":
			inner = "m.workspace"
		case "git_remote":
			inner = "m.git_remote"
		case "model":
			inner = "m.model"
		case "day":
			inner = "strftime('%Y-%m-%d', m.timestamp/1000, 'unixepoch')"
		case "week":
			inner = isoWeekExpr("m.timestamp")
		default:
			return "", fmt.Errorf("unsupported message group key: %s", groupBy)
		}
	case scopeSessions:
		switch groupBy {
		case "session_id":
			inner = "s.session_id"
		case "workspace":
			inner = "s.workspace"
		case "git_remote":
			inner = "s.git_remote"
		case "model":
			inner = "s.model"
		case "host_id":
			inner = "s.host_id"
		case "agent":
			inner = "s.agent"
		case "is_subagent":
			inner = "CASE WHEN s.is_subagent = 0 THEN 'false' ELSE 'true' END"
		case "day":
			inner = "strftime('%Y-%m-%d', s.first_message_at/1000, 'unixepoch')"
		case "week":
			inner = isoWeekExpr("s.first_message_at")
		default:
			return "", fmt.Errorf("unsupported session group key: %s", groupBy)
		}
	default:
		return "", fmt.Errorf("unsupported scope: %s", scope)
	}

	return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s), ''), '%s')", inner, emptyBucketKey), nil
}

func isoWeekExpr(msColumn string) string {
	return fmt.Sprintf(
		"printf('%%04d-W%%02d', CAST(strftime('%%Y', date(%s/1000, 'unixepoch', '-3 days', 'weekday 4')) AS INTEGER), 1 + CAST(strftime('%%W', date(%s/1000, 'unixepoch', '-3 days', 'weekday 4')) AS INTEGER))",
		msColumn,
		msColumn,
	)
}

func normalizeBucketValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return emptyBucketKey
	}
	return value
}

// normalizeBashCommandFamily folds raw bash commands into a command-family key.
func normalizeBashCommandFamily(raw string) string {
	cmd := collapseWhitespace(raw)
	if cmd == "" {
		return emptyBucketKey
	}

	cmd = unwrapShellWrapper(cmd)
	cmd = splitCommandChain(cmd)
	cmd = unwrapShellWrapper(cmd)
	cmd = unwrapMatchingQuotes(cmd)
	cmd = collapseWhitespace(cmd)
	if cmd == "" {
		return emptyBucketKey
	}

	tokens := strings.Fields(cmd)
	tokens = stripCommandPrefixes(tokens)
	if len(tokens) == 0 {
		return emptyBucketKey
	}
	if len(tokens) >= 2 {
		return tokens[0] + " " + tokens[1]
	}
	return tokens[0]
}

func stripCommandPrefixes(tokens []string) []string {
	for len(tokens) > 0 {
		tok := tokens[0]
		switch {
		case envAssignPattern.MatchString(tok):
			tokens = tokens[1:]
		case tok == "sudo":
			tokens = tokens[1:]
		case tok == "env":
			tokens = tokens[1:]
		default:
			return tokens
		}
	}
	return tokens
}

func unwrapShellWrapper(s string) string {
	for {
		s = collapseWhitespace(s)
		fields := strings.Fields(s)
		if len(fields) < 3 {
			return s
		}
		if (fields[0] != "bash" && fields[0] != "sh") || fields[1] != "-lc" {
			return s
		}
		prefix := fields[0] + " " + fields[1]
		inner := strings.TrimSpace(strings.TrimPrefix(s, prefix))
		inner = unwrapMatchingQuotes(inner)
		if inner == "" || inner == s {
			return s
		}
		s = inner
	}
}

func splitCommandChain(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	last := ""
	start := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == ';' {
			segment := strings.TrimSpace(s[start:i])
			if segment != "" {
				last = segment
			}
			start = i + 1
			continue
		}
		if i+1 < len(s) {
			next := s[i+1]
			if (ch == '&' && next == '&') || (ch == '|' && next == '|') {
				segment := strings.TrimSpace(s[start:i])
				if segment != "" {
					last = segment
				}
				start = i + 2
				i++
			}
		}
	}

	segment := strings.TrimSpace(s[start:])
	if segment != "" {
		last = segment
	}
	if last == "" {
		return s
	}
	return last
}

func unwrapMatchingQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	first := s[0]
	last := s[len(s)-1]
	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
