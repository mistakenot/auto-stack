package server

import (
	"context"
	"encoding/json"
	"errors"
)

// JSON-RPC 2.0 envelope + dispatcher. This file is transport-agnostic (no
// websocket import) so the routing logic can be unit-tested in isolation; ws.go
// pumps messages through it over a live connection.
//
// The three JSON-RPC message kinds map onto auto-ui's needs:
//   - Request (has id)    -> client RPC call, gets a correlated response
//   - Response (has id)   -> server reply to a call
//   - Notification (no id)-> fire-and-forget event; used in BOTH directions
//     (server push is just a server-initiated notification).

// Standard JSON-RPC 2.0 error codes (https://www.jsonrpc.org/specification).
const (
	codeParseError    = -32700
	codeMethod        = -32601
	codeInternalError = -32603
)

// rpcRequest is an incoming JSON-RPC message. A nil ID marks a notification,
// which by spec must not receive a response. Params/ID are kept as RawMessage
// so handlers decode their own shapes and id type (number or string) round-trips
// unchanged.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is an outgoing reply correlated to a request by ID. Exactly one of
// Result / Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// Handler implements a single RPC method. Returning a non-nil error becomes a
// JSON-RPC error response; an *rpcError is passed through verbatim (preserving
// its code/data), any other error becomes a generic internal error.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Dispatcher routes JSON-RPC methods to handlers. The method table is fixed
// after setup (registration happens before any connection serves), so reads
// need no lock.
type Dispatcher struct {
	methods map[string]Handler
}

func newDispatcher() *Dispatcher {
	return &Dispatcher{methods: make(map[string]Handler)}
}

// Register binds a handler to a method name.
func (d *Dispatcher) Register(method string, h Handler) {
	d.methods[method] = h
}

// dispatch parses one raw inbound message, invokes the matching handler, and
// returns the response to send. The bool is false when there is nothing to send
// back — i.e. the message was a notification (no id), which is the normal path
// for client->server events that expect no reply.
func (d *Dispatcher) dispatch(ctx context.Context, raw []byte) (*rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		// Parse errors have no id to correlate to, so reply with null id.
		return errResponse(json.RawMessage("null"), codeParseError, "parse error", err.Error()), true
	}

	isNotification := len(req.ID) == 0

	h, ok := d.methods[req.Method]
	if !ok {
		if isNotification {
			return nil, false // can't report an error for a notification
		}
		return errResponse(req.ID, codeMethod, "method not found", req.Method), true
	}

	result, err := h(ctx, req.Params)
	if isNotification {
		return nil, false // result/error are discarded for notifications
	}
	if err != nil {
		var re *rpcError
		if errors.As(err, &re) {
			return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: re}, true
		}
		return errResponse(req.ID, codeInternalError, "internal error", err.Error()), true
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

func errResponse(id json.RawMessage, code int, msg string, data any) *rpcResponse {
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
}
