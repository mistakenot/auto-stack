package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"

	gitextract "github.com/mistakenot/auto-etl/internal/git"
	ghclient "github.com/mistakenot/auto-etl/internal/github"
	"github.com/mistakenot/auto-etl/internal/parser"
	"github.com/mistakenot/auto-etl/internal/progress"
	"github.com/mistakenot/auto-etl/internal/transform"
	"github.com/mistakenot/auto-etl/internal/writer"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/spf13/cobra"
)

var (
	inputDir     string
	outputDir    string
	fullRun      bool
	onlyFlag     []string
	repoPathFlag []string
	sinceFlag    string
)

// validOnlyValues is the set of valid --only values.
var validOnlyValues = map[string]bool{
	"sessions": true,
	"github":   true,
	"git":      true,
}

func newRunCmd() *cobra.Command {
	defaultInput, defaultOutput := homeDefaults()

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the ETL pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			var runStart time.Time
			if debug {
				runStart = time.Now()
			}

			// Parse and validate --only flag
			sources, err := parseOnlyFlag(onlyFlag)
			if err != nil {
				return err
			}

			if fullRun {
				fmt.Printf("full rebuild: removing %s\n", outputDir)
				if err := os.RemoveAll(outputDir); err != nil {
					return fmt.Errorf("remove output dir: %w", err)
				}
			}

			fmt.Printf("input:  %s\n", inputDir)
			fmt.Printf("output: %s\n", outputDir)

			remotes := loadRemotesCache()
			hostID := loadHostID()

			// Session ETL phase
			if sources["sessions"] {
				if err := runSessionETL(hostID, remotes); err != nil {
					return err
				}
			}

			// GitHub PR sync phase
			if sources["github"] {
				explicitGitHub := len(onlyFlag) > 0 && !sources["sessions"]
				if err := runGitHubSync(cmd.Context(), hostID, remotes, explicitGitHub); err != nil {
					return err
				}
			}

			// Git history ETL phase
			if sources["git"] {
				if err := runGitETL(hostID, remotes, repoPathFlag, sinceFlag, fullRun); err != nil {
					return err
				}
			}

			// Persist updated remotes cache
			saveRemotesCache(remotes)

			if debug {
				fmt.Fprintf(os.Stderr, "[debug] total: %s\n", time.Since(runStart))
			}
			fmt.Println("done")
			return nil
		},
	}

	runCmd.Flags().StringVar(&inputDir, "input", defaultInput, "Input directory containing raw session data")
	runCmd.Flags().StringVar(&outputDir, "output", defaultOutput, "Output directory for transformed parquet files")
	runCmd.Flags().BoolVar(&fullRun, "full", false, "Delete output directory before running (full rebuild)")
	runCmd.Flags().StringSliceVar(&onlyFlag, "only", nil, "Run only specified ETL sources (sessions, github, git). Default: all.")
	runCmd.Flags().StringSliceVar(&repoPathFlag, "repo-path", nil, "Explicit git repo paths to index (for --only git)")
	runCmd.Flags().StringVar(&sinceFlag, "since", "", "Limit initial git history depth (e.g. 5m, 2h, 5d, 3w, 6mo, 1y)")

	return runCmd
}

// parseOnlyFlag validates and normalizes the --only flag values.
// Returns a set of sources to run. If no flag provided, all sources are enabled.
func parseOnlyFlag(values []string) (map[string]bool, error) {
	if len(values) == 0 {
		// Default: run all
		return map[string]bool{"sessions": true, "github": true, "git": true}, nil
	}

	sources := make(map[string]bool)
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if !validOnlyValues[v] {
			valid := make([]string, 0, len(validOnlyValues))
			for k := range validOnlyValues {
				valid = append(valid, k)
			}
			return nil, fmt.Errorf("invalid --only value %q; valid values: %s", v, strings.Join(valid, ", "))
		}
		sources[v] = true
	}
	return sources, nil
}

