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
	"regexp"
	"strings"
	"testing"
)

// uuidV4Re matches the UUIDv4 segment of an object key.
var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

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

// uploadFile uploads a temp file with the given args and returns the parsed
// output, registering a cleanup that deletes the object.
func uploadFile(t *testing.T, name, content string, extraArgs ...string) uploadJSON {
	t.Helper()
	file := writeTempFile(t, name, content)
	args := append([]string{"upload", file}, extraArgs...)
	res := runArtifact(t, nil, args...)
	if res.exitCode != 0 {
		t.Fatalf("upload %v exit %d; stderr: %s", extraArgs, res.exitCode, res.stderr)
	}
	out := decodeUpload(t, res.stdout)
	t.Cleanup(func() { runArtifact(t, nil, "delete", out.Key) })
	return out
}

// TestAC2_KeyStructure: key matches {retention}/{uuidv4}/{filename}.
func TestAC2_KeyStructure(t *testing.T) {
	gate(t)
	out := uploadFile(t, "ac2-test.txt", "test\n", "--retain", "30d")
	parts := strings.SplitN(out.Key, "/", 3)
	if len(parts) != 3 {
		t.Fatalf("key %q does not have 3 segments", out.Key)
	}
	if parts[0] != "30d" {
		t.Errorf("retention segment = %q, want 30d", parts[0])
	}
	if !uuidV4Re.MatchString(parts[1]) {
		t.Errorf("uuid segment %q is not a v4 UUID", parts[1])
	}
	if parts[2] != "ac2-test.txt" {
		t.Errorf("filename segment = %q, want ac2-test.txt", parts[2])
	}
}

// TestAC3_DefaultRetention: no --retain → key starts with 90d/.
func TestAC3_DefaultRetention(t *testing.T) {
	gate(t)
	out := uploadFile(t, "ac3-test.txt", "test\n")
	if !strings.HasPrefix(out.Key, "90d/") {
		t.Errorf("default key %q does not start with 90d/", out.Key)
	}
}

// TestAC4_RetentionTiers: each of the four tiers is accepted and prefixes the
// key; any other value is rejected non-zero before any S3 call.
func TestAC4_RetentionTiers(t *testing.T) {
	gate(t)
	for _, tier := range []string{"7d", "30d", "90d", "365d"} {
		out := uploadFile(t, "ac4-test.txt", "test\n", "--retain", tier)
		if !strings.HasPrefix(out.Key, tier+"/") {
			t.Errorf("--retain %s produced key %q", tier, out.Key)
		}
	}
	file := writeTempFile(t, "ac4-bad.txt", "test\n")
	res := runArtifact(t, nil, "upload", file, "--retain", "60d")
	if res.exitCode == 0 {
		t.Errorf("--retain 60d should be rejected, got exit 0; stdout: %s", res.stdout)
	}
}

// TestAC5_ContentType: a .png object is served with Content-Type image/png.
func TestAC5_ContentType(t *testing.T) {
	gate(t)
	out := uploadFile(t, "ac5-test.png", "\x89PNG\r\n\x1a\n")
	if out.ContentType != "image/png" {
		t.Errorf("upload content_type = %q, want image/png", out.ContentType)
	}
	_, hdr := httpGet(t, out.URL)
	if ct := hdr.Get("Content-Type"); ct != "image/png" {
		t.Errorf("served Content-Type = %q, want image/png", ct)
	}
}

// TestAC6_JSONFields: upload JSON has all required fields populated.
func TestAC6_JSONFields(t *testing.T) {
	gate(t)
	out := uploadFile(t, "ac6-test.txt", "test\n")
	if out.URL == "" || out.Bucket == "" || out.Key == "" || out.Retention == "" ||
		out.ContentType == "" || out.SizeBytes == 0 {
		t.Errorf("missing required JSON fields: %+v", out)
	}
}

