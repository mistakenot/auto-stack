package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/config"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize shared, autoreflect, and repository-local state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			reflectPath, _, reflectCreated, err := config.EnsureReflectSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shared settings: %s\n", sharedPath)
			if sharedCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created shared settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Shared settings.json already exists.")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Reflect settings: %s\n", reflectPath)
			if reflectCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created autoreflect settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Autoreflect settings.json already exists.")
			}

			repoInfo, err := gitutil.DetectRepo(application.CWD)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Project setup skipped: current directory is not inside a git repo.")
				return nil //nolint:nilerr // intentionally skip project setup outside git repositories
			}

			stateDir, err := store.EnsureStateDir(repoInfo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbookPath := store.PlaybookPath(repoInfo.Root)
			feedbackPath := store.FeedbackPath(repoInfo.Root)

			playbookCreated, err := ensurePlaybook(playbookPath)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			feedbackCreated, err := ensureFeedbackLog(feedbackPath)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Project state dir: %s\n", store.DisplayPath(application.CWD, stateDir))
			fmt.Fprintf(cmd.OutOrStdout(), "Playbook: %s\n", store.DisplayPath(application.CWD, playbookPath))
			if playbookCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created playbook.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Playbook already exists.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Feedback log: %s\n", store.DisplayPath(application.CWD, feedbackPath))
			if feedbackCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created feedback.jsonl.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Feedback.jsonl already exists.")
			}
			return nil
		},
	}
}

func ensurePlaybook(playbookPath string) (bool, error) {
	if _, err := os.Stat(playbookPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat playbook: %w", err)
	}

	empty := rules.Playbook{
		SchemaVersion: 1,
		Rules:         []rules.Rule{},
	}
	if err := store.WriteJSONFile(playbookPath, empty); err != nil {
		return false, fmt.Errorf("create playbook: %w", err)
	}
	return true, nil
}

func ensureFeedbackLog(feedbackPath string) (bool, error) {
	if info, err := os.Stat(feedbackPath); err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("feedback path is a directory: %s", feedbackPath)
		}
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat feedback log: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(feedbackPath), 0o755); err != nil {
		return false, fmt.Errorf("create feedback parent directory: %w", err)
	}
	if err := os.WriteFile(feedbackPath, []byte{}, 0o600); err != nil {
		return false, fmt.Errorf("create feedback log: %w", err)
	}
	return true, nil
}
