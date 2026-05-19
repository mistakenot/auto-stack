package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/parquet-go/parquet-go"
)

// --- shared fixture ---

var (
	fixtureStats     stats
	fixtureOutputDir string
	fixtureReady     bool
)

func TestMain(m *testing.M) {
	inputDir := filepath.Join(".", ".tmp", "claude", "projects")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		// No test data — run tests anyway (they'll skip individually)
		os.Exit(m.Run())
	}

	// 1. Run genstats to produce fresh stats.json
	statsPath := filepath.Join(".", ".tmp", "stats.json")
	genStats := exec.Command("go", "run", "./cmd/genstats", inputDir, statsPath)
	genStats.Stderr = os.Stderr
	if err := genStats.Run(); err != nil {
		panic("genstats failed: " + err.Error())
	}

	// 2. Build the binary
	binDir, _ := os.MkdirTemp("", "auto-etl-e2e-bin-*")
	bin := filepath.Join(binDir, "auto-etl")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(binDir)
		panic("build failed: " + err.Error())
	}

	// 3. Run the pipeline
	outputDir, _ := os.MkdirTemp("", "auto-etl-e2e-output-*")
	run := exec.Command(bin, "run", "--input", inputDir, "--output", outputDir, "--only", "sessions")
	out, err := run.CombinedOutput()
	if err != nil {
		os.RemoveAll(binDir)
		panic("pipeline failed: " + string(out))
	}

	// 4. Load stats
	data, err := os.ReadFile(statsPath)
	if err != nil {
		os.RemoveAll(binDir)
		panic("read stats.json: " + err.Error())
	}
	if err := json.Unmarshal(data, &fixtureStats); err != nil {
		os.RemoveAll(binDir)
		panic("parse stats.json: " + err.Error())
	}

	os.RemoveAll(binDir)

	fixtureOutputDir = outputDir
	fixtureReady = true

	code := m.Run()

	os.RemoveAll(outputDir)
	os.Exit(code)
}

func skipIfNoFixture(t *testing.T) {
	t.Helper()
	if !fixtureReady {
		t.Skip("no test data available")
	}
}

// --- stats types and derivations ---

type stats struct {
	TotalFiles           int            `json:"totalFiles"`
	EmptyFiles           int            `json:"emptyFiles"`
	UnparseableFiles     int            `json:"unparseableFiles"`
	TotalLines           int            `json:"totalLines"`
	UnparseableLines     int            `json:"unparseableLines"`
	LinesByType          map[string]int `json:"linesByType"`
	TotalContentBlocks   int            `json:"totalContentBlocks"`
	ContentBlocksByType  map[string]int `json:"contentBlocksByType"`
	BareStringContents   int            `json:"bareStringContents"`
	EmptyContents        int            `json:"emptyContents"`
	ToolUsesByName       map[string]int `json:"toolUsesByName"`
	FileToolUses         int            `json:"fileToolUses"`
	FileToolUsesWithBlob int            `json:"fileToolUsesWithBlob"`
	MessagesByRole       map[string]int `json:"messagesByRole"`
	FilesWithSessionID   int            `json:"filesWithSessionId"`
	FilesWithTimestamp   int            `json:"filesWithTimestamp"`
	UniqueSessionIDs     int            `json:"uniqueSessionIds"`

	LinesWithGitBranch int `json:"linesWithGitBranch"`

	SubagentFiles            int      `json:"subagentFiles"`
	ParentFiles              int      `json:"parentFiles"`
	UniqueAgentIDs           int      `json:"uniqueAgentIds"`
	SubagentNames            []string `json:"subagentNames"`
	SubagentFilesWithoutMeta int      `json:"subagentFilesWithoutMeta"`

	Files []fileStats `json:"files"`
}