// TestAC7_FormatText: --format text emits a single bare https URL, no JSON.
func TestAC7_FormatText(t *testing.T) {
	gate(t)
	file := writeTempFile(t, "ac7-test.txt", "test\n")
	res := runArtifact(t, nil, "upload", file, "--format", "text")
	if res.exitCode != 0 {
		t.Fatalf("upload --format text exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	out := strings.TrimSpace(res.stdout)
	if !strings.HasPrefix(out, "https://") {
		t.Errorf("text output does not start with https://: %q", out)
	}
	if strings.Contains(out, "{") {
		t.Errorf("text output contains JSON braces: %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("text output is not a single line: %q", out)
	}
	// Recover the key from the URL to clean up.
	if _, key, found := strings.Cut(out, ".amazonaws.com/"); found {
		t.Cleanup(func() { runArtifact(t, nil, "delete", key) })
	}
}

// TestAC8_AppendsJSONLog: each upload adds a record to uploads.jsonl with the
// required fields.
func TestAC8_AppendsJSONLog(t *testing.T) {
	gate(t)
	logPath := filepath.Join(os.Getenv("HOME"), ".auto", "artifact", "uploads.jsonl")
	before := countLines(logPath)

	out := uploadFile(t, "ac8-test.txt", "test\n")

	after := countLines(logPath)
	if after <= before {
		t.Fatalf("log line count did not grow: before=%d after=%d", before, after)
	}
	last := lastLine(t, logPath)
	var rec struct {
		Key          string `json:"key"`
		URL          string `json:"url"`
		OriginalPath string `json:"original_path"`
		Timestamp    string `json:"timestamp"`
		Retention    string `json:"retention"`
		SizeBytes    int64  `json:"size_bytes"`
		ContentType  string `json:"content_type"`
	}
	if err := json.Unmarshal([]byte(last), &rec); err != nil {
		t.Fatalf("decode last log line: %v\nline: %s", err, last)
	}
	if rec.Key != out.Key || rec.URL == "" || rec.OriginalPath == "" || rec.Timestamp == "" ||
		rec.Retention == "" || rec.SizeBytes == 0 || rec.ContentType == "" {
		t.Errorf("last log record missing fields: %+v", rec)
	}
}

// TestAC13_RejectsOversize: a >1 GiB file is rejected with a size error and no
// S3 call. The file is sparse (os.Truncate), so no disk is consumed.
func TestAC13_RejectsOversize(t *testing.T) {
	gate(t)
	path := filepath.Join(t.TempDir(), "ac13-test.bin")
	if err := os.Truncate(path, 1025*1024*1024); err != nil {
		// Truncate needs the file to exist first on some systems.
		f, ferr := os.Create(path)
		if ferr != nil {
			t.Fatalf("create sparse file: %v", ferr)
		}
		_ = f.Close()
		if err := os.Truncate(path, 1025*1024*1024); err != nil {
			t.Fatalf("truncate sparse file: %v", err)
		}
	}
	res := runArtifact(t, nil, "upload", path)
	if res.exitCode == 0 {
		t.Fatalf("oversize upload should fail; got exit 0")
	}
	msg := strings.ToLower(res.stderr + res.stdout)
	if !strings.Contains(msg, "size") && !strings.Contains(msg, "too large") && !strings.Contains(msg, "exceeds") {
		t.Errorf("error did not mention size/too large/exceeds: %s", res.stderr)
	}
}

// TestAC9_DeleteRemovesObject: after delete, a GET of the object URL returns
// 403 or 404.
func TestAC9_DeleteRemovesObject(t *testing.T) {
	gate(t)
	file := writeTempFile(t, "ac9-test.txt", "delete-me\n")
	res := runArtifact(t, nil, "upload", file)
	if res.exitCode != 0 {
		t.Fatalf("upload exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	out := decodeUpload(t, res.stdout)

	if code, _ := httpGet(t, out.URL); code < 200 || code >= 300 {
		t.Fatalf("uploaded object not fetchable: GET %s = %d", out.URL, code)
	}

	del := runArtifact(t, nil, "delete", out.Key)
	if del.exitCode != 0 {
		t.Fatalf("delete exit %d; stderr: %s", del.exitCode, del.stderr)
	}

	code, _ := httpGet(t, out.URL)
	if code != http.StatusForbidden && code != http.StatusNotFound {
		t.Errorf("after delete, GET %s = %d, want 403 or 404", out.URL, code)
	}
}

// TestAC11_InitStoresConfig: init under a throwaway HOME writes settings.json
// with all required fields (D-4 keeps it off the real settings file).
func TestAC11_InitStoresConfig(t *testing.T) {
	gate(t)
	home := t.TempDir()
	res := runArtifact(t, []string{"HOME=" + home}, "init",
		"--endpoint", "https://s3.us-east-1.amazonaws.com",
		"--bucket", "test-bucket",
		"--region", "us-east-1",
		"--access-key-id", "AKIATEST",
		"--secret-access-key", "secret",
	)
	if res.exitCode != 0 {
		t.Fatalf("init exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	data, err := os.ReadFile(filepath.Join(home, ".auto", "artifact", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	for _, f := range []string{"endpoint", "bucket", "region", "access_key_id", "secret_access_key", "default_retention"} {
		if _, ok := m[f]; !ok {
			t.Errorf("settings.json missing field %q", f)
		}
	}
}

// TestAC12a_DoctorValid: with valid config + bucket access, doctor exits 0.
func TestAC12a_DoctorValid(t *testing.T) {
	gate(t)
	res := runArtifact(t, nil, "doctor")
	if res.exitCode != 0 {
		t.Errorf("doctor with valid config exit %d; stdout: %s stderr: %s", res.exitCode, res.stdout, res.stderr)
	}
}

// TestAC12b_DoctorMissing: with no config (temp HOME), doctor exits non-zero
// with a diagnostic mentioning the problem.
func TestAC12b_DoctorMissing(t *testing.T) {
	gate(t)
	res := runArtifact(t, []string{"HOME=" + t.TempDir()}, "doctor")
	if res.exitCode == 0 {
		t.Fatalf("doctor with no config should exit non-zero")
	}
	combined := strings.ToLower(res.stdout + res.stderr)
	if !strings.Contains(combined, "missing") && !strings.Contains(combined, "invalid") && !strings.Contains(combined, "init") {
		t.Errorf("doctor diagnostic did not mention the problem: %s", combined)
	}
}

// TestAC14_UploadWithoutConfig: upload with no config (temp HOME) exits
// non-zero and directs the user to run init.
func TestAC14_UploadWithoutConfig(t *testing.T) {
	gate(t)
	file := writeTempFile(t, "ac14.txt", "hi\n")
	res := runArtifact(t, []string{"HOME=" + t.TempDir()}, "upload", file)
	if res.exitCode == 0 {
		t.Fatalf("upload without config should exit non-zero")
	}
	if !strings.Contains(strings.ToLower(res.stderr+res.stdout), "init") {
		t.Errorf("upload error did not mention init: %s", res.stderr)
	}
}

// TestAC10_SetupScript: `setup` emits a script containing all required AWS
// calls that passes `bash -n`. This needs no AWS access.
func TestAC10_SetupScript(t *testing.T) {
	gate(t)
	res := runArtifact(t, nil, "setup", "--region", "us-east-1", "--bucket", "test-bucket", "--profile", "default")
	if res.exitCode != 0 {
		t.Fatalf("setup exit %d; stderr: %s", res.exitCode, res.stderr)
	}
	for _, call := range []string{
		"create-bucket", "put-bucket-policy", "put-bucket-lifecycle-configuration",
		"create-role", "create-user", "create-access-key",
	} {
		if !strings.Contains(res.stdout, call) {
			t.Errorf("setup script missing %q", call)
		}
	}

	path := filepath.Join(t.TempDir(), "setup.sh")
	if err := os.WriteFile(path, []byte(res.stdout), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Errorf("bash -n on setup script failed: %v\n%s", err, out)
	}
}

// TestAC15_BucketNotListable: an unauthenticated GET of the bucket root (a
// ListObjects attempt) returns HTTP 403.
func TestAC15_BucketNotListable(t *testing.T) {
	gate(t)
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".auto", "artifact", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var cfg struct {
		Bucket string `json:"bucket"`
		Region string `json:"region"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/", cfg.Bucket, cfg.Region)
	code, _ := httpGet(t, url)
	if code != http.StatusForbidden {
		t.Errorf("unauthenticated bucket-root GET = %d, want 403 (bucket must not be listable)", code)
	}
}

func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func lastLine(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[len(lines)-1]
}
