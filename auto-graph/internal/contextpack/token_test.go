package contextpack

import "testing"

func TestEstimateTokens_EmptyString(t *testing.T) {
	got := EstimateTokens("")
	if got != 0 {
		t.Errorf("EstimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_ASCII(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"a", 1},           // 1 rune -> ceil(1/4) = 1
		{"ab", 1},          // 2 runes -> ceil(2/4) = 1
		{"abc", 1},         // 3 runes -> ceil(3/4) = 1
		{"abcd", 1},        // 4 runes -> ceil(4/4) = 1
		{"abcde", 2},       // 5 runes -> ceil(5/4) = 2
		{"abcdefgh", 2},    // 8 runes -> ceil(8/4) = 2
		{"abcdefghi", 3},   // 9 runes -> ceil(9/4) = 3
		{"hello world", 3}, // 11 runes -> ceil(11/4) = 3
	}
	for _, tt := range tests {
		got := EstimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestEstimateTokens_MultibytRunes(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"日", 1},       // 1 rune (3 bytes) -> ceil(1/4) = 1
		{"日本語", 1},     // 3 runes (9 bytes) -> ceil(3/4) = 1
		{"日本語x", 1},    // 4 runes -> ceil(4/4) = 1
		{"日本語xy", 2},   // 5 runes -> ceil(5/4) = 2
		{"こんにちは世界", 2}, // 7 runes -> ceil(7/4) = 2
		{"🎉🎉🎉🎉🎉", 2}, // 5 runes (20 bytes) -> ceil(5/4) = 2
	}
	for _, tt := range tests {
		got := EstimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestEstimateTokens_MinimumIsOne(t *testing.T) {
	// Any non-empty string should return at least 1
	got := EstimateTokens("x")
	if got < 1 {
		t.Errorf("EstimateTokens(\"x\") = %d, want >= 1", got)
	}
}

func TestEstimateContentTokens(t *testing.T) {
	content := "const x = 1;\n"
	got := EstimateContentTokens(content)
	want := EstimateTokens(content)
	if got != want {
		t.Errorf("EstimateContentTokens = %d, want %d", got, want)
	}
}

func TestBudgetFits(t *testing.T) {
	tests := []struct {
		current    int
		additional int
		limit      int
		want       bool
	}{
		{0, 100, 100, true},
		{50, 50, 100, true},
		{50, 51, 100, false},
		{100, 1, 100, false},
		{0, 0, 0, true},
	}
	for _, tt := range tests {
		got := BudgetFits(tt.current, tt.additional, tt.limit)
		if got != tt.want {
			t.Errorf("BudgetFits(%d, %d, %d) = %v, want %v",
				tt.current, tt.additional, tt.limit, got, tt.want)
		}
	}
}

func TestMinSeedBudget(t *testing.T) {
	seeds := []string{
		"const x = 1;\n",  // 14 chars -> ceil(14/4) = 4
		"export default x", // 16 chars -> ceil(16/4) = 4
	}
	formatOverhead := 10
	got := MinSeedBudget(seeds, formatOverhead)
	// 10 + 4 + 4 = 18
	if got != 18 {
		t.Errorf("MinSeedBudget = %d, want 18", got)
	}
}

func TestMinSeedBudget_NoSeeds(t *testing.T) {
	got := MinSeedBudget(nil, 5)
	if got != 5 {
		t.Errorf("MinSeedBudget(nil, 5) = %d, want 5", got)
	}
}

func TestBudgetFits_SeedBudgetFailureMinimum(t *testing.T) {
	// Simulates checking if seeds fit: a small limit should not fit large seeds
	seedContent := "import React from 'react';\nexport default function App() { return <div />; }\n"
	seedTokens := EstimateTokens(seedContent)
	formatOverhead := 20
	minBudget := MinSeedBudget([]string{seedContent}, formatOverhead)

	// With a budget smaller than minimum, seeds cannot fit
	if BudgetFits(0, minBudget, minBudget-1) {
		t.Error("expected seeds to not fit in budget smaller than minimum")
	}

	// With exact minimum budget, seeds should fit
	if !BudgetFits(0, minBudget, minBudget) {
		t.Error("expected seeds to fit in exact minimum budget")
	}

	_ = seedTokens // used above via minBudget
}

func TestBudgetFits_SelectedFormatAccounting(t *testing.T) {
	// Markdown and JSON have different overheads for the same content.
	// This test verifies that the budget helpers can be composed to
	// account for format-specific overhead.
	content := "const x = 1;\n"
	contentTokens := EstimateTokens(content)

	markdownOverhead := 50  // simulated markdown metadata cost
	jsonOverhead := 80      // simulated JSON metadata cost (keys, braces, etc.)

	markdownTotal := contentTokens + markdownOverhead
	jsonTotal := contentTokens + jsonOverhead

	limit := 60

	// Content fits in markdown budget but not JSON
	if !BudgetFits(0, markdownTotal, limit+markdownTotal) {
		t.Error("expected content to fit in markdown budget")
	}

	// With a tight limit, JSON overhead pushes it over
	if BudgetFits(0, jsonTotal, limit) {
		t.Error("expected JSON total to exceed tight budget")
	}
}
