// genstats independently analyzes raw Claude JSONL session files and produces
// stats.json for E2E test assertions. This intentionally does NOT import any
// auto-etl packages — it's an independent implementation.
//
// This tool does NO filtering. It records everything it encounters as metrics.
// The E2E tests then use these raw metrics to compute expected ETL output.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stats captures raw metrics from the JSONL files with no filtering.
type Stats struct {
	// File-level
	TotalFiles       int `json:"totalFiles"`
	EmptyFiles       int `json:"emptyFiles"`
	UnparseableFiles int `json:"unparseableFiles"`

	// Line-level
	TotalLines       int            `json:"totalLines"`
	UnparseableLines int            `json:"unparseableLines"`
	LinesByType      map[string]int `json:"linesByType"`

	// Content block-level (from lines that have message.content)
	TotalContentBlocks  int            `json:"totalContentBlocks"`
	ContentBlocksByType map[string]int `json:"contentBlocksByType"`
	BareStringContents  int            `json:"bareStringContents"`
	EmptyContents       int            `json:"emptyContents"`

	// Tool use details
	ToolUsesByName map[string]int `json:"toolUsesByName"`

	// Blob candidates: file tool uses that have extractable content
	FileToolUses         int `json:"fileToolUses"`
	FileToolUsesWithBlob int `json:"fileToolUsesWithBlob"`

	// Message roles (from message.role field, across ALL line types)
	MessagesByRole map[string]int `json:"messagesByRole"`

	// Session-level (files that have at least one line with a sessionId)
	FilesWithSessionID int `json:"filesWithSessionID"`
	FilesWithTimestamp int `json:"filesWithTimestamp"`
	UniqueSessionIDs   int `json:"uniqueSessionIds"`

	// Git metadata
	LinesWithGitBranch int `json:"linesWithGitBranch"`

	// Subagent-level
	SubagentFiles            int      `json:"subagentFiles"`
	ParentFiles              int      `json:"parentFiles"`
	UniqueAgentIDs           int      `json:"uniqueAgentIds"`
	SubagentNames            []string `json:"subagentNames"`
	SubagentFilesWithoutMeta int      `json:"subagentFilesWithoutMeta"`

	// Per-file details for debugging
	Files []FileStats `json:"files"`
}

type FileStats struct {
	Path             string         `json:"path"`
	TotalLines       int            `json:"totalLines"`
	UnparseableLines int            `json:"unparseableLines"`
	LinesByType      map[string]int `json:"linesByType"`
	HasSessionID     bool           `json:"hasSessionId"`
	HasTimestamp     bool           `json:"hasTimestamp"`
	SessionID        string         `json:"sessionId,omitempty"`
	IsSubagent       bool           `json:"isSubagent"`
	AgentID          string         `json:"agentId,omitempty"`
	SubagentName     string         `json:"subagentName,omitempty"`
}

