// [autodoc(e8d3cf9c@34e92e15, e8d2824a)]
package commands

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkcheck"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

// FixResult holds the collected issues from a fix scan.
type FixResult struct {
	DocIssues    []docIssue
	LinkIssues   []linkcheck.LinkIssue
	MalformedCnt int
}

// FixCollect scans for all doc and link issues without producing output.
func FixCollect(rootDir string, docsDir string, ignores []string) (*FixResult, error) {
	entries, err := doctree.WalkRepo(rootDir, docsDir, ignores...)
	if err != nil {
		return nil, fmt.Errorf("walking docs: %w", err)
	}

	docIssues := collectDocIssues(entries)

	if _, err := writeMissingDocIDs(entries); err != nil {
		return nil, fmt.Errorf("writing missing doc ids: %w", err)
	}

	entries, err = doctree.WalkRepo(rootDir, docsDir, ignores...)
	if err != nil {
		return nil, fmt.Errorf("re-walking docs after id assignment: %w", err)
	}

	scanResult, err := linkscan.ScanFiles(rootDir)
	if err != nil {
		return nil, fmt.Errorf("scanning source tags: %w", err)
	}
	markdownScan, err := linkscan.ScanMarkdownDocs(entries)
	if err != nil {
		return nil, fmt.Errorf("scanning markdown tags: %w", err)
	}
	scanResult.Tags = append(scanResult.Tags, markdownScan.Tags...)
	scanResult.Malformed = append(scanResult.Malformed, markdownScan.Malformed...)

	linkIssues, err := linkcheck.Check(scanResult.Tags, entries)
	if err != nil {
		return nil, fmt.Errorf("checking source links: %w", err)
	}
	linkIssues = append(linkIssues, linkcheck.IssuesFromMalformed(scanResult.Malformed)...)
	sort.Slice(linkIssues, func(i, j int) bool {
		if linkIssues[i].Tag.FilePath != linkIssues[j].Tag.FilePath {
			return linkIssues[i].Tag.FilePath < linkIssues[j].Tag.FilePath
		}
		if linkIssues[i].Tag.Line != linkIssues[j].Tag.Line {
			return linkIssues[i].Tag.Line < linkIssues[j].Tag.Line
		}
		return linkIssues[i].Status < linkIssues[j].Status
	})

	return &FixResult{
		DocIssues:    docIssues,
		LinkIssues:   linkIssues,
		MalformedCnt: len(scanResult.Malformed),
	}, nil
}

// Fix outputs instructions for an AI agent to fix documentation and code-link issues.
func Fix(w io.Writer, rootDir string, docsDir string, parallelism int, agentFiles []string, ignores []string) error {
	result, err := FixCollect(rootDir, docsDir, ignores)
	if err != nil {
		return err
	}

	if len(result.DocIssues) == 0 && len(result.LinkIssues) == 0 {
		fmt.Fprintln(w, "All documentation files are up to date. No fixes needed.")
		return nil
	}

	fmt.Fprintf(w, "# Documentation Fix Instructions\n\n")

	if len(result.DocIssues) > 0 {
		writeDocFreshness(w, parallelism, result.DocIssues)
	}
	if len(result.LinkIssues) > 0 {
		writeLinkFreshness(w, rootDir, result.LinkIssues)
	}

	parts := make([]string, 0, 3)
	if len(result.DocIssues) > 0 {
		parts = append(parts, fmt.Sprintf("%d doc(s) need attention", len(result.DocIssues)))
	}
	if len(result.LinkIssues) > 0 {
		parts = append(parts, fmt.Sprintf("%d link issue(s)", len(result.LinkIssues)))
	}
	if result.MalformedCnt > 0 {
		parts = append(parts, fmt.Sprintf("%d malformed tag(s)", result.MalformedCnt))
	}
	return fmt.Errorf("found issues: %s", strings.Join(parts, ", "))
}

type docIssue struct {
	RepoRelPath   string
	MissingFM     bool
	StaleHash     bool
	DefaultTitle  bool
	EmptySummary  bool
	EmptyReadWhen bool
}

func collectDocIssues(entries []doctree.Entry) []docIssue {
	issues := make([]docIssue, 0)

	for i := range entries {
		e := &entries[i]
		repoRelPath := entryDisplayPath(e)
		iss := docIssue{RepoRelPath: repoRelPath}

		if e.Title == "" && e.Summary == "" && e.Hash == "" {
			iss.MissingFM = true
		}

		if !iss.MissingFM {
			expected := frontmatter.ComputeHash(&frontmatter.Doc{
				Title:   e.Title,
				Summary: e.Summary,
				Body:    e.Body,
			})
			if e.Hash != expected {
				iss.StaleHash = true
			}
		}

		base := strings.TrimSuffix(path.Base(repoRelPath), ".md")
		if e.Title == base || e.Title == "" {
			iss.DefaultTitle = true
		}
		if e.Summary == "" && !iss.MissingFM {
			iss.EmptySummary = true
		}
		if e.ReadWhen == "" && !iss.MissingFM {
			iss.EmptyReadWhen = true
		}

		if iss.MissingFM || iss.StaleHash || iss.DefaultTitle || iss.EmptySummary || iss.EmptyReadWhen {
			issues = append(issues, iss)
		}
	}

	return issues
}

