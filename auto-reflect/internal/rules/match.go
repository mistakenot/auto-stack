package rules

import (
	"math"
	"sort"
	"strings"
)

// Match is one rule surfaced by Match, with its normalized score and whether it
// was injected purely because it is a domain-matching hard rule.
type Match struct {
	Rule         Rule
	MatchScore   float64
	HardInjected bool
}

// NormalizeKeywords lowercases, splits on whitespace, and dedupes a free-text
// query into keyword tokens, preserving first-seen order.
func NormalizeKeywords(query string) []string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	seen := make(map[string]struct{}, len(parts))
	keywords := make([]string, 0, len(parts))
	for _, p := range parts {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		keywords = append(keywords, p)
	}
	return keywords
}

// normalizeDomainFilter lowercases/trims/dedupes the --domain flag list.
func normalizeDomainFilter(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		normalized := strings.ToLower(strings.TrimSpace(d))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// Match scores rules against intent keywords, applies a non-excluding IDF-weighted
// in-domain boost from the domain filter, then injects domain-matching hard rules.
//
//   - domainFilter (the --domain flag): a non-excluding signal. Off-domain rules
//     are no longer dropped; instead, a rule whose domain intersects the
//     (normalized, deduped) filter is boosted by Σ IDF(tag) over the intersection
//     (rare in-domain tags lift more than near-universal ones). An empty filter
//     contributes no boost.
//   - scoring: use_when contributes 3 points per matched keyword, domain 1 point
//     per matched keyword; the domain boost is added to that raw score, then the
//     composite is normalized by max(1, 4*len(keywords)). Because the normalizer is
//     a per-call constant, MatchScore is order-identical to the raw composite (it
//     may exceed 1.0 when a boost fires).
//   - ordering: score DESC, then id ASC.
//   - hard injection: the match set is the domain filter when provided, else the
//     intent keywords; any hard rule whose domain intersects that set is included
//     regardless of keyword score and flagged HardInjected. A hard rule already
//     present in the scored results is flagged in place rather than duplicated.
//   - lifecycle: stale rules are never surfaced (not even as hard injections);
//     draft rules are surfaced only when includeDrafts is set. This filter applies
//     to both the scored candidate set and the hard-injection set.
func MatchRules(rules []Rule, intent string, domainFilter []string, includeDrafts bool) []Match {
	keywords := NormalizeKeywords(intent)
	filter := normalizeDomainFilter(domainFilter)
	idf := tagIDF(rules)

	// Candidates: NO domain exclusion — every surfaceable rule is a candidate.
	// Rules whose lifecycle makes them non-surfaceable (stale/enforced always;
	// draft unless asked) are still excluded.
	candidates := make([]Rule, 0, len(rules))
	for i := range rules {
		if !surfaceableLifecycle(rules[i].Lifecycle, includeDrafts) {
			continue
		}
		candidates = append(candidates, rules[i])
	}

	maxRaw := float64(4 * len(keywords))
	if maxRaw == 0 {
		maxRaw = 1 // keep boost-only ordering meaningful when intent has no keywords
	}
	scored := make([]Match, 0, len(candidates))
	inResults := make(map[string]int) // rule id -> index in scored
	for i := range candidates {
		raw := scoreRule(&candidates[i], keywords) +
			domainBoost(candidates[i].Domain, filter, idf)
		if raw <= 0 {
			continue
		}
		score := raw / maxRaw
		scored = append(scored, Match{Rule: candidates[i], MatchScore: score})
		inResults[candidates[i].ID] = len(scored) - 1
	}

	// Hard-rule injection set: --domain when provided, else intent keywords.
	injectionSet := filter
	if len(injectionSet) == 0 {
		injectionSet = keywords
	}
	for i := range rules {
		r := &rules[i]
		if r.RuleType != RuleTypeHard {
			continue
		}
		if !surfaceableLifecycle(r.Lifecycle, includeDrafts) {
			continue
		}
		if !domainsIntersect(r.Domain, injectionSet) {
			continue
		}
		if idx, ok := inResults[r.ID]; ok {
			scored[idx].HardInjected = true
			continue
		}
		scored = append(scored, Match{Rule: *r, MatchScore: 0, HardInjected: true})
		inResults[r.ID] = len(scored) - 1
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].MatchScore != scored[j].MatchScore {
			return scored[i].MatchScore > scored[j].MatchScore
		}
		return scored[i].Rule.ID < scored[j].Rule.ID
	})

	return scored
}