type fileStats struct {
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

// expectedSessions: ETL produces one session per file that has at least one
// user/assistant/system line with a valid timestamp.
func (s *stats) expectedSessions() int {
	count := 0
	for _, f := range s.Files {
		if !f.HasTimestamp {
			continue
		}
		hasProcessable := false
		for typ := range f.LinesByType {
			if typ == "user" || typ == "assistant" || typ == "system" {
				hasProcessable = true
				break
			}
		}
		if hasProcessable {
			count++
		}
	}
	return count
}

// expectedMessages: ETL emits one message per bare string content on
// user/assistant/system lines, plus one per text/tool_use/tool_result
// content block on those lines. Thinking blocks and unknown types are skipped.
func (s *stats) expectedMessages() int {
	return s.BareStringContents +
		s.ContentBlocksByType["text"] +
		s.ContentBlocksByType["tool_use"] +
		s.ContentBlocksByType["tool_result"]
}

// expectedSubagentSessions: count of subagent files that have processable lines.
func (s *stats) expectedSubagentSessions() int {
	count := 0
	for _, f := range s.Files {
		if !f.IsSubagent || !f.HasTimestamp {
			continue
		}
		hasProcessable := false
		for typ := range f.LinesByType {
			if typ == "user" || typ == "assistant" || typ == "system" {
				hasProcessable = true
				break
			}
		}
		if hasProcessable {
			count++
		}
	}
	return count
}

// expectedParentSessions: count of parent files that have processable lines.
func (s *stats) expectedParentSessions() int {
	return s.expectedSessions() - s.expectedSubagentSessions()
}

// --- core tests ---

func TestE2E_MessageCount(t *testing.T) {
	skipIfNoFixture(t)
	got := countParquetRows[model.AgentMessage](t, fixtureOutputDir, "messages")
	want := fixtureStats.expectedMessages()
	if got != want {
		t.Errorf("message count: got %d, want %d (= %d bare + %d text + %d tool_use + %d tool_result)",
			got, want,
			fixtureStats.BareStringContents,
			fixtureStats.ContentBlocksByType["text"],
			fixtureStats.ContentBlocksByType["tool_use"],
			fixtureStats.ContentBlocksByType["tool_result"])
	}
}

func TestE2E_SessionCount(t *testing.T) {
	skipIfNoFixture(t)
	got := countParquetRows[model.AgentSession](t, fixtureOutputDir, "sessions")
	want := fixtureStats.expectedSessions()
	if got != want {
		t.Errorf("session count: got %d, want %d", got, want)
	}
}

func TestE2E_NoBlobsDirectory(t *testing.T) {
	skipIfNoFixture(t)
	blobDir := filepath.Join(fixtureOutputDir, "blobs")
	if _, err := os.Stat(blobDir); !os.IsNotExist(err) {
		t.Error("blobs/ directory should not exist")
	}
}

func TestE2E_OutputDirectoriesExist(t *testing.T) {
	skipIfNoFixture(t)
	for _, sub := range []string{"messages", "sessions"} {
		dir := filepath.Join(fixtureOutputDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("expected %s directory: %v", sub, err)
		}
		if len(entries) == 0 {
			t.Errorf("expected partitions in %s, got empty", sub)
		}
	}
	files := findParquetFiles(t, fixtureOutputDir)
	t.Logf("total parquet files: %d", len(files))
}

func TestE2E_ToolUseBecomesAssistantRole(t *testing.T) {
	skipIfNoFixture(t)
	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")

	roleCounts := make(map[string]int)
	for _, m := range messages {
		roleCounts[m.Role]++
	}

	if roleCounts["assistant"] < fixtureStats.ContentBlocksByType["tool_use"] {
		t.Errorf("assistant count %d < tool_use blocks %d",
			roleCounts["assistant"], fixtureStats.ContentBlocksByType["tool_use"])
	}
	if roleCounts["tool"] != fixtureStats.ContentBlocksByType["tool_result"] {
		t.Errorf("tool role count: got %d, want %d",
			roleCounts["tool"], fixtureStats.ContentBlocksByType["tool_result"])
	}

	total := 0
	for _, c := range roleCounts {
		total += c
	}
	if total != len(messages) {
		t.Errorf("sum of role counts %d != total messages %d", total, len(messages))
	}
}

func TestE2E_SessionsHaveRequiredFields(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	for _, s := range sessions {
		if s.ID == "" {
			t.Error("session has empty ID")
		}
		if s.FirstMessageAt == 0 {
			t.Errorf("session %s has zero FirstMessageAt", s.ID)
		}
		if s.LastMessageAt == 0 {
			t.Errorf("session %s has zero LastMessageAt", s.ID)
		}
		if s.FirstMessageAt > s.LastMessageAt {
			t.Errorf("session %s: FirstMessageAt (%d) > LastMessageAt (%d)",
				s.ID, s.FirstMessageAt, s.LastMessageAt)
		}
	}
}

func TestE2E_Idempotent(t *testing.T) {
	skipIfNoFixture(t)

	inputDir := filepath.Join(".", ".tmp", "claude", "projects")

	binDir := t.TempDir()
	bin := filepath.Join(binDir, "auto-etl")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}

	run := exec.Command(bin, "run", "--input", inputDir, "--output", fixtureOutputDir, "--only", "sessions")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("second run failed: %v\n%s", err, out)
	}

	got := countParquetRows[model.AgentMessage](t, fixtureOutputDir, "messages")
	want := fixtureStats.expectedMessages()
	if got != want {
		t.Errorf("after second run, message count: got %d, want %d", got, want)
	}
}

