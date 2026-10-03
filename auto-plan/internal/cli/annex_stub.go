package cli

import (
	"errors"
	"os"
	"path/filepath"
)

// annexStub returns the Markdown written for a new annex when its file is
// absent. Phase 1 writes a minimal, deterministic stub (a single title
// heading); the per-kind section templates (the testing stub seeds `## e2e …
// ## manual`) land in Phase 4. The text is fixed so the e2e harness can diff
// it byte for byte.
func annexStub(kind, title string) string {
	heading := title
	if heading == "" {
		heading = kind
	}
	return "# " + heading + "\n"
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
