package contextpack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// FormatEstimator is a callback that estimates the token cost of a rendered
// pack payload in the selected output format. The builder uses this to gate
// non-seed candidate inclusion against the format-specific budget.
type FormatEstimator func(p *Pack) int

// BuildOptions configures the context pack builder.
type BuildOptions struct {
	// ProjectRoot is the absolute path to the project directory.
	ProjectRoot string

	// Seeds is the list of normalized seed file paths (relative, slash-separated).
	Seeds []string

	// TokenLimit is the maximum token budget for the rendered output.
	TokenLimit int

	// Graph is the file-level import graph.
	Graph *graph.Graph

	// Estimator estimates the total tokens for the rendered pack in the
	// selected output format. If nil, a default estimator sums file content
	// tokens plus a fixed overhead per file entry.
	Estimator FormatEstimator
}

// candidate is an internal representation of a file being considered for
// inclusion in the pack.
type candidate struct {
	path     string
	role     string
	priority int
	distance int
	reason   string
	flags    []string
	rel      *Relationship
}

// Build constructs a context pack from a graph and set of seed files. It
// selects candidates by priority, reads file contents, enforces the token
// budget using the format estimator, and generates concise guidance.
func Build(opts BuildOptions) (*Pack, error) {
	// Build adjacency maps.
	fwd, rev := buildAdjacencyMaps(opts.Graph)

	// Compute risk flags for all nodes.
	riskFlags := computeAllRiskFlags(opts.Graph, fwd, rev)

	// Collect candidates.
	candidates := collectCandidates(opts.Seeds, fwd, rev, riskFlags, opts.Graph)

	// Sort candidates deterministically.
	sortCandidates(candidates)

	// Read seed file contents; seeds are mandatory.
	pack := &Pack{
		ProjectRoot: opts.ProjectRoot,
		TokenLimit:  opts.TokenLimit,
		SeedFiles:   opts.Seeds,
	}

	estimator := opts.Estimator
	if estimator == nil {
		estimator = defaultEstimator
	}

	// refreshDerived recomputes relationships, reading order, and guidance
	// so the estimator sees the full rendered output, not just file entries.
	refreshDerived := func() {
		pack.Relationships = collectRelationships(pack.Files, fwd, rev, opts.Seeds)
		pack.ReadingOrder = buildReadingOrder(pack.Files)
		pack.Guidance = generateGuidance(pack, fwd, rev, opts.Graph)
	}

	// Phase 1: Add all seed files (mandatory).
	for _, c := range candidates {
		if c.role != "seed" {
			continue
		}
		content, err := readFileContent(opts.ProjectRoot, c.path)
		if err != nil {
			return nil, fmt.Errorf("reading seed file %s: %w", c.path, err)
		}
		entry := FileEntry{
			Path:            c.path,
			Role:            c.role,
			Reason:          c.reason,
			EstimatedTokens: EstimateTokens(content),
			Flags:           c.flags,
			Content:         content,
		}
		pack.Files = append(pack.Files, entry)
	}

	// Check seed budget with full derived sections.
	refreshDerived()
	seedEstimate := estimator(pack)
	if seedEstimate > opts.TokenLimit {
		minBudget := seedEstimate
		return nil, &SeedBudgetExceededError{
			MinimumBudget: minBudget,
			TokenLimit:    opts.TokenLimit,
		}
	}

	// Phase 2: Add non-seed candidates while within budget.
	for _, c := range candidates {
		if c.role == "seed" {
			continue
		}
		content, err := readFileContent(opts.ProjectRoot, c.path)
		if err != nil {
			pack.OmittedCandidates = append(pack.OmittedCandidates, OmittedCandidate{
				Path:            c.path,
				Role:            c.role,
				Reason:          c.reason,
				EstimatedTokens: 0,
			})
			continue
		}

		contentTokens := EstimateTokens(content)
		entry := FileEntry{
			Path:            c.path,
			Role:            c.role,
			Reason:          c.reason,
			EstimatedTokens: contentTokens,
			Flags:           c.flags,
			Content:         content,
		}

		// Tentatively add the file, recompute derived sections, and check budget.
		pack.Files = append(pack.Files, entry)
		refreshDerived()
		tentative := estimator(pack)
		if tentative > opts.TokenLimit {
			pack.Files = pack.Files[:len(pack.Files)-1]
			pack.OmittedCandidates = append(pack.OmittedCandidates, OmittedCandidate{
				Path:            c.path,
				Role:            c.role,
				Reason:          c.reason,
				EstimatedTokens: contentTokens,
			})
			refreshDerived()
		}
	}

	// Final accounting with all sections populated.
	refreshDerived()
	pack.EstimatedTokens = estimator(pack)
	omittedTotal := 0
	for _, oc := range pack.OmittedCandidates {
		omittedTotal += oc.EstimatedTokens
	}
	pack.OmittedTokens = omittedTotal

	return pack, nil
}

