package graph

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mistakenot/auto-plan/internal/schema"
)

// Validation codes. Lint reports these alongside its own rule codes.
const (
	CodeInvalidGraph     = "invalid-graph"
	CodeBadVersion       = "bad-version"
	CodeUnregisteredType = "unregistered-type"
	CodeUnknownField     = "unknown-field"
	CodeMissingField     = "missing-field"
	CodeInvalidField     = "invalid-field"
	CodeBadID            = "bad-id"
	CodeDuplicateID      = "duplicate-id"
	CodeMissingPlanNode  = "missing-plan-node"
	CodeDanglingRef      = "dangling-ref"
	CodeWrongEndpoint    = "wrong-endpoint"
	CodeDuplicateEdge    = "duplicate-edge"
)

// NodePath is the JSON path lint and validation use for a node.
func NodePath(id string) string { return "$.nodes[" + id + "]" }

// EdgePath is the JSON path for an edge, keyed by its endpoints.
func EdgePath(e Edge) string { return "$.edges[" + e.From + " " + e.Type + " " + e.To + "]" }

// Validate returns every structural problem in g as data, checked against the
// registry: the version and plan ID, unknown fields and types, bad or
// duplicate IDs (node and edge IDs share one space), field values against
// their FieldSpec, and edges against their EdgeType endpoints. It never
// returns nil.
//
// A graph of another major format version was written against another
// registry, so only its decode problems are reported: it is read best effort
// (lint adds an other-version warning) and never written (Frozen).
func Validate(g *Graph) []ValidationError {
	v := validator{reg: &schema.Registry}
	v.errs = append(v.errs, g.decodeIssues...)
	v.graph(g)
	return v.errs
}

type validator struct {
	reg  *schema.Schema
	errs []ValidationError
}

func (v *validator) add(code, path, field, msg string, value any) {
	v.errs = append(v.errs, ValidationError{Code: code, Path: path, Field: field, Message: msg, Value: value})
}

func (v *validator) graph(g *Graph) {
	if v.errs == nil {
		v.errs = []ValidationError{}
	}
	if _, ok := schema.ParseVersion(g.Version); !ok && !v.decodeIssueAt("$.version") {
		v.add(CodeBadVersion, "$.version", "version",
			"version must be a semver string MAJOR.MINOR.PATCH (this tool writes "+strconv.Quote(schema.Version)+")", g.Version)
	}
	if g.OtherMajor() {
		return
	}
	switch {
	case g.ID == "" && !v.decodeIssueAt("$.id"):
		v.add(CodeMissingField, "$.id", "id", "the plan ID is required (NNN-xxxx, e.g. 004-k7q2)", nil)
	case g.ID != "" && !PlanIDPattern.MatchString(g.ID):
		v.add(CodeBadID, "$.id", "id", "the plan ID must be the plan number, a hyphen and 4 Crockford base32 characters (e.g. 004-k7q2)", g.ID)
	}
	for _, k := range slices.Sorted(maps.Keys(g.Extra)) {
		v.add(CodeUnknownField, "$."+k, k, "unknown top-level key "+strconv.Quote(k), nil)
	}

	byID := map[string]Node{}
	planNodes := 0
	for i, n := range g.Nodes {
		path := NodePath(n.ID)
		if n.ID == "" {
			path = "$.nodes[" + strconv.Itoa(i) + "]"
		}
		if n.Type == schema.PlanNodeID {
			planNodes++
		}
		if _, dup := byID[n.ID]; dup && n.ID != "" {
			v.add(CodeDuplicateID, path, "id", "node ID "+strconv.Quote(n.ID)+" is used more than once", n.ID)
		} else {
			byID[n.ID] = n
		}
		v.node(path, n)
	}
	if planNodes == 0 {
		v.add(CodeMissingPlanNode, "$.nodes", "", "the graph has no plan node (ID "+strconv.Quote(schema.PlanNodeID)+")", nil)
	}

	seen := map[string]bool{}
	edgeIDs := map[string]bool{}
	for _, e := range g.Edges {
		path := EdgePath(e)
		switch _, nodeID := byID[e.ID]; {
		case e.ID == "":
			v.add(CodeMissingField, path+".id", "id", "an edge ID is required (e- + 4 Crockford base32 characters, e.g. e-7k2q)", nil)
		case !EdgeIDPattern.MatchString(e.ID):
			v.add(CodeBadID, path+".id", "id", "an edge ID must be \"e-\" + 4 Crockford base32 characters (e.g. e-7k2q)", e.ID)
		case nodeID || edgeIDs[e.ID]:
			v.add(CodeDuplicateID, path+".id", "id", "ID "+strconv.Quote(e.ID)+" is used more than once (node and edge IDs share one space)", e.ID)
		}
		edgeIDs[e.ID] = true
		key := e.From + "\x00" + e.Type + "\x00" + e.To
		if seen[key] {
			v.add(CodeDuplicateEdge, path, "", "edge is listed more than once", nil)
			continue
		}
		seen[key] = true
		v.edge(path, e, byID)
	}
}

