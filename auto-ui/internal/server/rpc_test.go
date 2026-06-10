package server

import (
	"context"
	"encoding/json"
	"testing"
)

// TestDispatchRequestResponse covers the request/response (id-correlated) path:
// a handler's result comes back tagged with the same id.
func TestDispatchRequestResponse(t *testing.T) {
	d := newDispatcher()
	d.Register("ping", func(_ context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Seq int64 `json:"seq"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{"pong": true, "seq": p.Seq}, nil
	})

	resp, send := d.dispatch(context.Background(),
		[]byte(`{"jsonrpc":"2.0","id":7,"method":"ping","params":{"seq":42}}`))
	if !send {
		t.Fatal("expected a response for a request with an id")
	}
	if string(resp.ID) != "7" {
		t.Errorf("response id = %s, want 7", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	got := resp.Result.(map[string]any)
	if got["pong"] != true {
		t.Errorf("pong = %v, want true", got["pong"])
	}
	if got["seq"] != int64(42) {
		t.Errorf("seq = %v, want 42", got["seq"])
	}
}

// TestDispatchNotification covers the push path: a message with no id is a
// notification and must NOT produce a response, even though the handler runs.
func TestDispatchNotification(t *testing.T) {
	d := newDispatcher()
	ran := false
	d.Register("event", func(context.Context, json.RawMessage) (any, error) {
		ran = true
		return "ignored", nil
	})

	resp, send := d.dispatch(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"event","params":{}}`))
	if send || resp != nil {
		t.Errorf("notification produced a response: %+v", resp)
	}
	if !ran {
		t.Error("handler did not run for notification")
	}
}

// TestDispatchUnknownMethod asserts the JSON-RPC method-not-found code (-32601).
func TestDispatchUnknownMethod(t *testing.T) {
	d := newDispatcher()

	resp, send := d.dispatch(context.Background(),
		[]byte(`{"jsonrpc":"2.0","id":"abc","method":"nope"}`))
	if !send {
		t.Fatal("expected an error response")
	}
	if resp.Error == nil || resp.Error.Code != codeMethod {
		t.Fatalf("error = %+v, want code %d", resp.Error, codeMethod)
	}
	if string(resp.ID) != `"abc"` {
		t.Errorf("response id = %s, want \"abc\"", resp.ID)
	}
}