type rawLine struct {
	Type        string  `json:"type"`
	SessionID   string  `json:"sessionId"`
	Timestamp   string  `json:"timestamp"`
	GitBranch   *string `json:"gitBranch"`
	IsSidechain bool    `json:"isSidechain"`
	AgentID     string  `json:"agentId"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	ToolUseID string          `json:"tool_use_id"`
}

var fileToolNames = map[string]bool{
	"Read": true, "Write": true, "Edit": true,
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: genstats <input-dir> <output-file>\n")
		os.Exit(1)
	}
	inputDir := os.Args[1]
	outputFile := os.Args[2]

	files, err := findJSONLFiles(inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan: %v\n", err)
		os.Exit(1)
	}

	stats := Stats{
		TotalFiles:          len(files),
		LinesByType:         make(map[string]int),
		ContentBlocksByType: make(map[string]int),
		ToolUsesByName:      make(map[string]int),
		MessagesByRole:      make(map[string]int),
	}

	sessionIDs := make(map[string]bool)
	agentIDs := make(map[string]bool)
	subagentNameSet := make(map[string]bool)

	for _, path := range files {
		fs := processFile(path, &stats, sessionIDs)
		stats.Files = append(stats.Files, fs)

		if fs.IsSubagent {
			stats.SubagentFiles++
			if fs.AgentID != "" {
				agentIDs[fs.AgentID] = true
			}
			if fs.SubagentName != "" {
				subagentNameSet[fs.SubagentName] = true
			} else {
				stats.SubagentFilesWithoutMeta++
			}
		}
	}

	stats.UniqueSessionIDs = len(sessionIDs)
	stats.UniqueAgentIDs = len(agentIDs)
	stats.ParentFiles = stats.TotalFiles - stats.SubagentFiles - stats.EmptyFiles - stats.UnparseableFiles
	for name := range subagentNameSet {
		stats.SubagentNames = append(stats.SubagentNames, name)
	}
	sort.Strings(stats.SubagentNames)

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputFile, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s (%d files, %d lines, %d content blocks)\n",
		outputFile, stats.TotalFiles, stats.TotalLines, stats.TotalContentBlocks)
}

func processFile(path string, stats *Stats, sessionIDs map[string]bool) FileStats {
	fs := FileStats{
		Path:        path,
		LinesByType: make(map[string]int),
	}

	f, err := os.Open(path)
	if err != nil {
		stats.UnparseableFiles++
		return fs
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// First pass: collect all lines (need tool_result map for blob detection)
	var lines []rawLine
	for scanner.Scan() {
		raw := make([]byte, len(scanner.Bytes()))
		copy(raw, scanner.Bytes())

		var line rawLine
		if err := json.Unmarshal(raw, &line); err != nil {
			fs.UnparseableLines++
			stats.UnparseableLines++
			fs.TotalLines++
			stats.TotalLines++
			continue
		}
		lines = append(lines, line)
		fs.TotalLines++
		stats.TotalLines++
	}

	if fs.TotalLines == 0 {
		stats.EmptyFiles++
	}

	// Build tool_result map for blob content detection
	toolResultContent := make(map[string]string)
	for i := range lines {
		blocks := parseBlocks(lines[i].Message.Content)
		for j := range blocks {
			b := &blocks[j]
			if b.Type == "tool_result" && b.ToolUseID != "" && len(b.Content) > 0 {
				var s string
				if err := json.Unmarshal(b.Content, &s); err == nil {
					toolResultContent[b.ToolUseID] = s
				}
			}
		}
	}

	// Detect subagent: any line with isSidechain=true
	for i := range lines {
		if lines[i].IsSidechain {
			fs.IsSubagent = true
		}
		if fs.AgentID == "" && lines[i].AgentID != "" {
			fs.AgentID = lines[i].AgentID
		}
	}

	// Load .meta.json for subagent files
	if fs.IsSubagent {
		base := strings.TrimSuffix(path, ".jsonl")
		metaPath := base + ".meta.json"
		if data, err := os.ReadFile(metaPath); err == nil {
			var meta struct {
				AgentType string `json:"agentType"`
			}
			if json.Unmarshal(data, &meta) == nil {
				fs.SubagentName = meta.AgentType
			}
		}
	}

	// Second pass: record all metrics
	for i := range lines {
		line := &lines[i]
		// Record line type
		lineType := line.Type
		if lineType == "" {
			lineType = "(empty)"
		}
		fs.LinesByType[lineType]++
		stats.LinesByType[lineType]++

		// Record session ID
		if line.SessionID != "" {
			fs.HasSessionID = true
			if fs.SessionID == "" {
				fs.SessionID = line.SessionID
			}
			sessionIDs[line.SessionID] = true
		}

		// Record timestamp
		if line.Timestamp != "" {
			fs.HasTimestamp = true
		}

		// Record git branch
		if line.GitBranch != nil && *line.GitBranch != "" {
			stats.LinesWithGitBranch++
		}

		// Record message role
		if line.Message.Role != "" {
			stats.MessagesByRole[line.Message.Role]++
		}

		// Record content details
		if len(line.Message.Content) == 0 {
			stats.EmptyContents++
			continue
		}

		text, blocks := parseContent(line.Message.Content)

		if blocks == nil && text != "" {
			stats.BareStringContents++
			continue
		}

		if blocks == nil && text == "" {
			stats.EmptyContents++
			continue
		}

		for j := range blocks {
			block := &blocks[j]
			stats.TotalContentBlocks++
			blockType := block.Type
			if blockType == "" {
				blockType = "(empty)"
			}
			stats.ContentBlocksByType[blockType]++

			if block.Type == "tool_use" {
				toolName := block.Name
				if toolName == "" {
					toolName = "(empty)"
				}
				stats.ToolUsesByName[toolName]++

				if fileToolNames[block.Name] {
					stats.FileToolUses++
					if hasBlob(block, toolResultContent) {
						stats.FileToolUsesWithBlob++
					}
				}
			}
		}
	}

	if fs.HasSessionID {
		stats.FilesWithSessionID++
	}
	if fs.HasTimestamp {
		stats.FilesWithTimestamp++
	}

	return fs
}

func parseContent(raw json.RawMessage) (string, []contentBlock) {
	if len(raw) == 0 {
		return "", nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", nil
		}
		return s, nil
	}
	if trimmed[0] == '[' {
		var blocks []contentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return "", nil
		}
		return "", blocks
	}
	return "", nil
}

func parseBlocks(raw json.RawMessage) []contentBlock {
	_, blocks := parseContent(raw)
	return blocks
}

func hasBlob(block *contentBlock, toolResults map[string]string) bool {
	var inputMap map[string]any
	if err := json.Unmarshal(block.Input, &inputMap); err != nil {
		return false
	}
	filePath, _ := inputMap["file_path"].(string)
	if filePath == "" {
		return false
	}
	var content string
	switch block.Name {
	case "Read":
		content = toolResults[block.ID]
	case "Write":
		content, _ = inputMap["content"].(string)
	case "Edit":
		var parts []string
		if old, ok := inputMap["old_string"].(string); ok {
			parts = append(parts, old)
		}
		if new_, ok := inputMap["new_string"].(string); ok {
			parts = append(parts, new_)
		}
		content = strings.Join(parts, "\n---\n")
	}
	return content != ""
}

func findJSONLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // intentionally skip inaccessible paths
		}
		if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
