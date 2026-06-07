package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
			if !cmd.Flags().Changed("port") {
				if p, err := config.UISettingsPath(); err == nil {
					if s, err := config.LoadUISettings(p); err == nil && s.Port != 0 {
						port = s.Port
					}
				}
			}

			// Cancel on SIGINT/SIGTERM so the server shuts down gracefully.
			// main.go passes context.Background() (matching every other auto-* binary),
			// so signal handling is wired here, in the long-running command — mirrors
			// auto-watch/internal/cli/ops.go.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			handler := server.New(web.FS(), web.Mode)
			srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: handler}

			go func() {
				<-ctx.Done()
				_ = srv.Shutdown(context.Background())
			}()

			fmt.Fprintf(application.Stderr, "autoui serving on http://localhost:%d (assets=%s)\n", port, web.Mode)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "port to serve on (overrides settings.json)")
	return cmd
}
