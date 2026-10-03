package lint

import (
	"slices"
	"strings"
)

// CycleArrow joins the IDs of a cycle path, as pd-lint does.
const CycleArrow = " → "

// Cycles returns every dependency cycle among nodes, as a closed path of IDs
// (`[s-a s-b s-c s-a]`), deterministically: the same graph yields the same
// cycles, paths and order on every run.
//
// It runs Tarjan's strongly connected components, mirrored from
// auto-graph/internal/contextpack/builder.go computeSCCs (nodes visited in
// sorted order), with adjacency lists sorted too. Each component with more
// than one node, or a node that depends on itself, is one cycle. Its path
// starts at the component's smallest ID and follows the shortest route back to
// it (breadth-first over sorted neighbours inside the component). Cycles are
// ordered by their starting ID. Edges to IDs outside nodes are ignored.
func Cycles(nodes []string, adj map[string][]string) [][]string {
	in := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		in[n] = true
	}
	next := make(map[string][]string, len(adj))
	for v, ws := range adj {
		for _, w := range ws {
			if in[v] && in[w] {
				next[v] = append(next[v], w)
			}
		}
	}
	for v := range next {
		slices.Sort(next[v])
		next[v] = slices.Compact(next[v])
	}

	var cycles [][]string
	for _, scc := range computeSCCs(nodes, next) {
		start := scc[0]
		if len(scc) == 1 && !slices.Contains(next[start], start) {
			continue
		}
		cycles = append(cycles, shortestCycle(start, scc, next))
	}
	slices.SortFunc(cycles, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return cycles
}

// computeSCCs is Tarjan's algorithm over sorted nodes and sorted adjacency;
// each component comes back sorted.
func computeSCCs(nodes []string, adj map[string][]string) [][]string {
	var (
		index   int
		stack   []string
		onStack = map[string]bool{}
		indices = map[string]int{}
		low     = map[string]int{}
		sccs    [][]string
	)

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		low[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, visited := indices[w]; !visited {
				strongconnect(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], indices[w])
			}
		}

		if low[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			slices.Sort(scc)
			sccs = append(sccs, scc)
		}
	}

	sorted := slices.Clone(nodes)
	slices.Sort(sorted)
	for _, v := range sorted {
		if _, visited := indices[v]; !visited {
			strongconnect(v)
		}
	}
	return sccs
}

// shortestCycle returns the shortest closed path start → … → start that stays
// inside scc, breaking ties by sorted neighbour order.
func shortestCycle(start string, scc []string, adj map[string][]string) []string {
	member := map[string]bool{}
	for _, n := range scc {
		member[n] = true
	}
	parent := map[string]string{}
	queue := []string{start}
	seen := map[string]bool{}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, w := range adj[v] {
			if !member[w] {
				continue
			}
			if w == start {
				path := []string{start}
				for u := v; u != start; u = parent[u] {
					path = append(path, u)
				}
				slices.Reverse(path[1:])
				return append(path, start)
			}
			if !seen[w] {
				seen[w] = true
				parent[w] = v
				queue = append(queue, w)
			}
		}
	}
	return []string{start, start} // unreachable for a real component
}