// decodeIssueAt reports whether decoding already flagged path.
func (v *validator) decodeIssueAt(path string) bool {
	for _, e := range v.errs {
		if e.Path == path {
			return true
		}
	}
	return false
}

func (v *validator) node(path string, n Node) {
	for _, k := range slices.Sorted(maps.Keys(n.Extra)) {
		v.add(CodeUnknownField, path+"."+k, k, "unknown node key "+strconv.Quote(k), nil)
	}
	nt, ok := v.reg.Node(n.Type)
	if !ok {
		v.add(CodeUnregisteredType, path+".type", "type", "node type "+strconv.Quote(n.Type)+" is not registered", n.Type)
		return
	}

	switch {
	case nt.Singleton():
		if n.ID != nt.Name {
			v.add(CodeBadID, path+".id", "id", fmt.Sprintf("a %s node's ID must be %q", nt.Name, nt.Name), n.ID)
		}
	case !IDPattern.MatchString(n.ID) || !strings.HasPrefix(n.ID, nt.Prefix+"-"):
		v.add(CodeBadID, path+".id", "id",
			fmt.Sprintf("a %s ID must be %q + 4 Crockford base32 characters (e.g. %s-3fxm)", nt.Name, nt.Prefix+"-", nt.Prefix), n.ID)
	}

	switch n.Status {
	case StatusActive, StatusRetired:
	case "":
		v.add(CodeMissingField, path+".status", "status", "status is required (active or retired)", nil)
	default:
		v.add(CodeInvalidField, path+".status", "status", "status must be active or retired", n.Status)
	}

	if !nt.Singleton() {
		switch {
		case n.Rank == "":
			v.add(CodeMissingField, path+".rank", "rank", "rank is required (a sortable string such as a0)", nil)
		case !RankPattern.MatchString(n.Rank):
			v.add(CodeInvalidField, path+".rank", "rank", "rank must match "+RankPattern.String(), n.Rank)
		}
	}

	v.fields(path+".fields", nt.Fields, n.Fields)
}

func (v *validator) fields(path string, specs []schema.FieldSpec, values map[string]any) {
	for _, k := range slices.Sorted(maps.Keys(values)) {
		if !slices.ContainsFunc(specs, func(f schema.FieldSpec) bool { return f.Name == k }) {
			v.add(CodeUnknownField, path+"."+k, k, "unknown field "+strconv.Quote(k), nil)
		}
	}
	for i := range specs {
		f := &specs[i]
		val, present := values[f.Name]
		fpath := path + "." + f.Name
		if !present {
			if f.Required {
				v.add(CodeMissingField, fpath, f.Name, f.Name+" is required", nil)
			}
			continue
		}
		v.value(fpath, *f, val)
	}
}

func (v *validator) value(path string, f schema.FieldSpec, val any) {
	switch f.Kind {
	case schema.KindString, schema.KindText, schema.KindEnum:
		s, ok := val.(string)
		if !ok {
			v.add(CodeInvalidField, path, f.Name, f.Name+" must be a string", val)
			return
		}
		v.scalar(path, f, s)
	case schema.KindList:
		arr, ok := val.([]any)
		if !ok {
			v.add(CodeInvalidField, path, f.Name, f.Name+" must be a list of strings", val)
			return
		}
		if f.Required && len(arr) == 0 {
			v.add(CodeMissingField, path, f.Name, f.Name+" needs at least one item", nil)
		}
		for i, item := range arr {
			s, ok := item.(string)
			ipath := path + "[" + strconv.Itoa(i) + "]"
			if !ok {
				v.add(CodeInvalidField, ipath, f.Name, f.Name+" items must be strings", item)
				continue
			}
			v.scalar(ipath, f, s)
		}
	case schema.KindObject:
		m, ok := val.(map[string]any)
		if !ok {
			v.add(CodeInvalidField, path, f.Name, f.Name+" must be an object", val)
			return
		}
		v.fields(path, f.Fields, m)
	}
}

