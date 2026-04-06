package textout

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mistakenot/auto-watch/internal/model"
)

func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func WriteJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func Preview(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 80 {
		return value
	}
	return value[:77] + "..."
}

func FormatValidationErrors(errs []model.ValidationError) string {
	lines := make([]string, 0, len(errs))
	for _, err := range errs {
		line := fmt.Sprintf("%s (%s.%s): %s", err.Code, err.Path, err.Field, err.Message)
		if err.Value != nil {
			line += fmt.Sprintf(" [value=%v]", err.Value)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func FormatEventLine(event *model.EventRecord) string {
	parts := []string{
		event.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		event.Level,
		event.EventType,
	}
	if event.ProjectID != "" {
		parts = append(parts, "project="+event.ProjectID)
	}
	if event.TriggerID != "" {
		parts = append(parts, "trigger="+event.TriggerID)
	}
	if event.TaskID != "" {
		parts = append(parts, "task="+event.TaskID)
	}
	keys := make([]string, 0, len(event.Metadata))
	for key := range event.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, event.Metadata[key]))
	}
	return strings.Join(parts, " ")
}
