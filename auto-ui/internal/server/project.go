package server

import (
	"context"
	"encoding/json"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/git"
)

// projectEntry is a single project returned by project.list.
type projectEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote"`
}

// projectListHandler returns a JSON-RPC Handler for the "project.list" method.
// It maps the project registry to one {id, name, path, remote} object per
// registered project. An empty registry returns an empty array (never an error,
// never null).
//
// project.list is a UI boundary, so the stored remote is passed through
// git.NormalizeRemoteURL before it is emitted — a credentialed remote must never
// reach the browser.
func projectListHandler(regProvider func() config.ProjectsConfig) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		reg := regProvider()
		entries := make([]projectEntry, 0, len(reg.Projects))
		for _, ref := range reg.Projects {
			entries = append(entries, projectEntry{
				ID:     ref.ID,
				Name:   ref.Name,
				Path:   ref.Path,
				Remote: git.NormalizeRemoteURL(ref.Remote),
			})
		}
		return entries, nil
	}
}
