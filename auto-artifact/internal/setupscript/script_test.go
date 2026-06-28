package setupscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateContainsRequiredCalls(t *testing.T) {
	script := Generate(Params{Region: "us-east-1", Bucket: "test-bucket", Profile: "default"})
	required := []string{
		"create-bucket",
		"put-bucket-policy",
		"put-bucket-lifecycle-configuration",
		"create-role",
		"create-user",
		"create-access-key",
	}
	for _, call := range required {
		if !strings.Contains(script, call) {
			t.Errorf("generated script missing %q", call)
		}
	}
}

// TestGenerateRoleIsReal guards against a bare grep-satisfying create-role: the
// role block must carry a trust policy, which makes it a valid AWS call (D-6).
func TestGenerateRoleIsReal(t *testing.T) {
	script := Generate(Params{Region: "eu-west-1", Bucket: "b", Profile: ""})
	if !strings.Contains(script, "--assume-role-policy-document") {
		t.Error("create-role block has no --assume-role-policy-document (would be an invalid AWS call)")
	}
	if !strings.Contains(script, "sts:AssumeRole") {
		t.Error("trust policy missing sts:AssumeRole")
	}
}

func TestGenerateParameterized(t *testing.T) {
	script := Generate(Params{Region: "ap-south-1", Bucket: "my-bucket", Profile: "prod"})
	for _, want := range []string{`BUCKET='my-bucket'`, `REGION='ap-south-1'`, `PROFILE='prod'`} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing baked value %q", want)
		}
	}
}

// TestGenerateShellQuotesParams ensures shell metacharacters in a parameter are
// emitted as inert single-quoted literals, not evaluated when the script runs.
func TestGenerateShellQuotesParams(t *testing.T) {
	script := Generate(Params{Region: "eu-west-1", Bucket: "b", Profile: "$(touch /tmp/pwned)"})
	if !strings.Contains(script, `PROFILE='$(touch /tmp/pwned)'`) {
		t.Errorf("profile not single-quoted as an inert literal:\n%s", script)
	}
	// Embedded single quotes must be escaped, keeping the script syntactically valid.
	tricky := Generate(Params{Region: "eu-west-1", Bucket: "b", Profile: "a'b"})
	if !strings.Contains(tricky, `PROFILE='a'\''b'`) {
		t.Errorf("embedded single quote not escaped:\n%s", tricky)
	}
	if _, err := exec.LookPath("bash"); err == nil {
		path := filepath.Join(t.TempDir(), "s.sh")
		if err := os.WriteFile(path, []byte(tricky), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
			t.Errorf("bash -n failed on escaped script: %v\n%s", err, out)
		}
	}
}

// TestGeneratePassesBashSyntaxCheck runs `bash -n` on the emitted script (AC-10).
func TestGeneratePassesBashSyntaxCheck(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	script := Generate(Params{Region: "us-east-1", Bucket: "test-bucket", Profile: "default"})
	path := filepath.Join(t.TempDir(), "setup.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", path).CombinedOutput()
	if err != nil {
		t.Errorf("bash -n failed: %v\n%s", err, out)
	}
}
