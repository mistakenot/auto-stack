package rules

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-reflect/internal/events"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/playbook.golden.json from the fixture fold")

// foldFixture reads the checked-in event fixture under testdata/events and folds
// it. The fixture root is repoRoot/.auto/reflect/events per the events layout;
// we symlink-free it by pointing ReadAllSharded at a temp repo whose events dir
// is the fixture. Simpler: copy fixture shards into a temp events dir.
func foldFixture(t *testing.T) FoldResult {
	t.Helper()
	repoRoot := t.TempDir()
	eventsDir := filepath.Join(repoRoot, ".auto", "reflect", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	srcDir := filepath.Join("testdata", "events")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatalf("read fixture %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(eventsDir, e.Name()), data, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", e.Name(), err)
		}
	}
	sharded, err := events.ReadAllSharded(repoRoot)
	if err != nil {
		t.Fatalf("read sharded: %v", err)
	}
	return Fold(sharded)
}

func TestFoldGoldenFixture(t *testing.T) {
	result := foldFixture(t)
	got, err := json.MarshalIndent(result.Playbook, "", "  ")
	if err != nil {
		t.Fatalf("marshal playbook: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "playbook.golden.json")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run `go test ./internal/rules/ -run TestFoldGoldenFixture -update-golden` to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fold does not match golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFoldDeterministicAcrossRuns(t *testing.T) {
	first := foldFixture(t)
	second := foldFixture(t)
	a, err := json.Marshal(first.Playbook)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	b, err := json.Marshal(second.Playbook)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("fold not deterministic across runs\nfirst:  %s\nsecond: %s", a, b)
	}
}

func TestFoldRecordsExactlyOneConflict(t *testing.T) {
	result := foldFixture(t)
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected exactly one conflict, got %d: %#v", len(result.Conflicts), result.Conflicts)
	}
	c := result.Conflicts[0]
	if c.RuleID != "r-00000001" {
		t.Fatalf("conflict rule_id = %q, want r-00000001", c.RuleID)
	}
	if c.Expected != 2 || c.Actual != 3 {
		t.Fatalf("conflict expected/actual = %d/%d, want 2/3", c.Expected, c.Actual)
	}
	if len(c.Fields) != 1 || c.Fields[0] != "content" {
		t.Fatalf("conflict fields = %#v, want [content]", c.Fields)
	}
}

func TestFoldDeterministicConflictWinner(t *testing.T) {
	result := foldFixture(t)
	r := findRule(t, result.Playbook, "r-00000001")
	// Last-writer in total order is the shard B content edit; version is bumped
	// from the current folded version (3) to 4, not to the event's to_version.
	if r.Version != 4 {
		t.Fatalf("version = %d, want 4", r.Version)
	}
	if r.Content != "register via newRootCmd AddCommand and add a quickstart line" {
		t.Fatalf("content = %q, want the shard B (last-writer) content", r.Content)
	}
}

func TestFoldRebuildsVersionHistory(t *testing.T) {
	result := foldFixture(t)
	r := findRule(t, result.Playbook, "r-00000001")
	// v1 created, v2 content edit, v3 use_when+lifecycle edit, v4 conflict edit.
	if r.Version != 4 {
		t.Fatalf("final version = %d, want 4", r.Version)
	}
	if r.UseWhen != "adding any new subcommand to the CLI" {
		t.Fatalf("use_when not from v3 edit: %q", r.UseWhen)
	}
	if r.Lifecycle != LifecycleConfirmed {
		t.Fatalf("lifecycle = %q, want confirmed (from v3 edit)", r.Lifecycle)
	}
	if r.CreatedAt != "2026-06-01T10:00:00Z" {
		t.Fatalf("created_at = %q, want the create event ts", r.CreatedAt)
	}
	if r.UpdatedAt != "2026-06-02T10:00:00Z" {
		t.Fatalf("updated_at = %q, want the last edit ts", r.UpdatedAt)
	}
}

func TestFoldedThroughCountsOnlyRuleEvents(t *testing.T) {
	result := foldFixture(t)
	ft := result.Playbook.FoldedThrough
	if got := ft["hosta-2026-06-01-aaaaaaaa.jsonl"]; got != 6 {
		t.Fatalf("shard A folded_through = %d, want 6 (last rule event seq)", got)
	}
	// Shard B seq 1 is a feedback event; the last RULE event is seq 2.
	if got := ft["hostb-2026-06-02-bbbbbbbb.jsonl"]; got != 2 {
		t.Fatalf("shard B folded_through = %d, want 2 (rule event seq, not feedback)", got)
	}
}

// shardedRuleEvent builds a single-shard ShardedEvent for an in-memory fold test.
func shardedRuleEvent(t *testing.T, seq int, ts, typ string, payload any) events.ShardedEvent {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return events.ShardedEvent{
		Shard: "host-2026-06-10-aaaaaaaa.jsonl",
		Event: events.Event{Type: typ, Seq: seq, TS: ts, Payload: raw},
	}
}

