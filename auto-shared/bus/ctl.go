package bus

import "fmt"

// Control-plane event types. These are infrastructure-level events emitted by
// the daemon for observability (logging, connection lifecycle, health). They
// are gated behind --ctl-events and off by default so they add no noise in
// normal operation.
const (
	TypeCtlLogInfo    = "ctl.log.info"
	TypeCtlLogWarn    = "ctl.log.warn"
	TypeCtlLogError   = "ctl.log.error"
	TypeCtlConnect    = "ctl.connect"
	TypeCtlDisconnect = "ctl.disconnect"
	TypeCtlHealth     = "ctl.health"
)

// CtlLogEvent is the data payload for ctl.log.* events. It carries a
// structured log entry from the daemon's control plane.
type CtlLogEvent struct {
	Level   string            `json:"level"`
	Op      string            `json:"op"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// NewCtlLog constructs a ctl.log.{level} event with the given operation name,
// message, and optional structured fields. Level must be one of "info",
// "warn", or "error"; unknown levels return an error.
func NewCtlLog(level, op, msg string, fields map[string]string) (Event, error) {
	var typ string
	switch level {
	case "info":
		typ = TypeCtlLogInfo
	case "warn":
		typ = TypeCtlLogWarn
	case "error":
		typ = TypeCtlLogError
	default:
		return Event{}, fmt.Errorf("unknown ctl log level: %q (must be info, warn, or error)", level)
	}
	return NewEvent(typ, "auto/watch/daemon", CtlLogEvent{
		Level:   level,
		Op:      op,
		Message: msg,
		Fields:  fields,
	})
}
