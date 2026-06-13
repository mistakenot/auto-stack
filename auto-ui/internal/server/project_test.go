package server_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// projectTestServer creates an httptest.Server with the given registry.
func projectTestServer(t *testing.T, reg config.ProjectsConfig) *httptest.Server {
	t.Helper()
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	return httptest.NewServer(handler)
}

// TestProjectListHappy verifies project.list returns one {id,name,path,remote}
// entry per registered project, with all four fields populated.
func TestProjectListHappy(t *testing.T) {
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{
			{ID: "alpha", Name: "Alpha", Path: "/home/u/alpha", Remote: "https://github.com/owner/alpha"},
			{ID: "beta", Name: "Beta", Path: "/home/u/beta", Remote: "https://github.com/owner/beta"},
		},
	}
	srv := projectTestServer(t, reg)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 2 {
		t.Fatalf("project.list returned %d entries, want 2: %v", len(result), result)
	}

	byID := map[string]map[string]any{}
	for _, entry := range result {
		e, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry not a map: %v", entry)
		}
		id, _ := e["id"].(string)
		byID[id] = e
	}

	alpha, ok := byID["alpha"]
	if !ok {
		t.Fatalf("missing alpha entry: %v", byID)
	}
	if alpha["name"] != "Alpha" {
		t.Errorf("alpha name = %v, want Alpha", alpha["name"])
	}
	if alpha["path"] != "/home/u/alpha" {
		t.Errorf("alpha path = %v, want /home/u/alpha", alpha["path"])
	}
	if alpha["remote"] != "https://github.com/owner/alpha" {
		t.Errorf("alpha remote = %v, want https://github.com/owner/alpha", alpha["remote"])
	}

	beta, ok := byID["beta"]
	if !ok {
		t.Fatalf("missing beta entry: %v", byID)
	}
	if beta["name"] != "Beta" {
		t.Errorf("beta name = %v, want Beta", beta["name"])
	}
	if beta["path"] != "/home/u/beta" {
		t.Errorf("beta path = %v, want /home/u/beta", beta["path"])
	}
	if beta["remote"] != "https://github.com/owner/beta" {
		t.Errorf("beta remote = %v, want https://github.com/owner/beta", beta["remote"])
	}
}

// TestProjectListEmpty verifies an empty registry returns an empty array (length
// 0), not an RPC error and not null.
func TestProjectListEmpty(t *testing.T) {
	srv := projectTestServer(t, config.ProjectsConfig{})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array (must marshal to []): %T %v", resp["result"], resp["result"])
	}
	if len(result) != 0 {
		t.Fatalf("project.list returned %d entries, want 0", len(result))
	}
}

// TestProjectListNormalizesCredentialedRemote verifies a stored remote carrying
// credentials is emitted credential-free — project.list is a UI boundary and
// must never leak a credentialed remote to the browser.
func TestProjectListNormalizesCredentialedRemote(t *testing.T) {
	reg := config.ProjectsConfig{
		Projects: []config.ProjectRef{{
			ID:     "secret",
			Name:   "Secret",
			Path:   "/home/u/secret",
			Remote: "https://user:token@github.com/owner/repo.git",
		}},
	}
	srv := projectTestServer(t, reg)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok || len(result) != 1 {
		t.Fatalf("project.list result unexpected: %T %v", resp["result"], resp["result"])
	}
	entry, _ := result[0].(map[string]any)
	remote, _ := entry["remote"].(string)

	if strings.Contains(remote, "@") || strings.Contains(remote, "token") || strings.Contains(remote, "user") {
		t.Fatalf("remote leaked credentials: %q", remote)
	}
	if remote != "https://github.com/owner/repo" {
		t.Errorf("remote = %q, want https://github.com/owner/repo", remote)
	}
}