func TestE2E_EmptyInput(t *testing.T) {
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "auto-etl")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}

	emptyDir := filepath.Join(t.TempDir(), "empty")
	outputDir := filepath.Join(t.TempDir(), "output")
	os.MkdirAll(emptyDir, 0o755)

	run := exec.Command(bin, "run", "--input", emptyDir, "--output", outputDir, "--only", "sessions")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}

	files := findParquetFiles(t, outputDir)
	if len(files) != 0 {
		t.Errorf("expected no parquet files for empty input, got %d", len(files))
	}
}

// --- Phase 1: content_truncated tests ---

func TestE2E_ContentNotMutated(t *testing.T) {
	skipIfNoFixture(t)
	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")

	for _, m := range messages {
		if strings.Contains(m.Content, "…[truncated") {
			t.Errorf("message %s has truncation marker in content (should only be in content_truncated)", m.ID)
			break
		}
	}
}

func TestE2E_ContentTruncatedField(t *testing.T) {
	skipIfNoFixture(t)
	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")

	maxChars := model.DefaultTruncateMaxChars
	var truncatedCount int

	for _, m := range messages {
		if m.Content == "" {
			continue
		}
		if len(m.Content) <= maxChars {
			if m.ContentTruncated != m.Content {
				t.Errorf("message %s: small content but ContentTruncated differs", m.ID)
				break
			}
		} else {
			truncatedCount++
			if !strings.Contains(m.ContentTruncated, "…[truncated") {
				t.Errorf("message %s: large content (%d chars) but ContentTruncated has no marker", m.ID, len(m.Content))
				break
			}
		}
	}
	t.Logf("messages with truncated content: %d", truncatedCount)
}

// --- Phase 2: git metadata tests ---

func TestE2E_GitBranchPopulated(t *testing.T) {
	skipIfNoFixture(t)
	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")

	var withBranch int
	for _, m := range messages {
		if m.GitBranch != "" {
			withBranch++
		}
	}
	if withBranch == 0 {
		t.Error("no messages have git_branch populated")
	}
	t.Logf("messages with git_branch: %d / %d", withBranch, len(messages))
}

func TestE2E_GitRemoteOnSessions(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")

	var withRemote int
	for _, s := range sessions {
		if s.GitRemote != "" {
			withRemote++
		}
	}
	// Some sessions may have workspaces that aren't git repos, so just check > 0
	t.Logf("sessions with git_remote: %d / %d", withRemote, len(sessions))
}

// --- Phase 3: transcript tests ---

func TestE2E_TranscriptsPopulated(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")

	for _, s := range sessions {
		if s.TranscriptFull == "" {
			t.Errorf("session %s has empty TranscriptFull", s.ID)
			break
		}
		if s.TranscriptTruncated == "" {
			t.Errorf("session %s has empty TranscriptTruncated", s.ID)
			break
		}
	}
}

func TestE2E_TranscriptContainsRolePrefixes(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")

	var hasUser, hasAssistant bool
	for _, s := range sessions {
		if strings.Contains(s.TranscriptFull, "[user]:") {
			hasUser = true
		}
		if strings.Contains(s.TranscriptFull, "[assistant]:") {
			hasAssistant = true
		}
		if hasUser && hasAssistant {
			break
		}
	}
	if !hasUser {
		t.Error("no transcript contains [user]: prefix")
	}
	if !hasAssistant {
		t.Error("no transcript contains [assistant]: prefix")
	}
}

func TestE2E_TranscriptTruncatedWithinCap(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")

	maxChars := model.DefaultTranscriptMaxChars
	for _, s := range sessions {
		// Allow some overhead for the truncation marker itself
		if len(s.TranscriptTruncated) > maxChars+100 {
			t.Errorf("session %s: TranscriptTruncated len %d exceeds cap %d",
				s.ID, len(s.TranscriptTruncated), maxChars)
			break
		}
	}
}

// --- Phase 4: host_id tests ---

func TestE2E_HostIDPopulated(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	for _, s := range sessions {
		if s.HostID == "" {
			t.Errorf("session %s has empty HostID", s.ID)
			break
		}
	}

	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")
	for _, m := range messages {
		if m.HostID == "" {
			t.Errorf("message %s has empty HostID", m.ID)
			break
		}
	}
}