// buildAdjacencyMaps creates forward (source->targets) and reverse (target->sources)
// adjacency maps from the graph edges.
func buildAdjacencyMaps(g *graph.Graph) (fwd map[string][]graph.Edge, rev map[string][]graph.Edge) {
	fwd = make(map[string][]graph.Edge)
	rev = make(map[string][]graph.Edge)
	for _, e := range g.Edges {
		fwd[e.Source] = append(fwd[e.Source], e)
		rev[e.Target] = append(rev[e.Target], e)
	}
	return fwd, rev
}

// collectCandidates gathers all candidate files from the graph neighborhood
// around the seed files, assigning roles and priorities.
func collectCandidates(seeds []string, fwd, rev map[string][]graph.Edge, riskFlags map[string][]string, g *graph.Graph) []*candidate {
	seedSet := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seedSet[s] = true
	}

	// Use a map to deduplicate candidates by path, merging reasons and flags.
	type candEntry struct {
		cand *candidate
	}
	candMap := make(map[string]*candidate)

	addCandidate := func(path, role string, priority, distance int, reason string) {
		if existing, ok := candMap[path]; ok {
			// Merge: keep lowest priority (highest rank).
			if priority < existing.priority {
				existing.role = role
				existing.priority = priority
				existing.distance = distance
			}
			// Merge reason.
			if !strings.Contains(existing.reason, reason) {
				existing.reason = existing.reason + "; " + reason
			}
		} else {
			candMap[path] = &candidate{
				path:     path,
				role:     role,
				priority: priority,
				distance: distance,
				reason:   reason,
			}
		}
	}

	// Priority 0: Seeds.
	for _, s := range seeds {
		addCandidate(s, "seed", 0, 0, "seed file")
	}

	// Priority 10: Direct runtime dependencies of seeds.
	for _, s := range seeds {
		for _, e := range fwd[s] {
			if seedSet[e.Target] {
				continue
			}
			if isRuntimeImport(e) {
				addCandidate(e.Target, "dependency", 10, 1,
					fmt.Sprintf("direct runtime dependency of %s", s))
			}
		}
	}

	// Priority 20: Direct runtime dependents of seeds.
	for _, s := range seeds {
		for _, e := range rev[s] {
			if seedSet[e.Source] {
				continue
			}
			if isRuntimeImport(e) {
				addCandidate(e.Source, "dependent", 20, 1,
					fmt.Sprintf("direct runtime dependent of %s", s))
			}
		}
	}

	// Priority 25: Direct neighbors with risk flags (not already added at higher priority).
	for _, s := range seeds {
		for _, e := range fwd[s] {
			if seedSet[e.Target] {
				continue
			}
			flags := riskFlags[e.Target]
			if len(flags) > 0 {
				addCandidate(e.Target, "dependency", 25, 1,
					fmt.Sprintf("direct neighbor of %s with risk flags", s))
			}
		}
		for _, e := range rev[s] {
			if seedSet[e.Source] {
				continue
			}
			flags := riskFlags[e.Source]
			if len(flags) > 0 {
				addCandidate(e.Source, "dependent", 25, 1,
					fmt.Sprintf("direct neighbor of %s with risk flags", s))
			}
		}
	}

	// Priority 30: Direct type-only dependencies/dependents.
	for _, s := range seeds {
		for _, e := range fwd[s] {
			if seedSet[e.Target] {
				continue
			}
			if isTypeOnlyImport(e) && !isRuntimeImport(e) {
				addCandidate(e.Target, "type_dependency", 30, 1,
					fmt.Sprintf("direct type-only dependency of %s", s))
			}
		}
		for _, e := range rev[s] {
			if seedSet[e.Source] {
				continue
			}
			if isTypeOnlyImport(e) && !isRuntimeImport(e) {
				addCandidate(e.Source, "type_dependent", 30, 1,
					fmt.Sprintf("direct type-only dependent of %s", s))
			}
		}
	}

	// Priority 35: Cycle members touching seeds.
	sccs := computeSCCs(g)
	for _, s := range seeds {
		for _, scc := range sccs {
			if len(scc) <= 1 {
				continue
			}
			inSCC := false
			for _, node := range scc {
				if node == s {
					inSCC = true
					break
				}
			}
			if !inSCC {
				continue
			}
			for _, node := range scc {
				if seedSet[node] || node == s {
					continue
				}
				addCandidate(node, "cycle_neighbor", 35, 1,
					fmt.Sprintf("cycle member with %s", s))
			}
		}
	}

	// Priority 40: Second-hop runtime dependencies/dependents.
	directNeighbors := make(map[string]bool)
	for _, s := range seeds {
		for _, e := range fwd[s] {
			directNeighbors[e.Target] = true
		}
		for _, e := range rev[s] {
			directNeighbors[e.Source] = true
		}
	}

	for _, s := range seeds {
		// Second-hop deps: deps of seed's direct deps.
		for _, e1 := range fwd[s] {
			if seedSet[e1.Target] {
				continue
			}
			for _, e2 := range fwd[e1.Target] {
				if seedSet[e2.Target] || directNeighbors[e2.Target] {
					continue
				}
				if isRuntimeImport(e2) {
					addCandidate(e2.Target, "transitive_neighbor", 40, 2,
						fmt.Sprintf("second-hop runtime dependency via %s", e1.Target))
				}
			}
		}
		// Second-hop dependents: dependents of seed's direct dependents.
		for _, e1 := range rev[s] {
			if seedSet[e1.Source] {
				continue
			}
			for _, e2 := range rev[e1.Source] {
				if seedSet[e2.Source] || directNeighbors[e2.Source] {
					continue
				}
				if isRuntimeImport(e2) {
					addCandidate(e2.Source, "transitive_neighbor", 40, 2,
						fmt.Sprintf("second-hop runtime dependent via %s", e1.Source))
				}
			}
		}
	}

	// Priority 50: Other second-hop type-only neighbors.
	for _, s := range seeds {
		for _, e1 := range fwd[s] {
			if seedSet[e1.Target] {
				continue
			}
			for _, e2 := range fwd[e1.Target] {
				if seedSet[e2.Target] || directNeighbors[e2.Target] {
					continue
				}
				if isTypeOnlyImport(e2) && !isRuntimeImport(e2) {
					addCandidate(e2.Target, "transitive_neighbor", 50, 2,
						fmt.Sprintf("second-hop type-only neighbor via %s", e1.Target))
				}
			}
		}
		for _, e1 := range rev[s] {
			if seedSet[e1.Source] {
				continue
			}
			for _, e2 := range rev[e1.Source] {
				if seedSet[e2.Source] || directNeighbors[e2.Source] {
					continue
				}
				if isTypeOnlyImport(e2) && !isRuntimeImport(e2) {
					addCandidate(e2.Source, "transitive_neighbor", 50, 2,
						fmt.Sprintf("second-hop type-only neighbor via %s", e1.Source))
				}
			}
		}
	}

	// Assign risk flags to all candidates.
	var result []*candidate
	for _, c := range candMap {
		c.flags = riskFlags[c.path]
		result = append(result, c)
	}

	return result
}

