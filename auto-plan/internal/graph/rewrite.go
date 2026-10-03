package graph

import (
	"maps"
	"regexp"

	"github.com/mistakenot/auto-plan/internal/schema"
)

// proseRefRE matches a `[[…]]` reference in Markdown prose.
var proseRefRE = regexp.MustCompile(`\[\[([^\[\]\n]*)\]\]`)

// ProseRefRE is the `[[…]]` reference pattern lint and RewritePlanRefs share.
func ProseRefRE() *regexp.Regexp { return proseRefRE }

// ShortRefFunc decides what a shorthand prose reference `[[NNN:id]]` becomes
// when its plan is renumbered. It returns the replacement reference (or ""
// to leave it unchanged), and ambiguous when the reference cannot be told
// apart between plans sharing the number, so rewriting it could change its
// meaning.
type ShortRefFunc func(number, id string) (replacement string, ambiguous bool)

// RewritePlanRefs rewrites every reference to plan oldID so it names newID:
// qualified edge targets (`old:x` → `new:x`), the registry's PlanRef fields
// (plan.epic, child.plan, rail.deferred) and qualified `[[old:x]]` prose
// references in text fields. Shorthand prose references `[[NNN:x]]` are
// passed to short (when non-nil). It reports whether anything changed, and
// the location of every shorthand reference short called ambiguous. Nodes
// and edges are replaced, never edited in place, so a clone shares nothing
// that changes.
func (g *Graph) RewritePlanRefs(oldID, newID string, short ShortRefFunc) (changed bool, ambiguous []string) {
	for i, e := range g.Edges {
		if p, id, q := ParseRef(e.To); q && p == oldID {
			e.To = Qualify(newID, id)
			g.Edges[i] = e
			changed = true
		}
	}
	for i, n := range g.Nodes {
		nt, ok := schema.Registry.Node(n.Type)
		if !ok {
			continue
		}
		fields, did, amb := rewriteFields(nt.Fields, n.Fields, oldID, newID, short, NodePath(n.ID)+".fields")
		ambiguous = append(ambiguous, amb...)
		if did {
			n.Fields = fields
			g.Nodes[i] = n
			changed = true
		}
	}
	return changed, ambiguous
}

// rewriteFields returns a copy of values with plan references rewritten,
// whether anything changed (values itself is never modified), and the paths
// of ambiguous shorthand references found under path.
func rewriteFields(specs []schema.FieldSpec, values map[string]any, oldID, newID string, short ShortRefFunc, path string) (map[string]any, bool, []string) {
	out := values
	changed := false
	var ambiguous []string
	set := func(k string, v any) {
		if !changed {
			out = maps.Clone(values)
			changed = true
		}
		out[k] = v
	}
	for i := range specs {
		f := &specs[i]
		val, present := values[f.Name]
		if !present {
			continue
		}
		switch {
		case f.PlanRef && f.Kind == schema.KindString:
			if s, ok := val.(string); ok && s == oldID {
				set(f.Name, newID)
			}
		case f.PlanRef && f.Kind == schema.KindList:
			arr, ok := val.([]any)
			if !ok {
				continue
			}
			next := make([]any, len(arr))
			did := false
			for j, item := range arr {
				next[j] = item
				if s, ok := item.(string); ok && s == oldID {
					next[j] = newID
					did = true
				}
			}
			if did {
				set(f.Name, next)
			}
		case f.Kind == schema.KindText:
			s, ok := val.(string)
			if !ok {
				continue
			}
			r, amb := rewriteProse(s, oldID, newID, short)
			for _, ref := range amb {
				ambiguous = append(ambiguous, path+"."+f.Name+": [["+ref+"]]")
			}
			if r != s {
				set(f.Name, r)
			}
		case f.Kind == schema.KindObject:
			m, ok := val.(map[string]any)
			if !ok {
				continue
			}
			next, did, amb := rewriteFields(f.Fields, m, oldID, newID, short, path+"."+f.Name)
			ambiguous = append(ambiguous, amb...)
			if did {
				set(f.Name, next)
			}
		}
	}
	return out, changed, ambiguous
}

func rewriteProse(s, oldID, newID string, short ShortRefFunc) (string, []string) {
	var ambiguous []string
	out := proseRefRE.ReplaceAllStringFunc(s, func(m string) string {
		ref := m[2 : len(m)-2]
		p, id, q := ParseRef(ref)
		switch {
		case !q:
			return m
		case p == oldID:
			return "[[" + Qualify(newID, id) + "]]"
		case short != nil && len(p) == 3:
			repl, amb := short(p, id)
			if amb {
				ambiguous = append(ambiguous, ref)
				return m
			}
			if repl != "" {
				return "[[" + repl + "]]"
			}
		}
		return m
	})
	return out, ambiguous
}
