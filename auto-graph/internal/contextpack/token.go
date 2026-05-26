package contextpack

import "unicode/utf8"

// EstimateTokens returns a deterministic token estimate for the given string.
// The formula is max(1, ceil(len([]rune(s)) / 4)).
// An empty string returns 0.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := utf8.RuneCountInString(s)
	return (n + 3) / 4 // ceil(n/4)
}

// EstimateContentTokens estimates the token cost of a file's content.
func EstimateContentTokens(content string) int {
	return EstimateTokens(content)
}

// BudgetFits returns true if adding additionalTokens would keep the total
// at or below the limit.
func BudgetFits(currentTokens, additionalTokens, limit int) bool {
	return currentTokens+additionalTokens <= limit
}

// MinSeedBudget estimates the minimum token budget required to render a pack
// containing only the given seed contents (with no non-seed candidates).
// The formatOverhead parameter accounts for the estimated overhead of the
// selected output format's metadata and structure wrapping the seed content.
func MinSeedBudget(seedContents []string, formatOverhead int) int {
	total := formatOverhead
	for _, content := range seedContents {
		total += EstimateTokens(content)
	}
	return total
}