// scalar checks one string value: required means non-blank, single-line kinds
// reject newlines, enums must match, and patterns must match.
func (v *validator) scalar(path string, f schema.FieldSpec, s string) {
	if strings.TrimSpace(s) == "" {
		if f.Required {
			v.add(CodeMissingField, path, f.Name, f.Name+" must not be blank", s)
		}
		return
	}
	if f.Kind != schema.KindText && strings.ContainsAny(s, "\r\n") {
		v.add(CodeInvalidField, path, f.Name, f.Name+" must be a single line", s)
	}
	if f.Kind == schema.KindEnum && !slices.Contains(f.Enum, s) {
		v.add(CodeInvalidField, path, f.Name, f.Name+" must be one of "+strings.Join(f.Enum, "|"), s)
	}
	if f.Pattern != "" && !compiled(f.Pattern).MatchString(s) {
		v.add(CodeInvalidField, path, f.Name, f.Name+" must match "+f.Pattern, s)
	}
}

func (v *validator) edge(path string, e Edge, byID map[string]Node) {
	for _, k := range slices.Sorted(maps.Keys(e.Extra)) {
		v.add(CodeUnknownField, path+"."+k, k, "unknown edge key "+strconv.Quote(k), nil)
	}
	et, ok := v.reg.Edge(e.Type)
	if !ok {
		v.add(CodeUnregisteredType, path+".type", "type", "edge type "+strconv.Quote(e.Type)+" is not registered", e.Type)
	}

	from, fromOK := byID[e.From]
	if !fromOK {
		v.add(CodeDanglingRef, path+".from", "from", "edge starts at "+strconv.Quote(e.From)+", which is not a node in this plan", e.From)
	} else if ok && !et.AllowsFrom(from.Type) {
		v.add(CodeWrongEndpoint, path+".from", "from",
			fmt.Sprintf("%s edges start at %s, not %s", et.Name, strings.Join(et.From, "|"), from.Type), e.From)
	}

	if _, _, qualified := ParseRef(e.To); qualified {
		switch {
		case ok && !et.CrossPlan:
			v.add(CodeWrongEndpoint, path+".to", "to", et.Name+" edges cannot target another plan", e.To)
		case ShortRefPattern.MatchString(e.To):
			v.add(CodeDanglingRef, path+".to", "to",
				"edge targets "+strconv.Quote(e.To)+", a bare plan number: a stored reference names the plan ID (NNN-xxxx:ID, e.g. 005-k7q2:r-8hw3)", e.To)
		case !QualifiedRefPattern.MatchString(e.To):
			v.add(CodeDanglingRef, path+".to", "to",
				"edge targets "+strconv.Quote(e.To)+", which is not a reference NNN-xxxx:ID (e.g. 005-k7q2:r-8hw3)", e.To)
		}
		// Well-formed qualified targets are resolved across plans
		// (workspace.PlanSet.CheckEdge), not here.
		return
	}
	to, toOK := byID[e.To]
	if !toOK {
		v.add(CodeDanglingRef, path+".to", "to", "edge targets "+strconv.Quote(e.To)+", which is not a node in this plan", e.To)
	} else if ok && !et.AllowsTo(to.Type) {
		v.add(CodeWrongEndpoint, path+".to", "to",
			fmt.Sprintf("%s edges end at %s, not %s", et.Name, et.ToLabel(), to.Type), e.To)
	} else if ok && et.SameType && fromOK && from.Type != to.Type {
		v.add(CodeWrongEndpoint, path+".to", "to",
			fmt.Sprintf("%s edges join nodes of the same type, not %s → %s", et.Name, from.Type, to.Type), e.To)
	}
}

var (
	patternMu    sync.Mutex
	patternCache = map[string]*regexp.Regexp{}
)

// compiled returns a cached compiled registry pattern. Registry patterns are
// checked to compile by the schema tests.
func compiled(pattern string) *regexp.Regexp {
	patternMu.Lock()
	defer patternMu.Unlock()
	re, ok := patternCache[pattern]
	if !ok {
		re = regexp.MustCompile(pattern)
		patternCache[pattern] = re
	}
	return re
}
