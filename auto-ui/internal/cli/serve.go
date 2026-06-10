package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/mistakenot/auto-ui/internal/config"
	"github.com/mistakenot/auto-ui/internal/server"
	"github.com/mistakenot/auto-ui/web"
	"github.com/spf13/cobra"
)

func newServeCmd(application *app.App) *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the auto-ui dashboard locally",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If --port was not set explicitly, fall back to the configured
			// Settings.Port (precedence: flag > settings > built-in default 8080).
			// A missing settings file is normal (pre-init); a present-but-invalid
			// one is surfaced as a warning rather than silently ignored.
			if !cmd.Flags().Changed("port") {
				if p, err := config.UISettingsPath(); err == nil {
					s, err := config.LoadUISettings(p)
					switch {
					case err == nil && s.Port != 0:
						port = s.Port
					case err != nil && !errors.Is(err, os.ErrNotExist):
						fmt.Fprintf(application.Stderr, "warning: ignoring %s: %v (using port %d)\n", p, err, port)
					}
				}
			}

			// Cancel on SIGINT/SIGTERM so the server shuts down gracefully.
			// main.go passes context.Background() (matching every other auto-* binary),
			// so signal handling is wired here, in the long-running command — mirrors
			// auto-watch/internal/cli/ops.go.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Every request context derives from baseCtx (via BaseContext below),
			// so cancelling it on shutdown propagates into live WebSocket handlers:
			// their r.Context() fires, the read loop's Read returns, and the
			// connection closes. Without this, a streaming /api/ws connection never
			// goes idle and srv.Shutdown would block until the deadline below.
			baseCtx, cancelBase := context.WithCancel(context.Background())
			defer cancelBase()

			// Bind to loopback only: auto-ui is a local-dev/internal tool, so it
			// must not be reachable from the LAN. ReadHeaderTimeout guards against
			// a stalled client holding the connection open indefinitely.
			handler := server.New(web.FS(), web.Mode)
			srv := &http.Server{
				Addr:              fmt.Sprintf("127.0.0.1:%d", port),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext:       func(net.Listener) context.Context { return baseCtx },
			}

			// done is closed only after Shutdown finishes draining. We join on it
			// before returning so the process can't exit mid-drain: Shutdown closes
			// the listener (unblocking ListenAndServe with ErrServerClosed) and
			// then drains in-flight connections, so without this join RunE could
			// return — and cobra exit the process — while the drain is still running.
			done := make(chan struct{})
			go func() {
				defer close(done)
				<-ctx.Done()
				// Cancel live WebSocket handlers first so they close promptly,
				// then drain. The bounded deadline is a backstop so SIGINT/SIGTERM
				// always returns control to the shell even if a client misbehaves.
				cancelBase()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()

			fmt.Fprintf(application.Stderr, "auto ui serving on http://localhost:%d (assets=%s)\n", port, web.Mode)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return &ExitError{Code: 1, Err: err}
			}
			<-done
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "port to serve on (overrides settings.json)")
	return cmd
}
