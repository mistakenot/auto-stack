// Package idhash mints deterministic, content-derived entity ids for the reflect
// loop. An id is its prefix joined to the first 8 hex characters of the SHA-256
// digest over the entity's canonical content, e.g. "ob-3a9c1d77". Identical
// content yields an identical id, which makes re-running a mutation (a dry-run
// then an apply, or two identical captures) idempotent. The format matches
// ^<prefix>-[0-9a-f]{8}$, preserving the existing id shapes.
package idhash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Derive returns prefix + "-" + the first 8 hex characters of
// sha256(strings.Join(parts, "\n")). It is deterministic: the same prefix and
// parts always produce the same id. Callers pass an entity's canonical content
// fields as parts so that distinct content yields a distinct id while identical
// content collapses to one id.
func Derive(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return prefix + "-" + hex.EncodeToString(sum[:])[:8]
}
