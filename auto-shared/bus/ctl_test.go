package bus

import (
	"regexp"
	"testing"
)

// dottedTypeRe is a copy of the production regex for test assertions.
var dottedTypeRe = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9]+)+$`)

func TestCtlTypeConstantsMatchDottedType(t *testing.T) {
	types := []string{
		TypeCtlLogInfo,
		TypeCtlLogWarn,
		TypeCtlLogError,
		TypeCtlConnect,
		TypeCtlDisconnect,
		TypeCtlHealth,
	}
	for _, typ := range types {
		if !dottedTypeRe.MatchString(typ) {
			t.Errorf("type constant %q does not match dottedType regex", typ)
		}
	}
}

func TestNewCtlLogInfo(t *testing.T) {
	ev, err := NewCtlLog("info", "rpc.served", "served request", map[string]string{"method": "daemon.status"})
	if err != nil {
		t.Fatalf("NewCtlLog: %v", err)
	}

	// Validate passes (AC-1).
	if errs := ev.Validate(); len(errs) != 0 {
		t.Errorf("Validate should pass, got %+v", errs)
	}

	// Host is populated.
	if ev.Host == "" {
		t.Error("Host should be populated by NewEvent")
	}

	// Type is correct.
	if ev.Type != TypeCtlLogInfo {
		t.Errorf("Type = %q, want %q", ev.Type, TypeCtlLogInfo)
	}

	// Source is the daemon.
	if ev.Source != "auto/watch/daemon" {
		t.Errorf("Source = %q, want auto/watch/daemon", ev.Source)
	}
}

func TestNewCtlLogRoundTrip(t *testing.T) {
	fields := map[string]string{"method": "daemon.status"}
	ev, err := NewCtlLog("info", "rpc.served", "served request", fields)
	if err != nil {
		t.Fatalf("NewCtlLog: %v", err)
	}

	got, err := DecodeData[CtlLogEvent](ev)
	if err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.Level != "info" {
		t.Errorf("Level = %q, want info", got.Level)
	}
	if got.Op != "rpc.served" {
		t.Errorf("Op = %q, want rpc.served", got.Op)
	}
	if got.Message != "served request" {
		t.Errorf("Message = %q, want served request", got.Message)
	}
	if got.Fields["method"] != "daemon.status" {
		t.Errorf("Fields[method] = %q, want daemon.status", got.Fields["method"])
	}
}

func TestNewCtlLogWarn(t *testing.T) {
	ev, err := NewCtlLog("warn", "slow.client", "buffer full", nil)
	if err != nil {
		t.Fatalf("NewCtlLog: %v", err)
	}
	if ev.Type != TypeCtlLogWarn {
		t.Errorf("Type = %q, want %q", ev.Type, TypeCtlLogWarn)
	}
}

func TestNewCtlLogError(t *testing.T) {
	ev, err := NewCtlLog("error", "rpc.dispatch", "handler panic", nil)
	if err != nil {
		t.Fatalf("NewCtlLog: %v", err)
	}
	if ev.Type != TypeCtlLogError {
		t.Errorf("Type = %q, want %q", ev.Type, TypeCtlLogError)
	}
}

func TestNewCtlLogInvalidLevel(t *testing.T) {
	_, err := NewCtlLog("invalid", "op", "msg", nil)
	if err == nil {
		t.Fatal("expected error for unknown level, got nil")
	}
}
