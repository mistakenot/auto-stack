package merge

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// ITFTrace represents a parsed ITF (Informal Trace Format) trace file.
// See: https://apalache-mc.org/docs/adr/015adr-trace.html
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
	// Named variables — keyed by variable name from the trace.
	// Values are raw ITF-decoded Go values (use typed accessors below).
	Vars map[string]json.RawMessage

	// Typed accessors for etl_merge_test module variables.
	// Populated by parseETLMergeState; nil for other modules.
	StoreA []MessageRecord
	StoreB []MessageRecord
	StoreC []MessageRecord
	SessA  []SessionRecord
	SessB  []SessionRecord
	SessC  []SessionRecord
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

	state := &ITFState{
		Vars: make(map[string]json.RawMessage),
	}

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

	// Parse MBT metadata
	if v, ok := raw["mbt::actionTaken"]; ok {
		var action string
		if err := json.Unmarshal(v, &action); err != nil {
			return nil, fmt.Errorf("parsing actionTaken: %w", err)
		}
		state.ActionTaken = action
	}

	if v, ok := raw["mbt::nondetPicks"]; ok {
		var picks map[string]interface{}
		if err := json.Unmarshal(v, &picks); err != nil {
			return nil, fmt.Errorf("parsing nondetPicks: %w", err)
		}
		state.NondetPicks = picks
	}

	// Store all variable raw data for generic access
	for k, v := range raw {
		if k == "#meta" || k == "mbt::actionTaken" || k == "mbt::nondetPicks" {
			continue
		}
		state.Vars[k] = v
	}

	// Try to populate typed accessors for etl_merge_test variables
	if err := parseETLMergeState(state, raw); err != nil {
		return nil, err
	}

	return state, nil
}

// parseETLMergeState populates the typed StoreA/B/C and SessA/B/C fields
// if those variables are present in the trace state.
func parseETLMergeState(state *ITFState, raw map[string]json.RawMessage) error {
	var err error

	if v, ok := raw["storeA"]; ok {
		state.StoreA, err = parseMessageSet(v)
		if err != nil {
			return fmt.Errorf("parsing storeA: %w", err)
		}
	}
	if v, ok := raw["storeB"]; ok {
		state.StoreB, err = parseMessageSet(v)
		if err != nil {
			return fmt.Errorf("parsing storeB: %w", err)
		}
	}
	if v, ok := raw["storeC"]; ok {
		state.StoreC, err = parseMessageSet(v)
		if err != nil {
			return fmt.Errorf("parsing storeC: %w", err)
		}
	}
	if v, ok := raw["sessA"]; ok {
		state.SessA, err = parseSessionSet(v)
		if err != nil {
			return fmt.Errorf("parsing sessA: %w", err)
		}
	}
	if v, ok := raw["sessB"]; ok {
		state.SessB, err = parseSessionSet(v)
		if err != nil {
			return fmt.Errorf("parsing sessB: %w", err)
		}
	}
	if v, ok := raw["sessC"]; ok {
		state.SessC, err = parseSessionSet(v)
		if err != nil {
			return fmt.Errorf("parsing sessC: %w", err)
		}
	}

	return nil
}

// parseMessageSet parses an ITF set of MessageRecords.
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
		sv, err := decodeBigInt(rec["schema_version"])
		if err != nil {
			return nil, fmt.Errorf("decoding schema_version: %w", err)
		}
		da, err := decodeBigInt(rec["deleted_at"])
		if err != nil {
			return nil, fmt.Errorf("decoding deleted_at: %w", err)
		}
		msg := MessageRecord{
			ID:            rec["id"].(string),
			Content:       rec["content"].(string),
			SchemaVersion: sv,
			DeletedAt:     da,
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
		lma, err := decodeBigInt(rec["last_message_at"])
		if err != nil {
			return nil, fmt.Errorf("decoding last_message_at: %w", err)
		}
		mc, err := decodeBigInt(rec["message_count"])
		if err != nil {
			return nil, fmt.Errorf("decoding message_count: %w", err)
		}
		sv, err := decodeBigInt(rec["schema_version"])
		if err != nil {
			return nil, fmt.Errorf("decoding schema_version: %w", err)
		}
		da, err := decodeBigInt(rec["deleted_at"])
		if err != nil {
			return nil, fmt.Errorf("decoding deleted_at: %w", err)
		}
		sess := SessionRecord{
			ID:            rec["id"].(string),
			LastMessageAt: lma,
			MessageCount:  mc,
			SchemaVersion: sv,
			DeletedAt:     da,
		}
		records = append(records, sess)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].ID < records[j].ID
	})
	return records, nil
}

// --- ITF type decoders ---
// See: https://apalache-mc.org/docs/adr/015adr-trace.html

// parseITFSet extracts elements from {"#set": [...]}
func parseITFSet(data json.RawMessage) ([]interface{}, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing ITF set wrapper: %w", err)
	}

	setElems, ok := wrapper["#set"]
	if !ok {
		return nil, fmt.Errorf("expected #set key in ITF encoding, got keys: %v", mapKeys(wrapper))
	}

	arr, ok := setElems.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array in #set, got %T", setElems)
	}

	return arr, nil
}

// ParseITFMap extracts key-value pairs from {"#map": [[k1,v1], [k2,v2], ...]}
func ParseITFMap(data json.RawMessage) ([][2]interface{}, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing ITF map wrapper: %w", err)
	}

	mapElems, ok := wrapper["#map"]
	if !ok {
		return nil, fmt.Errorf("expected #map key in ITF encoding, got keys: %v", mapKeys(wrapper))
	}

	arr, ok := mapElems.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array in #map, got %T", mapElems)
	}

	var pairs [][2]interface{}
	for i, elem := range arr {
		pair, ok := elem.([]interface{})
		if !ok || len(pair) != 2 {
			return nil, fmt.Errorf("#map entry %d: expected [key, value] pair, got %T (len %d)", i, elem, len(pair))
		}
		pairs = append(pairs, [2]interface{}{pair[0], pair[1]})
	}

	return pairs, nil
}

// ParseITFTuple extracts elements from {"#tup": [e1, e2, ...]}
func ParseITFTuple(data json.RawMessage) ([]interface{}, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing ITF tuple wrapper: %w", err)
	}

	tupElems, ok := wrapper["#tup"]
	if !ok {
		return nil, fmt.Errorf("expected #tup key in ITF encoding, got keys: %v", mapKeys(wrapper))
	}

	arr, ok := tupElems.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array in #tup, got %T", tupElems)
	}

	return arr, nil
}

// decodeBigInt decodes an ITF bigint: {"#bigint": "123"} -> 123
// Returns an error on malformed input instead of silently returning 0.
func decodeBigInt(v interface{}) (int, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		s, ok := val["#bigint"]
		if !ok {
			return 0, fmt.Errorf("expected #bigint key, got keys: %v", mapKeys(val))
		}
		str, ok := s.(string)
		if !ok {
			return 0, fmt.Errorf("#bigint value is %T, expected string", s)
		}
		n, err := strconv.Atoi(str)
		if err != nil {
			return 0, fmt.Errorf("parsing #bigint %q: %w", str, err)
		}
		return n, nil
	case float64:
		return int(val), nil
	case nil:
		return 0, fmt.Errorf("unexpected nil value for integer field")
	default:
		return 0, fmt.Errorf("unexpected type %T for integer field", v)
	}
}

// --- NondetPicks helpers ---

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
	n, err := decodeBigInt(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