// sortCandidates sorts candidates deterministically: by priority, then distance, then path.
func sortCandidates(candidates []*candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].path < candidates[j].path
	})
}

// isRuntimeImport returns true if the edge represents a runtime import
// (not purely type-only).
func isRuntimeImport(e graph.Edge) bool {
	kinds := getImportKinds(e)
	for _, k := range kinds {
		if k != "type_only" {
			return true
		}
	}
	return len(kinds) == 0 // default to runtime if no kinds specified
}

// isTypeOnlyImport returns true if the edge has at least one type-only import kind.
func isTypeOnlyImport(e graph.Edge) bool {
	kinds := getImportKinds(e)
	for _, k := range kinds {
		if k == "type_only" {
			return true
		}
	}
	return false
}

// getImportKinds reads the import kinds from an edge, preferring attrs.import_kinds
// (comma-separated) and falling back to attrs.import_kind.
func getImportKinds(e graph.Edge) []string {
	if e.Attrs == nil {
		return nil
	}
	if kindsStr, ok := e.Attrs["import_kinds"]; ok && kindsStr != "" {
		return strings.Split(kindsStr, ",")
	}
	if kind, ok := e.Attrs["import_kind"]; ok && kind != "" {
		return []string{kind}
	}
	return nil
}

// hasImportKind checks whether any edge touching a file (in or out) contains
// the given import kind.
func hasImportKind(path string, kind string, fwd, rev map[string][]graph.Edge) bool {
	for _, e := range fwd[path] {
		for _, k := range getImportKinds(e) {
			if k == kind {
				return true
			}
		}
	}
	for _, e := range rev[path] {
		for _, k := range getImportKinds(e) {
			if k == kind {
				return true
			}
		}
	}
	return false
}

