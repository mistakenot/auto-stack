package rules

import (
	"sort"
	"strings"
)

type scoredRule struct {
	rawScore float64
	rule     Rule
}

func normalizeKeywords(query string) []string {
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

func rankMatches(rules []Rule, keywords []string) []Match {
	if len(keywords) == 0 {
		return []Match{}
	}

	scored := make([]scoredRule, 0, len(rules))
	maxRaw := float64(3 * len(keywords))
	for _, rule := range rules {
		content := strings.ToLower(rule.Content)
		category := strings.ToLower(rule.Category)
		tags := make([]string, 0, len(rule.Tags))
		for _, tag := range rule.Tags {
			tags = append(tags, strings.ToLower(tag))
		}

		raw := 0.0
		for _, keyword := range keywords {
			if strings.Contains(content, keyword) {
				raw += 3
			}
			if strings.Contains(category, keyword) {
				raw += 2
			}
			for _, tag := range tags {
				if strings.Contains(tag, keyword) {
					raw += 1
					break
				}
			}
		}

		if raw > 0 {
			scored = append(scored, scoredRule{rawScore: raw, rule: rule})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].rawScore == scored[j].rawScore {
			return scored[i].rule.ID < scored[j].rule.ID
		}
		return scored[i].rawScore > scored[j].rawScore
	})

	out := make([]Match, 0, len(scored))
	for _, score := range scored {
		out = append(out, Match{
			ID:         score.rule.ID,
			Content:    score.rule.Content,
			Category:   score.rule.Category,
			Tags:       append([]string{}, score.rule.Tags...),
			MatchScore: score.rawScore / maxRaw,
		})
	}
	return out
}
