package contextpack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestPack() *Pack {
	return &Pack{
		ProjectRoot:     "/tmp/project",
		TokenLimit:      12000,
		EstimatedTokens: 850,
		OmittedTokens:   620,
		SeedFiles:       []string{"src/App.tsx", "src/hooks/useAuth.ts"},
		ReadingOrder: []ReadingOrderItem{
			{Path: "src/App.tsx", Reason: "seed file"},
			{Path: "src/hooks/useAuth.ts", Reason: "seed file"},
			{Path: "src/services/userService.ts", Reason: "direct runtime dependency of src/hooks/useAuth.ts"},
		},
		Files: []FileEntry{
			{
				Path:            "src/App.tsx",
				Role:            "seed",
				Reason:          "seed file",
				EstimatedTokens: 430,
				Content:         "import React from 'react';\nexport function App() { return <div/>; }\n",
			},
			{
				Path:            "src/hooks/useAuth.ts",
				Role:            "seed",
				Reason:          "seed file",
				EstimatedTokens: 200,
				Content:         "export function useAuth() { return { user: null }; }\n",
			},
			{
				Path:            "src/services/userService.ts",
				Role:            "dependency",
				Reason:          "direct runtime dependency of src/hooks/useAuth.ts",
				EstimatedTokens: 180,
				Flags:           []string{"high_fan_in"},
				Content:         "export async function getUser(id: string) { return { id }; }\n",
			},
		},
		Relationships: []Relationship{
			{
				Source:            "src/hooks/useAuth.ts",
				Target:            "src/services/userService.ts",
				Direction:         "forward",
				PrimaryImportKind: "static",
				ImportKinds:       []string{"static"},
				Distance:          1,
				Reason:            "src/hooks/useAuth.ts imports src/services/userService.ts",
			},
		},
		Guidance: Guidance{
			Watch: []string{
				"Changing src/hooks/useAuth.ts may affect src/components/App.tsx.",
				"src/config.ts has a side-effect import of src/utils/validate.ts.",
			},
		},
		OmittedCandidates: []OmittedCandidate{
			{
				Path:            "src/services/analyticsService.ts",
				Role:            "transitive_neighbor",
				Reason:          "second-hop dynamic dependency via src/services/userService.ts",
				EstimatedTokens: 620,
			},
		},
	}
}

func TestMarkdownShape(t *testing.T) {
	p := newTestPack()
	md := RenderMarkdown(p)

	// Must start with the Context Pack header.
	if !strings.HasPrefix(md, "# Context Pack\n") {
		t.Errorf("markdown should start with '# Context Pack\\n', got prefix: %q", md[:min(40, len(md))])
	}

	// Budget line must contain estimated/limit tokens.
	if !strings.Contains(md, "Budget: 850/12000 tokens") {
		t.Error("markdown should contain 'Budget: 850/12000 tokens'")
	}

	// Omitted total must appear.
	if !strings.Contains(md, "Omitted: 620 tokens") {
		t.Error("markdown should contain 'Omitted: 620 tokens'")
	}

	// Seeds line.
	if !strings.Contains(md, "Seeds: src/App.tsx, src/hooks/useAuth.ts") {
		t.Error("markdown should contain seeds line")
	}

	// Read First section.
	if !strings.Contains(md, "## Read First") {
		t.Error("markdown should contain '## Read First' section")
	}
	if !strings.Contains(md, "1. src/App.tsx - seed file") {
		t.Error("markdown should contain numbered reading order items")
	}

	// Watch section.
	if !strings.Contains(md, "## Watch") {
		t.Error("markdown should contain '## Watch' section")
	}
	if !strings.Contains(md, "- Changing src/hooks/useAuth.ts may affect src/components/App.tsx.") {
		t.Error("markdown should contain watch guidance items")
	}

	// Files section with fenced content.
	if !strings.Contains(md, "## Files") {
		t.Error("markdown should contain '## Files' section")
	}
	if !strings.Contains(md, "### src/App.tsx") {
		t.Error("markdown should contain file heading")
	}
	if !strings.Contains(md, "Role: seed. Tokens: 430.") {
		t.Error("markdown should contain role and token info")
	}
	if !strings.Contains(md, "```tsx") {
		t.Error("markdown should contain fenced code block with language")
	}

	// Flags should appear for files that have them.
	if !strings.Contains(md, "Flags: high_fan_in.") {
		t.Error("markdown should show flags for files that have them")
	}

	// Omitted section.
	if !strings.Contains(md, "## Omitted") {
		t.Error("markdown should contain '## Omitted' section")
	}
	if !strings.Contains(md, "src/services/analyticsService.ts") {
		t.Error("markdown should list omitted candidates")
	}
	if !strings.Contains(md, "620 tokens") {
		t.Error("markdown should show omitted candidate token counts")
	}
}

