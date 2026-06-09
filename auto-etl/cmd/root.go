package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

var debug bool

// NewRootCmd builds the auto-etl command tree (mounted as `auto etl`).
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "etl",
		Short:   "Transform raw coding agent sessions into structured parquet files",
		Version: version.Version,
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Print timing and diagnostic information to stderr")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newZenCmd())
	rootCmd.AddCommand(newUpdateCmd())

	return rootCmd
}

// Execute runs the auto-etl command tree. Kept for `main.go` and the
// fixturegen `go run .` path.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// homeDefaults computes the default input/output directories for `run`.
func homeDefaults() (input, output string) {
	home, _ := os.UserHomeDir()
	input = filepath.Join(home, ".claude", "projects")
	output = filepath.Join(home, ".auto", "etl", "output")
	return input, output
}
