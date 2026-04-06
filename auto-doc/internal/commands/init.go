package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/datadyne-io/autodoc/internal/config"
	"github.com/datadyne-io/autodoc/internal/doctree"
)

// InitGlobal initializes global autodoc config at ~/.auto/doc/settings.json
// and creates ~/.auto/host.json if it doesn't exist.
func InitGlobal(w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	// Create ~/.auto/doc/ directory
	globalDocDir := filepath.Join(home, config.GlobalDocDir)
	if err := os.MkdirAll(globalDocDir, 0o755); err != nil {
		return fmt.Errorf("creating global config dir: %w", err)
	}

	// Create ~/.auto/doc/settings.json if it doesn't exist
	globalCfgPath := filepath.Join(globalDocDir, config.DefaultConfigFile)
	if _, err := os.Stat(globalCfgPath); os.IsNotExist(err) {
		cfg := config.GlobalConfig{}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling global config: %w", err)
		}
		if err := os.WriteFile(globalCfgPath, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("writing global config: %w", err)
		}
		fmt.Fprintln(w, "Created ~/.auto/doc/settings.json")
	} else {
		fmt.Fprintln(w, "~/.auto/doc/settings.json already exists")
	}

	// Create ~/.auto/host.json if it doesn't exist
	hostPath := filepath.Join(home, config.GlobalAutoDir, config.HostConfigFile)
	if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		hostCfg := config.HostConfig{HostID: hostname}
		data, err := json.MarshalIndent(hostCfg, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling host config: %w", err)
		}
		if err := os.WriteFile(hostPath, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("writing host config: %w", err)
		}
		fmt.Fprintln(w, "Created ~/.auto/host.json")
	} else {
		fmt.Fprintln(w, "~/.auto/host.json already exists")
	}

	return nil
}

// InitProject initializes a project for autodoc.
// Also ensures global init has been run.
func InitProject(w io.Writer, rootDir string) error {
	// Ensure global config exists
	globalCfgPath, err := config.GlobalConfigPath()
	if err == nil {
		if _, err := os.Stat(globalCfgPath); os.IsNotExist(err) {
			if err := InitGlobal(w); err != nil {
				return fmt.Errorf("global init: %w", err)
			}
			fmt.Fprintln(w)
		}
	}

	configDir := filepath.Join(rootDir, config.DefaultConfigDir)
	cfgPath := filepath.Join(configDir, config.DefaultConfigFile)

	// Create .auto/doc/ directory
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// Create .auto/doc/.gitignore if it doesn't exist
	gitignorePath := filepath.Join(configDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		gitignoreContent := "*\n!.gitignore\n!settings.json\n"
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
			return fmt.Errorf("writing .gitignore: %w", err)
		}
	}

	// Create settings.json if it doesn't exist
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := config.Config{
			DocsDir:     config.DefaultDocsDir,
			AgentFiles:  config.DefaultAgentFiles,
			Parallelism: config.DefaultParallelism,
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}
		if err := os.WriteFile(cfgPath, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Fprintln(w, "Created .auto/doc/settings.json")
	} else {
		fmt.Fprintln(w, ".auto/doc/settings.json already exists")
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Create docs dir if it doesn't exist
	docsPath := filepath.Join(rootDir, cfg.DocsDir)
	if _, err := os.Stat(docsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(docsPath, 0o755); err != nil {
			return fmt.Errorf("creating docs dir: %w", err)
		}
		fmt.Fprintf(w, "Created %s/\n", cfg.DocsDir)
	}

	// Run tree
	entries, err := doctree.Walk(docsPath, cfg.Ignores...)
	if err != nil {
		return fmt.Errorf("walking docs: %w", err)
	}

	fmt.Fprintln(w)
	TreeOutput(w, entries, cfg.DocsDir)

	// Check for stale
	result := CheckStale(entries)
	if result.HasStale {
		fmt.Fprintf(w, "\n%d file(s) need attention. Run `autodoc fix` to see instructions.\n", len(result.StaleFiles))
	}

	return nil
}
