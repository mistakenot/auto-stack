package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HitID generates a stable short hash for a search hit based on scope, mode,
// normalized query, normalized filters, and a matched identifier.
func HitID(scope, mode, query, filters, matchedID string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s", scope, mode, query, filters, matchedID)
	return hex.EncodeToString(h.Sum(nil))[:12]
}
