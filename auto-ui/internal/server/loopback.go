package server

import (
	"io"
	"net"
	"net/http"
	"strings"
)

// loopbackOnly wraps h so only requests whose TCP peer is a loopback address
// (127.0.0.0/8 or ::1) are served; every other peer gets 403 with a short
// explanation.
//
// This is a defense-in-depth guard. auto-ui already binds to 127.0.0.1, but
// this guarantees the dashboard never answers a non-local peer even if the
// bind address is later changed, an env var pushes it to 0.0.0.0, or the
// process ends up fronted by a misconfigured proxy — so a port can't be
// accidentally exposed to the LAN or the internet.
//
// The decision is made on r.RemoteAddr (the real socket peer) ONLY. Forwarded
// headers (X-Forwarded-For / X-Real-IP) are deliberately ignored: they are
// client-controlled and trusting them would defeat the guard. This is also why
// `tailscale serve` keeps working — tailscaled terminates the tailnet
// connection and proxies to the loopback backend, so the peer auto-ui sees is
// 127.0.0.1, not the remote tailnet client. (The real client IP lands in
// X-Forwarded-For, which we ignore for the allow decision.)
func loopbackOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackAddr(r.RemoteAddr) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			// The peer address is deliberately NOT reflected into the body:
			// echoing a tainted RemoteAddr trips gosec's XSS taint analysis,
			// and it adds nothing the operator can't see from the server logs.
			_, _ = io.WriteString(w, forbiddenBody)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// forbiddenBody is the 403 explanation served to non-loopback peers.
const forbiddenBody = `403 Forbidden: auto-ui only accepts connections from localhost (127.0.0.1 / ::1).

This request came from a non-loopback address, so it was refused. auto-ui is a
local-only dashboard; this guard exists so the port can't be accidentally
exposed to your LAN or the internet.

To reach it from another machine, front it with a proxy that connects to the
loopback backend, for example ` + "`tailscale serve`" + ` — those requests still
arrive over loopback and are allowed.
`

// hostOnly returns the host portion of a "host:port" RemoteAddr, falling back
// to the raw value when it has no port (and trimming any IPv6 zone).
func hostOnly(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return host
}

// isLoopbackAddr reports whether the peer in a net/http RemoteAddr is a
// loopback IP.
func isLoopbackAddr(remoteAddr string) bool {
	ip := net.ParseIP(hostOnly(remoteAddr))
	return ip != nil && ip.IsLoopback()
}
