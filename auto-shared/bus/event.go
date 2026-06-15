// Package bus defines the auto-bus event envelope, hub broadcast core, and
// typed payload helpers. The envelope is CloudEvents-shaped with workspace
// provenance attributes; the data payload is opaque (json.RawMessage) with
// typed constructor/decoder helpers for hub-authored events.
package bus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"

	"github.com/mistakenot/auto-shared/config"
)

// SpecVersion is the bus envelope version stamped on every event.
const SpecVersion = "1.0"

// ValidationError is the shared structured field-level error.
type ValidationError = config.ValidationError

// dottedType matches dotted hierarchical event types like "agent.tool.post".
var dottedType = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9]+)+$`)

// Event is the canonical bus envelope. It is strongly typed and validated;
// the Data payload is opaque and typed only where the bus authors it.
type Event struct {
	SpecVersion string `json:"specversion"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	ID          string `json:"id"`
	Time        string `json:"time"`
	Project     string `json:"project,omitempty"`
	Session     string `json:"session,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Worktree    string `json:"worktree,omitempty"`
	Commit      string `json:"commit,omitempty"`
	// Env carries optional terminal/orchestrator context captured from the
	// hook process environment (NTM_*/TMUX_* variables). It is omitted when no
	// such variables are present, and never participates in envelope validation.
	Env  map[string]string `json:"env,omitempty"`
	Data json.RawMessage   `json:"data,omitempty"`
}

// NewEvent constructs an Event with the given type, source, and data payload.
// It sets specversion, a random id, and the current time. Data is marshalled
// to JSON; pass nil for an event with no data.
func NewEvent(typ, source string, data any) (Event, error) {
	ev := Event{
		SpecVersion: SpecVersion,
		Type:        typ,
		Source:      source,
		ID:          newID(),
		Time:        time.Now().UTC().Format(time.RFC3339),
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return Event{}, err
		}
		ev.Data = raw
	}
	return ev, nil
}

// Validate checks the structural integrity of the event envelope, returning
// structured errors. An empty slice means the event is valid.
func (e Event) Validate() []ValidationError {
	var errs []ValidationError
	if e.SpecVersion == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "specversion", Message: "specversion is required"})
	}
	if e.Type == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "type", Message: "type is required"})
	} else if !dottedType.MatchString(e.Type) {
		errs = append(errs, ValidationError{Code: "format", Field: "type", Message: "type must be dotted (e.g. agent.tool.post)", Value: e.Type})
	}
	if e.Source == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "source", Message: "source is required"})
	}
	if e.ID == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "id", Message: "id is required"})
	}
	if e.Time == "" {
		errs = append(errs, ValidationError{Code: "required", Field: "time", Message: "time is required"})
	} else if _, err := time.Parse(time.RFC3339, e.Time); err != nil {
		if _, err2 := time.Parse(time.RFC3339Nano, e.Time); err2 != nil {
			errs = append(errs, ValidationError{Code: "format", Field: "time", Message: "time must be RFC 3339", Value: e.Time})
		}
	}
	return errs
}

// Notification is the JSON-RPC 2.0 notification wrapper for bus events.
// It is bus-defined because auto-shared cannot reference auto-ui's unexported
// rpcRequest type. session.enqueue(any) + wsjson.Write marshal it.
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  Event  `json:"params"`
}

// AsNotification wraps the event as a JSON-RPC 2.0 notification using
// the event's Type as the method.
func (e Event) AsNotification() Notification {
	return Notification{
		JSONRPC: "2.0",
		Method:  e.Type,
		Params:  e,
	}
}

// newID generates a random 16-hex-character event id.
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use current time nanos as entropy (should never happen).
		return hex.EncodeToString([]byte(time.Now().String()))[:16]
	}
	return hex.EncodeToString(b)
}
