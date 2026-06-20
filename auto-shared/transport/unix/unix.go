// Package unix provides a Unix domain socket transport. It implements the
// Listener and Dialer contracts defined by the parent transport package
// structurally (no import of the parent) to avoid import cycles.
package unix

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
)

// Listener wraps a net.UnixListener and removes the socket file on Close.
type Listener struct {
	ln   *net.UnixListener
	path string
	once sync.Once
}

// Listen creates a Unix domain socket listener at path with safe stale-socket
// handling:
//   - If path exists but is not a socket → error (will not clobber files).
//   - If path is a live socket (dial succeeds) → "address in use" error.
//   - If path is a stale socket (dial fails) → remove and re-bind.
//   - If path does not exist → bind normally.
func Listen(path string) (*Listener, error) {
	info, err := os.Lstat(path)
	if err == nil {
		// Path exists — check what it is.
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("unix: path %q exists but is not a socket", path)
		}
		// It's a socket — probe for a live server.
		probe, dialErr := net.Dial("unix", path)
		if dialErr == nil {
			probe.Close()
			return nil, fmt.Errorf("unix: address in use: %s", path)
		}
		// Stale socket — safe to remove.
		if removeErr := os.Remove(path); removeErr != nil {
			return nil, fmt.Errorf("unix: cannot remove stale socket %q: %w", path, removeErr)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("unix: cannot stat %q: %w", path, err)
	}

	addr := &net.UnixAddr{Name: path, Net: "unix"}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("unix: listen %q: %w", path, err)
	}
	return &Listener{ln: ln, path: path}, nil
}

// Accept waits for and returns the next connection.
func (l *Listener) Accept() (net.Conn, error) {
	return l.ln.Accept()
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.ln.Addr()
}

// Close stops the listener. Go's net.UnixListener unlinks the socket on close,
// so no extra os.Remove is needed.
func (l *Listener) Close() error {
	var closeErr error
	l.once.Do(func() {
		closeErr = l.ln.Close()
	})
	return closeErr
}

// Dialer connects to a Unix domain socket at the configured path.
type Dialer struct {
	path string
}

// NewDialer creates a Dialer that connects to the given socket path.
func NewDialer(path string) *Dialer {
	return &Dialer{path: path}
}

// Dial connects to the Unix domain socket, honouring the context.
func (d *Dialer) Dial(ctx context.Context) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, "unix", d.path)
}
