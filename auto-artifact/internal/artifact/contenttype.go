package artifact

import (
	"mime"
	"path/filepath"
)

// DefaultContentType is used when the extension maps to no known MIME type.
const DefaultContentType = "application/octet-stream"

// DetectContentType maps a file's extension to a MIME type via the stdlib mime
// package so browsers render images/videos inline. Unknown extensions fall back
// to application/octet-stream.
func DetectContentType(path string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return DefaultContentType
}
