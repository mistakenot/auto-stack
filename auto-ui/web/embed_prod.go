//go:build !dev

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var content embed.FS

// Mode reports where assets are served from (for diagnostics).
const Mode = "embed"

// FS returns the SPA file system rooted at the static dir.
func FS() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err) // embed.FS is compile-time known; sub cannot fail
	}
	return sub
}