func runSessionETL(hostID string, remotes map[string]string) error {
	var phaseStart time.Time

	// Parse phase
	if debug {
		phaseStart = time.Now()
	}
	parseBar := &progress.Bar{Label: "parsing"}
	sessions, err := parser.ScanAndParse(inputDir, func(current, total int) {
		parseBar.Total = total
		parseBar.Update(current)
	})
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	parseBar.Done()
	fmt.Printf("parsed %d sessions\n", len(sessions))
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] parse: %s\n", time.Since(phaseStart))
	}

	// Transform phase
	var remotesMu sync.Mutex
	cfg := transform.DefaultConfig()
	cfg.HostID = hostID
	cfg.GitRemoteForSession = func(workspace string) string {
		remotesMu.Lock()
		defer remotesMu.Unlock()
		return resolveGitRemote(workspace, remotes)
	}

	if debug {
		phaseStart = time.Now()
	}
	transformBar := &progress.Bar{Label: "transforming", Total: len(sessions)}
	rows, err := transform.Transform(sessions, cfg, func(current, total int) {
		transformBar.Update(current)
	})
	if err != nil {
		return fmt.Errorf("transform: %w", err)
	}
	transformBar.Done()
	fmt.Printf("transformed: %d messages, %d sessions\n",
		len(rows.Messages), len(rows.Sessions))
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] transform: %s\n", time.Since(phaseStart))
	}

	// Write phase
	if debug {
		phaseStart = time.Now()
	}
	if err := writer.Write(outputDir, rows); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] write: %s\n", time.Since(phaseStart))
	}

	return nil
}

func runGitHubSync(ctx context.Context, hostID string, remotes map[string]string, explicitOnly bool) error {
	var phaseStart time.Time

	// Resolve token
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		var err error
		token, err = ghclient.ResolveToken()
		if err != nil {
			if explicitOnly {
				return fmt.Errorf("GitHub auth failed: %w (set GITHUB_TOKEN or run 'gh auth login')", err)
			}
			fmt.Fprintf(os.Stderr, "warning: GitHub auth not available (%v), skipping GitHub sync\n", err)
			return nil
		}
	}

	// Discover repos from remotes cache
	repos := ghclient.DiscoverRepos(remotes)
	if len(repos) == 0 {
		if explicitOnly {
			return errors.New("no GitHub repos found in settings cache")
		}
		return nil
	}

	client := ghclient.NewRealClient(token)
	fetchCfg := ghclient.FetchConfig{
		HostID:        hostID,
		SyncStatePath: ghclient.SyncStatePath(),
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if debug {
		phaseStart = time.Now()
	}
	result, summary, err := ghclient.FetchAll(ctx, client, repos, fetchCfg)
	if err != nil {
		return fmt.Errorf("github sync: %w", err)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] github fetch: %s\n", time.Since(phaseStart))
	}

	// Write parquet output
	if debug {
		phaseStart = time.Now()
	}
	if err := writer.WriteGitHub(outputDir, result); err != nil {
		return fmt.Errorf("write github: %w", err)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] github write: %s\n", time.Since(phaseStart))
	}

	// Print summary
	fmt.Fprintf(os.Stderr, "GitHub PR sync complete: %d repos, %d PRs synced", summary.ReposProcessed, summary.PRsSynced)
	if len(summary.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, ", %d warnings:\n", len(summary.Warnings))
		for _, w := range summary.Warnings {
			if w.PR > 0 {
				fmt.Fprintf(os.Stderr, "  - %s#%d: %s\n", w.Repo, w.PR, w.Message)
			} else {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", w.Repo, w.Message)
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n")
	}

	return nil
}

// --- host ID ---

func loadHostID() string {
	hostPath, err := sharedconfig.HostConfigPath()
	if err != nil {
		hostname, _ := os.Hostname()
		if hostname == "" {
			return "unknown"
		}
		return hostname
	}

	cfg, err := sharedconfig.LoadHost(hostPath)
	if err == nil {
		return cfg.HostID
	}

	// Fallback to hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	fmt.Fprintf(os.Stderr, "warning: %s missing valid hostId, using hostname %q\n", hostPath, hostname)
	return hostname
}

