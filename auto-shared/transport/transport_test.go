package transport_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-shared/transport/tcp"
	"github.com/mistakenot/auto-shared/transport/unix"
)

// TestRoundtrip verifies that data written in both directions arrives intact
// across unix, tcp, and tcp with OS-assigned port.
func TestRoundtrip(t *testing.T) {
	type setup struct {
		name     string
		listener func(t *testing.T) (transport.Listener, string) // returns listener and dial address
	}
	cases := []setup{
		{
			name: "unix",
			listener: func(t *testing.T) (transport.Listener, string) {
				t.Helper()
				path := filepath.Join(t.TempDir(), "test.sock")
				ln, err := unix.Listen(path)
				if err != nil {
					t.Fatalf("unix.Listen: %v", err)
				}
				return ln, path
			},
		},
		{
			name: "tcp",
			listener: func(t *testing.T) (transport.Listener, string) {
				t.Helper()
				ln, err := tcp.Listen("127.0.0.1:0")
				if err != nil {
					t.Fatalf("tcp.Listen: %v", err)
				}
				return ln, ln.Addr().String()
			},
		},
		{
			name: "tcp-:0",
			listener: func(t *testing.T) (transport.Listener, string) {
				t.Helper()
				ln, err := tcp.Listen(":0")
				if err != nil {
					t.Fatalf("tcp.Listen: %v", err)
				}
				return ln, ln.Addr().String()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ln, dialAddr := tc.listener(t)
			defer ln.Close()

			// Accept in a goroutine.
			accepted := make(chan net.Conn, 1)
			acceptErr := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					acceptErr <- err
					return
				}
				accepted <- conn
			}()

			// Dial.
			var clientConn net.Conn
			var err error
			ctx := context.Background()
			if strings.HasPrefix(tc.name, "unix") {
				d := unix.NewDialer(dialAddr)
				clientConn, err = d.Dial(ctx)
			} else {
				d := tcp.NewDialer(dialAddr)
				clientConn, err = d.Dial(ctx)
			}
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer clientConn.Close()

			// Wait for accept.
			var serverConn net.Conn
			select {
			case serverConn = <-accepted:
				defer serverConn.Close()
			case err := <-acceptErr:
				t.Fatalf("Accept: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("Accept timed out")
			}

			// Write client→server.
			msg1 := []byte("hello from client")
			if _, err := clientConn.Write(msg1); err != nil {
				t.Fatalf("client write: %v", err)
			}
			buf := make([]byte, 256)
			n, err := serverConn.Read(buf)
			if err != nil {
				t.Fatalf("server read: %v", err)
			}
			if string(buf[:n]) != string(msg1) {
				t.Errorf("server got %q, want %q", buf[:n], msg1)
			}

			// Write server→client.
			msg2 := []byte("hello from server")
			if _, err := serverConn.Write(msg2); err != nil {
				t.Fatalf("server write: %v", err)
			}
			n, err = clientConn.Read(buf)
			if err != nil {
				t.Fatalf("client read: %v", err)
			}
			if string(buf[:n]) != string(msg2) {
				t.Errorf("client got %q, want %q", buf[:n], msg2)
			}
		})
	}
}

// TestAddrResolvedPort verifies that Addr() reports the actual bound port
// when listening on :0.
func TestAddrResolvedPort(t *testing.T) {
	ln, err := tcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp.Listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	if port == "0" || port == "" {
		t.Errorf("Addr() port not resolved: %q", addr)
	}
}

// TestCloseUnblocksAccept verifies that closing a listener unblocks a pending
// Accept call.
func TestCloseUnblocksAccept(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T) transport.Listener
	}{
		{
			name: "unix",
			make: func(t *testing.T) transport.Listener {
				t.Helper()
				path := filepath.Join(t.TempDir(), "test.sock")
				ln, err := unix.Listen(path)
				if err != nil {
					t.Fatalf("unix.Listen: %v", err)
				}
				return ln
			},
		},
		{
			name: "tcp",
			make: func(t *testing.T) transport.Listener {
				t.Helper()
				ln, err := tcp.Listen("127.0.0.1:0")
				if err != nil {
					t.Fatalf("tcp.Listen: %v", err)
				}
				return ln
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ln := tc.make(t)

			done := make(chan error, 1)
			go func() {
				_, err := ln.Accept()
				done <- err
			}()

			// Give Accept a moment to block.
			time.Sleep(50 * time.Millisecond)

			if err := ln.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Accept returned nil error after Close; expected an error")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Accept not unblocked after Close")
			}
		})
	}
}

