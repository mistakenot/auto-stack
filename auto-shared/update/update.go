package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/mistakenot/auto-shared/version"
)

const (
	repo       = "mistakenot/auto-stack"
	apiURL     = "https://api.github.com/repos/" + repo + "/releases/latest"
	installURL = "https://raw.githubusercontent.com/" + repo + "/refs/tags/%s/install.sh"
)

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

// Result describes what happened during an update check.
type Result struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Updated        bool   `json:"updated"`
	Message        string `json:"message"`
}

// Run checks for a newer release and, if found, downloads and executes install.sh.
func Run(stdout, stderr io.Writer) (*Result, error) {
	current := version.Version

	latest, err := latestTag()
	if err != nil {
		return nil, fmt.Errorf("failed to check latest release: %w", err)
	}

	if !isNewer(current, latest) {
		return &Result{
			CurrentVersion: current,
			LatestVersion:  latest,
			Updated:        false,
			Message:        fmt.Sprintf("already up to date (%s)", current),
		}, nil
	}

	fmt.Fprintf(stderr, "updating %s -> %s\n", current, latest)

	// install.sh's progress output is diagnostic, not payload: route it to
	// stderr so callers can keep stdout as a pure JSON result (the CLAUDE.md
	// JSON-mode stdout contract). The stdout param is retained for API
	// compatibility with existing callers but carries no payload here.
	if err := runInstallScript(stderr, stderr, latest); err != nil {
		return nil, fmt.Errorf("install failed: %w", err)
	}

	return &Result{
		CurrentVersion: current,
		LatestVersion:  latest,
		Updated:        true,
		Message:        fmt.Sprintf("updated %s -> %s", current, latest),
	}, nil
}

func latestTag() (string, error) {
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.TagName == "" {
		return "", fmt.Errorf("no tag_name in release response")
	}
	return info.TagName, nil
}

// baseVersion extracts the semver portion from a git describe string.
// "v0.8.0-3-gc3c0162-dirty" -> "0.8.0", "v0.8.0" -> "0.8.0"
func baseVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if i := strings.Index(v, "-"); i > 0 {
		v = v[:i]
	}
	return v
}

// isNewer returns true if latest is a different semver than current.
// Returns true for "dev" builds so they always update.
func isNewer(current, latest string) bool {
	if current == "dev" {
		return true
	}
	return baseVersion(current) != baseVersion(latest)
}

func runInstallScript(stdout, stderr io.Writer, tag string) error {
	scriptURL := fmt.Sprintf(installURL, tag)

	resp, err := http.Get(scriptURL)
	if err != nil {
		return fmt.Errorf("failed to download install.sh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download install.sh: HTTP %d", resp.StatusCode)
	}

	script, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read install.sh: %w", err)
	}

	cmd := exec.Command("bash", "-s", "--")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}
