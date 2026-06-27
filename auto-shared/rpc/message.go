// Package rpc defines transport-agnostic JSON-RPC 2.0 message types, error
// codes, and an NDJSON codec. It mirrors the struct shapes from
// auto-ui/internal/server/rpc.go as exported types so downstream tasks can
// share a single framing layer across transports (unix, tcp, pipe).
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Standard JSON-RPC 2.0 error codes, mirrored verbatim from
// auto-ui/internal/server/rpc.go.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// Request is an incoming JSON-RPC 2.0 message. A nil/absent ID marks a
// notification, which by spec must not receive a response. Params and ID
// are kept as json.RawMessage so handlers decode their own shapes and the
// id type (number or string) round-trips unchanged.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is an outgoing reply correlated to a request by ID. Exactly one of
// Result or Error is set on the wire. Result is json.RawMessage so that a
// present "result":null (non-nil raw bytes containing the JSON literal null)
// is distinguishable from an absent result (nil json.RawMessage).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is the JSON-RPC 2.0 error object. It implements the error interface
// so handlers can return it directly.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Message }

// Handler implements a single RPC method. Returning a non-nil error becomes a
// JSON-RPC error response; an *Error is passed through verbatim (preserving
// its code/data), any other error becomes a generic internal error.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// frameKind distinguishes the three JSON-RPC message types.
type frameKind int

const (
	kindRequest      frameKind = iota // has method + id
	kindNotification                  // has method, no id
	kindResponse                      // has result or error, no method
)

// classify determines if a raw JSON frame is a request, notification, or
// response and validates the envelope structure.
func classify(raw json.RawMessage) (frameKind, error) {
	// Decode into a map to inspect which fields are present vs absent.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return 0, fmt.Errorf("classify: invalid JSON: %w", err)
	}

	// jsonrpc must be "2.0".
	ver, ok := fields["jsonrpc"]
	if !ok {
		return 0, errors.New("classify: missing required field \"jsonrpc\"")
	}
	var verStr string
	if err := json.Unmarshal(ver, &verStr); err != nil || verStr != "2.0" {
		return 0, fmt.Errorf("classify: jsonrpc must be \"2.0\", got %s", ver)
	}

	_, hasMethod := fields["method"]
	_, hasResult := fields["result"]
	_, hasError := fields["error"]
	_, hasID := fields["id"]

	// A response carries result and/or error, no method.
	if hasResult || hasError {
		if hasMethod {
			return 0, errors.New("classify: frame has both method and result/error")
		}
		if hasResult && hasError {
			return 0, errors.New("classify: response must carry exactly one of result or error, got both")
		}
		if !hasResult && !hasError {
			// Unreachable given the outer condition, but defensive.
			return 0, errors.New("classify: response must carry exactly one of result or error, got neither")
		}
		return kindResponse, nil
	}

	// A request or notification requires a method.
	if !hasMethod {
		return 0, errors.New("classify: frame has no method and no result/error; not a valid JSON-RPC message")
	}

	// Validate method is a string.
	var methodStr string
	if err := json.Unmarshal(fields["method"], &methodStr); err != nil {
		return 0, fmt.Errorf("classify: method must be a string: %w", err)
	}

	if hasID {
		return kindRequest, nil
	}
	return kindNotification, nil
}
