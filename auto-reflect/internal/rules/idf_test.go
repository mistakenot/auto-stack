package rules

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

// idf_test.go pins the IDF core (tagIDF, domainBoost) with a hand-computed table
// plus testing/quick property tests over randomly generated corpora and filters.
// No new dependency: only stdlib testing/quick, deterministically seeded so runs
// are reproducible (no wall-clock seeding).

const idfFloatTol = 1e-9

func idfAlmostEqual(a, b float64) bool { return math.Abs(a-b) <= idfFloatTol }

// idfQuickConfig returns a deterministically-seeded testing/quick config so the
// property runs are reproducible.
func idfQuickConfig(seed int64) *quick.Config {
	return &quick.Config{MaxCount: 300, Rand: rand.New(rand.NewSource(seed))}
}

// reversedStrings returns a copy of s in reverse order.
func reversedStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

// domainDocFreq independently recomputes per-tag document frequency (rules-per-tag,
// counted once per rule) so property tests can reason about rarity.
func domainDocFreq(rules []Rule) map[string]int {
	df := map[string]int{}
	for i := range rules {
		seen := map[string]struct{}{}
		for _, d := range rules[i].Domain {
			t := strings.ToLower(strings.TrimSpace(d))
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	return df
}

// --- Random scenario generator ---------------------------------------------

var idfTagVocab = []string{"go", "cli", "testing", "rare", "deploy", "logs", "etl"}
var idfWordVocab = []string{"writing", "tests", "logs", "deploy", "command", "flaky", "wiring"}

// idfScenario is a randomly generated corpus + domain filter + intent. Generated
// domains are deduped (each vocab tag is considered at most once per rule) to
// mirror the validated rule contract: ValidateRule rejects duplicate domain tags.
type idfScenario struct {
	rules  []Rule
	filter []string
	intent string
}

// Generate implements quick.Generator for idfScenario.
func (idfScenario) Generate(rng *rand.Rand, _ int) reflect.Value {
	n := rng.Intn(6) + 1 // 1..6 rules
	rs := make([]Rule, 0, n)
	for i := range n {
		dom := make([]string, 0, len(idfTagVocab))
		for _, tag := range idfTagVocab {
			if rng.Intn(2) == 0 {
				dom = append(dom, tag) // deduped: each vocab tag added at most once
			}
		}
		rs = append(rs, Rule{
			ID:        fmt.Sprintf("r-%08d", i),
			UseWhen:   idfWordVocab[rng.Intn(len(idfWordVocab))],
			Domain:    dom,
			RuleType:  RuleTypeSoft,
			Lifecycle: LifecycleConfirmed,
			Version:   1,
		})
	}
	filter := make([]string, 0, len(idfTagVocab))
	for _, tag := range idfTagVocab {
		if rng.Intn(3) == 0 {
			filter = append(filter, tag)
		}
	}
	intent := idfWordVocab[rng.Intn(len(idfWordVocab))]
	return reflect.ValueOf(idfScenario{rules: rs, filter: filter, intent: intent})
}

// --- Hand-computed tables ---------------------------------------------------

func handCorpus() []Rule {
	// N=4; df: go=4 (universal), testing=2, cli=1, rare=1.
	return []Rule{
		softRule("r-00000001", "x", "go", "testing"),
		softRule("r-00000002", "x", "go", "cli"),
		softRule("r-00000003", "x", "go", "testing"),
		softRule("r-00000004", "x", "go", "rare"),
	}
}

func TestTagIDFHandComputedTable(t *testing.T) {
	idf := tagIDF(handCorpus())
	want := map[string]float64{
		"go":      math.Log(1.0), // df==N (universal) ⇒ IDF 0
		"testing": math.Log(4.0 / 2.0),
		"cli":     math.Log(4.0 / 1.0),
		"rare":    math.Log(4.0 / 1.0),
	}
	for tag, w := range want {
		if !idfAlmostEqual(idf[tag], w) {
			t.Errorf("tagIDF[%q] = %v, want %v", tag, idf[tag], w)
		}
	}
	if !idfAlmostEqual(idf["go"], 0) {
		t.Errorf("universal tag go must have IDF 0, got %v", idf["go"])
	}
}

func TestDomainBoostHandComputedTable(t *testing.T) {
	idf := tagIDF(handCorpus())
	cases := []struct {
		name   string
		domain []string
		filter []string
		want   float64
	}{
		{"single rare-ish in-domain tag", []string{"go", "testing"}, []string{"testing"}, math.Log(2)},
		{"universal tag boosts zero", []string{"go", "testing"}, []string{"go"}, 0},
		{"two matched tags sum (go=0 + rare)", []string{"go", "rare"}, []string{"go", "rare"}, math.Log(4)},
		{"disjoint filter ⇒ 0", []string{"cli"}, []string{"deploy"}, 0},
		{"empty filter ⇒ 0", []string{"go", "cli"}, nil, 0},
	}
	for _, c := range cases {
		got := domainBoost(c.domain, c.filter, idf)
		if !idfAlmostEqual(got, c.want) {
			t.Errorf("%s: domainBoost(%v, %v) = %v, want %v", c.name, c.domain, c.filter, got, c.want)
		}
	}
}

// --- Property tests (testing/quick) ----------------------------------------

// IDF is >= 0 for every tag, a universal tag (df == N) ⇒ IDF = 0, and IDF is
// strictly monotonic in rarity (lower df ⇒ higher IDF).
func TestTagIDFInvariants(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		n := len(s.rules)
		df := domainDocFreq(s.rules)
		for tag, v := range idf {
			if v < -idfFloatTol {
				return false // IDF >= 0
			}
			if df[tag] == n && math.Abs(v) > idfFloatTol {
				return false // universal tag ⇒ IDF 0
			}
		}
		tags := make([]string, 0, len(idf))
		for tag := range idf {
			tags = append(tags, tag)
		}
		for _, a := range tags {
			for _, b := range tags {
				if df[a] < df[b] && !(idf[a] > idf[b]) {
					return false // rarer tag must have strictly higher IDF
				}
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(1)); err != nil {
		t.Error(err)
	}
}

// boost is >= 0 always; an empty filter ⇒ 0; a disjoint filter (no overlap) ⇒ 0.
func TestDomainBoostBoundsAndZeroCases(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		for i := range s.rules {
			dom := s.rules[i].Domain
			if domainBoost(dom, s.filter, idf) < -idfFloatTol {
				return false // boost >= 0
			}
			if domainBoost(dom, nil, idf) != 0 {
				return false // empty filter ⇒ 0
			}
			// Tags guaranteed absent from the vocab ⇒ disjoint ⇒ 0.
			if domainBoost(dom, []string{"zzz-absent", "qqq-absent"}, idf) != 0 {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(2)); err != nil {
		t.Error(err)
	}
}

// Adding a matching tag to the filter never decreases the boost (monotonic under
// added matching tags).
func TestDomainBoostMonotonicUnderAddedTag(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		for i := range s.rules {
			dom := s.rules[i].Domain
			base := domainBoost(dom, s.filter, idf)
			for _, extra := range idfTagVocab {
				bigger := append(append([]string{}, s.filter...), extra)
				if domainBoost(dom, bigger, idf) < base-idfFloatTol {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(3)); err != nil {
		t.Error(err)
	}
}

// Boost is invariant to case, surrounding whitespace, and tag order in both the
// filter and rule.Domain.
func TestDomainBoostNormalizationInvariance(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		for i := range s.rules {
			dom := s.rules[i].Domain
			base := domainBoost(dom, s.filter, idf)
			recased := make([]string, len(s.filter))
			for j, f := range s.filter {
				recased[j] = "  " + strings.ToUpper(f) + " "
			}
			if !idfAlmostEqual(domainBoost(dom, recased, idf), base) {
				return false
			}
			if !idfAlmostEqual(domainBoost(dom, reversedStrings(s.filter), idf), base) {
				return false
			}
			if !idfAlmostEqual(domainBoost(reversedStrings(dom), s.filter, idf), base) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(4)); err != nil {
		t.Error(err)
	}
}

// A tag repeated in the filter does not double-count (the filter is treated as a
// set).
func TestDomainBoostFilterDuplicateIdempotent(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		for i := range s.rules {
			dom := s.rules[i].Domain
			base := domainBoost(dom, s.filter, idf)
			doubled := append(append([]string{}, s.filter...), s.filter...)
			if !idfAlmostEqual(domainBoost(dom, doubled, idf), base) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(5)); err != nil {
		t.Error(err)
	}
}

// Over realistic (deduped) domains — the validated rule contract — the boost
// equals Σ IDF over the set intersection of rule.Domain and the filter.
func TestDomainBoostDeduplicatedDomainSetwise(t *testing.T) {
	prop := func(s idfScenario) bool {
		idf := tagIDF(s.rules)
		want := map[string]struct{}{}
		for _, f := range s.filter {
			want[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
		}
		for i := range s.rules {
			dom := s.rules[i].Domain
			ref := 0.0
			seen := map[string]struct{}{}
			for _, d := range dom {
				tag := strings.ToLower(strings.TrimSpace(d))
				if _, dup := seen[tag]; dup {
					continue
				}
				seen[tag] = struct{}{}
				if _, ok := want[tag]; ok {
					ref += idf[tag]
				}
			}
			if !idfAlmostEqual(domainBoost(dom, s.filter, idf), ref) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(6)); err != nil {
		t.Error(err)
	}
}

// domainBoost sums over rule.Domain positionally (it does not set-dedupe the
// domain), exactly like the frozen Python reference boost_idf_tag
// (`for d in rule.domain ...`). Duplicate domain tags are prevented upstream by
// ValidateRule (duplicate domain tag ⇒ validation error), not here. This test
// pins that contract so the Go↔Python conformance stays faithful.
func TestDomainBoostRawDuplicateDomainDoubleCountsByDesign(t *testing.T) {
	rules := []Rule{
		softRule("r-00000001", "x", "go"),
		softRule("r-00000002", "x", "go"),
		softRule("r-00000003", "x", "cli"),
	}
	idf := tagIDF(rules) // df(go)=2, N=3 ⇒ IDF(go)=log(1.5) > 0
	single := domainBoost([]string{"go"}, []string{"go"}, idf)
	dup := domainBoost([]string{"go", "go"}, []string{"go"}, idf)
	if single <= 0 {
		t.Fatalf("precondition: go must have positive IDF here, got %v", single)
	}
	if !idfAlmostEqual(dup, 2*single) {
		t.Fatalf("raw duplicate domain should double-count (faithful to Python reference): single=%v dup=%v", single, dup)
	}
}

// Anti-catastrophe (the core safety property of this task): a non-empty filter
// never EXCLUDES any rule that surfaced without a filter, and never LOWERS its
// composite MatchScore — the boost is purely additive and >= 0.
func TestMatchRulesFilterNeverLowersScore(t *testing.T) {
	prop := func(s idfScenario) bool {
		noFilter := MatchRules(s.rules, s.intent, nil, true)
		withFilter := MatchRules(s.rules, s.intent, s.filter, true)
		fScore := map[string]float64{}
		for _, m := range withFilter {
			fScore[m.Rule.ID] = m.MatchScore
		}
		for _, m := range noFilter {
			got, ok := fScore[m.Rule.ID]
			if !ok {
				return false // a filter must never exclude a rule
			}
			if got < m.MatchScore-idfFloatTol {
				return false // boost is additive ⇒ score never drops
			}
		}
		return true
	}
	if err := quick.Check(prop, idfQuickConfig(7)); err != nil {
		t.Error(err)
	}
}