// surfaceableLifecycle reports whether a rule with the given lifecycle may be
// surfaced by retrieval. Stale and enforced rules are never surfaceable (stale is
// retired; enforced has graduated into a static lint check, so re-surfacing it as
// guidance would be redundant). Draft rules surface only when includeDrafts is
// set. Any other value (confirmed, or an empty/legacy lifecycle) is treated as
// surfaceable so unset rules are not accidentally hidden.
func surfaceableLifecycle(lifecycle string, includeDrafts bool) bool {
	switch lifecycle {
	case LifecycleStale, LifecycleEnforced:
		return false
	case LifecycleDraft:
		return includeDrafts
	default:
		return true
	}
}

// tagIDF builds log(N/df) over the rule set's domain-tag vocabulary, so a rare
// in-domain tag boosts far more than a near-universal one (`go` is on ~78% of rules).
func tagIDF(rules []Rule) map[string]float64 {
	n := float64(len(rules))
	df := map[string]int{}
	for i := range rules {
		seen := map[string]struct{}{}
		for _, d := range rules[i].Domain {
			t := strings.ToLower(strings.TrimSpace(d))
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue // df counts rules, once per tag per rule
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	idf := make(map[string]float64, len(df))
	for t, c := range df {
		idf[t] = math.Log(n / float64(c))
	}
	return idf
}

// domainBoost = Σ IDF(tag) over rule.Domain ∩ filter. Zero when no filter — so
// callers that pass no domain (e.g. dedupe) get the pure lexical score.
func domainBoost(domain, filter []string, idf map[string]float64) float64 {
	if len(filter) == 0 {
		return 0
	}
	want := map[string]struct{}{}
	for _, f := range filter {
		want[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
	}
	b := 0.0
	for _, d := range domain {
		t := strings.ToLower(strings.TrimSpace(d))
		if _, ok := want[t]; ok {
			b += idf[t]
		}
	}
	return b
}

// scoreRule sums keyword hits: use_when substring = 3, any domain tag substring
// = 1 (counted once per keyword).
func scoreRule(r *Rule, keywords []string) float64 {
	useWhen := strings.ToLower(r.UseWhen)
	domain := make([]string, 0, len(r.Domain))
	for _, d := range r.Domain {
		domain = append(domain, strings.ToLower(d))
	}

	raw := 0.0
	for _, kw := range keywords {
		if strings.Contains(useWhen, kw) {
			raw += 3
		}
		for _, d := range domain {
			if strings.Contains(d, kw) {
				raw += 1
				break
			}
		}
	}
	return raw
}

// DomainsIntersect reports whether any normalized domain tag in domain appears in
// set. Exported for callers (e.g. consolidate dedupe) that replicate the old
// domain pre-filter; it mirrors the matcher's internal intersection check exactly
// (case-insensitive, trimmed).
func DomainsIntersect(domain, set []string) bool {
	return domainsIntersect(domain, set)
}

// domainsIntersect reports whether any normalized domain tag appears in the set.
func domainsIntersect(domain, set []string) bool {
	if len(domain) == 0 || len(set) == 0 {
		return false
	}
	want := make(map[string]struct{}, len(set))
	for _, s := range set {
		want[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	for _, d := range domain {
		if _, ok := want[strings.ToLower(strings.TrimSpace(d))]; ok {
			return true
		}
	}
	return false
}
