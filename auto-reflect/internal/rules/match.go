package rules

import (
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

// Match scores rules against intent keywords, optionally pre-filtered by an
// ANY-of domain intersection, then injects domain-matching hard rules.
//
//   - domainFilter (the --domain flag): when non-empty, a rule is kept only if
//     its domain intersects the (normalized, deduped) filter list (ANY-of).
//   - scoring: use_when contributes 3 points per matched keyword, domain 1 point
//     per matched keyword; the raw score is normalized by 4*len(keywords).
//   - ordering: score DESC, then id ASC.
//   - hard injection: the match set is the domain filter when provided, else the
//     intent keywords; any hard rule whose domain intersects that set is included
//     regardless of keyword score and flagged HardInjected. A hard rule already
//     present in the scored results is flagged in place rather than duplicated.
func MatchRules(rules []Rule, intent string, domainFilter []string) []Match {
	keywords := NormalizeKeywords(intent)
	filter := normalizeDomainFilter(domainFilter)

	// Determine the candidate set under the ANY-of domain filter.
	candidates := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if len(filter) > 0 && !domainsIntersect(r.Domain, filter) {
			continue
		}
		candidates = append(candidates, r)
	}

	maxRaw := float64(4 * len(keywords))
	scored := make([]Match, 0, len(candidates))
	inResults := make(map[string]int) // rule id -> index in scored
	for _, r := range candidates {
		raw := scoreRule(r, keywords)
		if raw <= 0 {
			continue
		}
		score := 0.0
		if maxRaw > 0 {
			score = raw / maxRaw
		}
		scored = append(scored, Match{Rule: r, MatchScore: score})
		inResults[r.ID] = len(scored) - 1
	}

	// Hard-rule injection set: --domain when provided, else intent keywords.
	injectionSet := filter
	if len(injectionSet) == 0 {
		injectionSet = keywords
	}
	for _, r := range rules {
		if r.RuleType != RuleTypeHard {
			continue
		}
		if !domainsIntersect(r.Domain, injectionSet) {
			continue
		}
		if idx, ok := inResults[r.ID]; ok {
			scored[idx].HardInjected = true
			continue
		}
		scored = append(scored, Match{Rule: r, MatchScore: 0, HardInjected: true})
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

// scoreRule sums keyword hits: use_when substring = 3, any domain tag substring
// = 1 (counted once per keyword).
func scoreRule(r Rule, keywords []string) float64 {
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
