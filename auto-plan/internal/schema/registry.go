// Package schema is the single extension point of auto-plan: every node type
// and edge type a plan graph may hold is declared here, and validation, lint,
// the generated `add` flags and the renderers all read this table. Adding a
// type means adding an entry below and changing nothing else.
package schema

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Version is the graph.json format version this tool reads and writes, as
// semver. A plan records the version it was written with; it is never
// migrated. This tool writes only plans of its own major version that are
// not newer than it (see Writable); it reads every 1.x.y, and a plan of
// another major best-effort (D-2).
const Version = "1.0.0"

// SemVer is a parsed MAJOR.MINOR.PATCH version.
type SemVer struct{ Major, Minor, Patch int }

// VersionPattern is the shape of a graph.json version.
const VersionPattern = `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`

var versionRE = regexp.MustCompile(VersionPattern)

// ParseVersion parses a MAJOR.MINOR.PATCH string.
func ParseVersion(s string) (SemVer, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return SemVer{}, false
	}
	var v SemVer
	for i, dst := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return SemVer{}, false
		}
		*dst = n
	}
	return v, true
}

// Compare orders two versions (-1, 0, +1).
func (v SemVer) Compare(o SemVer) int {
	return cmp.Or(cmp.Compare(v.Major, o.Major), cmp.Compare(v.Minor, o.Minor), cmp.Compare(v.Patch, o.Patch))
}

// Current is Version parsed.
func Current() SemVer {
	v, _ := ParseVersion(Version)
	return v
}

// Lifecycle is the step a plan has reached. Lint rules and node types declare
// the earliest step at which they apply, so partial plans stay valid (D-10).
// The ordered sequence of steps is owned by the registry (Schema.Lifecycle),
// so a future format version can change it without touching old plans; these
// constants only name the steps of this version.
type Lifecycle string

const (
	LifecycleRequirements Lifecycle = "requirements"
	LifecycleSolution     Lifecycle = "solution"
	LifecyclePlan         Lifecycle = "plan"
	LifecycleExecuting    Lifecycle = "executing"
	LifecycleDone         Lifecycle = "done"
)

// lifecycleSteps is this version's lifecycle sequence. It is declared apart
// from Registry only so the plan node's lifecycle enum can be built from it
// without an initialization cycle; Registry.Lifecycle is the one place
// everything reads it from.
var lifecycleSteps = []Lifecycle{
	LifecycleRequirements,
	LifecycleSolution,
	LifecyclePlan,
	LifecycleExecuting,
	LifecycleDone,
}

// Index returns the position of l in the registry's lifecycle sequence, or
// -1 when l is not a step of it.
func (l Lifecycle) Index() int { return slices.Index(Registry.Lifecycle, l) }

// AtLeast reports whether l has reached min. An unknown l reaches nothing.
func (l Lifecycle) AtLeast(minimum Lifecycle) bool {
	i := l.Index()
	return i >= 0 && i >= minimum.Index()
}

// LifecycleNames returns the registry's lifecycle steps as strings, in order.
func LifecycleNames() []string { return stepNames(Registry.Lifecycle) }

func stepNames(steps []Lifecycle) []string {
	out := make([]string, len(steps))
	for i, l := range steps {
		out[i] = string(l)
	}
	return out
}

// FieldKind is the value shape of a node field.
type FieldKind string

const (
	// KindString is a single line of text.
	KindString FieldKind = "string"
	// KindText is Markdown prose that may span lines.
	KindText FieldKind = "text"
	// KindEnum is a single string drawn from FieldSpec.Enum.
	KindEnum FieldKind = "enum"
	// KindList is a list of single-line strings.
	KindList FieldKind = "list"
	// KindObject is a nested object whose members are FieldSpec.Fields.
	KindObject FieldKind = "object"
)

