package rules

import "testing"

func softRule(id, useWhen string, domain ...string) Rule {
	return Rule{ID: id, UseWhen: useWhen, Domain: domain, RuleType: RuleTypeSoft, Lifecycle: LifecycleConfirmed, Version: 1}
}

func hardRule(id, useWhen string, domain ...string) Rule {
	return Rule{ID: id, UseWhen: useWhen, Domain: domain, RuleType: RuleTypeHard, Lifecycle: LifecycleConfirmed, Version: 1}
}

func findMatch(matches []Match, id string) (Match, bool) {
	for _, m := range matches {
		if m.Rule.ID == id {
			return m, true
		}
	}
	return Match{}, false
}

func TestMatchHardRuleSurfacesWithZeroScoreOnDomainKeyword(t *testing.T) {
	rules := []Rule{
		hardRule("r-aaaaaaaa", "writing flaky e2e tests", "testing"),
	}
	// Intent has no overlap with use_when, but "testing" appears as a keyword and
	// matches the hard rule's domain → injected with zero keyword score.
	matches := MatchRules(rules, "testing", nil)
	m, ok := findMatch(matches, "r-aaaaaaaa")
	if !ok {
		t.Fatalf("hard rule not surfaced: %#v", matches)
	}
	if !m.HardInjected {
		t.Fatalf("expected hard_injected=true, got %#v", m)
	}
}

func TestMatchHardRuleAbsentWithoutDomainOrIntentOverlap(t *testing.T) {
	rules := []Rule{
		hardRule("r-aaaaaaaa", "writing flaky e2e tests", "testing"),
	}
	// No --domain flag and intent keywords don't intersect the rule's domain.
	matches := MatchRules(rules, "deploying to production", nil)
	if _, ok := findMatch(matches, "r-aaaaaaaa"); ok {
		t.Fatalf("hard rule should be absent, got %#v", matches)
	}
}

func TestMatchDomainFilterIsAnyOfNotAllOf(t *testing.T) {
	rules := []Rule{
		softRule("r-aaaaaaaa", "wiring a cobra command", "go", "cli"),
	}
	// Rule domain {go,cli}; filter {cli, deploy}. ANY-of: cli intersects → kept.
	matches := MatchRules(rules, "command", []string{"cli", "deploy"})
	if _, ok := findMatch(matches, "r-aaaaaaaa"); !ok {
		t.Fatalf("ANY-of filter should keep rule sharing one domain, got %#v", matches)
	}

	// ALL-of would require both; confirm a non-intersecting filter excludes it.
	none := MatchRules(rules, "command", []string{"python", "deploy"})
	if _, ok := findMatch(none, "r-aaaaaaaa"); ok {
		t.Fatalf("rule with no domain intersection should be excluded, got %#v", none)
	}
}

func TestMatchHardInjectionUsesDomainFilterWhenProvided(t *testing.T) {
	rules := []Rule{
		hardRule("r-aaaaaaaa", "no keyword overlap here", "testing"),
	}
	// --domain testing pins the hard rule even though intent is unrelated.
	matches := MatchRules(rules, "unrelated intent words", []string{"testing"})
	m, ok := findMatch(matches, "r-aaaaaaaa")
	if !ok || !m.HardInjected {
		t.Fatalf("hard rule should be injected via --domain, got %#v", matches)
	}
}

func TestMatchScoringAndOrdering(t *testing.T) {
	rules := []Rule{
		softRule("r-bbbbbbbb", "logs in tests", "testing"), // use_when match: "logs","tests"
		softRule("r-aaaaaaaa", "domain only", "logs"),      // domain match: "logs"
	}
	matches := MatchRules(rules, "logs tests", nil)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %#v", len(matches), matches)
	}
	// r-bbbbbbbb scores higher (use_when hits) → first.
	if matches[0].Rule.ID != "r-bbbbbbbb" {
		t.Fatalf("expected highest scorer first, got %#v", matches)
	}
	// maxRaw = 4*2 = 8; r-bbbbbbbb raw = 3+3 = 6 → 0.75.
	if matches[0].MatchScore != 0.75 {
		t.Fatalf("score = %v, want 0.75", matches[0].MatchScore)
	}
}

func TestMatchScoredHardRuleFlaggedInPlace(t *testing.T) {
	rules := []Rule{
		hardRule("r-aaaaaaaa", "writing tests with logs", "logs"),
	}
	// Keyword scores AND domain ("logs") is in the intent keywords → flagged in
	// place, not duplicated.
	matches := MatchRules(rules, "tests logs", nil)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one entry (no duplicate), got %d: %#v", len(matches), matches)
	}
	if !matches[0].HardInjected {
		t.Fatalf("scored hard rule should be flagged hard_injected, got %#v", matches[0])
	}
	if matches[0].MatchScore <= 0 {
		t.Fatalf("scored hard rule should retain its keyword score, got %v", matches[0].MatchScore)
	}
}
