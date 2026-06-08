package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "auto",
		Short:   "Autonomous coding stack",
		Version: version.Version,
	}
	// Tool subcommands are mounted in Phase 3.
	return root
}

func main() {
	root := newRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