// FieldSpec declares one field of a node type. It drives decode validation,
// the generated CLI flags, and card rendering.
type FieldSpec struct {
	Name     string
	Kind     FieldKind
	Required bool
	Enum     []string
	Pattern  string
	Help     string
	// Fixed marks a field that is set when the node is created and that
	// `update` refuses to change (a plan's name must match its folder).
	Fixed bool
	// Computed marks a field the tool writes itself (never a CLI flag, neither
	// `add` nor `update`): annex.hash is set only by the freeze step. Unlike
	// Fixed, a Computed field is not known at create, so it is not required.
	Computed bool
	// Fields holds the members of a KindObject field. Their flags are named
	// `--<field>-<member>`.
	Fields []FieldSpec
	// PlanRef marks a field (or list) whose values are plan IDs (`004-k7q2`).
	// The CLI expands a bare plan number (`004`) to the plan's ID before
	// saving, and `renumber` rewrites the values when a plan is renumbered.
	PlanRef bool
}

// NodeType declares one node type.
type NodeType struct {
	// Name is the machine type name written in graph.json (`ac`, `goal`).
	Name string
	// Prefix starts every generated ID of this type (`ac` → `ac-3fxm`). An
	// empty Prefix marks a singleton whose ID is its Name: the plan node's ID
	// is literally `plan` (D-9).
	Prefix string
	// Term is the canonical glossary term for this type
	// (docs/concepts/UBIQUITOUS_LANGUAGE.md).
	Term   string
	Fields []FieldSpec
	// MinLifecycle is the earliest lifecycle step at which the type is
	// expected in a plan.
	MinLifecycle Lifecycle
	// RankScope names the outgoing edge type whose target groups siblings for
	// reading order (an AC's siblings are the ACs proving the same goal). Empty
	// means every node of the type is a sibling.
	RankScope string
	// UniqueBy names a field of this type that at most one active node may
	// share a value of: an annex has at most one node per `kind`. Validate
	// enforces it (code `duplicate-<type>-<field>`), so a cap on a type is a
	// registry entry, not a bespoke check. Empty means no such cap.
	UniqueBy string
	Help     string
}

// Singleton reports whether the type has exactly one node with a fixed ID.
func (n NodeType) Singleton() bool { return n.Prefix == "" }

// Field returns the named field spec.
func (n NodeType) Field(name string) (FieldSpec, bool) {
	for i := range n.Fields {
		if n.Fields[i].Name == name {
			return n.Fields[i], true
		}
	}
	return FieldSpec{}, false
}

// AnyType in an EdgeType's To list accepts every registered node type,
// including types registered later (`about` is tree → any).
const AnyType = "*"

// EdgeType declares one edge type and the node types allowed at each end.
type EdgeType struct {
	Name string
	From []string
	// To lists the node types the edge may end at; AnyType accepts every type.
	To []string
	// CrossPlan marks edge types whose `to` may be a qualified reference
	// into another plan (`005-k7q2:r-8hw3`).
	CrossPlan bool
	// SameType requires both endpoints to have the same type: `dependsOn`
	// joins stage → stage and child → child, never stage → child.
	SameType bool
	Help     string
}

// AllowsFrom reports whether a node of type typ may start the edge.
func (e EdgeType) AllowsFrom(typ string) bool { return slices.Contains(e.From, typ) }

// AllowsTo reports whether a node of type typ may end the edge.
func (e EdgeType) AllowsTo(typ string) bool {
	return slices.Contains(e.To, AnyType) || slices.Contains(e.To, typ)
}

// ToLabel is the allowed target types for help text ("any" for AnyType).
func (e EdgeType) ToLabel() string {
	if slices.Contains(e.To, AnyType) {
		return "any"
	}
	return strings.Join(e.To, "|")
}

// Schema is the full set of registered node and edge types, and the
// lifecycle sequence plans of this version move through.
type Schema struct {
	// Lifecycle lists the lifecycle steps in order. Node types and lint
	// rules name the step they switch on at; `update … plan --lifecycle`
	// accepts exactly these.
	Lifecycle []Lifecycle
	Nodes     []NodeType
	Edges     []EdgeType
}

// Node returns the named node type.
func (s *Schema) Node(name string) (NodeType, bool) {
	for i := range s.Nodes {
		if s.Nodes[i].Name == name {
			return s.Nodes[i], true
		}
	}
	return NodeType{}, false
}

// Edge returns the named edge type.
func (s *Schema) Edge(name string) (EdgeType, bool) {
	for _, e := range s.Edges {
		if e.Name == name {
			return e, true
		}
	}
	return EdgeType{}, false
}

