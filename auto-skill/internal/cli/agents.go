package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

const autoskillSnippet = `**autoskill** — Author and lint reusable agent skills. Run ` + "`autoskill quickstart`" + ` to learn more.`

const (
	fencedStart = "<!-- autoskill: start -->"
	fencedEnd   = "<!-- autoskill: end -->"
)

// agentFiles are the files to update, checked in order.
var agentFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"}

func newAgentsCmd(resolveEnv envResolver) *cobra.Command {
	var uninstall bool

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Register autoskill in local agent memory files",
		Long:  "Writes a fenced section with skill snippets and <important if> blocks into CLAUDE.md, AGENTS.md, and GEMINI.md. Idempotent and symlink-safe.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			return runAgentsUpdate(cmd, env, uninstall)
		},
	}

	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the autoskill fenced section from agent files")
	return cmd
}

// runAgentsUpdate performs the agent file update logic, shared by agents and sync commands.
func runAgentsUpdate(cmd *cobra.Command, env skill.Env, uninstall bool) error {
	// Collect important_if skills
	var importantSkills []skill.ImportantIfSkill
	var err error
	if !uninstall {
		importantSkills, err = skill.ListImportantIfSkills(env)
		if err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("listing important_if skills: %w", err)}
		}
	}

	// Track files we've already processed by resolved path to handle symlinks.
	seen := make(map[string]bool)

	for _, name := range agentFiles {
		p := filepath.Join(env.Root, name)

		// Resolve symlinks so we don't write the same file twice.
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			if seen[resolved] {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: skipped (symlink to already-processed file)\n", name)
				continue
			}
			seen[resolved] = true
		}
		// If EvalSymlinks failed the file doesn't exist yet — that's fine, we'll create it.

		if uninstall {
			removed, err := removeFencedSection(p)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("updating %s: %w", name, err)}
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: uninstalled\n", name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: nothing to remove\n", name)
			}
		} else {
			updated, err := ensureSkillSnippet(p, importantSkills)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("updating %s: %w", name, err)}
			}
			if updated {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: updated\n", name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: already present\n", name)
			}
		}
	}
	return nil
}

// buildFencedSection generates the complete fenced section content.
func buildFencedSection(importantSkills []skill.ImportantIfSkill) string {
	var b strings.Builder
	b.WriteString(fencedStart)
	b.WriteByte('\n')
	b.WriteString(autoskillSnippet)
	b.WriteByte('\n')

	for _, s := range importantSkills {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "<important if=\"%s\">\n", s.ImportantIf)
		b.WriteString(s.ImportantIfBody)
		b.WriteByte('\n')
		b.WriteString("</important>")
		b.WriteByte('\n')
	}

	b.WriteString(fencedEnd)
	return b.String()
}

// ensureSkillSnippet ensures the agent file contains the fenced autoskill section.
// It handles three cases:
//  1. File already has fenced markers -> replace content between markers
//  2. File has the old un-fenced snippet (no markers) -> migrate to fenced section
//  3. Neither exists -> append fenced section
//
// Returns true if the file was modified.
func ensureSkillSnippet(path string, importantSkills []skill.ImportantIfSkill) (bool, error) {
	fenced := buildFencedSection(importantSkills)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, os.WriteFile(path, []byte(fenced+"\n"), 0o644)
		}
		return false, err
	}

	content := string(data)

	// Case 1: fenced markers already exist -> replace between markers
	startIdx := strings.Index(content, fencedStart)
	endIdx := strings.Index(content, fencedEnd)
	if startIdx >= 0 && endIdx >= 0 && endIdx > startIdx {
		existing := content[startIdx : endIdx+len(fencedEnd)]
		if existing == fenced {
			return false, nil
		}
		newContent := content[:startIdx] + fenced + content[endIdx+len(fencedEnd):]
		return true, os.WriteFile(path, []byte(newContent), 0o644)
	}

	// Case 2: old un-fenced snippet present -> migrate to fenced section
	if strings.Contains(content, autoskillSnippet) {
		snippetIdx := strings.Index(content, autoskillSnippet)
		// Find the line boundaries around the snippet
		lineStart := snippetIdx
		for lineStart > 0 && content[lineStart-1] != '\n' {
			lineStart--
		}
		lineEnd := snippetIdx + len(autoskillSnippet)
		for lineEnd < len(content) && content[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd < len(content) {
			lineEnd++ // include the trailing newline
		}

		newContent := content[:lineStart] + fenced + "\n" + content[lineEnd:]
		return true, os.WriteFile(path, []byte(newContent), 0o644)
	}

	// Case 3: neither exists -> append
	if !strings.HasSuffix(content, "\n") && len(content) > 0 {
		content += "\n"
	}
	content += "\n" + fenced + "\n"
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// removeFencedSection removes the fenced autoskill section from a file.
// Returns true if the section was found and removed.
func removeFencedSection(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	content := string(data)

	startIdx := strings.Index(content, fencedStart)
	endIdx := strings.Index(content, fencedEnd)
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		// Also try removing old un-fenced snippet
		if strings.Contains(content, autoskillSnippet) {
			snippetIdx := strings.Index(content, autoskillSnippet)
			lineStart := snippetIdx
			for lineStart > 0 && content[lineStart-1] != '\n' {
				lineStart--
			}
			lineEnd := snippetIdx + len(autoskillSnippet)
			for lineEnd < len(content) && content[lineEnd] != '\n' {
				lineEnd++
			}
			if lineEnd < len(content) {
				lineEnd++
			}
			// Remove extra blank line before if present
			if lineStart > 0 && content[lineStart-1] == '\n' {
				lineStart--
			}
			newContent := content[:lineStart] + content[lineEnd:]
			return true, os.WriteFile(path, []byte(newContent), 0o644)
		}
		return false, nil
	}

	// Find the full line boundaries
	lineStart := startIdx
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := endIdx + len(fencedEnd)
	for lineEnd < len(content) && content[lineEnd] != '\n' {
		lineEnd++
	}
	if lineEnd < len(content) {
		lineEnd++ // include the trailing newline
	}

	// Remove extra blank line before the section if present
	if lineStart > 0 && content[lineStart-1] == '\n' {
		lineStart--
	}

	newContent := content[:lineStart] + content[lineEnd:]
	return true, os.WriteFile(path, []byte(newContent), 0o644)
}
