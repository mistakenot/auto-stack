package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/store"
)

// emptyPlaybook is the seed snapshot written by init and used when no events
// exist yet.
func emptyPlaybook() Playbook {
	return Playbook{
		SchemaVersion: SchemaVersion,
		FoldedThrough: map[string]int{},
		Rules:         []Rule{},
	}
}

// LoadSnapshot reads the playbook snapshot at path. A missing file yields an
// empty playbook (not an error) so first-run reads work before init seeds it.
func LoadSnapshot(path string) (Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyPlaybook(), nil
		}
		return Playbook{}, fmt.Errorf("read playbook: %w", err)
	}
	var pb Playbook
	if err := json.Unmarshal(data, &pb); err != nil {
		return Playbook{}, errors.New("playbook parse error: delete .auto/reflect/playbook.json and run `auto reflect rebuild`")
	}
	if pb.FoldedThrough == nil {
		pb.FoldedThrough = map[string]int{}
	}
	return pb, nil
}

// WriteSnapshot atomically writes the playbook snapshot via store.WriteJSONFile.
func WriteSnapshot(path string, pb Playbook) error {
	if pb.FoldedThrough == nil {
		pb.FoldedThrough = map[string]int{}
	}
	if pb.Rules == nil {
		pb.Rules = []Rule{}
	}
	if err := store.WriteJSONFile(path, pb); err != nil {
		return fmt.Errorf("write playbook: %w", err)
	}
	return nil
}

// maxRuleSeqPerShard returns the highest rule-event (rule_created/rule_edited)
// seq for each shard. Non-rule events never appear here, so loop traffic never
// makes the snapshot look stale.
func maxRuleSeqPerShard(sharded []events.ShardedEvent) map[string]int {
	out := make(map[string]int)
	for _, se := range sharded {
		if !events.IsRuleEvent(se.Event.Type) {
			continue
		}
		if se.Event.Seq > out[se.Shard] {
			out[se.Shard] = se.Event.Seq
		}
	}
	return out
}

// isStale reports whether the snapshot's folded_through lags the event log's
// per-shard rule-event high-water marks. A shard present in the log but absent
// from folded_through (or with a higher rule seq) means the snapshot is stale.
func isStale(folded, current map[string]int) bool {
	for shard, seq := range current {
		if folded[shard] < seq {
			return true
		}
	}
	return false
}

// Load returns the current rule projection for repoRoot, refolding from the
// event log only when the snapshot is stale with respect to rule events. The
// returned playbook is written back to path when a refold occurred so the
// committed snapshot stays current; non-rule events never trigger a rewrite.
func Load(repoRoot, path string) (Playbook, error) {
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		return Playbook{}, err
	}

	sharded, err := events.ReadAllSharded(repoRoot)
	if err != nil {
		return Playbook{}, err
	}

	current := maxRuleSeqPerShard(sharded)
	if !isStale(snapshot.FoldedThrough, current) && snapshot.SchemaVersion == SchemaVersion {
		return snapshot, nil
	}

	result := Fold(sharded)
	if err := WriteSnapshot(path, result.Playbook); err != nil {
		return Playbook{}, err
	}
	return result.Playbook, nil
}

// Rebuild forces a full refold from the event log, writes the snapshot, and
// returns any conflicts encountered so the caller can surface them.
func Rebuild(repoRoot, path string) (Playbook, []Conflict, error) {
	sharded, err := events.ReadAllSharded(repoRoot)
	if err != nil {
		return Playbook{}, nil, err
	}
	result := Fold(sharded)
	if err := WriteSnapshot(path, result.Playbook); err != nil {
		return Playbook{}, nil, err
	}
	return result.Playbook, result.Conflicts, nil
}
