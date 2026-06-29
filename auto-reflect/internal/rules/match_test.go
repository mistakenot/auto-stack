package rules

import "testing"

func softRule(id, useWhen string, domain ...string) Rule {
	return Rule{ID: id, UseWhen: useWhen, Domain: domain, RuleType: RuleTypeSoft, Lifecycle: LifecycleConfirmed, Version: 1}
}

func hardRule(id, useWhen string, domain ...string) Rule {
	return Rule{ID: id, UseWhen: useWhen, Domain: domain, RuleType: RuleTypeHard, Lifecycle: LifecycleConfirmed, Version: 1}
}

func ruleWithLifecycle(id, useWhen, ruleType, lifecycle string, domain ...string) Rule {
	return Rule{ID: id, UseWhen: useWhen, Domain: domain, RuleType: ruleType, Lifecycle: lifecycle, Version: 1}
}

func findMatch(matches []Match, id string) (Match, bool) {
	for i := range matches {
		if matches[i].Rule.ID == id {
			return matches[i], true
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
	matches := MatchRules(rules, "testing", nil, true)
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
	matches := MatchRules(rules, "deploying to production", nil, true)
	if _, ok := findMatch(matches, "r-aaaaaaaa"); ok {
		t.Fatalf("hard rule should be absent, got %#v", matches)
	}
}

func TestMatchDomainFilterIsAnyOfNotAllOf(t *testing.T) {
	rules := []Rule{
		softRule("r-aaaaaaaa", "wiring a cobra command", "go", "cli"),
	}
	// Rule domain {go,cli}; filter {cli, deploy}. ANY-of intersection: cli
	// intersects → the rule is boosted (and surfaces).
	matches := MatchRules(rules, "command", []string{"cli", "deploy"}, true)
	if _, ok := findMatch(matches, "r-aaaaaaaa"); !ok {
		t.Fatalf("intersecting filter should surface the rule, got %#v", matches)
	}

	// Domain filter is a non-excluding boost: a non-intersecting filter no longer
	// drops the rule — it still surfaces (lexically), just unboosted.
	none := MatchRules(rules, "command", []string{"python", "deploy"}, true)
	if _, ok := findMatch(none, "r-aaaaaaaa"); !ok {
		t.Fatalf("non-intersecting filter must NOT exclude the rule (boost, not gate), got %#v", none)
	}
}

func TestMatchHardInjectionUsesDomainFilterWhenProvided(t *testing.T) {
	rules := []Rule{
		hardRule("r-aaaaaaaa", "no keyword overlap here", "testing"),
	}
	// --domain testing pins the hard rule even though intent is unrelated.
	matches := MatchRules(rules, "unrelated intent words", []string{"testing"}, true)
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
	matches := MatchRules(rules, "logs tests", nil, true)
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
	matches := MatchRules(rules, "tests logs", nil, true)
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

func TestMatchStaleRuleNeverSurfaces(t *testing.T) {
	// A soft stale rule that scores on keywords, and a hard stale rule that would
	// otherwise be domain-injected. Neither may surface, with or without drafts.
	rules := []Rule{
		ruleWithLifecycle("r-aaaaaaaa", "writing tests with logs", RuleTypeSoft, LifecycleStale, "logs"),
		ruleWithLifecycle("r-bbbbbbbb", "no keyword overlap", RuleTypeHard, LifecycleStale, "logs"),
	}
	for _, includeDrafts := range []bool{true, false} {
		matches := MatchRules(rules, "tests logs", []string{"logs"}, includeDrafts)
		if _, ok := findMatch(matches, "r-aaaaaaaa"); ok {
			t.Fatalf("stale soft rule surfaced (includeDrafts=%v): %#v", includeDrafts, matches)
		}
		if _, ok := findMatch(matches, "r-bbbbbbbb"); ok {
			t.Fatalf("stale hard rule injected (includeDrafts=%v): %#v", includeDrafts, matches)
		}
	}
}

func TestMatchEnforcedRuleNeverSurfaces(t *testing.T) {
	// An enforced soft rule that scores on keywords, and an enforced hard rule that
	// would otherwise be domain-injected. Neither may surface, with or without
	// drafts: an enforced rule has graduated into a static lint check.
	rules := []Rule{
		ruleWithLifecycle("r-aaaaaaaa", "writing tests with logs", RuleTypeSoft, LifecycleEnforced, "logs"),
		ruleWithLifecycle("r-bbbbbbbb", "no keyword overlap", RuleTypeHard, LifecycleEnforced, "logs"),
	}
	for _, includeDrafts := range []bool{true, false} {
		matches := MatchRules(rules, "tests logs", []string{"logs"}, includeDrafts)
		if _, ok := findMatch(matches, "r-aaaaaaaa"); ok {
			t.Fatalf("enforced soft rule surfaced (includeDrafts=%v): %#v", includeDrafts, matches)
		}
		if _, ok := findMatch(matches, "r-bbbbbbbb"); ok {
			t.Fatalf("enforced hard rule injected (includeDrafts=%v): %#v", includeDrafts, matches)
		}
	}
}

func TestMatchDraftRuleRespectsIncludeDrafts(t *testing.T) {
	rules := []Rule{
		ruleWithLifecycle("r-aaaaaaaa", "writing tests with logs", RuleTypeSoft, LifecycleDraft, "logs"),
	}
	// Included when includeDrafts=true.
	if _, ok := findMatch(MatchRules(rules, "tests logs", nil, true), "r-aaaaaaaa"); !ok {
		t.Fatal("draft rule should surface when includeDrafts=true")
	}
	// Excluded when includeDrafts=false.
	if _, ok := findMatch(MatchRules(rules, "tests logs", nil, false), "r-aaaaaaaa"); ok {
		t.Fatal("draft rule should be excluded when includeDrafts=false")
	}
}

func TestMatchDraftHardRuleRespectsIncludeDrafts(t *testing.T) {
	rules := []Rule{
		ruleWithLifecycle("r-aaaaaaaa", "no keyword overlap", RuleTypeHard, LifecycleDraft, "logs"),
	}
	// Domain-injected only when drafts are included.
	if m, ok := findMatch(MatchRules(rules, "unrelated", []string{"logs"}, true), "r-aaaaaaaa"); !ok || !m.HardInjected {
		t.Fatal("draft hard rule should be injected when includeDrafts=true")
	}
	if _, ok := findMatch(MatchRules(rules, "unrelated", []string{"logs"}, false), "r-aaaaaaaa"); ok {
		t.Fatal("draft hard rule should be excluded when includeDrafts=false")
	}
}

func TestMatchConfirmedAndUnsetLifecycleAlwaysSurface(t *testing.T) {
	rules := []Rule{
		ruleWithLifecycle("r-aaaaaaaa", "writing tests with logs", RuleTypeSoft, LifecycleConfirmed, "logs"),
		ruleWithLifecycle("r-bbbbbbbb", "writing tests with logs", RuleTypeSoft, "", "logs"), // legacy/unset
	}
	// Even with drafts excluded, confirmed and unset lifecycles surface.
	matches := MatchRules(rules, "tests logs", nil, false)
	if _, ok := findMatch(matches, "r-aaaaaaaa"); !ok {
		t.Fatalf("confirmed rule should always surface: %#v", matches)
	}
	if _, ok := findMatch(matches, "r-bbbbbbbb"); !ok {
		t.Fatalf("unset-lifecycle rule should surface (not hidden as draft/stale): %#v", matches)
	}
}