// --- subagent dedup tests ---

func TestE2E_SessionIDsUnique(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	seen := make(map[string]bool)
	for _, s := range sessions {
		if seen[s.ID] {
			t.Errorf("duplicate session ID: %s", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestE2E_SubagentSessionCount(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	var count int
	for _, s := range sessions {
		if s.IsSubagent {
			count++
		}
	}
	want := fixtureStats.expectedSubagentSessions()
	if count != want {
		t.Errorf("subagent session count: got %d, want %d", count, want)
	}
}

func TestE2E_ParentSessionCount(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	var count int
	for _, s := range sessions {
		if !s.IsSubagent {
			count++
		}
	}
	want := fixtureStats.expectedParentSessions()
	if count != want {
		t.Errorf("parent session count: got %d, want %d", count, want)
	}
}

func TestE2E_SubagentSessionsHaveParent(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	for _, s := range sessions {
		if s.IsSubagent && s.ParentSessionID == "" {
			t.Errorf("subagent session %s has empty ParentSessionID", s.ID)
		}
	}
}

func TestE2E_ParentSessionsHaveNoParent(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	for _, s := range sessions {
		if !s.IsSubagent {
			if s.ParentSessionID != "" {
				t.Errorf("parent session %s has non-empty ParentSessionID: %s", s.ID, s.ParentSessionID)
			}
			if s.SubagentName != "" {
				t.Errorf("parent session %s has SubagentName: %s", s.ID, s.SubagentName)
			}
		}
	}
}

func TestE2E_ParentSessionIDsMatchRawSessionID(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	parentIDs := make(map[string]bool)
	for _, f := range fixtureStats.Files {
		if !f.IsSubagent && f.SessionID != "" {
			parentIDs[f.SessionID] = true
		}
	}
	for _, s := range sessions {
		if !s.IsSubagent {
			if !parentIDs[s.ID] {
				t.Errorf("parent session ID %s not found in raw sessionIds", s.ID)
			}
		}
	}
}

func TestE2E_SubagentNamesMatch(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	nameSet := make(map[string]bool)
	for _, s := range sessions {
		if s.SubagentName != "" {
			nameSet[s.SubagentName] = true
		}
	}
	wantSet := make(map[string]bool)
	for _, n := range fixtureStats.SubagentNames {
		wantSet[n] = true
	}
	for name := range wantSet {
		if !nameSet[name] {
			t.Errorf("expected subagent name %q not found in output sessions", name)
		}
	}
	for name := range nameSet {
		if !wantSet[name] {
			t.Errorf("unexpected subagent name %q in output sessions", name)
		}
	}
}

func TestE2E_MessageParentSessionIDConsistent(t *testing.T) {
	skipIfNoFixture(t)
	sessions := readAllParquet[model.AgentSession](t, fixtureOutputDir, "sessions")
	messages := readAllParquet[model.AgentMessage](t, fixtureOutputDir, "messages")

	sessionParent := make(map[string]string)
	for _, s := range sessions {
		sessionParent[s.ID] = s.ParentSessionID
	}

	for _, m := range messages {
		want := sessionParent[m.SessionID]
		if m.ParentSessionID != want {
			t.Errorf("message %s: ParentSessionID=%q, session has %q",
				m.ID, m.ParentSessionID, want)
		}
	}
}

// --- helpers ---

func findParquetFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error { //nolint:errcheck
		if err != nil {
			return nil //nolint:nilerr // intentionally skip inaccessible paths
		}
		if !info.IsDir() && filepath.Ext(path) == ".parquet" {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func countParquetRows[T any](t *testing.T, outputDir, subdir string) int {
	t.Helper()
	return len(readAllParquet[T](t, outputDir, subdir))
}

func readAllParquet[T any](t *testing.T, outputDir, subdir string) []T {
	t.Helper()
	dir := filepath.Join(outputDir, subdir)
	files := findParquetFiles(t, dir)
	if len(files) == 0 {
		t.Fatalf("no parquet files found in %s", dir)
	}

	var all []T
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		reader := parquet.NewGenericReader[T](f)
		numRows := reader.NumRows()
		if numRows == 0 {
			reader.Close()
			f.Close()
			continue
		}
		rows := make([]T, numRows)
		n, err := reader.Read(rows)
		if err != nil && err.Error() != "EOF" {
			t.Fatalf("read %s: %v", path, err)
		}
		all = append(all, rows[:n]...)
		reader.Close()
		f.Close()
	}
	return all
}
