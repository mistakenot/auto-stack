// project_aggregate.go owns the server-side RPCs that fan OUT across every
// connected backend rather than routing to a single one. Today that is the
// aggregating project.list (merged + host-tagged across all backends); a later
// phase adds the backends.list health RPC here too. This is the multi-host
// counterpart to proxy.go's single-backend proxyCall.
package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/mistakenot/auto-ui/internal/backend"
)

// aggregateCallTimeout bounds each per-backend project.list call so a single slow
// or hung backend cannot block the whole aggregated list.
const aggregateCallTimeout = 10 * time.Second

// aggregateProjectList returns a Handler that fans project.list out to every
// connected backend CONCURRENTLY, tags each returned project with its
// authoritative host id (GR-F8), and merges the results in a deterministic,
// hostID-sorted order. A backend whose call errors (or returns an undecodable
// payload) is SKIPPED so partial results are still returned — the list never
// fails just because one backend is down. Zero connected backends yields an
// empty (non-nil) slice, never an error.
func aggregateProjectList(mgr *backend.Manager) Handler {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
		if mgr == nil {
			return nil, &rpcError{Code: codeInternalError, Message: "no backend configured"}
		}

		peers := mgr.ConnectedPeers() // already sorted by hostID

		// Collect each peer's results indexed by its position so the merged output
		// order is stable regardless of goroutine completion order.
		perPeer := make([][]map[string]any, len(peers))

		var wg sync.WaitGroup
		for i, ref := range peers {
			wg.Go(func() {
				callCtx, cancel := context.WithTimeout(ctx, aggregateCallTimeout)
				defer cancel()
				raw, err := ref.Peer.Call(callCtx, "project.list", nil)
				if err != nil {
					return // skip this backend — partial results, never fail the list
				}
				var entries []json.RawMessage
				if err := json.Unmarshal(raw, &entries); err != nil {
					return
				}
				out := make([]map[string]any, 0, len(entries))
				for _, e := range entries {
					var m map[string]any
					if err := json.Unmarshal(e, &m); err != nil {
						continue
					}
					m["host"] = ref.HostID // authoritative host id (GR-F8)
					out = append(out, m)
				}
				perPeer[i] = out
			})
		}
		wg.Wait()

		merged := make([]map[string]any, 0)
		for _, out := range perPeer {
			merged = append(merged, out...)
		}
		return merged, nil
	}
}

// backendsList returns a Handler that reports every known backend's health
// (connected or pending) verbatim from Manager.Health(), so the SPA can render
// a per-backend status indicator (AC-6). Params are ignored. Unlike the
// aggregator this does NOT fan out — Health() is a single-lock local snapshot —
// so it always succeeds against the live conn set (empty slice when none).
func backendsList(mgr *backend.Manager) Handler {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		if mgr == nil {
			return nil, &rpcError{Code: codeInternalError, Message: "no backend configured"}
		}
		return mgr.Health(), nil
	}
}