// --- git remote cache ---

type etlSettings struct {
	Remotes map[string]string `json:"remotes"`
}

func etlSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".auto", "etl", "settings.json")
}

func loadRemotesCache() map[string]string {
	data, err := os.ReadFile(etlSettingsPath())
	if err != nil {
		return make(map[string]string)
	}
	var settings etlSettings
	if json.Unmarshal(data, &settings) != nil || settings.Remotes == nil {
		return make(map[string]string)
	}
	return settings.Remotes
}

func saveRemotesCache(remotes map[string]string) {
	if len(remotes) == 0 {
		return
	}
	settings := etlSettings{Remotes: remotes}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	path := etlSettingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}

func resolveGitRemote(workspace string, cache map[string]string) string {
	if workspace == "" {
		return ""
	}

	// Check cache (empty string is a valid cached result)
	if val, ok := cache[workspace]; ok {
		return val
	}

	// Try to resolve
	remote := gitRemoteOrigin(workspace)
	cache[workspace] = remote
	return remote
}

func gitRemoteOrigin(dir string) string {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runGitETL(hostID string, remotes map[string]string, explicitPaths []string, since string, fullRebuild bool) error {
	var phaseStart time.Time
	if debug {
		phaseStart = time.Now()
	}

	if fullRebuild {
		statePath := gitextract.GitSyncStatePath()
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: could not remove git sync state %s: %v\n", statePath, err)
		}
	}

	etlRunID := fmt.Sprintf("git-%d", time.Now().UnixMilli())
	collectedAt := time.Now().UnixMilli()

	statePath := gitextract.GitSyncStatePath()
	syncState := gitextract.LoadGitSyncState(statePath)

	repos := gitextract.DiscoverRepos(remotes, explicitPaths)
	if len(repos) == 0 {
		fmt.Fprintf(os.Stderr, "git ETL: no repos found\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "git ETL: discovered %d repo(s)\n", len(repos))

	var totalCommits, totalFiles, totalHunks int

	for _, repo := range repos {
		normalized := sharedgit.NormalizeRemoteURL(repo.Remote)
		var repoID string
		if normalized != "" {
			repoID = sharedgit.ComputeRepoID(normalized)
		} else {
			repoID = sharedgit.ComputeRepoIDFromPath(repo.Path)
		}

		repoState := syncState.GetRepo(repoID)
		config := gitextract.ExtractConfig{
			HostID:      hostID,
			ETLRunID:    etlRunID,
			CollectedAt: collectedAt,
			Since:       since,
			SeenSHAs:    repoState.SeenSHAs,
		}

		result, err := gitextract.ExtractRepo(repo, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: git ETL failed for %s: %v\n", repo.Path, err)
			continue
		}

		messagesDir := filepath.Join(outputDir, "messages")
		if err := gitextract.LinkSessionIDs(result.Commits, messagesDir, normalized); err != nil {
			fmt.Fprintf(os.Stderr, "warning: session link fallback: %v\n", err)
		}

		if err := writer.WriteGit(outputDir, result); err != nil {
			return fmt.Errorf("write git %s: %w", repo.Path, err)
		}

		var newSHAs []string
		for i := range result.Commits {
			sha := strings.TrimPrefix(result.Commits[i].ID, repoID+"-")
			newSHAs = append(newSHAs, sha)
		}
		repoState.MarkSeen(newSHAs)

		totalCommits += len(result.Commits)
		totalFiles += len(result.Files)
		totalHunks += len(result.Hunks)

		fmt.Fprintf(os.Stderr, "  %s: %d commits, %d files, %d hunks\n",
			repo.Path, len(result.Commits), len(result.Files), len(result.Hunks))
	}

	if err := syncState.Save(statePath); err != nil {
		return fmt.Errorf("save git sync state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "git ETL complete: %d commits, %d files, %d hunks\n",
		totalCommits, totalFiles, totalHunks)

	if debug {
		fmt.Fprintf(os.Stderr, "[debug] git ETL: %s\n", time.Since(phaseStart))
	}

	return nil
}
