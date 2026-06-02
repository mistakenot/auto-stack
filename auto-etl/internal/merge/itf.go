package merge

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// ITFTrace represents a parsed ITF (Informal Trace Format) trace file.
type ITFTrace struct {
	Meta   ITFMeta
	Vars   []string
	States []ITFState
}

// ITFMeta contains trace metadata.
type ITFMeta struct {
	Format      string
	Source      string
	Status      string
	Description string
}

// ITFState represents one state in the trace.
type ITFState struct {
	Index       int
	ActionTaken string
	NondetPicks map[string]interface{}
	StoreA      []MessageRecord
	StoreB      []MessageRecord
	StoreC      []MessageRecord
	SessA       []SessionRecord
	SessB       []SessionRecord
	SessC       []SessionRecord
}

// ParseITFFile reads and parses an ITF JSON trace file.
func ParseITFFile(path string) (*ITFTrace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading ITF file: %w", err)
	}
	return ParseITF(data)
}

// ParseITF parses ITF JSON bytes into an ITFTrace.
func ParseITF(data []byte) (*ITFTrace, error) {
	var raw struct {
		Meta   json.RawMessage   `json:"#meta"`
		Vars   []string          `json:"vars"`
		States []json.RawMessage `json:"states"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing ITF JSON: %w", err)
	}

	trace := &ITFTrace{
		Vars: raw.Vars,
	}

	// Parse meta
	if raw.Meta != nil {
		var meta struct {
			Format      string `json:"format"`
			Source      string `json:"source"`
			Status      string `json:"status"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw.Meta, &meta); err != nil {
			return nil, fmt.Errorf("parsing ITF meta: %w", err)
		}
		trace.Meta = ITFMeta{
			Format:      meta.Format,
			Source:      meta.Source,
			Status:      meta.Status,
			Description: meta.Description,
		}
	}

	// Parse states
	for i, rawState := range raw.States {
		state, err := parseITFState(rawState)
		if err != nil {
			return nil, fmt.Errorf("parsing state %d: %w", i, err)
		}
		trace.States = append(trace.States, *state)
	}

	return trace, nil
}

func parseITFState(data json.RawMessage) (*ITFState, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing state object: %w", err)
	}

	state := &ITFState{}

	// Parse #meta for index
	if metaRaw, ok := raw["#meta"]; ok {
		var meta struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return nil, fmt.Errorf("parsing state meta: %w", err)
		}
		state.Index = meta.Index
	}

	// Parse actionTaken
	if v, ok := raw["mbt::actionTaken"]; ok {
		var action string
		if err := json.Unmarshal(v, &action); err != nil {
			return nil, fmt.Errorf("parsing actionTaken: %w", err)
		}
		state.ActionTaken = action
	}

	// Parse nondetPicks
	if v, ok := raw["mbt::nondetPicks"]; ok {
		var picks map[string]interface{}
		if err := json.Unmarshal(v, &picks); err != nil {
			return nil, fmt.Errorf("parsing nondetPicks: %w", err)
		}
		state.NondetPicks = picks
	}

	// Parse message stores
	var err error
	state.StoreA, err = parseMessageSet(raw["storeA"])
	if err != nil {
		return nil, fmt.Errorf("parsing storeA: %w", err)
	}
	state.StoreB, err = parseMessageSet(raw["storeB"])
	if err != nil {
		return nil, fmt.Errorf("parsing storeB: %w", err)
	}
	state.StoreC, err = parseMessageSet(raw["storeC"])
	if err != nil {
		return nil, fmt.Errorf("parsing storeC: %w", err)
	}

	// Parse session stores
	state.SessA, err = parseSessionSet(raw["sessA"])
	if err != nil {
		return nil, fmt.Errorf("parsing sessA: %w", err)
	}
	state.SessB, err = parseSessionSet(raw["sessB"])
	if err != nil {
		return nil, fmt.Errorf("parsing sessB: %w", err)
	}
	state.SessC, err = parseSessionSet(raw["sessC"])
	if err != nil {
		return nil, fmt.Errorf("parsing sessC: %w", err)
	}

	return state, nil
}

// parseMessageSet parses an ITF set of MessageRecords.
// ITF encoding: {"#set": [record1, record2, ...]}
func parseMessageSet(data json.RawMessage) ([]MessageRecord, error) {
	if data == nil {
		return nil, nil
	}

	elements, err := parseITFSet(data)
	if err != nil {
		return nil, err
	}

	var records []MessageRecord
	for _, elem := range elements {
		rec, ok := elem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected message record object, got %T", elem)
		}
		msg := MessageRecord{
			ID:            rec["id"].(string),
			Content:       rec["content"].(string),
			SchemaVersion: decodeBigInt(rec["schema_version"]),
			DeletedAt:     decodeBigInt(rec["deleted_at"]),
		}
		records = append(records, msg)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})
	return records, nil
}

// parseSessionSet parses an ITF set of SessionRecords.
func parseSessionSet(data json.RawMessage) ([]SessionRecord, error) {
	if data == nil {
		return nil, nil
	}

	elements, err := parseITFSet(data)
	if err != nil {
		return nil, err
	}

	var records []SessionRecord
	for _, elem := range elements {
		rec, ok := elem.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected session record object, got %T", elem)
		}
		sess := SessionRecord{
			ID:            rec["id"].(string),
			LastMessageAt: decodeBigInt(rec["last_message_at"]),
			MessageCount:  decodeBigInt(rec["message_count"]),
			SchemaVersion: decodeBigInt(rec["schema_version"]),
			DeletedAt:     decodeBigInt(rec["deleted_at"]),
		}
		records = append(records, sess)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})
	return records, nil
}

// parseITFSet extracts elements from an ITF set encoding: {"#set": [...]}
func parseITFSet(data json.RawMessage) ([]interface{}, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing ITF set wrapper: %w", err)
	}

	setElems, ok := wrapper["#set"]
	if !ok {
		return nil, fmt.Errorf("expected #set key in ITF set encoding, got keys: %v", mapKeys(wrapper))
	}

	arr, ok := setElems.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array in #set, got %T", setElems)
	}

	return arr, nil
}

// decodeBigInt decodes an ITF bigint: {"#bigint": "123"} -> 123
func decodeBigInt(v interface{}) int {
	switch val := v.(type) {
	case map[string]interface{}:
		if s, ok := val["#bigint"]; ok {
			if str, ok := s.(string); ok {
				n, _ := strconv.Atoi(str)
				return n
			}
		}
	case float64:
		return int(val)
	}
	return 0
}

// DecodeNondetPick extracts a value from a nondetPicks Option type.
// Returns (value, true) for {"tag": "Some", "value": ...} and (nil, false) for {"tag": "None"}.
func DecodeNondetPick(picks map[string]interface{}, key string) (interface{}, bool) {
	v, ok := picks[key]
	if !ok {
		return nil, false
	}
	opt, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	tag, _ := opt["tag"].(string)
	if tag == "Some" {
		return opt["value"], true
	}
	return nil, false
}

// DecodeNondetString extracts a string from nondetPicks.
func DecodeNondetString(picks map[string]interface{}, key string) (string, bool) {
	v, ok := DecodeNondetPick(picks, key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// DecodeNondetInt extracts an int from nondetPicks (handles #bigint encoding).
func DecodeNondetInt(picks map[string]interface{}, key string) (int, bool) {
	v, ok := DecodeNondetPick(picks, key)
	if !ok {
		return 0, false
	}
	return decodeBigInt(v), true
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
