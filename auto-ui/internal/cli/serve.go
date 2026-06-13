package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/mistakenot/auto-ui/internal/config"
	"github.com/mistakenot/auto-ui/internal/server"
	"github.com/mistakenot/auto-ui/web"
	"github.com/spf13/cobra"
)

func newServeCmd(application *app.App) *cobra.Command {
	var port int
	var readyFile string
	var projectsPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the auto-ui dashboard locally",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the listen port with precedence:
			//   explicit --port flag > AUTO_UI_PORT env > settings.json > 8080.
			// --port 0 is a valid explicit value (OS-assigned port), so we key
			// off cmd.Flags().Changed("port") rather than a zero check.
			// A missing settings file is normal (pre-init); a present-but-invalid
			// one is surfaced as a warning rather than silently ignored.
			if !cmd.Flags().Changed("port") {
				if v := os.Getenv("AUTO_UI_PORT"); v != "" {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						port = n
					}
				} else if p, err := config.UISettingsPath(); err == nil {
					s, err := config.LoadUISettings(p)
					switch {
					case err == nil && s.Port != 0:
						port = s.Port
					case err != nil && !errors.Is(err, os.ErrNotExist):
						fmt.Fprintf(application.Stderr, "warning: ignoring %s: %v (using port %d)\n", p, err, port)
					}
				}
			}

			// Resolve the projects registry path: --projects flag > AUTO_PROJECTS_PATH
			// env. When set, the registry provider loads from this path instead of
			// the default ~/.auto/projects.json — this isolates an agent harness from
			// the host's real registry.
			if projectsPath == "" {
				projectsPath = os.Getenv("AUTO_PROJECTS_PATH")
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
			handler := server.New(web.FS(), web.Mode,
				server.WithRegistryProvider(func() sharedconfig.ProjectsConfig {
					p := projectsPath
					if p == "" {
						var err error
						p, err = sharedconfig.ProjectsConfigPath()
						if err != nil {
							return sharedconfig.ProjectsConfig{}
						}
					}
					cfg, err := sharedconfig.LoadProjects(p)
					if err != nil {
						return sharedconfig.ProjectsConfig{}
					}
					return cfg
				}),
				server.WithDebug(os.Getenv("AUTO_UI_DEBUG") == "1"),
			)
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

			// Bind the listener explicitly so we can discover the real port when
			// --port 0 asks the OS to assign one, and so a harness can learn the
			// bound address via --ready-file before issuing requests.
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			boundAddr := ln.Addr().String()

			if readyFile != "" {
				line := fmt.Sprintf("{\"addr\":%q}\n", boundAddr)
				if err := os.WriteFile(readyFile, []byte(line), 0o644); err != nil {
					_ = ln.Close()
					return &ExitError{Code: 1, Err: fmt.Errorf("writing ready file %s: %w", readyFile, err)}
				}
			}

			fmt.Fprintf(application.Stderr, "auto ui serving on http://%s (assets=%s)\n", boundAddr, web.Mode)
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return &ExitError{Code: 1, Err: err}
			}
			<-done
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "port to serve on (overrides settings.json; 0 = OS-assigned)")
	cmd.Flags().StringVar(&readyFile, "ready-file", "", "after binding, write {\"addr\":...} JSON to this path")
	cmd.Flags().StringVar(&projectsPath, "projects", "", "path to projects.json registry (overrides AUTO_PROJECTS_PATH / default)")
	return cmd
}