func TestFoldProvenanceFromCreateAndEdit(t *testing.T) {
	// rule_created carries provenance; a later observation_ids edit replaces it.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-06-10T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-aaaaaaaa", Domain: []string{"go"}, UseWhen: "x", Content: "c",
			CausalNote: "n", RuleType: RuleTypeSoft, Lifecycle: LifecycleDraft,
			ObservationIDs: []string{"ob-00000001", "ob-00000002"},
		}),
		shardedRuleEvent(t, 2, "2026-06-10T11:00:00Z", events.TypeRuleEdited, events.RuleEditedPayload{
			RuleID: "r-aaaaaaaa", FromVersion: 1, ToVersion: 2,
			Deltas: []events.FieldDelta{{
				Field: FieldObservationIDs,
				Old:   []string{"ob-00000001", "ob-00000002"},
				New:   []string{"ob-00000001", "ob-00000002", "ob-00000003"},
			}},
		}),
	})
	r := findRule(t, result.Playbook, "r-aaaaaaaa")
	if len(r.ObservationIDs) != 3 || r.ObservationIDs[2] != "ob-00000003" {
		t.Fatalf("provenance not folded from create+edit: %#v", r.ObservationIDs)
	}
}

func TestFoldEmptyProvenanceStaysNil(t *testing.T) {
	// A rule with no provenance must fold to a nil slice (omitempty drops it),
	// not an empty [] that would noise up the golden snapshot diff.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-06-10T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-bbbbbbbb", Domain: []string{"go"}, UseWhen: "x", Content: "c",
			CausalNote: "n", RuleType: RuleTypeSoft, Lifecycle: LifecycleDraft,
		}),
	})
	r := findRule(t, result.Playbook, "r-bbbbbbbb")
	if r.ObservationIDs != nil {
		t.Fatalf("expected nil provenance, got %#v", r.ObservationIDs)
	}
}

func TestFoldLineageAndLintRefFromCreate(t *testing.T) {
	// A rule_created carrying lineage + lint_ref folds those fields onto the rule.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-06-10T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-cccccccc", Domain: []string{"go"}, UseWhen: "x", Content: "c",
			CausalNote: "n", RuleType: RuleTypeSoft, Lifecycle: LifecycleEnforced,
			PredecessorIDs: []string{"r-00000001"},
			SuccessorIDs:   []string{"r-00000002"},
			LintRef:        &events.LintRef{Linter: "golangci-lint", Check: "errcheck"},
		}),
	})
	r := findRule(t, result.Playbook, "r-cccccccc")
	if len(r.PredecessorIDs) != 1 || r.PredecessorIDs[0] != "r-00000001" {
		t.Fatalf("predecessor_ids not folded: %#v", r.PredecessorIDs)
	}
	if len(r.SuccessorIDs) != 1 || r.SuccessorIDs[0] != "r-00000002" {
		t.Fatalf("successor_ids not folded: %#v", r.SuccessorIDs)
	}
	if r.LintRef == nil || r.LintRef.Linter != "golangci-lint" || r.LintRef.Check != "errcheck" {
		t.Fatalf("lint_ref not folded: %#v", r.LintRef)
	}
}

func TestFoldLifecycleToEnforcedWithLintRefDelta(t *testing.T) {
	// A graduate edit carries a lifecycle->enforced delta plus a lint_ref delta in
	// one event; both fold onto the rule with a single version bump.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-06-10T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-dddddddd", Domain: []string{"go"}, UseWhen: "x", Content: "c",
			CausalNote: "n", RuleType: RuleTypeSoft, Lifecycle: LifecycleConfirmed,
		}),
		shardedRuleEvent(t, 2, "2026-06-10T11:00:00Z", events.TypeRuleEdited, events.RuleEditedPayload{
			RuleID: "r-dddddddd", FromVersion: 1, ToVersion: 2,
			Deltas: []events.FieldDelta{
				{Field: FieldLifecycle, Old: LifecycleConfirmed, New: LifecycleEnforced},
				{Field: FieldLintRef, Old: nil, New: &events.LintRef{Linter: "golangci-lint", Check: "errcheck"}},
			},
		}),
	})
	r := findRule(t, result.Playbook, "r-dddddddd")
	if r.Lifecycle != LifecycleEnforced {
		t.Fatalf("lifecycle = %q, want enforced", r.Lifecycle)
	}
	if r.Version != 2 {
		t.Fatalf("version = %d, want 2 (single bump)", r.Version)
	}
	if r.LintRef == nil || r.LintRef.Check != "errcheck" {
		t.Fatalf("lint_ref delta not folded: %#v", r.LintRef)
	}
}