func TestMarkdownOmittedTokenTotal(t *testing.T) {
	p := newTestPack()
	p.OmittedTokens = 1500
	md := RenderMarkdown(p)

	if !strings.Contains(md, "Omitted: 1500 tokens") {
		t.Error("markdown should report the total omitted tokens")
	}
}

func TestMarkdownNoGenericProse(t *testing.T) {
	p := newTestPack()
	md := RenderMarkdown(p)

	// Must not contain generic command tutorials or API references.
	genericPhrases := []string{
		"autograph code context",
		"--token-limit",
		"--file",
		"--format",
		"Usage:",
		"Example:",
		"API Reference",
		"Getting Started",
		"Installation",
	}
	for _, phrase := range genericPhrases {
		if strings.Contains(md, phrase) {
			t.Errorf("markdown should not contain generic prose %q", phrase)
		}
	}
}

func TestJSONValidFields(t *testing.T) {
	p := newTestPack()
	output, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	// Must be parseable JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("JSON output is not parseable: %v", err)
	}

	// Check required top-level fields exist.
	requiredFields := []string{
		"project_root",
		"token_limit",
		"estimated_tokens",
		"omitted_tokens",
		"seed_files",
		"reading_order",
		"files",
		"relationships",
		"guidance",
		"omitted_candidates",
	}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("JSON output missing required field %q", field)
		}
	}

	// Verify file entries have required fields.
	files, ok := parsed["files"].([]interface{})
	if !ok || len(files) == 0 {
		t.Fatal("JSON output should have non-empty 'files' array")
	}
	fileObj, ok := files[0].(map[string]interface{})
	if !ok {
		t.Fatal("JSON file entry should be an object")
	}
	fileFields := []string{"path", "role", "reason", "estimated_tokens", "content"}
	for _, field := range fileFields {
		if _, ok := fileObj[field]; !ok {
			t.Errorf("JSON file entry missing required field %q", field)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	p := newTestPack()
	output, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	// Parse JSON back into a Pack struct.
	var roundTrip Pack
	if err := json.Unmarshal([]byte(output), &roundTrip); err != nil {
		t.Fatalf("JSON round-trip unmarshal failed: %v", err)
	}

	// Verify key fields.
	if roundTrip.ProjectRoot != p.ProjectRoot {
		t.Errorf("project_root mismatch: got %q, want %q", roundTrip.ProjectRoot, p.ProjectRoot)
	}
	if roundTrip.TokenLimit != p.TokenLimit {
		t.Errorf("token_limit mismatch: got %d, want %d", roundTrip.TokenLimit, p.TokenLimit)
	}
	if roundTrip.EstimatedTokens != p.EstimatedTokens {
		t.Errorf("estimated_tokens mismatch: got %d, want %d", roundTrip.EstimatedTokens, p.EstimatedTokens)
	}
	if len(roundTrip.Files) != len(p.Files) {
		t.Errorf("files count mismatch: got %d, want %d", len(roundTrip.Files), len(p.Files))
	}
	if len(roundTrip.SeedFiles) != len(p.SeedFiles) {
		t.Errorf("seed_files count mismatch: got %d, want %d", len(roundTrip.SeedFiles), len(p.SeedFiles))
	}
}

func TestMarkdownStableOutput(t *testing.T) {
	p := newTestPack()

	// Render twice and verify identical output.
	md1 := RenderMarkdown(p)
	md2 := RenderMarkdown(p)

	if md1 != md2 {
		t.Error("markdown output should be stable across repeated renders")
	}
}

func TestJSONStableOutput(t *testing.T) {
	p := newTestPack()

	// Render twice and verify identical output.
	json1, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("first RenderJSON failed: %v", err)
	}
	json2, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("second RenderJSON failed: %v", err)
	}

	if json1 != json2 {
		t.Error("JSON output should be stable across repeated renders")
	}
}

func TestMarkdownEstimator(t *testing.T) {
	p := newTestPack()
	est := MarkdownEstimator()
	tokens := est(p)

	if tokens <= 0 {
		t.Errorf("MarkdownEstimator should return positive tokens, got %d", tokens)
	}

	// Should be a reasonable estimate based on rendered content size.
	md := RenderMarkdown(p)
	expected := EstimateTokens(md)
	if tokens != expected {
		t.Errorf("MarkdownEstimator returned %d, expected %d from rendered content", tokens, expected)
	}
}

