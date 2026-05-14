package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"time"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/config"
	"github.com/mistakenot/auto-env/internal/manifest"
	"github.com/mistakenot/auto-env/internal/port"
	"github.com/mistakenot/auto-env/internal/registry"
	"github.com/mistakenot/auto-env/internal/template"
	"github.com/mistakenot/auto-env/internal/worktree"
	"github.com/spf13/cobra"
)

func newUpCmd(application *app.App) *cobra.Command {
	var force bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Render templates and start services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := worktree.Detect(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: errors.New("not a git repository: autoenv requires a git repository")}
			}
			repoRoot := info.RepoRoot

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			manifestPath := config.ManifestPath(repoRoot)
			if manifest.Exists(manifestPath) {
				fmt.Fprintln(cmd.ErrOrStderr(), "Environment already provisioned, running down first...")
				if err := runDown(repoRoot, cfg, manifestPath); err != nil {
					return err
				}
			}

			filesDir := config.FilesPath(repoRoot)
			paths, err := template.Discover(filesDir)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			portNames, err := template.ScanPortNames(filesDir, paths, cfg.Delimiters)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			ports, err := port.Allocate(portNames, cfg.PortBase, cfg.PortStride, info.Slot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			data := template.Data{
				Port:         ports,
				Name:         info.Name,
				Branch:       info.Branch,
				BranchSlug:   info.BranchSlug,
				Slot:         info.Slot,
				RepoRoot:     info.RepoRoot,
				WorktreePath: info.WorktreePath,
			}

			results, err := template.Render(filesDir, paths, &data, cfg.Delimiters)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if dryRun {
				fmt.Fprint(cmd.OutOrStdout(), template.FormatDryRun(results))
				return nil
			}

			if !force {
				var conflicts []string
				for _, r := range results {
					dest := filepath.Join(repoRoot, r.RelPath)
					if _, err := os.Stat(dest); err == nil {
						conflicts = append(conflicts, r.RelPath)
					}
				}
				if len(conflicts) > 0 {
					return &ExitError{Code: 1, Err: fmt.Errorf("destination files already exist (use --force to overwrite): %v", conflicts)}
				}
			}

			var generatedFiles []string
			for _, r := range results {
				dest := filepath.Join(repoRoot, r.RelPath)
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("create directory for %s: %w", r.RelPath, err)}
				}
				if err := os.WriteFile(dest, r.Content, 0644); err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("write %s: %w", r.RelPath, err)}
				}
				generatedFiles = append(generatedFiles, r.RelPath)
			}

			if err := manifest.Write(manifestPath, generatedFiles); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("write manifest: %w", err)}
			}

			checkGitignore(repoRoot, generatedFiles, cmd)

			shCmd := exec.Command("sh", "-c", cfg.UpCommand)
			shCmd.Dir = repoRoot
			shCmd.Stdout = os.Stdout
			shCmd.Stderr = os.Stderr
			if err := shCmd.Run(); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("up_command failed: %w (generated files left in place for debugging, run autoenv down to clean up)", err)}
			}

			reg, err := registry.Default()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not access environment registry: %v\n", err)
			} else {
				entry := &registry.Entry{
					RepoRoot:   repoRoot,
					Branch:     info.Branch,
					BranchSlug: info.BranchSlug,
					Slot:       info.Slot,
					Ports:      ports,
					Files:      generatedFiles,
					CreatedAt:  time.Now(),
				}
				if err := reg.Add(entry); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not register environment: %v\n", err)
				}
			}

			output := map[string]any{
				"name":  info.Name,
				"slot":  info.Slot,
				"ports": ports,
			}
			return writeJSON(cmd.OutOrStdout(), output)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing destination files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print rendered output without writing files or running commands")
	return cmd
}

func runDown(repoRoot string, cfg *config.Config, manifestPath string) *ExitError {
	shCmd := exec.Command("sh", "-c", cfg.DownCommand)
	shCmd.Dir = repoRoot
	shCmd.Stdout = os.Stdout
	shCmd.Stderr = os.Stderr
	if err := shCmd.Run(); err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("down_command failed during auto-restart: %w", err)}
	}

	files, err := manifest.Read(manifestPath)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("read manifest: %w", err)}
	}
	for _, f := range files {
		_ = os.Remove(filepath.Join(repoRoot, f))
	}
	_ = os.Remove(manifestPath)
	if reg, err := registry.Default(); err == nil {
		_ = reg.Remove(repoRoot)
	}
	return nil
}

func checkGitignore(repoRoot string, files []string, cmd *cobra.Command) {
	for _, f := range files {
		chk := exec.Command("git", "check-ignore", "-q", f)
		chk.Dir = repoRoot
		if err := chk.Run(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s is not gitignored\n", f)
		}
	}
}
