package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AgentsFile and ClaudeFile are the agent memory files `init` and `new`
// scaffold in docs/plans/: AGENTS.md says the folders are plans managed by
// auto plan, and CLAUDE.md is a relative symlink to it.
const (
	AgentsFile = "AGENTS.md"
	ClaudeFile = "CLAUDE.md"
)

// AgentsText is the content of a scaffolded docs/plans/AGENTS.md.
const AgentsText = "# " + PlansDir + "\n\n" +
	"Each `NNN-name/` folder here is a plan managed by `auto plan`: one `graph.json` per plan.\n" +
	"Don't edit `graph.json` by hand. Change plans with `auto plan` (add, update, link, …) and read them\n" +
	"with `auto plan list`, `show`, `get` and `search`.\n\n" +
	"Run `auto plan quickstart` for the workflow and `auto plan docs` for the full reference.\n"

// EnsureScaffold creates docs/plans/ and, when missing, docs/plans/AGENTS.md
// and docs/plans/CLAUDE.md (a symlink to AGENTS.md). It never overwrites:
// an existing AGENTS.md may hold a user's edits, and an existing CLAUDE.md
// of any form is left alone. It returns the repo-relative paths it created
// and notes for stderr: a CLAUDE.md that is not a symlink to AGENTS.md, or a
// symlink that failed and fell back to a copy.
func (w *Workspace) EnsureScaffold() (created, notes []string, err error) {
	dir := w.PlansPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	agentsRel := PlansDir + "/" + AgentsFile
	claudeRel := PlansDir + "/" + ClaudeFile
	agents := filepath.Join(dir, AgentsFile)
	claude := filepath.Join(dir, ClaudeFile)

	// O_EXCL: create only when missing, without a check-then-write race.
	f, err := os.OpenFile(agents, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case err == nil:
		_, werr := f.WriteString(AgentsText)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return created, notes, werr
		}
		created = append(created, agentsRel)
	case !errors.Is(err, os.ErrExist):
		return created, notes, err
	}

	if _, err := os.Lstat(claude); err == nil {
		if target, lerr := os.Readlink(claude); lerr != nil || target != AgentsFile {
			notes = append(notes, fmt.Sprintf("%s exists and is not a symlink to %s; left as is", claudeRel, AgentsFile))
		}
		return created, notes, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return created, notes, err
	}
	if err := os.Symlink(AgentsFile, claude); err != nil {
		data, rerr := os.ReadFile(agents)
		if rerr != nil {
			return created, notes, rerr
		}
		if werr := os.WriteFile(claude, data, 0o644); werr != nil {
			return created, notes, werr
		}
		notes = append(notes, fmt.Sprintf("could not symlink %s to %s (%v); wrote a copy instead", claudeRel, AgentsFile, err))
	}
	created = append(created, claudeRel)
	return created, notes, nil
}
