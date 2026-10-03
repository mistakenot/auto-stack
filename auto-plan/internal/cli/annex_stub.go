package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-plan/internal/schema"
)

// Per-kind annex stub bodies, appended after the `# <title>` heading. They are
// fixed text — no timestamps, no randomness — so the e2e harness can diff the
// written file byte for byte. The `testing` body is built from the registry
// ac.layer enum (testingStub), so adding a layer updates the stub with no edit
// here; `usage` and `structures` carry a hand-written skeleton.
const (
	usageStub      = "\n## Synopsis\n\n_One line on what this does and when to reach for it._\n\n## Example\n\n```console\n$ auto ...\n```\n"
	structuresStub = "\n## Overview\n\n_The shapes this plan introduces or changes, and how they fit together._\n"
)

// annexStub returns the deterministic Markdown written for a new annex when its
// file is absent. The body is keyed off the annex kind (D-5): `usage` and
// `structures` get a fixed skeleton, and `testing` seeds one `## <layer>`
// section per ac.layer enum member, in registry order (`## e2e` … `## manual`).
func annexStub(kind, title string) string {
	heading := title
	if heading == "" {
		heading = kind
	}
	var b strings.Builder
	b.WriteString("# " + heading + "\n")
	switch kind {
	case "usage":
		b.WriteString(usageStub)
	case "structures":
		b.WriteString(structuresStub)
	case "testing":
		b.WriteString(testingStub())
	}
	return b.String()
}

// testingStub seeds one `## <layer>` section per ac.layer enum member, in
// registry order, each with a short placeholder line. Deriving the layers from
// the registry keeps the stub in step with the enum: a new layer appears here
// automatically, and the annex-testing-layers lint reads the same enum.
func testingStub() string {
	var b strings.Builder
	for _, layer := range acLayers() {
		b.WriteString("\n## " + layer + "\n\n_What the " + layer + " layer covers, and how to run it._\n")
	}
	return b.String()
}

// acLayers is the ac.layer enum, in registry order (empty if the field is ever
// removed).
func acLayers() []string {
	nt, ok := schema.Registry.Node("ac")
	if !ok {
		return nil
	}
	f, ok := nt.Field("layer")
	if !ok {
		return nil
	}
	return f.Enum
}

// writeAnnexStub writes the stub Markdown for a freshly added annex, but only
// when the file is absent — create-only-when-missing, like the scaffold writer
// (D-5). An existing file (user content, or a re-run of the same add) is never
// overwritten. It returns the repo-relative path it created, or "" when the
// file already existed or the annex has no path.
func writeAnnexStub(l *loaded, fields map[string]any) (string, error) {
	path, _ := fields["path"].(string)
	if path == "" {
		return "", nil
	}
	kind, _ := fields["kind"].(string)
	title, _ := fields["title"].(string)
	abs := filepath.Join(l.ws.FolderPath(l.plan), filepath.FromSlash(path))
	// O_EXCL: create only when missing, without a check-then-write race.
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case errors.Is(err, os.ErrExist):
		return "", nil
	case err != nil:
		return "", err
	}
	_, werr := f.WriteString(annexStub(kind, title))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	return l.plan.Dir + "/" + path, nil
}