func TestFoldPreChangeEventLogIsBackwardCompatible(t *testing.T) {
	// A pre-change event log carries NONE of the lineage/lint/provenance fields
	// added in task 049: rule_created/rule_edited with bare payloads and an
	// observation with no task_id and evidence with no file/line/commit. Folding
	// it under the current code must project identically to the old behavior:
	// the new fields stay empty/nil, the schema stays version 1, and no panic.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-05-01T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-0000000a", Domain: []string{"go"}, UseWhen: "editing go files",
			Content: "run go build ./... from the module dir", CausalNote: "unbuilt files hide errors",
			RuleType: RuleTypeSoft, Lifecycle: LifecycleDraft,
		}),
		shardedRuleEvent(t, 2, "2026-05-01T11:00:00Z", events.TypeRuleEdited, events.RuleEditedPayload{
			RuleID: "r-0000000a", FromVersion: 1, ToVersion: 2,
			Deltas: []events.FieldDelta{
				{Field: FieldLifecycle, Old: LifecycleDraft, New: LifecycleConfirmed},
			},
		}),
		shardedRuleEvent(t, 3, "2026-05-01T12:00:00Z", events.TypeObservation, events.ObservationPayload{
			ObservationID: "ob-0000000a", Kind: "gap", Subject: "no go build guidance",
			Evidence: []events.ObservationEvidence{{SessionID: "sess-1"}},
			Severity: "normal",
		}),
	})

	if result.Playbook.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", result.Playbook.SchemaVersion, SchemaVersion)
	}
	r := findRule(t, result.Playbook, "r-0000000a")
	if r.Version != 2 {
		t.Fatalf("version = %d, want 2", r.Version)
	}
	if r.Lifecycle != LifecycleConfirmed {
		t.Fatalf("lifecycle = %q, want confirmed", r.Lifecycle)
	}
	// The new fields must fold to empty/nil so omitempty drops them entirely —
	// a pre-change log serializes byte-identically to the old projection.
	if r.PredecessorIDs != nil {
		t.Fatalf("predecessor_ids = %#v, want nil for a pre-change log", r.PredecessorIDs)
	}
	if r.SuccessorIDs != nil {
		t.Fatalf("successor_ids = %#v, want nil for a pre-change log", r.SuccessorIDs)
	}
	if r.LintRef != nil {
		t.Fatalf("lint_ref = %#v, want nil for a pre-change log", r.LintRef)
	}
	if r.ObservationIDs != nil {
		t.Fatalf("observation_ids = %#v, want nil for a pre-change log", r.ObservationIDs)
	}
}

func TestFoldSurfacesTimestampsForListView(t *testing.T) {
	// Fold sets created_at from the create event ts and updated_at from the last
	// edit ts (F6). The `rule list` view projects these onto each row, so a folded
	// rule must carry non-empty timestamps that survive into the list projection.
	result := Fold([]events.ShardedEvent{
		shardedRuleEvent(t, 1, "2026-06-10T10:00:00Z", events.TypeRuleCreated, events.RuleCreatedPayload{
			RuleID: "r-eeeeeeee", Domain: []string{"go"}, UseWhen: "x", Content: "c",
			CausalNote: "n", RuleType: RuleTypeSoft, Lifecycle: LifecycleDraft,
		}),
		shardedRuleEvent(t, 2, "2026-06-10T11:00:00Z", events.TypeRuleEdited, events.RuleEditedPayload{
			RuleID: "r-eeeeeeee", FromVersion: 1, ToVersion: 2,
			Deltas: []events.FieldDelta{{Field: FieldContent, Old: "c", New: "c2"}},
		}),
	})
	r := findRule(t, result.Playbook, "r-eeeeeeee")
	if r.CreatedAt != "2026-06-10T10:00:00Z" {
		t.Fatalf("created_at = %q, want the create event ts", r.CreatedAt)
	}
	if r.UpdatedAt != "2026-06-10T11:00:00Z" {
		t.Fatalf("updated_at = %q, want the last edit ts", r.UpdatedAt)
	}

	// The `rule list` view (cli/rule.go) projects a row map per rule; mirror that
	// projection here and confirm it now carries created_at/updated_at (F6: the
	// hand-rolled list map previously dropped them).
	row := map[string]any{
		"id":              r.ID,
		"use_when":        r.UseWhen,
		"domain":          r.Domain,
		"rule_type":       r.RuleType,
		"lifecycle":       r.Lifecycle,
		"observation_ids": r.ObservationIDs,
		"created_at":      r.CreatedAt,
		"updated_at":      r.UpdatedAt,
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal list row: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal list row: %v", err)
	}
	if got, ok := decoded["created_at"].(string); !ok || got != r.CreatedAt {
		t.Fatalf("list row created_at = %v (ok=%v), want %q", decoded["created_at"], ok, r.CreatedAt)
	}
	if got, ok := decoded["updated_at"].(string); !ok || got != r.UpdatedAt {
		t.Fatalf("list row updated_at = %v (ok=%v), want %q", decoded["updated_at"], ok, r.UpdatedAt)
	}
}

func findRule(t *testing.T, pb Playbook, id string) Rule {
	t.Helper()
	for i := range pb.Rules {
		if pb.Rules[i].ID == id {
			return pb.Rules[i]
		}
	}
	t.Fatalf("rule %s not found in playbook", id)
	return Rule{}
}