func TestJSONEstimator(t *testing.T) {
	p := newTestPack()
	est := JSONEstimator()
	tokens := est(p)

	if tokens <= 0 {
		t.Errorf("JSONEstimator should return positive tokens, got %d", tokens)
	}

	// Should be a reasonable estimate based on rendered content size.
	rendered, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}
	expected := EstimateTokens(rendered)
	if tokens != expected {
		t.Errorf("JSONEstimator returned %d, expected %d from rendered content", tokens, expected)
	}
}

func TestMarkdownEmptyPack(t *testing.T) {
	p := &Pack{
		ProjectRoot:     "/tmp/empty",
		TokenLimit:      1000,
		EstimatedTokens: 0,
		OmittedTokens:   0,
		SeedFiles:       []string{},
	}
	md := RenderMarkdown(p)

	if !strings.HasPrefix(md, "# Context Pack\n") {
		t.Error("empty pack should still have header")
	}
	if !strings.Contains(md, "Budget: 0/1000 tokens") {
		t.Error("empty pack should show budget")
	}
}

func TestJSONEmptyPack(t *testing.T) {
	p := &Pack{
		ProjectRoot:     "/tmp/empty",
		TokenLimit:      1000,
		EstimatedTokens: 0,
		OmittedTokens:   0,
		SeedFiles:       []string{},
	}
	output, err := RenderJSON(p)
	if err != nil {
		t.Fatalf("RenderJSON on empty pack failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("empty pack JSON is not parseable: %v", err)
	}
}

// goldenDir returns the path to testdata/golden/context-pack relative to this
// test file's location.
func goldenDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "golden", "context-pack")
}

func TestGolden_NormalBudgetMarkdown(t *testing.T) {
	path := filepath.Join(goldenDir(), "normal-budget.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	md := string(data)

	if !strings.HasPrefix(md, "# Context Pack\n") {
		t.Error("golden markdown should start with '# Context Pack'")
	}
	if !strings.Contains(md, "Seeds: src/App.tsx") {
		t.Error("golden markdown should contain seeds line")
	}
	if !strings.Contains(md, "## Files") {
		t.Error("golden markdown should contain '## Files' section")
	}
	// Normal budget should have 0 omitted tokens.
	if !strings.Contains(md, "Omitted: 0 tokens") {
		t.Error("normal budget golden should have 'Omitted: 0 tokens'")
	}
}

func TestGolden_NormalBudgetJSON(t *testing.T) {
	path := filepath.Join(goldenDir(), "normal-budget.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("parsing golden JSON: %v", err)
	}

	// Normal budget: estimated_tokens <= token_limit.
	if pack.EstimatedTokens > pack.TokenLimit {
		t.Errorf("normal budget: estimated_tokens (%d) should be <= token_limit (%d)",
			pack.EstimatedTokens, pack.TokenLimit)
	}

	// Should have files.
	if len(pack.Files) == 0 {
		t.Error("normal budget JSON should have files")
	}

	// Should have no omitted candidates at normal budget.
	if len(pack.OmittedCandidates) != 0 {
		t.Errorf("normal budget should have 0 omitted candidates, got %d", len(pack.OmittedCandidates))
	}

	// Seed file should be first.
	if len(pack.Files) > 0 && pack.Files[0].Role != "seed" {
		t.Errorf("first file should be seed, got role %q", pack.Files[0].Role)
	}
}

func TestGolden_ConstrainedBudgetMarkdown(t *testing.T) {
	path := filepath.Join(goldenDir(), "constrained-budget.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	md := string(data)

	if !strings.HasPrefix(md, "# Context Pack\n") {
		t.Error("golden markdown should start with '# Context Pack'")
	}
	// Constrained budget should have omitted files.
	if !strings.Contains(md, "## Omitted") {
		t.Error("constrained budget golden should have '## Omitted' section")
	}
	// Should report non-zero omitted tokens.
	if strings.Contains(md, "Omitted: 0 tokens") {
		t.Error("constrained budget golden should have non-zero omitted tokens")
	}
}

func TestGolden_ConstrainedBudgetJSON(t *testing.T) {
	path := filepath.Join(goldenDir(), "constrained-budget.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		t.Fatalf("parsing golden JSON: %v", err)
	}

	// Constrained budget should have omitted candidates.
	if len(pack.OmittedCandidates) == 0 {
		t.Error("constrained budget should have omitted candidates")
	}

	// Omitted tokens should be > 0.
	if pack.OmittedTokens == 0 {
		t.Error("constrained budget should have non-zero omitted tokens")
	}

	// Seed file should still be present.
	if len(pack.Files) == 0 {
		t.Fatal("constrained budget should have at least the seed file")
	}
	if pack.Files[0].Role != "seed" {
		t.Errorf("first file should be seed, got role %q", pack.Files[0].Role)
	}
}
