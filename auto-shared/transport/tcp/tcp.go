// Package tcp provides a TCP transport. It implements the Listener and Dialer
// contracts defined by the parent transport package structurally (no import of
// the parent) to avoid import cycles.
package tcp

import (
	"context"
	"net"
)

// Listener wraps a net.Listener bound to a TCP address. It supports
// 127.0.0.1:0 for OS-assigned port allocation; Addr() returns the resolved
// address with the actual port.
type Listener struct {
	ln net.Listener
}

// Listen creates a TCP listener on the given address (e.g. "127.0.0.1:0").
func Listen(addr string) (*Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Listener{ln: ln}, nil
}

// Accept waits for and returns the next connection.
func (l *Listener) Accept() (net.Conn, error) {
	return l.ln.Accept()
}

// Addr returns the listener's bound address with the resolved port.
func (l *Listener) Addr() net.Addr {
	return l.ln.Addr()
}

// Close stops the listener.
func (l *Listener) Close() error {
	return l.ln.Close()
}

// Dialer connects to a TCP address.
type Dialer struct {
	addr string
}

// NewDialer creates a Dialer that connects to the given TCP address.
func NewDialer(addr string) *Dialer {
	return &Dialer{addr: addr}
}

// Dial connects to the TCP address, honouring the context.
func (d *Dialer) Dial(ctx context.Context) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, "tcp", d.addr)
}
