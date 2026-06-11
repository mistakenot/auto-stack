package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
)

// handleRPC returns an http.HandlerFunc for the POST /api/rpc ingest endpoint.
// It accepts a JSON-RPC notification containing a bus.Event in params, validates
// the envelope, broadcasts to all WebSocket clients, derives doc.changed events
// where applicable, and broadcasts those too.
//
// Malformed or invalid frames receive an HTTP 400 with a JSON-RPC error body.
// This is a deliberate deviation from JSON-RPC 2.0 (notifications normally get
// no reply) justified for an HTTP one-shot binding where the producer needs
// feedback on errors.
//
// Valid frames receive 204 No Content (fire-and-forget).
func handleRPC(hub *bus.Hub, regProvider func() config.ProjectsConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeRPCError(w, http.StatusUnsupportedMediaType, codeParseError, "Content-Type must be application/json")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			writeRPCError(w, http.StatusForbidden, codeParseError, "cross-origin requests are not accepted")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			writeRPCError(w, http.StatusBadRequest, codeParseError, "read body failed")
			return
		}

		// Parse the JSON-RPC notification frame.
		var frame struct {
			JSONRPC string    `json:"jsonrpc"`
			Method  string    `json:"method"`
			Params  bus.Event `json:"params"`
		}
		if err := json.Unmarshal(body, &frame); err != nil {
			writeRPCError(w, http.StatusBadRequest, codeParseError, "invalid JSON: "+err.Error())
			return
		}

		if frame.JSONRPC != "2.0" {
			writeRPCError(w, http.StatusBadRequest, codeParseError, "jsonrpc must be \"2.0\"")
			return
		}

		// params.type is authoritative; inbound method is advisory.
		ev := frame.Params

		if errs := ev.Validate(); len(errs) > 0 {
			writeRPCError(w, http.StatusBadRequest, codeParseError, "envelope validation failed")
			return
		}

		// Broadcast the raw event to all connected clients.
		hub.Broadcast(ev)

		// Derive doc.changed events and broadcast each.
		reg := regProvider()
		for _, d := range bus.DeriveDocChanged(ev, reg) {
			hub.Broadcast(d)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// writeRPCError writes an HTTP error response with a JSON-RPC error body.
func writeRPCError(w http.ResponseWriter, httpStatus, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error:   &rpcError{Code: code, Message: msg},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
