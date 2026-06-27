package rpcmethods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-shared/rpc"
)

// DocRawResult is the response payload for the "doc.raw" method.
type DocRawResult struct {
	Path          string `json:"path"`
	ContentType   string `json:"contentType"`
	ContentBase64 string `json:"contentBase64"`
}

func (h *Handlers) handleDocList(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Project  string `json:"project"`
		Worktree string `json:"worktree"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}

	root, err := resolveRoot(h.reg(), p.Project, p.Worktree)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: err.Error()}
	}

	entries, err := walkDocs(root)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "failed to list docs"}
	}
	return entries, nil
}

func (h *Handlers) handleDocGet(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Project  string `json:"project"`
		Path     string `json:"path"`
		Worktree string `json:"worktree"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}

	if p.Path == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "path is required"}
	}

	root, err := resolveRoot(h.reg(), p.Project, p.Worktree)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: err.Error()}
	}

	cleaned := cleanDocPath(p.Path, ".md")
	if cleaned == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid path"}
	}

	absPath := filepath.Join(root, filepath.FromSlash(cleaned))

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "doc not found", Data: map[string]string{"kind": "not_found"}}
	}

	return map[string]string{
		"path":     cleaned,
		"markdown": string(data),
	}, nil
}

func (h *Handlers) handleDocRaw(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Project  string `json:"project"`
		Path     string `json:"path"`
		Worktree string `json:"worktree"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}

	if p.Path == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "path is required"}
	}

	root, err := resolveRoot(h.reg(), p.Project, p.Worktree)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: err.Error()}
	}

	cleaned := cleanDocPath(p.Path, ".html")
	if cleaned == "" {
		return nil, &rpc.Error{Code: rpc.InvalidParams, Message: "invalid path"}
	}

	absPath := filepath.Join(root, filepath.FromSlash(cleaned))

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.InternalError, Message: "doc not found", Data: map[string]string{"kind": "not_found"}}
	}

	return DocRawResult{
		Path:          cleaned,
		ContentType:   "text/html; charset=utf-8",
		ContentBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}
