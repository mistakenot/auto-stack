package feedback

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/store"
)

type Service struct {
	Now func() time.Time
}

func NewService() *Service {
	return &Service{Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Add(cwd string, in *AddInput) (AddResult, []ValidationError, error) {
	if in == nil {
		return AddResult{}, []ValidationError{{
			Code:    "required",
			Field:   "input",
			Message: "input is required",
		}}, nil
	}

	errs := ValidateAddInput(in)
	if len(errs) > 0 {
		return AddResult{}, errs, nil
	}

	repoInfo, err := gitutil.DetectRepo(cwd)
	if err != nil {
		return AddResult{}, nil, err
	}
	if _, err := store.EnsureStateDir(repoInfo.Root); err != nil {
		return AddResult{}, nil, err
	}

	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	comment := strings.TrimSpace(in.Comment)
	var contextPtr *string
	if ctx := strings.TrimSpace(in.Context); ctx != "" {
		contextPtr = &ctx
	}

	repoRelative, err := NormalizeRepoRelativePath(in.File)
	if err != nil {
		return AddResult{}, nil, err
	}

	subject := Subject{Type: "missing_context"}
	if repoRelative != "" {
		subject = Subject{
			Type: "file_span",
			File: &repoRelative,
		}
		if in.Start != nil || in.End != nil {
			startLine, endLine := resolvedSpanRange(in.Start, in.End)
			span, spanErr := CaptureSpan(repoInfo.Root, repoRelative, startLine, endLine, repoInfo.Dirty)
			if spanErr != nil {
				return AddResult{}, nil, spanErr
			}
			subject.StartLine = &span.StartLine
			subject.EndLine = &span.EndLine
			subject.StartByte = &span.StartByte
			subject.EndByte = &span.EndByte
			subject.ContentSnippet = &span.ContentSnippet
			subject.SnippetSHA256 = &span.SnippetSHA256
			subject.HeadBlobSHA = span.HeadBlobSHA
			subject.ObservedBlobSHA = &span.ObservedBlobSHA
			subject.CaptureSource = &span.CaptureSource
			subject.WorktreeDirty = &span.WorktreeDirty
		}
	}

	workspaceName := filepath.Base(repoInfo.Root)
	workspacePath := repoInfo.Root
	timestamp := s.Now().UTC().Format(time.RFC3339)

	effectiveAt, err := ParseEffectiveAt(strings.TrimSpace(in.EffectiveAt))
	if err != nil {
		return AddResult{}, nil, fmt.Errorf("parse effective_at: %w", err)
	}
	effectiveAtStr := effectiveAt.Format(time.RFC3339)

	event := Event{
		ID:            newFeedbackID(timestamp, repoInfo.Head, comment),
		Kind:          kind,
		Comment:       comment,
		Context:       contextPtr,
		Timestamp:     timestamp,
		EffectiveAt:   effectiveAtStr,
		GitHash:       repoInfo.Head,
		GitTreeSHA:    repoInfo.Tree,
		GitRemote:     repoInfo.Remote,
		WorkspaceName: workspaceName,
		WorkspacePath: &workspacePath,
		SessionID:     detectSessionID(),
		Agent:         detectAgent(),
		Subject:       subject,
	}

	logPath := store.FeedbackPath(repoInfo.Root)
	if err := store.AppendJSONLine(logPath, event); err != nil {
		return AddResult{}, nil, err
	}

	return AddResult{Path: logPath, Event: event}, nil, nil
}

func (s *Service) List(cwd string, input ListInput) (ListResult, []ValidationError, error) {
	inputErrs := ValidateListInput(input)
	if len(inputErrs) > 0 {
		return ListResult{}, inputErrs, nil
	}

	repoInfo, err := gitutil.DetectRepo(cwd)
	if err != nil {
		return ListResult{}, nil, err
	}

	path := store.FeedbackPath(repoInfo.Root)
	if _, err := store.EnsureStateDir(repoInfo.Root); err != nil {
		return ListResult{}, nil, err
	}

	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	normalizedFile := strings.ToLower(strings.TrimSpace(input.File))
	result := ListResult{Events: []Event{}}
	validationErrs := make([]ValidationError, 0)

	err = store.ReadJSONLines(path, func(lineNumber int, line []byte) error {
		var event Event
		if !decodeEventLine(line, &event) {
			validationErrs = append(validationErrs, ValidationError{
				Code:    "invalid_jsonl_line",
				Path:    filepath.ToSlash(path),
				Field:   fmt.Sprintf("line[%d]", lineNumber),
				Message: "invalid JSONL record",
				Value:   string(line),
			})
			return nil
		}

		if input.Kind != "" && event.Kind != input.Kind {
			return nil
		}
		if normalizedFile != "" {
			candidate := ""
			if event.Subject.File != nil {
				candidate = strings.ToLower(strings.TrimSpace(*event.Subject.File))
			}
			if !strings.Contains(candidate, normalizedFile) {
				return nil
			}
		}

		ts, ok := parseRFC3339(event.Timestamp)
		if !ok {
			validationErrs = append(validationErrs, ValidationError{
				Code:    "invalid_timestamp",
				Path:    filepath.ToSlash(path),
				Field:   fmt.Sprintf("line[%d].timestamp", lineNumber),
				Message: "timestamp must be RFC3339",
				Value:   event.Timestamp,
			})
			return nil
		}
		if input.After != nil && ts.Before(*input.After) {
			return nil
		}
		if input.Before != nil && !ts.Before(*input.Before) {
			return nil
		}

		result.Events = append(result.Events, event)
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, validationErrs, nil
		}
		return ListResult{}, nil, err
	}

	slices.SortFunc(result.Events, func(a, b Event) int {
		at, _ := time.Parse(time.RFC3339, a.Timestamp)
		bt, _ := time.Parse(time.RFC3339, b.Timestamp)
		if at.Equal(bt) {
			if a.ID > b.ID {
				return -1
			}
			if a.ID < b.ID {
				return 1
			}
			return 0
		}
		if at.After(bt) {
			return -1
		}
		return 1
	})

	if input.Limit > 0 && len(result.Events) > input.Limit {
		result.Events = result.Events[:input.Limit]
	}

	return result, validationErrs, nil
}

func newFeedbackID(timestamp, head, comment string) string {
	seed := fmt.Sprintf("%s|%s|%s", timestamp, head, comment)
	hash := sha256Hex(seed)
	nonce := randomHex(2)
	return "f-" + hash[:6] + nonce
}

func sha256Hex(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func randomHex(byteLen int) string {
	if byteLen <= 0 {
		return ""
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "0000"
	}
	return hex.EncodeToString(buf)
}

func resolvedSpanRange(start, end *int) (int, int) {
	if start == nil && end == nil {
		return 0, 0
	}
	if start != nil && end != nil {
		return *start, *end
	}
	if start != nil {
		return *start, *start
	}
	return *end, *end
}

func detectSessionID() *string {
	keys := []string{"AUTO_SESSION_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID"}
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return &value
		}
	}
	return nil
}

func detectAgent() *string {
	keys := []string{"AUTO_AGENT", "CODEX_AGENT", "CLAUDE_AGENT"}
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return &value
		}
	}
	return nil
}

func parseRFC3339(value string) (time.Time, bool) {
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return timestamp, true
}

func decodeEventLine(line []byte, event *Event) bool {
	return json.Unmarshal(line, event) == nil
}
