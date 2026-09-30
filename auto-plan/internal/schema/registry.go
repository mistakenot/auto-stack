// Package schema is the single extension point of auto-plan: every node type
// and edge type a plan graph may hold is declared here, and validation, lint,
// the generated `add` flags and the renderers all read this table. Adding a
// type means adding an entry below and changing nothing else.
package schema

import (
	"slices"
	"strings"
)

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
	// Fixed marks a field that is set when the node is created and that
	// `update` refuses to change (a plan's name must match its folder).
	Fixed bool
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
	// into another plan (`005:r-8hw3`).
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
	// PlanNumberPattern is a plan's 3-digit number.
	PlanNumberPattern = `^[0-9]{3}$`
	// RepoPathPattern is a repo-relative path: no leading slash, no spaces.
	RepoPathPattern = `^[^/\s]\S*$`
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
	Nodes: []NodeType{
		{
			Name: "plan", Term: "Plan", MinLifecycle: LifecycleRequirements,
			Help: "The plan header: exactly one per graph, ID `plan`.",
			Fields: []FieldSpec{
				{Name: "name", Kind: KindString, Required: true, Pattern: NamePattern, Fixed: true, Help: "Kebab-case plan name (matches the folder)"},
				{Name: "kind", Kind: KindEnum, Required: true, Enum: []string{"task", "epic"}, Help: "task or epic"},
				{Name: "lifecycle", Kind: KindEnum, Required: true, Enum: LifecycleNames(), Help: "Lifecycle step; selects which lint rules apply"},
				{Name: "created", Kind: KindString, Required: true, Pattern: DatePattern, Fixed: true, Help: "Creation date (YYYY-MM-DD)"},
				{Name: "epic", Kind: KindString, Pattern: PlanNumberPattern, Help: "Number of the epic plan this plan belongs to"},
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
			Help:      "A given/when/then criterion that proves one goal.",
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
				{Name: "deferred", Kind: KindList, Pattern: PlanNumberPattern, Help: "Child plan numbers excused from honouring the rail for now"},
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
				{Name: "plan", Kind: KindString, Required: true, Pattern: PlanNumberPattern, Help: "The child plan's number (NNN)"},
				{Name: "title", Kind: KindString, Help: "What the child delivers, in one line"},
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
		{Name: "about", From: []string{"tree"}, To: []string{AnyType}, Help: "The node this tree illustrates"},
		{Name: "in", From: []string{"leg"}, To: []string{"journey"}, Help: "The journey this leg belongs to"},
		{Name: "honors", From: []string{"plan"}, To: []string{"rail"}, CrossPlan: true, Help: "An epic rail this plan honours"},
		{Name: "delivers", From: []string{"plan"}, To: []string{"leg"}, CrossPlan: true, Help: "An epic leg this plan delivers"},
		{Name: "builds-on", From: []string{"plan"}, To: []string{"decision"}, CrossPlan: true, Help: "An epic decision this plan builds on"},
	},
}
