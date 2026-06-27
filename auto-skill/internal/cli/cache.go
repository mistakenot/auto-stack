package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/spf13/cobra"
)

func newCacheCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the upstream git cache",
	}

	cmd.AddCommand(
		newCacheListCmd(resolveEnv),
		newCachePathCmd(resolveEnv),
		newCachePruneCmd(resolveEnv),
	)

	return cmd
}

func newCacheListCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cached repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			c := cache.NewCache(env.UpstreamCacheDir())
			repos, err := c.List()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				if len(repos) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no cached repositories")
					return nil
				}
				for _, r := range repos {
					fmt.Fprintf(cmd.OutOrStdout(), "%-50s %8s  %s\n",
						r.Identity,
						formatSize(r.SizeBytes),
						r.LastFetch.Format(time.RFC3339))
				}
				return nil
			}

			data, err := json.Marshal(repos)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOutput, "text", false, "human-readable output")
	return cmd
}

func newCachePathCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path <identity-or-url>",
		Short: "Print the on-disk cache path for a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			_, id, err := transport.CanonicalizeURL(args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			c := cache.NewCache(env.UpstreamCacheDir())
			repo, err := c.RepoPath(id)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintln(cmd.OutOrStdout(), repo)
			return nil
		},
	}
	return cmd
}

func newCachePruneCmd(resolveEnv envResolver) *cobra.Command {
	var dryRun bool
	var unreferenced bool
	var maxAge string
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Evict stale or unreferenced cached repositories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			opts := cache.PruneOptions{DryRun: dryRun}

			if maxAge != "" {
				d, err := parseDuration(maxAge)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("invalid --max-age: %w", err)}
				}
				opts.MaxAge = d
			}

			if unreferenced {
				opts.Unreferenced = true
				opts.ReferencedIDs = loadReferencedIDs(env)
			}

			c := cache.NewCache(env.UpstreamCacheDir())
			result, err := c.Prune(opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				action := "evicted"
				if dryRun {
					action = "would evict"
				}
				for _, r := range result.Evicted {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (%s)\n", action, r.Identity, formatSize(r.SizeBytes))
				}
				for _, r := range result.Skipped {
					fmt.Fprintf(cmd.ErrOrStderr(), "skipped: %s\n", r.Identity)
				}
				for _, e := range result.Errors {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e)
				}
				return nil
			}

			data, err := json.Marshal(result)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			if len(result.Errors) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview evictions without removing")
	cmd.Flags().BoolVar(&unreferenced, "unreferenced", false, "also evict repos not referenced by any project")
	cmd.Flags().StringVar(&maxAge, "max-age", "", "evict repos not fetched within this duration (e.g. 90d)")
	cmd.Flags().BoolVar(&textOutput, "text", false, "human-readable output")
	return cmd
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration too short: %q", s)
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid number in duration %q", s)
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit %q; use m/h/d/w", string(unit))
	}
}

// loadReferencedIDs loads lock files from known projects and builds a set
// of canonical repo identities that are in use.
func loadReferencedIDs(env skill.Env) map[string]bool {
	ids := map[string]bool{}
	lockData, err := env.LoadLockFile()
	if err != nil {
		return ids
	}
	lock, err := skill.ParseLock(lockData)
	if err != nil {
		return ids
	}
	for _, entry := range lock.Skills {
		if entry.URL == "" {
			continue
		}
		_, cid, err := transport.CanonicalizeURL(entry.URL)
		if err != nil {
			continue
		}
		ids[cid.RelPath()] = true
	}
	return ids
}
