package rpcmethods

import (
	"context"
	"encoding/json"

	"github.com/mistakenot/auto-shared/git"
)

func (h *Handlers) handleProjectList(_ context.Context, _ json.RawMessage) (any, error) {
	reg := h.reg()
	entries := make([]projectEntry, 0, len(reg.Projects))
	for _, ref := range reg.Projects {
		entries = append(entries, projectEntry{
			ID:     ref.ID,
			Name:   ref.Name,
			Path:   ref.Path,
			Remote: git.NormalizeRemoteURL(ref.Remote),
			Host:   h.hostID,
		})
	}
	return entries, nil
}
