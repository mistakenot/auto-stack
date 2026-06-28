// Package artifact holds auto-artifact's domain logic: object-key construction,
// retention tiers, content-type detection, and the local upload log.
package artifact

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
)

// NewUUIDv4 returns a random RFC 4122 version-4 UUID string. The 122 bits of
// entropy in the key prefix are what make objects unguessable in a public,
// non-listable bucket.
func NewUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// BuildKey constructs the object key {retention}/{uuid}/{filename}. The filename
// is the base name of the source path, preserving the on-disk name.
func BuildKey(retention, uuid, sourcePath string) string {
	return retention + "/" + uuid + "/" + filepath.Base(sourcePath)
}
