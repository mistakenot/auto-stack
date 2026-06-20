package rpc_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestLayeringGate enforces the import-separation invariants between the rpc
// and transport subtrees (AC-7). It uses `go list -json ./...` to inspect
// each package's imports and asserts:
//
//  1. No rpc package (excluding rpc/conformance) imports any transport package.
//  2. No transport package imports any rpc package.
//  3. rpc/conformance is the SINGLE explicit exception — it may import both.
//  4. auto-shared/go.mod has no require block (no new deps).
func TestLayeringGate(t *testing.T) {
	// Run go list -json ./... from the auto-shared module root.
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = findModuleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json ./...: %v", err)
	}

	// go list -json outputs concatenated JSON objects (not a JSON array).
	// Parse them one at a time.
	dec := json.NewDecoder(strings.NewReader(string(out)))

	const (
		rpcBase       = "github.com/mistakenot/auto-shared/rpc"
		conformance   = "github.com/mistakenot/auto-shared/rpc/conformance"
		transportBase = "github.com/mistakenot/auto-shared/transport"
	)

	type pkgInfo struct {
		ImportPath string
		Imports    []string
	}

	for dec.More() {
		var pkg pkgInfo
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode package info: %v", err)
		}

		isRPC := strings.HasPrefix(pkg.ImportPath, rpcBase) && pkg.ImportPath != conformance
		isTransport := strings.HasPrefix(pkg.ImportPath, transportBase)
		isConformance := pkg.ImportPath == conformance

		for _, imp := range pkg.Imports {
			impIsRPC := strings.HasPrefix(imp, rpcBase)
			impIsTransport := strings.HasPrefix(imp, transportBase)

			// Rule 1: rpc packages (except conformance) must not import transport.
			if isRPC && impIsTransport {
				t.Errorf("LAYERING VIOLATION: rpc package %s imports transport package %s", pkg.ImportPath, imp)
			}

			// Rule 2: transport packages must not import rpc.
			if isTransport && impIsRPC {
				t.Errorf("LAYERING VIOLATION: transport package %s imports rpc package %s", pkg.ImportPath, imp)
			}

			// Rule 3: conformance is the single exception — it may import both.
			// (No assertion needed; we just skip conformance from rules 1 and 2.)
			_ = isConformance
		}
	}

	// Rule 4: go.mod has no require block.
	assertNoRequireBlock(t)
}

// findModuleRoot locates the auto-shared module root by running go env GOMOD.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" {
		t.Fatal("go env GOMOD returned empty; not in a module")
	}
	// The module root is the directory containing go.mod.
	idx := strings.LastIndex(gomod, "/")
	if idx < 0 {
		t.Fatalf("unexpected GOMOD path: %s", gomod)
	}
	return gomod[:idx]
}

// assertNoRequireBlock reads go.mod and asserts there is no `require` directive.
func assertNoRequireBlock(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "mod", "edit", "-json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go mod edit -json: %v", err)
	}

	var modFile struct {
		Require []struct {
			Path    string
			Version string
		}
	}
	if err := json.Unmarshal(out, &modFile); err != nil {
		t.Fatalf("parse go.mod JSON: %v", err)
	}
	if len(modFile.Require) > 0 {
		for _, r := range modFile.Require {
			t.Errorf("go.mod has unexpected dependency: %s %s", r.Path, r.Version)
		}
		t.Error("auto-shared/go.mod must have no require block (stdlib only)")
	}
}
