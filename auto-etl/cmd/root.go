package cmd

import (
	"fmt"
	"os"

	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

var debug bool

var rootCmd = &cobra.Command{
	Use:   "autoetl",
	Short: "Transform raw coding agent sessions into structured parquet files",
}

func init() {
	rootCmd.Version = version.Version
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Print timing and diagnostic information to stderr")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
