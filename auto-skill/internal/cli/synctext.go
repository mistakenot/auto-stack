package cli

import "github.com/mistakenot/auto-skill/internal/sync"

// shortCommit truncates a git SHA to 7 chars for human output; a short or empty
// value is returned unchanged.
func shortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// commitMove is a per-skill upstream advance derived from the plan — the lock's
// pinned commit moved this run. This is the "what changed" the user asks about.
type commitMove struct {
	Name string
	From string // short locked commit ("" when the skill was previously unresolved)
	To   string // short target commit
}

// arrow renders a commit move as "from → to", or "→ to" for a first resolution.
func (m commitMove) arrow() string {
	if m.From == "" {
		return "→ " + m.To
	}
	return m.From + " → " + m.To
}

// planMoves returns the skills whose locked commit advanced this run. Skills
// that errored or are unavailable are excluded — their diagnostics go to stderr
// — so a move is always a real, applied upstream change.
func planMoves(plan []sync.SkillPlan) []commitMove {
	var moves []commitMove
	for i := range plan {
		p := &plan[i]
		if p.Action == sync.ActionError || p.Action == sync.ActionUnavailable {
			continue
		}
		if p.TargetCommit != "" && p.TargetCommit != p.LockedCommit {
			moves = append(moves, commitMove{
				Name: p.Name,
				From: shortCommit(p.LockedCommit),
				To:   shortCommit(p.TargetCommit),
			})
		}
	}
	return moves
}

// planActive counts the skills a run actually acted on (excluding errored /
// unavailable ones), used as the "N checked" denominator in human summaries.
func planActive(plan []sync.SkillPlan) int {
	n := 0
	for i := range plan {
		switch plan[i].Action {
		case sync.ActionError, sync.ActionUnavailable:
			// reported on stderr; not part of the checked total
		default:
			n++
		}
	}
	return n
}

// staleLabel renders a StaleItem's identity as "target/skill" (or just "skill").
func staleLabel(s *sync.StaleItem) string {
	if s.Target != "" {
		return s.Target + "/" + s.Skill
	}
	return s.Skill
}