// EdgesFrom returns the edge types a node of type typ may start, in
// registry order.
func (s *Schema) EdgesFrom(typ string) []EdgeType {
	var out []EdgeType
	for _, e := range s.Edges {
		if e.AllowsFrom(typ) {
			out = append(out, e)
		}
	}
	return out
}

// Common patterns shared by field specs.
const (
	// NamePattern is the kebab-case slug a plan name must match.
	NamePattern = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
	// DatePattern is an ISO 8601 calendar date.
	DatePattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
	// PlanNumberPattern is a plan's 3-digit number (its folder's prefix).
	PlanNumberPattern = `^[0-9]{3}$`
	// PlanIDPattern is a plan's ID: its number, a hyphen and 4 Crockford
	// base32 characters (`004-k7q2`).
	PlanIDPattern = `^[0-9]{3}-[0-9a-hjkmnp-tv-z]{4}$`
	// RepoPathPattern is a canonical repo-relative path: "/"-separated
	// segments with no spaces, backslashes or colons, and no empty, "." or ".."
	// segment, so it can never leave the repo (no leading slash, no drive
	// letter). A trailing slash names a directory.
	RepoPathPattern = `^` + repoPathSegment + `(?:/` + repoPathSegment + `)*/?$`

	// repoPathSegment is one path segment other than "." and "..".
	repoPathSegment = `(?:[^./\\:\s][^/\\:\s]*|\.[^./\\:\s][^/\\:\s]*|\.\.[^/\\:\s]+)`

	// AnnexPathPattern is a flat kebab-case Markdown filename, relative to the
	// plan folder (`usage.md`, `test-coverage.md`): no directory segments, so
	// the folder rename in `renumber` moves the file with no path rewrite.
	AnnexPathPattern = `^[a-z0-9]+(?:-[a-z0-9]+)*\.md$`
)

// PlanNodeID is the fixed ID of the one plan node in every graph.
const PlanNodeID = "plan"

func titleField(help string) FieldSpec {
	return FieldSpec{Name: "title", Kind: KindString, Required: true, Help: help}
}

func descriptionField() FieldSpec {
	return FieldSpec{Name: "description", Kind: KindText, Help: "Optional Markdown detail"}
}