// computeAllRiskFlags computes deterministic risk flags for all graph nodes.
func computeAllRiskFlags(g *graph.Graph, fwd, rev map[string][]graph.Edge) map[string][]string {
	// Compute SCCs for cycle detection.
	sccs := computeSCCs(g)
	cycleNodes := make(map[string]bool)
	for _, scc := range sccs {
		if len(scc) > 1 {
			for _, node := range scc {
				cycleNodes[node] = true
			}
		}
	}
	// Also check self-edges.
	for _, e := range g.Edges {
		if e.Source == e.Target {
			cycleNodes[e.Source] = true
		}
	}

	result := make(map[string][]string)
	for _, n := range g.Nodes {
		var flags []string

		// Import-kind flags: any relationship touching the file.
		if hasImportKind(n.Path, "side_effect", fwd, rev) {
			flags = append(flags, "side_effect_import")
		}
		if hasImportKind(n.Path, "dynamic", fwd, rev) {
			flags = append(flags, "dynamic_import")
		}
		if hasImportKind(n.Path, "reexport", fwd, rev) {
			flags = append(flags, "reexport")
		}

		// Cycle flag.
		if cycleNodes[n.Path] {
			flags = append(flags, "cycle")
		}

		// Entrypoint-like flag.
		if isEntrypointLike(n.Path) {
			flags = append(flags, "entrypoint_like")
		}

		// High fan-in: >= 3 distinct source files.
		if len(rev[n.Path]) >= 3 {
			flags = append(flags, "high_fan_in")
		}

		// High fan-out: >= 5 distinct target files.
		if len(fwd[n.Path]) >= 5 {
			flags = append(flags, "high_fan_out")
		}

		// Test-like flag.
		if isTestLike(n.Path) {
			flags = append(flags, "test_like")
		}

		if len(flags) > 0 {
			sort.Strings(flags)
			result[n.Path] = flags
		}
	}
	return result
}

// isEntrypointLike determines if a path matches entrypoint-like patterns.
// Path base is index.ts/tsx, main.ts/tsx, app.ts/tsx, or path contains
// /pages/, /routes/, or /app/.
func isEntrypointLike(path string) bool {
	base := filepath.Base(path)
	entrypointBases := []string{
		"index.ts", "index.tsx",
		"main.ts", "main.tsx",
		"app.ts", "app.tsx",
	}
	for _, eb := range entrypointBases {
		if strings.EqualFold(base, eb) {
			return true
		}
	}

	// Check path segments.
	slashPath := filepath.ToSlash(path)
	if strings.Contains(slashPath, "/pages/") ||
		strings.Contains(slashPath, "/routes/") ||
		strings.Contains(slashPath, "/app/") {
		return true
	}
	return false
}

