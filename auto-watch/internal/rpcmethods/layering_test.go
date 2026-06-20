package rpcmethods

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestLayering_NoTransportImport(t *testing.T) {
	// Run go list -json to get the full dependency tree for this package.
	// Tests run in the package directory, so ../.. gets us to auto-watch root.
	cmd := exec.Command("go", "list", "-json", "./internal/rpcmethods/...")
	cmd.Dir = "../.." // run from auto-watch root so the module path resolves
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json: %v", err)
	}

	// Parse the JSON output. go list -json emits one JSON object per package.
	type listPkg struct {
		Deps []string `json:"Deps"`
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg listPkg
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		for _, dep := range pkg.Deps {
			if strings.Contains(dep, "auto-shared/transport") {
				t.Errorf("rpcmethods transitively depends on %q — layering violation", dep)
			}
		}
	}
}

func TestLayering_GoModUnchanged(t *testing.T) {
	// Verify go.mod has no uncommitted changes that might indicate new
	// dependencies were pulled in.
	cmd := exec.Command("git", "diff", "--quiet", "--", "go.mod")
	cmd.Dir = "../.." // run from auto-watch root
	if err := cmd.Run(); err != nil {
		t.Errorf("go.mod has uncommitted changes — new dependencies may have been added: %v", err)
	}
}
