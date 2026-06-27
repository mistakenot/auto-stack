// Package transport defines the wire-level seam for the auto-stack RPC
// infrastructure. It provides Listener, Dialer, and Conn abstractions that
// decouple RPC method handlers from the underlying transport (unix socket,
// TCP, future WebSocket/iroh). Concrete implementations live in subpackages
// (transport/unix, transport/tcp); this package dispatches by URI scheme.
package transport

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/mistakenot/auto-shared/transport/tcp"
	"github.com/mistakenot/auto-shared/transport/unix"
)

// Conn is a type alias for net.Conn. Every transport yields a standard
// duplex connection.
type Conn = net.Conn

// Listener accepts incoming connections on a transport-specific address.
type Listener interface {
	// Accept blocks until a connection arrives or the listener is closed.
	Accept() (net.Conn, error)
	// Addr returns the listener's bound address (resolved port for :0).
	Addr() net.Addr
	// Close stops the listener and releases resources.
	Close() error
}

// Dialer establishes an outbound connection over a specific transport.
type Dialer interface {
	// Dial connects to the remote address, honouring the context.
	Dial(ctx context.Context) (net.Conn, error)
}

// Listen creates a Listener from a URI string. Supported schemes:
//
//	unix:///path/to/socket
//	tcp://host:port
//
// Unknown schemes return an error with remediation guidance.
func Listen(uri string) (Listener, error) {
	scheme, addr, err := parseURI(uri)
	if err != nil {
		return nil, err
	}
	switch scheme {
	case "unix":
		return unix.Listen(addr)
	case "tcp":
		return tcp.Listen(addr)
	default:
		return nil, fmt.Errorf("transport: unsupported scheme %q in URI %q; use unix:// or tcp://", scheme, uri)
	}
}

// Dial connects to a remote address parsed from a URI string.
func Dial(ctx context.Context, uri string) (net.Conn, error) {
	scheme, addr, err := parseURI(uri)
	if err != nil {
		return nil, err
	}
	switch scheme {
	case "unix":
		d := unix.NewDialer(addr)
		return d.Dial(ctx)
	case "tcp":
		d := tcp.NewDialer(addr)
		return d.Dial(ctx)
	default:
		return nil, fmt.Errorf("transport: unsupported scheme %q in URI %q; use unix:// or tcp://", scheme, uri)
	}
}

// parseURI splits a URI into scheme and address. It handles the double-slash
// convention: unix:///tmp/sock → path=/tmp/sock, tcp://127.0.0.1:8080 → addr=127.0.0.1:8080.
func parseURI(uri string) (scheme, addr string, err error) {
	before, after, ok := strings.Cut(uri, "://")
	if !ok {
		return "", "", fmt.Errorf("transport: invalid URI %q; expected scheme://address (e.g. unix:///tmp/sock or tcp://127.0.0.1:8080)", uri)
	}
	scheme = before
	addr = after
	if addr == "" {
		return "", "", fmt.Errorf("transport: empty address in URI %q", uri)
	}
	return scheme, addr, nil
}