// isTestLike determines if a path matches test-like patterns.
// Path base ends with .test.ts/tsx, .spec.ts/tsx, or path contains
// /__tests__/, /test/, or /tests/.
func isTestLike(path string) bool {
	base := filepath.Base(path)
	testSuffixes := []string{
		".test.ts", ".test.tsx",
		".spec.ts", ".spec.tsx",
	}
	for _, ts := range testSuffixes {
		if strings.HasSuffix(base, ts) {
			return true
		}
	}

	// Check path segments.
	slashPath := filepath.ToSlash(path)
	if strings.Contains(slashPath, "/__tests__/") ||
		strings.Contains(slashPath, "/test/") ||
		strings.Contains(slashPath, "/tests/") {
		return true
	}
	return false
}

// computeSCCs computes strongly connected components using Tarjan's algorithm.
func computeSCCs(g *graph.Graph) [][]string {
	// Build a node set and adjacency for Tarjan's.
	nodeIndex := make(map[string]int)
	for i, n := range g.Nodes {
		nodeIndex[n.Path] = i
	}

	adj := make(map[string][]string)
	for _, e := range g.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
	}

	var (
		index   int
		stack   []string
		onStack = make(map[string]bool)
		indices = make(map[string]int)
		low     = make(map[string]int)
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
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if indices[w] < low[v] {
					low[v] = indices[w]
				}
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
			sort.Strings(scc)
			sccs = append(sccs, scc)
		}
	}

	// Process nodes in sorted order for determinism.
	var sortedNodes []string
	for _, n := range g.Nodes {
		sortedNodes = append(sortedNodes, n.Path)
	}
	sort.Strings(sortedNodes)

	for _, v := range sortedNodes {
		if _, visited := indices[v]; !visited {
			strongconnect(v)
		}
	}

	return sccs
}

// collectRelationships extracts relationships for all included files.
func collectRelationships(files []FileEntry, fwd, rev map[string][]graph.Edge, seeds []string) []Relationship {
	included := make(map[string]bool)
	for _, f := range files {
		included[f.Path] = true
	}

	seedSet := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seedSet[s] = true
	}

	seen := make(map[string]bool)
	var rels []Relationship

	for _, f := range files {
		// Forward edges from this file.
		for _, e := range fwd[f.Path] {
			if !included[e.Target] {
				continue
			}
			key := e.Source + "->" + e.Target
			if seen[key] {
				continue
			}
			seen[key] = true

			kinds := getImportKinds(e)
			primaryKind := ""
			if e.Attrs != nil {
				primaryKind = e.Attrs["import_kind"]
			}

			distance := 1
			reason := fmt.Sprintf("%s imports %s", e.Source, e.Target)

			rels = append(rels, Relationship{
				Source:            e.Source,
				Target:            e.Target,
				Direction:         "forward",
				PrimaryImportKind: primaryKind,
				ImportKinds:       kinds,
				Distance:          distance,
				Reason:            reason,
			})
		}

		// Reverse edges to this file (from other included files).
		for _, e := range rev[f.Path] {
			if !included[e.Source] {
				continue
			}
			key := e.Source + "->" + e.Target
			if seen[key] {
				continue
			}
			seen[key] = true

			kinds := getImportKinds(e)
			primaryKind := ""
			if e.Attrs != nil {
				primaryKind = e.Attrs["import_kind"]
			}

			distance := 1
			reason := fmt.Sprintf("%s imports %s", e.Source, e.Target)

			rels = append(rels, Relationship{
				Source:            e.Source,
				Target:            e.Target,
				Direction:         "forward",
				PrimaryImportKind: primaryKind,
				ImportKinds:       kinds,
				Distance:          distance,
				Reason:            reason,
			})
		}
	}

	// Sort relationships deterministically.
	sort.Slice(rels, func(i, j int) bool {
		if rels[i].Source != rels[j].Source {
			return rels[i].Source < rels[j].Source
		}
		return rels[i].Target < rels[j].Target
	})

	return rels
}

