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

// RewritePlanRefs rewrites every reference to plan oldID so it names newID:
// qualified edge targets (`old:x` → `new:x`), the registry's PlanRef fields
// (plan.epic, child.plan, rail.deferred) and qualified `[[old:x]]` prose
// references in text fields. When oldShort is set, shorthand prose
// references `[[oldShort:x]]` are rewritten to `[[newShort:x]]` too (the
// caller sets it only when the number named this plan unambiguously). It
// reports whether anything changed. Nodes and edges are replaced, never
// edited in place, so a clone shares nothing that changes.
func (g *Graph) RewritePlanRefs(oldID, newID, oldShort, newShort string) bool {
	changed := false
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
		fields, did := rewriteFields(nt.Fields, n.Fields, oldID, newID, oldShort, newShort)
		if did {
			n.Fields = fields
			g.Nodes[i] = n
			changed = true
		}
	}
	return changed
}

// rewriteFields returns a copy of values with plan references rewritten,
// and whether anything changed (values itself is never modified).
func rewriteFields(specs []schema.FieldSpec, values map[string]any, oldID, newID, oldShort, newShort string) (map[string]any, bool) {
	out := values
	changed := false
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
			if r := rewriteProse(s, oldID, newID, oldShort, newShort); r != s {
				set(f.Name, r)
			}
		case f.Kind == schema.KindObject:
			m, ok := val.(map[string]any)
			if !ok {
				continue
			}
			if next, did := rewriteFields(f.Fields, m, oldID, newID, oldShort, newShort); did {
				set(f.Name, next)
			}
		}
	}
	return out, changed
}

func rewriteProse(s, oldID, newID, oldShort, newShort string) string {
	return proseRefRE.ReplaceAllStringFunc(s, func(m string) string {
		ref := m[2 : len(m)-2]
		p, id, q := ParseRef(ref)
		switch {
		case !q:
			return m
		case p == oldID:
			return "[[" + Qualify(newID, id) + "]]"
		case oldShort != "" && p == oldShort:
			return "[[" + Qualify(newShort, id) + "]]"
		}
		return m
	})
}
