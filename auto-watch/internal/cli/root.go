package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mistakenot/auto-shared/version"
	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/mistakenot/auto-watch/internal/store"
	"github.com/mistakenot/auto-watch/internal/textout"
	"github.com/spf13/cobra"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	application := app.New(stdout, stderr)
	rootCmd := NewRootCmd(application)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				fmt.Fprintln(stderr, exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func NewRootCmd(application *app.App) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "autowatch",
		Short:         "Monitor repo changes and run scheduled agent tasks",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetOut(application.Stdout)
	rootCmd.SetErr(application.Stderr)

	rootCmd.AddCommand(
		newInitCmd(application),
		newTaskCmd(application),
		newTriggerCmd(application),
		newDaemonCmd(application),
		newStartCmd(application),
		newDoctorCmd(application),
		newLogsCmd(application),
		newStatusCmd(application),
		newHealthCmd(application),
		newCleanCmd(application),
	)

	return rootCmd
}

func openStore(ctx context.Context) (*store.Store, error) {
	if err := config.EnsureGlobalDirs(); err != nil {
		return nil, err
	}
	path, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	db, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func requireRepoRoot(cwd string) (string, error) {
	repoRoot, err := gitx.FindRepoRoot(cwd)
	if err != nil {
		return "", &ExitError{
			Code: 1,
			Err:  errors.New("current directory is not inside a git repo; run autowatch init inside a repository"),
		}
	}
	return repoRoot, nil
}

func requireProjectConfig(cwd string) (string, model.ProjectConfig, error) {
	repoRoot, err := requireRepoRoot(cwd)
	if err != nil {
		return "", model.ProjectConfig{}, err
	}
	cfg, err := config.LoadProjectConfig(repoRoot)
	if err != nil {
		return "", model.ProjectConfig{}, &ExitError{
			Code: 1,
			Err:  fmt.Errorf("failed to load %s: %w; run autowatch init", config.ProjectConfigPath(repoRoot), err),
		}
	}
	return repoRoot, cfg, nil
}

func saveValidatedProjectConfig(repoRoot string, cfg model.ProjectConfig) error {
	if errs := config.ValidateProjectConfig(cfg); len(errs) > 0 {
		return &ExitError{
			Code: 1,
			Err:  fmt.Errorf("project config is invalid:\n%s", textout.FormatValidationErrors(errs)),
		}
	}
	if err := config.SaveProjectConfig(repoRoot, cfg); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

func normalizeTaskSet(tasks []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if _, ok := seen[task]; ok {
			continue
		}
		seen[task] = struct{}{}
		out = append(out, task)
	}
	return out
}

func writeValidationErrors(stderr io.Writer, errs []model.ValidationError) {
	if len(errs) == 0 {
		return
	}
	fmt.Fprintln(stderr, textout.FormatValidationErrors(errs))
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