// buildReadingOrder creates the reading order from included files.
func buildReadingOrder(files []FileEntry) []ReadingOrderItem {
	var order []ReadingOrderItem
	for _, f := range files {
		order = append(order, ReadingOrderItem{
			Path:   f.Path,
			Reason: f.Reason,
		})
	}
	return order
}

// generateGuidance creates concise guidance from graph facts.
func generateGuidance(pack *Pack, fwd, rev map[string][]graph.Edge, g *graph.Graph) Guidance {
	var watch []string

	includedSet := make(map[string]bool)
	for _, f := range pack.Files {
		includedSet[f.Path] = true
	}

	seedSet := make(map[string]bool, len(pack.SeedFiles))
	for _, s := range pack.SeedFiles {
		seedSet[s] = true
	}

	// Warn about dependents that may break if seeds change.
	for _, s := range pack.SeedFiles {
		var dependents []string
		for _, e := range rev[s] {
			if !seedSet[e.Source] {
				dependents = append(dependents, e.Source)
			}
		}
		sort.Strings(dependents)
		for _, dep := range dependents {
			watch = append(watch, fmt.Sprintf("Changing %s may affect %s.", s, dep))
		}
	}

	// Warn about side-effect imports.
	for _, f := range pack.Files {
		for _, e := range fwd[f.Path] {
			kinds := getImportKinds(e)
			for _, k := range kinds {
				if k == "side_effect" {
					watch = append(watch, fmt.Sprintf("%s has a side-effect import of %s.", f.Path, e.Target))
				}
			}
		}
	}

	// Warn about dynamic imports.
	for _, f := range pack.Files {
		for _, e := range fwd[f.Path] {
			kinds := getImportKinds(e)
			for _, k := range kinds {
				if k == "dynamic" {
					watch = append(watch, fmt.Sprintf("%s has a dynamic import of %s.", f.Path, e.Target))
				}
			}
		}
	}

	// Warn about re-exports.
	for _, f := range pack.Files {
		for _, e := range fwd[f.Path] {
			kinds := getImportKinds(e)
			for _, k := range kinds {
				if k == "reexport" {
					watch = append(watch, fmt.Sprintf("%s re-exports from %s.", f.Path, e.Target))
				}
			}
		}
	}

	// Warn about cycles.
	sccs := computeSCCs(g)
	for _, scc := range sccs {
		if len(scc) <= 1 {
			continue
		}
		// Check if any included file is in this SCC.
		var inPack []string
		for _, node := range scc {
			if includedSet[node] {
				inPack = append(inPack, node)
			}
		}
		if len(inPack) >= 2 {
			watch = append(watch, fmt.Sprintf("Cycle detected: %s.", strings.Join(inPack, ", ")))
		}
	}

	// Suggest omitted files worth fetching.
	if len(pack.OmittedCandidates) > 0 {
		var topOmitted []string
		limit := 3
		if len(pack.OmittedCandidates) < limit {
			limit = len(pack.OmittedCandidates)
		}
		for i := 0; i < limit; i++ {
			oc := pack.OmittedCandidates[i]
			topOmitted = append(topOmitted, fmt.Sprintf("%s (%d tokens)", oc.Path, oc.EstimatedTokens))
		}
		watch = append(watch, fmt.Sprintf("Omitted files worth fetching with more budget: %s.", strings.Join(topOmitted, ", ")))
	}

	return Guidance{Watch: watch}
}

// readFileContent reads the content of a file relative to the project root.
func readFileContent(projectRoot, relPath string) (string, error) {
	absPath := filepath.Join(projectRoot, filepath.FromSlash(relPath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// defaultEstimator is a simple format estimator that sums file content tokens
// plus a fixed overhead for pack metadata.
func defaultEstimator(p *Pack) int {
	// Base overhead for metadata (project root, budget line, headers, etc.)
	overhead := 50
	total := overhead
	for _, f := range p.Files {
		total += EstimateTokens(f.Content)
		// Per-file metadata overhead (path, role, fences, etc.)
		total += 10
	}
	// Relationships and guidance overhead.
	total += len(p.Relationships) * 5
	if len(p.Guidance.Watch) > 0 {
		for _, w := range p.Guidance.Watch {
			total += EstimateTokens(w)
		}
	}
	return total
}
