// Package conformance is the end-to-end acceptance harness for `auto artifact`.
// It builds the unified `auto` binary and shells out to `auto artifact ...`
// against the live S3 bucket configured in ~/.auto/artifact/settings.json,
// porting the 16 acceptance criteria from auto-artifact/docs/requirements.md.
//
// The suite hits real AWS and is gated behind AUTO_ARTIFACT_E2E=1, so ordinary
// `go test ./...` and PR CI stay green and free (D-3). Run it with:
//
//	AUTO_ARTIFACT_E2E=1 go test ./auto-artifact/conformance/...
package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// uploadJSON mirrors the JSON object `auto artifact upload` prints.
type uploadJSON struct {
	URL         string `json:"url"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Retention   string `json:"retention"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// decodeUpload parses upload stdout, failing the test on malformed JSON.
func decodeUpload(t *testing.T, stdout string) uploadJSON {
	t.Helper()
	var out uploadJSON
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("decode upload JSON: %v\nstdout: %s", err, stdout)
	}
	return out
}

// binPath is the absolute path to the built unified `auto` binary, set by
// TestMain when the suite is enabled.
var binPath string

const e2eEnvVar = "AUTO_ARTIFACT_E2E"

func e2eEnabled() bool { return os.Getenv(e2eEnvVar) == "1" }

// gate skips a test unless the E2E suite is enabled.
func gate(t *testing.T) {
	t.Helper()
	if !e2eEnabled() {
		t.Skipf("set %s=1 to run the live-AWS conformance suite", e2eEnvVar)
	}
}

func TestMain(m *testing.M) {
	if e2eEnabled() {
		if err := buildBinary(); err != nil {
			fmt.Fprintf(os.Stderr, "conformance: build unified auto binary: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// buildBinary builds the *unified* `auto` binary by import path. Building the
// import path (not a relative ./cmd/...) makes go resolve it through go.work no
// matter where `go test` is invoked from, and guarantees the artifact
// subcommand is mounted (a module-local cmd/autoartifact would not have it).
func buildBinary() error {
	dir, err := os.MkdirTemp("", "auto-artifact-conformance-*")
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, "auto")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/mistakenot/auto-cli/cmd/auto")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	binPath = bin
	return nil
}

// cliResult captures the outcome of one `auto artifact` invocation.
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// runArtifact runs `auto artifact <args...>`. extraEnv entries (KEY=VALUE) are
// appended to the current environment — e.g. HOME=<tmp> to isolate
// config-mutating ACs from the real settings.json (D-4).
func runArtifact(t *testing.T, extraEnv []string, args ...string) cliResult {
	t.Helper()
	if binPath == "" {
		t.Fatal("binPath unset: TestMain did not build the binary")
	}
	cmd := exec.Command(binPath, append([]string{"artifact"}, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run auto artifact %v: %v", args, err)
		}
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}
}

// httpGet performs a plain GET and returns the status code and response headers.
func httpGet(t *testing.T, url string) (int, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header
}

// writeTempFile writes content to a uniquely-named temp file and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// TestAC1_UploadReturnsPublicURL: upload returns a non-empty url that is
// fetchable over HTTPS.
func TestAC1_UploadReturnsPublicURL(t *testing.T) {
	gate(t)
	file := writeTempFile(t, "ac1-test.txt", "hello\n")

	res := runArtifact(t, nil, "upload", file)
	if res.exitCode != 0 {
		t.Fatalf("upload exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	out := decodeUpload(t, res.stdout)
	if out.URL == "" {
		t.Fatal("upload returned empty url")
	}
	if !strings.HasPrefix(out.URL, "https://") {
		t.Errorf("url is not https: %s", out.URL)
	}

	code, _ := httpGet(t, out.URL)
	if code < 200 || code >= 300 {
		t.Errorf("GET %s = %d, want 2xx", out.URL, code)
	}

	t.Cleanup(func() { runArtifact(t, nil, "delete", out.Key) })
}

// TestAC16_BinaryRegistersArtifactSubcommand: the unified binary builds and
// `auto artifact --help` shows the artifact command group.
func TestAC16_BinaryRegistersArtifactSubcommand(t *testing.T) {
	gate(t)
	res := runArtifact(t, nil, "--help")
	if res.exitCode != 0 {
		t.Fatalf("auto artifact --help exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stdout+res.stderr), "artifact") {
		t.Errorf("auto artifact --help did not mention artifact:\n%s\n%s", res.stdout, res.stderr)
	}
}