// Registry is the one table every part of auto-plan reads.
var Registry = Schema{
	Lifecycle: lifecycleSteps,
	Nodes: []NodeType{
		{
			Name: "plan", Term: "Plan", MinLifecycle: LifecycleRequirements,
			Help: "The plan header: exactly one per graph, ID `plan`.",
			Fields: []FieldSpec{
				{Name: "name", Kind: KindString, Required: true, Pattern: NamePattern, Fixed: true, Help: "Kebab-case plan name (matches the folder)"},
				{Name: "kind", Kind: KindEnum, Required: true, Enum: []string{"task", "epic"}, Help: "task or epic"},
				{Name: "lifecycle", Kind: KindEnum, Required: true, Enum: stepNames(lifecycleSteps), Help: "Lifecycle step; selects which lint rules apply"},
				{Name: "created", Kind: KindString, Required: true, Pattern: DatePattern, Fixed: true, Help: "Creation date (YYYY-MM-DD)"},
				{Name: "epic", Kind: KindString, Pattern: PlanIDPattern, PlanRef: true, Help: "ID of the epic plan this plan belongs to (NNN expands)"},
			},
		},
		{
			Name: "goal", Prefix: "g", Term: "Goal", MinLifecycle: LifecycleRequirements,
			Help: "One outcome the plan commits to.",
			Fields: []FieldSpec{
				titleField("One-line statement of the outcome"),
				descriptionField(),
			},
		},
		{
			Name: "ac", Prefix: "ac", Term: "Acceptance Criterion", MinLifecycle: LifecycleSolution,
			RankScope: "proves",
			Help:      "A given/when/then criterion that proves one or more goals.",
			Fields: []FieldSpec{
				titleField("One-line summary of the criterion"),
				{Name: "gwt", Kind: KindText, Required: true, Help: "Given/when/then Markdown"},
				{
					Name: "verify", Kind: KindObject, Help: "How the criterion is verified",
					Fields: []FieldSpec{
						{Name: "cmd", Kind: KindString, Help: "Reproducible verify command"},
						{Name: "tests", Kind: KindList, Help: "Test names the command runs"},
						{Name: "kind", Kind: KindEnum, Enum: []string{"command", "manual"}, Help: "How it is verified (default command)"},
					},
				},
				{Name: "layer", Kind: KindEnum, Enum: []string{"e2e", "integration", "golden", "unit", "manual"}, Help: "Test layer this criterion is covered at (optional)"},
			},
		},
		{
			Name: "decision", Prefix: "d", Term: "Decision", MinLifecycle: LifecycleRequirements,
			Help: "A settled choice, with why and the alternatives it rejected.",
			Fields: []FieldSpec{
				titleField("One-line statement of the decision"),
				{Name: "chosen", Kind: KindText, Required: true, Help: "What was chosen"},
				{Name: "why", Kind: KindText, Required: true, Help: "Why it was chosen"},
				{Name: "by", Kind: KindString, Required: true, Help: "Who decided"},
				{Name: "reversibility", Kind: KindEnum, Enum: []string{"one-way", "two-way"}, Help: "How hard the decision is to undo"},
			},
		},
		{
			Name: "alternative", Prefix: "a", Term: "Alternative", MinLifecycle: LifecycleRequirements,
			Help: "An option a decision considered and rejected.",
			Fields: []FieldSpec{
				titleField("One-line statement of the option"),
				{Name: "why", Kind: KindText, Required: true, Help: "Why it was rejected"},
			},
		},
		{
			Name: "rail", Prefix: "r", Term: "Rail", MinLifecycle: LifecycleRequirements,
			Help: "A constraint an epic places on every child plan.",
			Fields: []FieldSpec{
				titleField("One-line statement of the constraint"),
				descriptionField(),
				{Name: "deferred", Kind: KindList, Pattern: PlanIDPattern, PlanRef: true, Help: "Child plan IDs excused from honouring the rail for now (NNN expands)"},
			},
		},
		{
			Name: "defect", Prefix: "df", Term: "Defect", MinLifecycle: LifecycleRequirements,
			Help: "A known problem in existing code that the plan fixes.",
			Fields: []FieldSpec{
				titleField("One-line statement of the problem"),
				descriptionField(),
			},
		},
		{
			Name: "stage", Prefix: "s", Term: "Stage", MinLifecycle: LifecyclePlan,
			Help: "An ordered slice of work that leaves the system shippable.",
			Fields: []FieldSpec{
				titleField("One-line statement of the stage"),
				{Name: "steps", Kind: KindList, Required: true, Help: "The stage's steps, in order"},
				{Name: "commit", Kind: KindString, Required: true, Help: "Commit message subject"},
				{Name: "status", Kind: KindEnum, Enum: []string{"todo", "doing", "done"}, Help: "Progress (default todo)"},
			},
		},
		{
			Name: "file", Prefix: "f", Term: "File Change", MinLifecycle: LifecycleSolution,
			Help: "One planned addition, edit or deletion of a repo file.",
			Fields: []FieldSpec{
				{Name: "path", Kind: KindString, Required: true, Pattern: RepoPathPattern, Help: "Repo-relative path"},
				{Name: "change", Kind: KindEnum, Required: true, Enum: []string{"add", "edit", "delete"}, Help: "What happens to the file"},
				{Name: "why", Kind: KindText, Required: true, Help: "Why the file changes"},
			},
		},
		{
			Name: "question", Prefix: "q", Term: "Question", MinLifecycle: LifecycleRequirements,
			Help: "A point the plan cannot settle alone; an open question gates the plan.",
			Fields: []FieldSpec{
				titleField("The question, in one line"),
				{Name: "status", Kind: KindEnum, Required: true, Enum: []string{"open", "answered"}, Help: "Whether it is still open"},
				{Name: "recommended", Kind: KindText, Help: "The recommended answer"},
				{Name: "answer", Kind: KindText, Help: "The answer given"},
			},
		},
		{
			Name: "tree", Prefix: "t", Term: "Tree", MinLifecycle: LifecycleSolution,
			Help: "Structured text in show-me notation: a call, file, dependency or pseudocode tree.",
			Fields: []FieldSpec{
				titleField("What the tree shows"),
				{Name: "kind", Kind: KindEnum, Required: true, Enum: []string{"call", "file", "dep", "pseudo"}, Help: "Tree kind"},
				{Name: "body", Kind: KindText, Required: true, Help: "Show-me notation body"},
			},
		},
		{
			Name: "journey", Prefix: "j", Term: "Journey", MinLifecycle: LifecycleRequirements,
			Help: "An end-to-end path an actor takes through the system, cut into legs.",
			Fields: []FieldSpec{
				titleField("One-line statement of the journey"),
				descriptionField(),
			},
		},
		{
			Name: "leg", Prefix: "l", Term: "Leg", MinLifecycle: LifecycleRequirements,
			RankScope: "in",
			Help:      "One actor-and-action step of a journey.",
			Fields: []FieldSpec{
				{Name: "actor", Kind: KindString, Required: true, Help: "Who acts"},
				{Name: "action", Kind: KindString, Required: true, Help: "What they do"},
			},
		},
		{
			Name: "child", Prefix: "c", Term: "Child Plan", MinLifecycle: LifecycleRequirements,
			Help: "An epic's reference to one of the plans that delivers part of it.",
			Fields: []FieldSpec{
				{Name: "plan", Kind: KindString, Required: true, Pattern: PlanIDPattern, PlanRef: true, Help: "The child plan's ID (NNN expands)"},
				{Name: "title", Kind: KindString, Help: "What the child delivers, in one line"},
			},
		},
		{
			Name: "annex", Prefix: "ax", Term: "Annex", MinLifecycle: LifecycleSolution,
			UniqueBy: "kind",
			Help:     "A Markdown file beside graph.json, registered as a node, holding plan-time exposition (at most one per kind).",
			Fields: []FieldSpec{
				{Name: "kind", Kind: KindEnum, Required: true, Enum: []string{"usage", "structures", "testing"}, Help: "Which exposition this annex holds"},
				{Name: "path", Kind: KindString, Required: true, Pattern: AnnexPathPattern, Help: "Flat <kebab>.md filename, relative to the plan folder"},
				titleField("One-line title of the annex"),
				{Name: "hash", Kind: KindString, Computed: true, Help: "SHA-256 hex of the file's bytes, recorded at freeze (computed; no flag)"},
			},
		},
	},
	Edges: []EdgeType{
		{Name: "proves", From: []string{"ac"}, To: []string{"goal"}, Help: "The goal this AC proves"},
		{Name: "constrains", From: []string{"decision"}, To: []string{"goal", "ac"}, Help: "A goal or AC this decision constrains"},
		{Name: "rejects", From: []string{"decision"}, To: []string{"alternative"}, Help: "An alternative this decision rejected"},
		{Name: "wouldBreak", From: []string{"alternative"}, To: []string{"ac", "goal", "rail"}, CrossPlan: true, Help: "What this alternative would have broken"},
		{Name: "supersedes", From: []string{"decision"}, To: []string{"decision"}, Help: "An earlier decision this one replaces"},
		{Name: "dependsOn", From: []string{"stage", "child"}, To: []string{"stage", "child"}, SameType: true, Help: "A stage (or child plan) that must land first"},
		{Name: "touches", From: []string{"stage"}, To: []string{"file"}, Help: "A file change this stage makes"},
		{Name: "covers", From: []string{"stage"}, To: []string{"ac"}, Help: "An AC this stage makes pass"},
		{Name: "discharges", From: []string{"ac"}, To: []string{"rail"}, CrossPlan: true, Help: "A rail this AC proves is honoured"},
		{Name: "addresses", From: []string{"goal"}, To: []string{"defect"}, Help: "A defect this goal fixes"},
		{Name: "about", From: []string{"tree", "annex"}, To: []string{AnyType}, Help: "The node this tree or annex illustrates"},
		{Name: "in", From: []string{"leg"}, To: []string{"journey"}, Help: "The journey this leg belongs to"},
		{Name: "honors", From: []string{"plan"}, To: []string{"rail"}, CrossPlan: true, Help: "An epic rail this plan honours"},
		{Name: "delivers", From: []string{"plan"}, To: []string{"leg"}, CrossPlan: true, Help: "An epic leg this plan delivers"},
		{Name: "builds-on", From: []string{"plan"}, To: []string{"decision"}, CrossPlan: true, Help: "An epic decision this plan builds on"},
	},
}