func writeDocFreshness(w io.Writer, parallelism int, issues []docIssue) {
	fmt.Fprintf(w, "## Documentation Freshness\n\n")
	fmt.Fprintf(w, "Found %d file(s) that need attention.\n\n", len(issues))
	fmt.Fprintln(w, "Each doc file must have YAML frontmatter at the top in this format:")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```yaml")
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w, `title: "Human-Readable Title"`)
	fmt.Fprintln(w, `summary: "One-line summary of the document's content"`)
	fmt.Fprintln(w, `read_when: "modifying the auth middleware"`)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Only set `title`, `summary`, and `read_when`. `auto doc fix` manages `id`, and hashes are managed by `auto doc fixed`.")
	fmt.Fprintln(w)

	numGroups := parallelism
	numGroups = max(numGroups, 1)
	numGroups = min(numGroups, len(issues))
	groups := make([][]docIssue, numGroups)
	for i, iss := range issues {
		groups[i%numGroups] = append(groups[i%numGroups], iss)
	}

	fmt.Fprintf(w, "Files are split into %d groups. Assign each group to a separate sub-agent. Each agent works through its group sequentially.\n", numGroups)
	fmt.Fprintln(w, "Use a cheaper model (e.g. Sonnet, Haiku) for each sub-agent — this is straightforward read-and-summarize work.")
	fmt.Fprintln(w)

	for gi, group := range groups {
		fmt.Fprintf(w, "## Group %d of %d\n\n", gi+1, len(groups))
		fmt.Fprintln(w, "Fix each file in this group sequentially:")
		fmt.Fprintln(w)

		for _, iss := range group {
			fullPath := filepath.ToSlash(iss.RepoRelPath)
			fmt.Fprintf(w, "### `%s`\n\n", fullPath)

			if iss.MissingFM {
				fmt.Fprintln(w, "- Add frontmatter with `title`, `summary`, and `read_when` fields.")
				fmt.Fprintln(w, "- Set `summary` to a one-line description of the file's content.")
				fmt.Fprintln(w, "- Set `read_when` to a short phrase describing the situation an agent should read this file (omit the leading \"when\").")
			}
			if iss.DefaultTitle {
				fmt.Fprintln(w, "- Set `title` to a human-readable version based on the content or main H1 heading.")
			}
			if iss.EmptySummary && !iss.MissingFM {
				fmt.Fprintln(w, "- Set `summary` to a one-line description of the file's content.")
			}
			if iss.EmptyReadWhen && !iss.MissingFM {
				fmt.Fprintln(w, "- Set `read_when` to a short phrase describing the situation an agent should read this file (omit the leading \"when\").")
			}
			if iss.StaleHash && !iss.MissingFM && !iss.DefaultTitle && !iss.EmptySummary {
				fmt.Fprintln(w, "- Review that `title` and `summary` still accurately reflect the content.")
			}
			fmt.Fprintf(w, "- Run `auto doc fixed %s`\n", fullPath)
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintln(w, "## After All Groups Complete")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "1. Run `auto doc fix` again to check for remaining issues.")
	fmt.Fprintln(w, "2. Run `auto doc agents` to update agent memory files.")
	fmt.Fprintln(w)
}

func writeLinkFreshness(w io.Writer, rootDir string, issues []linkcheck.LinkIssue) {
	fmt.Fprintln(w, "## Link Freshness")
	fmt.Fprintln(w)

	for i := range issues {
		issue := &issues[i]
		sourcePath := issue.Tag.FilePath
		if rel, err := filepath.Rel(rootDir, issue.Tag.FilePath); err == nil {
			sourcePath = rel
		}
		sourcePath = filepath.ToSlash(sourcePath)
		tagText := formatTag(&issue.Tag)
		docPath := ""
		if issue.DocFile != "" {
			docPath = filepath.ToSlash(issue.DocFile)
		}

		switch issue.Status {
		case linkcheck.ScopeHashMismatch:
			fmt.Fprintln(w, "LINK STALE: source changed, doc may need updating")
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag:       %s\n", tagText)
			fmt.Fprintf(w, "  doc:       %s (id: %s)\n", docPath, issue.Tag.DocId)
			fmt.Fprintf(w, "  current doc hash:   %s (unchanged)\n", issue.CurrentDocHash)
			fmt.Fprintf(w, "  current scope hash: %s (was %s)\n", issue.CurrentScopeHash, issue.Tag.ScopeHash)
			fmt.Fprintln(w, "  action: Read the source scope and the doc. If the doc is still accurate,")
			fmt.Fprintf(w, "          update the tag to [autodoc"+"(%s@%s, %s)].\n", issue.Tag.DocId, issue.CurrentDocHash, issue.CurrentScopeHash)
			fmt.Fprintln(w, "          If the doc needs updating, update the doc content first,")
			fmt.Fprintln(w, "          then run `auto doc fixed <docPath>` to get the new doc hash,")
			fmt.Fprintln(w, "          then update the tag with both new hashes.")
		case linkcheck.DocHashMismatch:
			fmt.Fprintln(w, "LINK STALE: doc updated, source tag needs refresh")
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag:       %s\n", tagText)
			fmt.Fprintf(w, "  doc:       %s (id: %s)\n", docPath, issue.Tag.DocId)
			fmt.Fprintf(w, "  current doc hash:   %s (was %s)\n", issue.CurrentDocHash, issue.Tag.DocHash)
			fmt.Fprintf(w, "  current scope hash: %s (unchanged)\n", issue.CurrentScopeHash)
			fmt.Fprintf(w, "  action: Update the docHash in the source tag to %s.\n", issue.CurrentDocHash)
			fmt.Fprintf(w, "          New tag: [autodoc"+"(%s@%s, %s)]\n", issue.Tag.DocId, issue.CurrentDocHash, issue.Tag.ScopeHash)
		case linkcheck.BothMismatch:
			fmt.Fprintln(w, "LINK STALE: both source and doc changed since last sync")
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag:       %s\n", tagText)
			fmt.Fprintf(w, "  doc:       %s (id: %s)\n", docPath, issue.Tag.DocId)
			fmt.Fprintf(w, "  current doc hash:   %s (was %s)\n", issue.CurrentDocHash, issue.Tag.DocHash)
			fmt.Fprintf(w, "  current scope hash: %s (was %s)\n", issue.CurrentScopeHash, issue.Tag.ScopeHash)
			fmt.Fprintln(w, "  action: Read both the source scope and the doc carefully.")
			fmt.Fprintln(w, "          Update the doc if needed, then run `auto doc fixed <docPath>`.")
			fmt.Fprintln(w, "          Update the tag with both current hashes.")
		case linkcheck.OrphanedTag:
			fmt.Fprintf(w, "LINK ORPHANED: doc not found for id %s\n", issue.Tag.DocId)
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag:       %s\n", tagText)
			fmt.Fprintln(w, "  action: The referenced doc no longer exists. Remove the tag or")
			fmt.Fprintln(w, "          update it to reference a valid doc ID.")
		case linkcheck.MalformedTag:
			fmt.Fprintln(w, "LINK ERROR: malformed autodoc tag")
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag text:  %s\n", issue.Tag.RawTag)
			fmt.Fprintln(w, "  action: Fix the tag format to:")
			fmt.Fprintln(w, "          [autodoc"+"(<docId>@<docHash>, <scopeHash>)]")
		case linkcheck.SelfReferencingTag:
			fmt.Fprintln(w, "LINK ERROR: self-referencing autodoc tag")
			fmt.Fprintf(w, "  location:  %s:%d\n", sourcePath, issue.Tag.Line)
			fmt.Fprintf(w, "  tag:       %s\n", tagText)
			fmt.Fprintln(w, "  action: Remove the tag or point it at a different doc ID.")
		}
		fmt.Fprintln(w)
	}
}

func formatTag(tag *linkscan.Tag) string {
	if tag.RawTag != "" {
		return tag.RawTag
	}
	if tag.DocId == "" || tag.DocHash == "" || tag.ScopeHash == "" {
		return "[autodoc(...)]"
	}
	return fmt.Sprintf("[autodoc"+"(%s@%s, %s)]", tag.DocId, tag.DocHash, tag.ScopeHash)
}

func writeMissingDocIDs(entries []doctree.Entry) (int, error) {
	knownIDs := make(map[string]bool, len(entries))
	for i := range entries {
		if entries[i].Id != "" {
			knownIDs[entries[i].Id] = true
		}
	}

	updated := 0
	for i := range entries {
		e := &entries[i]
		if e.Id != "" {
			continue
		}

		fullPath := e.AbsPath
		if fullPath == "" {
			fullPath = e.RepoRelPath
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return updated, err
		}
		doc := frontmatter.Parse(string(data))

		// Skip bare markdown files with no frontmatter; those are handled as normal fix instructions.
		if doc.Title == "" && doc.Summary == "" && doc.Hash == "" {
			continue
		}

		newID, err := generateUniqueDocID(knownIDs)
		if err != nil {
			return updated, err
		}
		doc.Id = newID

		if err := os.WriteFile(fullPath, []byte(frontmatter.Serialize(&doc)), 0o644); err != nil {
			return updated, err
		}
		knownIDs[newID] = true
		updated++
	}

	return updated, nil
}

func generateUniqueDocID(existing map[string]bool) (string, error) {
	for range 16 {
		id, err := generateDocID()
		if err != nil {
			return "", err
		}
		if !existing[id] {
			return id, nil
		}
	}
	return "", errors.New("unable to generate unique doc id after retries")
}

func generateDocID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