// TestUnixLiveServerRejectsSecondListen verifies that attempting to listen on a
// path with a live server returns an "address in use" error.
func TestUnixLiveServerRejectsSecondListen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.sock")

	ln1, err := unix.Listen(path)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer ln1.Close()

	_, err = unix.Listen(path)
	if err == nil {
		t.Fatal("second Listen succeeded; want error")
	}
	if !strings.Contains(err.Error(), "address in use") {
		t.Errorf("error = %q; want it to contain 'address in use'", err)
	}
}

// TestUnixStaleSocketReused verifies that a stale socket (server closed without
// explicit unlink) is detected and reused.
func TestUnixStaleSocketReused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.sock")

	// Create a listener, then simulate a stale socket by closing the listener's
	// underlying fd but leaving the socket file on disk. We do this by binding
	// with net.ListenUnix and setting its unlink-on-close to false.
	addr := &net.UnixAddr{Name: path, Net: "unix"}
	rawLn, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("raw listen: %v", err)
	}
	// Prevent automatic socket removal on close.
	rawLn.SetUnlinkOnClose(false)
	rawLn.Close()

	// Verify the socket file still exists (stale).
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket missing after close: %v", err)
	}

	// Now our Listen should detect staleness and bind successfully.
	ln2, err := unix.Listen(path)
	if err != nil {
		t.Fatalf("Listen on stale socket: %v", err)
	}
	defer ln2.Close()

	// Verify it actually works.
	go func() {
		conn, err := ln2.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial after stale reclaim: %v", err)
	}
	conn.Close()
}

// TestUnixNonSocketFileRejectsListen verifies that a non-socket file at the
// path produces a clear error.
func TestUnixNonSocketFileRejectsListen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-socket")

	// Create a regular file at the path.
	if err := os.WriteFile(path, []byte("nope"), 0644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	_, err := unix.Listen(path)
	if err == nil {
		t.Fatal("Listen on non-socket succeeded; want error")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Errorf("error = %q; want it to contain 'not a socket'", err)
	}
}

// TestUnixCloseRemovesSocket verifies that closing the listener removes the
// socket file from disk.
func TestUnixCloseRemovesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.sock")

	ln, err := unix.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Verify socket exists.
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("socket not found before close: %v", err)
	}

	ln.Close()

	// Verify socket is removed.
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("socket still exists after Close (err=%v)", err)
	}
}

// TestURIDispatch verifies that the top-level Listen and Dial functions
// correctly dispatch by URI scheme.
func TestURIDispatch(t *testing.T) {
	// Valid unix URI.
	t.Run("unix-uri", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "uri.sock")
		uri := "unix://" + path
		ln, err := transport.Listen(uri)
		if err != nil {
			t.Fatalf("Listen(%q): %v", uri, err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err == nil {
				conn.Close()
			}
		}()

		ctx := context.Background()
		conn, err := transport.Dial(ctx, uri)
		if err != nil {
			t.Fatalf("Dial(%q): %v", uri, err)
		}
		conn.Close()
	})

	// Valid tcp URI.
	t.Run("tcp-uri", func(t *testing.T) {
		ln, err := transport.Listen("tcp://127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
		defer ln.Close()

		go func() {
			conn, err := ln.Accept()
			if err == nil {
				conn.Close()
			}
		}()

		uri := fmt.Sprintf("tcp://%s", ln.Addr().String())
		ctx := context.Background()
		conn, err := transport.Dial(ctx, uri)
		if err != nil {
			t.Fatalf("Dial(%q): %v", uri, err)
		}
		conn.Close()
	})

	// Unknown scheme.
	t.Run("unknown-scheme", func(t *testing.T) {
		_, err := transport.Listen("ws://localhost:8080")
		if err == nil {
			t.Fatal("Listen with unknown scheme succeeded; want error")
		}
		if !strings.Contains(err.Error(), "unsupported scheme") {
			t.Errorf("error = %q; want 'unsupported scheme'", err)
		}

		_, err = transport.Dial(context.Background(), "ws://localhost:8080")
		if err == nil {
			t.Fatal("Dial with unknown scheme succeeded; want error")
		}
		if !strings.Contains(err.Error(), "unsupported scheme") {
			t.Errorf("error = %q; want 'unsupported scheme'", err)
		}
	})

	// Missing scheme.
	t.Run("no-scheme", func(t *testing.T) {
		_, err := transport.Listen("127.0.0.1:8080")
		if err == nil {
			t.Fatal("Listen without scheme succeeded; want error")
		}
		if !strings.Contains(err.Error(), "invalid URI") {
			t.Errorf("error = %q; want 'invalid URI'", err)
		}
	})
}
