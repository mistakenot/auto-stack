package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/datadyne-io/autodoc/internal/config"
)

// DoctorCheck represents a single health check result.
type DoctorCheck struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Doctor runs configuration health checks and writes results to w.
// Returns true if all checks pass.
func Doctor(w io.Writer, rootDir string, jsonMode bool) (bool, error) {
	var checks []DoctorCheck

	// 1. Global config
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		checks = append(checks, DoctorCheck{"global_config", "fail", "Cannot determine home directory"})
	} else if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		checks = append(checks, DoctorCheck{"global_config", "fail", globalPath + " not found. Run `auto doc init` to create it."})
	} else {
		checks = append(checks, DoctorCheck{"global_config", "pass", globalPath})
	}

	// 2. Host config
	hostPath, err := config.HostConfigPath()
	if err != nil {
		checks = append(checks, DoctorCheck{"host_config", "fail", "Cannot determine home directory"})
	} else if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		checks = append(checks, DoctorCheck{"host_config", "fail", hostPath + " not found. Run `auto doc init` to create it."})
	} else if _, err := config.LoadHost(hostPath); err != nil {
		checks = append(checks, DoctorCheck{"host_config", "fail", fmt.Sprintf("%s is invalid: %v. Run `auto doc init` to recreate it.", hostPath, err)})
	} else {
		checks = append(checks, DoctorCheck{"host_config", "pass", hostPath})
	}

	// 3. Project config
	projectPath := filepath.Join(rootDir, config.DefaultConfigDir, config.DefaultConfigFile)
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		checks = append(checks, DoctorCheck{"project_config", "fail", projectPath + " not found. Run `auto doc init --project` to create it."})
	} else {
		data, err := os.ReadFile(projectPath)
		if err != nil {
			checks = append(checks, DoctorCheck{"project_config", "fail", fmt.Sprintf("Cannot read %s: %v", projectPath, err)})
		} else {
			var raw json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				checks = append(checks, DoctorCheck{"project_config", "fail", fmt.Sprintf("%s is not valid JSON: %v", projectPath, err)})
			} else {
				checks = append(checks, DoctorCheck{"project_config", "pass", projectPath})
			}
		}
	}

	// 4. Docs dir
	cfg, loadErr := config.Load(projectPath)
	if cfg == nil {
		cfg = &config.Config{DocsDir: config.DefaultDocsDir}
		_ = loadErr
	}
	docsPath := filepath.Join(rootDir, cfg.DocsDir)
	if _, err := os.Stat(docsPath); os.IsNotExist(err) {
		checks = append(checks, DoctorCheck{"docs_dir", "fail", cfg.DocsDir + "/ not found. Run `auto doc init --project` to create it."})
	} else {
		checks = append(checks, DoctorCheck{"docs_dir", "pass", docsPath})
	}

	// 5. Search index
	indexPath := filepath.Join(rootDir, config.DefaultConfigDir, "index")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		checks = append(checks, DoctorCheck{"search_index", "fail", indexPath + " not found. Run `auto doc search reindex` to create it."})
	} else {
		checks = append(checks, DoctorCheck{"search_index", "pass", indexPath})
	}

	// Output
	allPass := true
	for _, c := range checks {
		if c.Status == "fail" {
			allPass = false
			break
		}
	}

	if jsonMode {
		data, err := json.MarshalIndent(checks, "", "  ")
		if err != nil {
			return false, err
		}
		fmt.Fprintln(w, string(data))
	} else {
		for _, c := range checks {
			icon := "✓"
			if c.Status == "fail" {
				icon = "✗"
			}
			fmt.Fprintf(w, "%s %s: %s\n", icon, c.Check, c.Message)
		}
	}

	return allPass, nil
}
