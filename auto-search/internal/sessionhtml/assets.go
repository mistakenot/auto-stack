package sessionhtml

import _ "embed"

// templateHTML is the self-contained viewer shell (HTML + inline CSS + vanilla
// JS, no external network deps). Render injects the model JSON at the
// /*__DATA__*/ marker.
//
//go:embed template.html
var templateHTML string
