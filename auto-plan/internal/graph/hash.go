package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// HashPattern is the exact shape HashBytes produces: 64 lowercase hex
// characters. A stored annex hash must match it, so a value lost or mangled by
// a merge, hand edit or external producer is caught rather than trusted.
var HashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HashBytes returns the SHA-256 of b as a 64-character lowercase hex string.
// It is the one content hash auto-plan computes: an annex's bytes are hashed
// into its `hash` field when a plan is frozen (lifecycle done), and lint
// recomputes it to report an annex that changed afterward (annex-changed).
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
