package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/backend"
)

// proxyCall returns a JSON-RPC Handler that forwards method to an autowatch
// backend resolved by the optional `host` field in params, and returns the
// backend's result verbatim. The UI owns no local doc/project data anymore:
// every doc.list/doc.get/project.list call is a thin pass-through to the
// backend that actually holds the files, so a resolve failure or backend error
// surfaces as a clean JSON-RPC error rather than leaking anything local.
func proxyCall(mgr *backend.Manager, method string) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if mgr == nil {
			return nil, &rpcError{Code: codeInternalError, Message: "no backend configured"}
		}

		var p struct {
			Host   string `json:"host"`
			HostID string `json:"hostId"`
		}
		if params != nil {
			_ = json.Unmarshal(params, &p)
		}

		// hostId is the documented routing selector; host is accepted as an
		// alias so either spelling routes explicitly. Empty → single-backend
		// default (Resolve errors on ambiguity).
		host := p.HostID
		if host == "" {
			host = p.Host
		}
		peer, err := mgr.Resolve(host)
		if err != nil {
			return nil, resolveRPCError(err)
		}

		// Forward the params verbatim (autowatch handlers ignore unknown fields,
		// so a stray `host` is harmless). The raw result round-trips as `any` and
		// is re-emitted byte-for-byte by the dispatcher.
		res, err := peer.Call(ctx, method, params)
		if err != nil {
			return nil, backendCallRPCError(err)
		}
		return res, nil
	}
}

// handleDocRawProxy serves verbatim doc bytes from GET /api/doc/raw by
// forwarding to the backend's doc.raw method and writing the decoded payload.
// It replicates the local route's HTTP contract: GET-only, path required, and a
// backend not-found mapped to 404 without leaking details.
func handleDocRawProxy(mgr *backend.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		reqPath := q.Get("path")
		if reqPath == "" {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}

		if mgr == nil {
			http.Error(w, "no backend configured", http.StatusBadGateway)
			return
		}

		host := q.Get("hostId")
		if host == "" {
			host = q.Get("host")
		}
		peer, err := mgr.Resolve(host)
		if err != nil {
			http.Error(w, resolveHTTPMessage(err), resolveHTTPStatus(err))
			return
		}

		params := map[string]string{
			"project":  q.Get("project"),
			"path":     reqPath,
			"worktree": q.Get("worktree"),
		}
		raw, err := peer.Call(r.Context(), "doc.raw", params)
		if err != nil {
			// Backend error (incl. not found) — don't leak details.
			http.Error(w, "doc not found", http.StatusNotFound)
			return
		}

		var res struct {
			ContentType   string `json:"contentType"`
			ContentBase64 string `json:"contentBase64"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			http.Error(w, "doc not found", http.StatusNotFound)
			return
		}

		data, err := base64.StdEncoding.DecodeString(res.ContentBase64)
		if err != nil {
			http.Error(w, "doc not found", http.StatusNotFound)
			return
		}

		ct := res.ContentType
		if ct == "" {
			ct = "text/html; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		// Serving verbatim doc bytes is the explicit purpose of this route; the
		// payload is produced by the backend's validated doc.raw handler.
		_, _ = w.Write(data) //nolint:gosec // G705: verbatim doc bytes are this route's contract
	}
}

// resolveRPCError maps a backend.Manager.Resolve error onto a JSON-RPC error so
// the client gets a clear failure rather than silent local data.
func resolveRPCError(err error) *rpcError {
	switch {
	case errors.Is(err, backend.ErrNoBackend):
		return &rpcError{Code: codeInternalError, Message: "no backend connected"}
	case errors.Is(err, backend.ErrUnknownHost):
		return &rpcError{Code: codeInvalidParams, Message: "unknown host"}
	case errors.Is(err, backend.ErrAmbiguousHost):
		return &rpcError{Code: codeInvalidParams, Message: "ambiguous host; specify a host"}
	default:
		return &rpcError{Code: codeInternalError, Message: err.Error()}
	}
}

// backendCallRPCError maps a backend peer.Call error onto a JSON-RPC error,
// preserving an *rpc.Error's code/message so the backend's own error reaches
// the client.
func backendCallRPCError(err error) *rpcError {
	var re *rpc.Error
	if errors.As(err, &re) {
		return &rpcError{Code: re.Code, Message: re.Message}
	}
	return &rpcError{Code: codeInternalError, Message: err.Error()}
}

// resolveHTTPStatus maps a Resolve error onto an HTTP status for /api/doc/raw:
// no backend connected is a 502 (the upstream is down), an unknown/ambiguous
// host is a 400 (bad client request).
func resolveHTTPStatus(err error) int {
	if errors.Is(err, backend.ErrNoBackend) {
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}

// resolveHTTPMessage returns a non-leaking message for a Resolve error.
func resolveHTTPMessage(err error) string {
	switch {
	case errors.Is(err, backend.ErrNoBackend):
		return "no backend connected"
	case errors.Is(err, backend.ErrUnknownHost):
		return "unknown host"
	case errors.Is(err, backend.ErrAmbiguousHost):
		return "ambiguous host; specify a host"
	default:
		return err.Error()
	}
}
