package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/datadyne-io/autodoc/internal/commands"
	"github.com/datadyne-io/autodoc/internal/config"
	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

var jsonOutput bool

func main() {
	rootCmd := &cobra.Command{
		Use:   "autodoc",
		Short: "Documentation management for AI coding agents",
		Long:  "autodoc helps AI coding agents find, navigate and manage documentation in a repository.",
	}

	rootCmd.Version = version.Version
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	rootCmd.AddCommand(
		newInitCmd(),
		newTreeCmd(),
		newStaleCmd(),
		newAgentsCmd(),
		newFixCmd(),
		newFixedCmd(),
		newSearchCmd(),
		newQuickstartCmd(),
		newDocsCmd(),
		newDoctorCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	cfgPath := filepath.Join(cwd, config.DefaultConfigDir, config.DefaultConfigFile)
	cfg, err := config.LoadWithGlobal(cfgPath)
	if err != nil {
		return nil, "", err
	}
	return cfg, cwd, nil
}

func newInitCmd() *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize autodoc (global by default, --project for repo-local)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				return commands.InitProject(os.Stdout, cwd)
			}
			return commands.InitGlobal(os.Stdout)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Initialize project-local config in current directory")
	return cmd
}

func newTreeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tree",
		Short: "Pretty-print all doc files with title and summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cwd, err := loadConfig()
			if err != nil {
				return err
			}
			entries, err := doctree.WalkRepo(cwd, cfg.DocsDir, cfg.Ignores...)
			if err != nil {
				return err
			}
			if jsonOutput {
				return commands.TreeOutputJSON(os.Stdout, entries)
			}
			commands.TreeOutput(os.Stdout, entries, ".")
			staleResult := commands.CheckStale(entries)
			staleCount := len(staleResult.StaleFiles)
			if staleCount > 0 {
				fmt.Fprintf(os.Stdout, "\n%d docs, %d stale\n", len(entries), staleCount)
			} else {
				fmt.Fprintf(os.Stdout, "\n%d docs\n", len(entries))
			}
			return nil
		},
	}
}

func newStaleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stale",
		Short: "List files with incorrect or missing hashes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cwd, err := loadConfig()
			if err != nil {
				return err
			}
			entries, err := doctree.WalkRepo(cwd, cfg.DocsDir, cfg.Ignores...)
			if err != nil {
				return err
			}
			result := commands.CheckStale(entries)
			if !result.HasStale {
				if jsonOutput {
					fmt.Println("[]")
				} else {
					fmt.Println("No stale files found.")
				}
				return nil
			}
			if jsonOutput {
				if err := commands.StaleOutputJSON(os.Stdout, result.StaleFiles); err != nil {
					return err
				}
			} else {
				commands.StaleOutput(os.Stdout, entries, result.StaleFiles, ".")
				fmt.Fprintf(os.Stderr, "\n%d stale file(s). Run `autodoc fix` to see instructions.\n", len(result.StaleFiles))
			}
			os.Exit(1)
			return nil
		},
	}
}

func newAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Insert tree output into agent memory files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cwd, err := loadConfig()
			if err != nil {
				return err
			}
			updatedFiles, err := commands.AgentsWithResult(cwd, cfg.DocsDir, cfg.AgentFiles, cfg.Ignores)
			if err != nil {
				return err
			}
			if jsonOutput {
				return commands.WriteJSON(os.Stdout, updatedFiles)
			}
			fmt.Println("Agent files updated.")
			return nil
		},
	}
}

func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix",
		Short: "Output instructions to fix documentation issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cwd, err := loadConfig()
			if err != nil {
				return err
			}
			if jsonOutput {
				result, err := commands.FixCollect(cwd, cfg.DocsDir, cfg.Ignores)
				if err != nil {
					return err
				}
				return commands.FixOutputJSON(os.Stdout, result.DocIssues, result.LinkIssues)
			}
			return commands.Fix(os.Stdout, cwd, cfg.DocsDir, cfg.Parallelism, cfg.AgentFiles, cfg.Ignores)
		},
	}
}

func newSearchCmd() *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search documentation using BM25 keyword matching",
	}

	searchCmd.AddCommand(
		&cobra.Command{
			Use:   "reindex",
			Short: "Rebuild the full search index from all doc files",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, cwd, err := loadConfig()
				if err != nil {
					return err
				}
				indexPath := filepath.Join(cwd, config.DefaultConfigDir, "index")
				return commands.SearchReindex(os.Stdout, indexPath, cwd, cfg.DocsDir, cfg.Ignores)
			},
		},
		&cobra.Command{
			Use:   "keyword <query>",
			Short: "Run a BM25 keyword search against the index",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				indexPath := filepath.Join(cwd, config.DefaultConfigDir, "index")
				return commands.SearchKeyword(os.Stdout, indexPath, args[0])
			},
		},
	)

	return searchCmd
}

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Show a comprehensive usage guide with examples",
		Run: func(cmd *cobra.Command, args []string) {
			commands.Quickstart(os.Stdout)
		},
	}
}

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Show complete command reference",
		Run: func(cmd *cobra.Command, args []string) {
			commands.Docs(os.Stdout)
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration health and report problems",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			allPass, err := commands.Doctor(os.Stdout, cwd, jsonOutput)
			if err != nil {
				return err
			}
			if !allPass {
				os.Exit(1)
			}
			return nil
		},
	}
}

func newFixedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fixed <filepath>",
		Short: "Recalculate and write the hash for a doc file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}

			// Read old hash before fixing
			oldData, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			oldDoc := frontmatter.Parse(string(oldData))

			indexPath := filepath.Join(cwd, config.DefaultConfigDir, "index")
			if err := commands.Fixed(path, indexPath, args[0]); err != nil {
				return err
			}

			// Read new hash
			newData, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			newDoc := frontmatter.Parse(string(newData))

			if jsonOutput {
				return commands.WriteJSON(os.Stdout, commands.FixedResultJSON{
					Path:    args[0],
					OldHash: oldDoc.Hash,
					NewHash: newDoc.Hash,
				})
			}
			fmt.Printf("Updated hash for %s\n", args[0])
			return nil
		},
	}
}
