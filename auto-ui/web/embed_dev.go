//go:build dev

package web

import (
	"io/fs"
	"os"
)

const Mode = "disk"

// FS serves live from disk so edits need only a browser refresh.
// Path is relative to the module root (run from auto-ui/).
func FS() fs.FS { return os.DirFS("web/static") }
