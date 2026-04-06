package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/mistakenot/auto-etl/internal/parser"
	"github.com/mistakenot/auto-etl/internal/progress"
	"github.com/mistakenot/auto-etl/internal/transform"
	"github.com/mistakenot/auto-etl/internal/writer"
	"github.com/spf13/cobra"
)

var (
	inputDir  string
	outputDir string
	fullRun   bool
)

func init() {
	home, _ := os.UserHomeDir()
	defaultInput := filepath.Join(home, ".claude", "projects")
	defaultOutput := filepath.Join(home, ".auto", "etl", "output")

	runCmd.Flags().StringVar(&inputDir, "input", defaultInput, "Input directory containing raw session data")
	runCmd.Flags().StringVar(&outputDir, "output", defaultOutput, "Output directory for transformed parquet files")
	runCmd.Flags().BoolVar(&fullRun, "full", false, "Delete output directory before running (full rebuild)")

	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the ETL pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		if fullRun {
			fmt.Printf("full rebuild: removing %s\n", outputDir)
			if err := os.RemoveAll(outputDir); err != nil {
				return fmt.Errorf("remove output dir: %w", err)
			}
		}

		fmt.Printf("input:  %s\n", inputDir)
		fmt.Printf("output: %s\n", outputDir)

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

		// Build transform config
		cfg := transform.DefaultConfig()
		cfg.HostID = loadHostID()

		// Set up git remote resolver with caching
		remotes := loadRemotesCache()
		cfg.GitRemoteForSession = func(workspace string) string {
			return resolveGitRemote(workspace, remotes)
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

		if err := writer.Write(outputDir, rows); err != nil {
			return fmt.Errorf("write: %w", err)
		}

		// Persist updated remotes cache
		saveRemotesCache(remotes)

		fmt.Println("done")
		return nil
	},
}

// --- host ID ---

func loadHostID() string {
	hostPath := hostConfigPath()

	data, err := os.ReadFile(hostPath)
	if err == nil {
		var cfg model.HostConfig
		if json.Unmarshal(data, &cfg) == nil && cfg.HostID != "" {
			return cfg.HostID
		}
	}

	// Fallback to hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	fmt.Fprintf(os.Stderr, "warning: %s missing valid hostId, using hostname %q\n", hostPath, hostname)
	return hostname
}

func hostConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".auto", "host.json")
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
