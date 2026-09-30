// Package schema is the single extension point of auto-plan: every node type
// and edge type a plan graph may hold is declared here, and validation, lint,
// the generated `add` flags and the renderers all read this table. Adding a
// type means adding an entry below and changing nothing else.
package schema

import "slices"

// Lifecycle is the step a plan has reached. Lint rules and node types declare
// the earliest step at which they apply, so partial plans stay valid (D-10).
type Lifecycle string

const (
	LifecycleRequirements Lifecycle = "requirements"
	LifecycleSolution     Lifecycle = "solution"
	LifecyclePlan         Lifecycle = "plan"
	LifecycleExecuting    Lifecycle = "executing"
	LifecycleDone         Lifecycle = "done"
)

// Lifecycles lists every lifecycle step in order.
var Lifecycles = []Lifecycle{
	LifecycleRequirements,
	LifecycleSolution,
	LifecyclePlan,
	LifecycleExecuting,
	LifecycleDone,
}

// Index returns the position of l in Lifecycles, or -1 when l is unknown.
func (l Lifecycle) Index() int { return slices.Index(Lifecycles, l) }

// AtLeast reports whether l has reached min. An unknown l reaches nothing.
func (l Lifecycle) AtLeast(minimum Lifecycle) bool {
	i := l.Index()
	return i >= 0 && i >= minimum.Index()
}

// LifecycleNames returns the lifecycle steps as strings, for enums and help.
func LifecycleNames() []string {
	out := make([]string, len(Lifecycles))
	for i, l := range Lifecycles {
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
	// Fields holds the members of a KindObject field. Their flags are named
	// `--<field>-<member>`.
	Fields []FieldSpec
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
	Help      string
}

// Singleton reports whether the type has exactly one node with a fixed ID.
func (n NodeType) Singleton() bool { return n.Prefix == "" }

// Field returns the named field spec.
func (n NodeType) Field(name string) (FieldSpec, bool) {
	for _, f := range n.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldSpec{}, false
}

// EdgeType declares one edge type and the node types allowed at each end.
type EdgeType struct {
	Name string
	From []string
	To   []string
	// CrossPlan marks edge types whose `to` may be a qualified reference
	// into another plan (`005:r-8hw3`).
	CrossPlan bool
	Help      string
}

// Schema is the full set of registered node and edge types.
type Schema struct {
	Nodes []NodeType
	Edges []EdgeType
}

// Node returns the named node type.
func (s *Schema) Node(name string) (NodeType, bool) {
	for _, n := range s.Nodes {
		if n.Name == name {
			return n, true
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
		if slices.Contains(e.From, typ) {
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
	// PlanNumberPattern is a plan's 3-digit number.
	PlanNumberPattern = `^[0-9]{3}$`
)

// PlanNodeID is the fixed ID of the one plan node in every graph.
const PlanNodeID = "plan"

func titleField(help string) FieldSpec {
	return FieldSpec{Name: "title", Kind: KindString, Required: true, Help: help}
}

// Registry is the one table every part of auto-plan reads.
var Registry = Schema{
	Nodes: []NodeType{
		{
			Name: "plan", Term: "Plan", MinLifecycle: LifecycleRequirements,
			Help: "The plan header: exactly one per graph, ID `plan`.",
			Fields: []FieldSpec{
				{Name: "name", Kind: KindString, Required: true, Pattern: NamePattern, Help: "Kebab-case plan name"},
				{Name: "kind", Kind: KindEnum, Required: true, Enum: []string{"task", "epic"}, Help: "task or epic"},
				{Name: "lifecycle", Kind: KindEnum, Required: true, Enum: LifecycleNames(), Help: "Lifecycle step; selects which lint rules apply"},
				{Name: "created", Kind: KindString, Required: true, Pattern: DatePattern, Help: "Creation date (YYYY-MM-DD)"},
				{Name: "epic", Kind: KindString, Pattern: PlanNumberPattern, Help: "Number of the epic plan this plan belongs to"},
			},
		},
		{
			Name: "goal", Prefix: "g", Term: "Goal", MinLifecycle: LifecycleRequirements,
			Help: "One outcome the plan commits to.",
			Fields: []FieldSpec{
				titleField("One-line statement of the outcome"),
				{Name: "description", Kind: KindText, Help: "Optional Markdown detail"},
			},
		},
		{
			Name: "ac", Prefix: "ac", Term: "Acceptance Criterion", MinLifecycle: LifecycleSolution,
			RankScope: "proves",
			Help:      "A given/when/then criterion that proves one goal.",
			Fields: []FieldSpec{
				titleField("One-line summary of the criterion"),
				{Name: "gwt", Kind: KindText, Help: "Given/when/then Markdown"},
				{
					Name: "verify", Kind: KindObject, Help: "How the criterion is verified",
					Fields: []FieldSpec{
						{Name: "cmd", Kind: KindString, Help: "Reproducible verify command"},
						{Name: "tests", Kind: KindList, Help: "Test names the command runs"},
						{Name: "kind", Kind: KindEnum, Enum: []string{"command", "manual"}, Help: "How it is verified (default command)"},
					},
				},
			},
		},
		{
			Name: "decision", Prefix: "d", Term: "Decision", MinLifecycle: LifecycleRequirements,
			Help: "A settled choice, with why and the alternatives it rejected.",
			Fields: []FieldSpec{
				titleField("One-line statement of the decision"),
				{Name: "chosen", Kind: KindText, Help: "What was chosen"},
				{Name: "why", Kind: KindText, Help: "Why it was chosen"},
				{Name: "by", Kind: KindString, Help: "Who decided"},
			},
		},
	},
	Edges: []EdgeType{
		{Name: "proves", From: []string{"ac"}, To: []string{"goal"}, Help: "The goal this AC proves"},
		{Name: "constrains", From: []string{"decision"}, To: []string{"goal", "ac"}, Help: "A goal or AC this decision constrains"},
	},
}
