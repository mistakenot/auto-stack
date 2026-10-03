package graph

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashBytes returns the SHA-256 of b as a 64-character lowercase hex string.
// It is the one content hash auto-plan computes: an annex's bytes are hashed
// into its `hash` field when a plan is frozen (lifecycle done), and lint
// recomputes it to report an annex that changed afterward (annex-changed).
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
