package rpcserver

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
)

// HookIngest returns an http.Handler that accepts JSON-RPC notifications
// containing bus.Event payloads. It validates the envelope and returns
// 204 No Content on success. No persistence, derivation, or relay is
// performed — events are stamped with the daemon hostId, derived
// (doc.changed for registered projects), and broadcast to the hub.
//
// Only POST from loopback addresses is accepted; all other methods receive
// 405 and non-loopback origins receive 403. Requests with a browser Origin
// header or non-JSON Content-Type are rejected to prevent CSRF via
// CORS-safelisted simple requests.
func HookIngest(hub *bus.Hub, hostID string, regProvider func() config.ProjectsConfig, ctlEvents bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Restrict to loopback.
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			// If parsing fails, treat as non-loopback.
			writeIngestError(w, http.StatusForbidden, -32600, "non-loopback request rejected")
			return
		}
		if !isLoopback(host) {
			writeIngestError(w, http.StatusForbidden, -32600, "non-loopback request rejected")
			return
		}

		// Reject browser-origin requests (CSRF via CORS-safelisted POSTs).
		if origin := r.Header.Get("Origin"); origin != "" {
			writeIngestError(w, http.StatusForbidden, -32600, "cross-origin requests are not accepted")
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeIngestError(w, http.StatusUnsupportedMediaType, -32600, "Content-Type must be application/json")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			writeIngestError(w, http.StatusBadRequest, -32603, "read body failed")
			return
		}

		var frame struct {
			JSONRPC string    `json:"jsonrpc"`
			Method  string    `json:"method"`
			Params  bus.Event `json:"params"`
		}
		if err := json.Unmarshal(body, &frame); err != nil {
			writeIngestError(w, http.StatusBadRequest, -32700, "invalid JSON: "+err.Error())
			return
		}

		if frame.JSONRPC != "2.0" {
			writeIngestError(w, http.StatusBadRequest, -32600, "jsonrpc must be \"2.0\"")
			return
		}

		ev := frame.Params
		if errs := ev.Validate(); len(errs) > 0 {
			writeIngestError(w, http.StatusBadRequest, -32602, "envelope validation failed")
			return
		}

		// Stamp the daemon's hostId (overwrite-always, D-40-3).
		ev.Host = hostID

		// Broadcast the validated event.
		if hub != nil {
			hub.Broadcast(ev)

			// Derive doc.changed for registered projects (mirrors auto-ui rpc_ingest.go:75-88).
			derived := bus.DeriveDocChanged(ev, regProvider())
			for i := range derived {
				hub.Broadcast(derived[i])
			}
		}

		// Emit ctl log for successful ingest.
		if ctlEvents && hub != nil {
			ctlEv, err := bus.NewCtlLog("info", "hook.ingested", "hook event ingested", map[string]string{
				"type": ev.Type,
			})
			if err == nil {
				hub.Broadcast(ctlEv)
			}
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

// isLoopback returns true if host is a loopback address (127.0.0.1, ::1, or
// localhost).
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// writeIngestError writes a JSON-RPC error response body with the given HTTP
// status code.
func writeIngestError(w http.ResponseWriter, httpStatus, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	resp := struct {
		JSONRPC string `json:"jsonrpc"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		JSONRPC: "2.0",
	}
	resp.Error.Code = code
	resp.Error.Message = msg
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
}
