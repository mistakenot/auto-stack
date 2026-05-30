package cochange

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mistakenot/auto-search/internal/etlscan"
)

// Options configure one co-change run.
type Options struct {
	// InputPath is the file the user asked about (as given on the CLI).
	InputPath string
	// RepoIDOverride bypasses git-remote matching when set (--repo-id).
	RepoIDOverride string
	// InputRoot is the etl output root containing the git datasets
	// (default config.DefaultInputPath()).
	InputRoot string
	// Limit caps related files (default 50; 0 = no cap). Negative is treated as 0.
	Limit int
	// TauDays is the time-decay constant in days (default 90).
	TauDays float64
	// NoDecay disables time decay.
	NoDecay bool
	// RequestID is echoed back in _meta when set.
	RequestID string
}

// Run is the top-level entry point for `autosearch co-change`. It resolves the
// repo from the input path, loads the repo's git parquet into an ephemeral
// in-memory SQLite database, aggregates co-change coupling, scores in Go, and
// assembles the AC-4/AC-5 JSON Result.
//
// It returns a *Result on success (including the metadata-only AC-9 unknown-file
// and AC-3c insufficient-history cases, which are NOT errors). Resolution and
// data errors are returned as typed errors (see repo.go ErrOutsideRepo etc.)
// for the CLI to map to exit codes.
func Run(opts Options) (*Result, error) {
	start := time.Now()

	tau := opts.TauDays
	if tau <= 0 {
		tau = 90
	}
	limit := opts.Limit
	if limit < 0 {
		limit = 0
	}

	// 1. Resolve repo. git_repositories is needed for origin-remote matching.
	repoSources, err := etlscan.DiscoverDatasets(opts.InputRoot, []string{"git_repositories"})
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read git datasets under %q; run `autoetl run --only git`", ErrMissingParquet, opts.InputRoot)
	}
	repos, err := readAllRepos(repoSources)
	if err != nil {
		return nil, err
	}

	resolved, err := ResolveRepo(opts.InputPath, opts.RepoIDOverride, repos)
	if err != nil {
		return nil, err
	}

	// 2. Discover + load the git datasets for the resolved repo.
	gitSources, err := etlscan.DiscoverDatasets(opts.InputRoot, []string{"commits", "commit_files", "git_refs"})
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read git datasets under %q; run `autoetl run --only git`", ErrMissingParquet, opts.InputRoot)
	}
	if !hasDataset(gitSources, "commits") || !hasDataset(gitSources, "commit_files") {
		return nil, fmt.Errorf("%w: no commits/commit_files parquet under %q; run `autoetl run --only git`", ErrMissingParquet, opts.InputRoot)
	}

	db, err := Load(gitSources, LoadParams{RepoID: resolved.RepoID, TauDays: tau, NoDecay: opts.NoDecay})
	if err != nil {
		return nil, fmt.Errorf("load git data: %w", err)
	}
	defer func() { _ = db.Close() }()

	// 3. Aggregate.
	agg, err := Aggregate(db, resolved.ResolvedPath)
	if err != nil {
		return nil, fmt.Errorf("aggregate co-change: %w", err)
	}

	params := ParamsUsed{
		DecayTauDays:      tau,
		LargeCommitCutoff: LargeCommitCutoff,
		MinCoCommits:      MinCoCommits,
		MinCommitsA:       MinCommitsA,
		Limit:             limit,
	}

	meta := Meta{
		RequestID: opts.RequestID,
		Command:   "co-change",
		ElapsedMs: time.Since(start).Milliseconds(),
	}

	metadata := Metadata{
		File:                    opts.InputPath,
		ResolvedPath:            resolved.ResolvedPath,
		ExistsInWorkspace:       existsInWorkspace(resolved.RepoRoot, resolved.ResolvedPath),
		Language:                languageForPath(resolved.ResolvedPath),
		Repo:                    repoLabel(resolved),
		TotalCommits:            agg.CommitsA,
		TopAuthors:              []AuthorCountJSON{},
		TopSessions:             []string{},
		RenamedFrom:             []RenamedFromJSON{},
		RefTipsAtTouchedCommits: []RefTipJSON{},
		ParamsUsed:              params,
	}

	// AC-9: A never appears in history -> metadata-only, total_commits 0.
	if agg.CommitsA == 0 {
		metadata.RelatedFilesFound = 0
		return &Result{Meta: meta, Metadata: metadata, RelatedFiles: []RelatedFile{}}, nil
	}

	// Populate A-level metadata (authors/sessions/first/last/avg) for any A with
	// history, including the insufficient-history case.
	aMeta, err := MetaForA(db, agg.CanonicalA)
	if err != nil {
		return nil, err
	}
	metadata.FirstTouched = isoDate(aMeta.FirstTouched)
	metadata.LastTouched = isoDate(aMeta.LastTouched)
	metadata.AvgFilesPerCommit = aMeta.AvgFilesPerCommit
	metadata.TopAuthors = authorsToJSON(aMeta.TopAuthors)
	if aMeta.TopSessions != nil {
		metadata.TopSessions = aMeta.TopSessions
	}

	renamed, err := RenamedFrom(db, agg.CanonicalA)
	if err != nil {
		return nil, err
	}
	metadata.RenamedFrom = renamedToJSON(renamed)

	refTips, err := RefTips(db, agg.CanonicalA)
	if err != nil {
		return nil, err
	}
	metadata.RefTipsAtTouchedCommits = refTipsToJSON(refTips)

	// AC-3c: A has too little history -> metadata-only + warning.
	if InsufficientHistory(agg) {
		metadata.Warning = "insufficient history"
		metadata.RelatedFilesFound = 0
		return &Result{Meta: meta, Metadata: metadata, RelatedFiles: []RelatedFile{}}, nil
	}

	// 4. Score, filter, sort, limit, then fetch per-candidate detail for only the
	// surviving top-N (the detail queries are the hot path on large repos).
	scored := ScoreAndRank(agg, limit)
	if err := FillCandidateDetails(db, agg.CanonicalA, scored); err != nil {
		return nil, err
	}
	related := make([]RelatedFile, 0, len(scored))
	for i := range scored {
		related = append(related, relatedToJSON(scored[i]))
	}
	metadata.RelatedFilesFound = len(related)

	return &Result{Meta: meta, Metadata: metadata, RelatedFiles: related}, nil
}

// readAllRepos reads every git_repositories parquet source into one slice.
func readAllRepos(sources []etlscan.ParquetSource) ([]etlscan.GitRepoSlim, error) {
	var all []etlscan.GitRepoSlim
	for _, s := range sources {
		if s.Dataset != "git_repositories" {
			continue
		}
		rows, err := etlscan.ReadGitRepos(s.Path)
		if err != nil {
			return nil, fmt.Errorf("read git_repositories %s: %w", s.Path, err)
		}
		all = append(all, rows...)
	}
	return all, nil
}

func hasDataset(sources []etlscan.ParquetSource, dataset string) bool {
	for _, s := range sources {
		if s.Dataset == dataset {
			return true
		}
	}
	return false
}

func existsInWorkspace(repoRoot, resolvedPath string) bool {
	if resolvedPath == "" {
		return false
	}
	full := filepath.Join(repoRoot, filepath.FromSlash(resolvedPath))
	_, err := os.Stat(full)
	return err == nil
}

func repoLabel(r ResolvedRepo) string {
	if strings.TrimSpace(r.Remote) != "" {
		return r.Remote
	}
	return r.RepoID
}

// isoDate converts a unix-ms timestamp to a YYYY-MM-DD date string in UTC. A
// zero/negative timestamp yields the empty string.
func isoDate(unixMs int64) string {
	if unixMs <= 0 {
		return ""
	}
	return time.UnixMilli(unixMs).UTC().Format("2006-01-02")
}

func authorsToJSON(in []AuthorCount) []AuthorCountJSON {
	out := make([]AuthorCountJSON, 0, len(in))
	for _, a := range in {
		out = append(out, AuthorCountJSON{Name: a.Name, Count: a.Count})
	}
	return out
}

func renamedToJSON(in []RenameStep) []RenamedFromJSON {
	out := make([]RenamedFromJSON, 0, len(in))
	for _, r := range in {
		out = append(out, RenamedFromJSON{Path: r.Path, UntilDate: isoDate(r.UntilDate)})
	}
	return out
}

func refTipsToJSON(in []RefTip) []RefTipJSON {
	out := make([]RefTipJSON, 0, len(in))
	for _, r := range in {
		out = append(out, RefTipJSON{RefName: r.RefName, RefType: r.RefType, IsDefault: r.IsDefault})
	}
	return out
}

func relatedToJSON(s ScoredCandidate) RelatedFile {
	c := s.Candidate
	samples := make([]SampleCommitJSON, 0, len(c.SampleCommit))
	for _, sc := range c.SampleCommit {
		samples = append(samples, SampleCommitJSON{SHA: sc.SHA, Date: isoDate(sc.Date), Subject: sc.Subject})
	}
	sessions := c.TopSessions
	if sessions == nil {
		sessions = []string{}
	}
	return RelatedFile{
		Path:           c.Path,
		Score:          s.Score,
		CoCommits:      c.CoCommits,
		ConfidenceAtoB: s.ConfidenceAtoB,
		ConfidenceBtoA: s.ConfidenceBtoA,
		Lift:           s.Lift,
		LastCoChange:   isoDate(c.LastCoChange),
		TopAuthors:     authorsToJSON(c.TopAuthors),
		TopSessions:    sessions,
		SampleCommits:  samples,
	}
}

// languageForPath infers a coarse language label from a file extension. It is a
// best-effort hint for the metadata header (AC-5); unknown extensions yield "".
func languageForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		// Some well-known extensionless files.
		switch strings.ToLower(filepath.Base(path)) {
		case "makefile":
			return "make"
		case "dockerfile":
			return "dockerfile"
		}
		return ""
	}
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx", ".jsx":
		return "react"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".sh", ".bash":
		return "shell"
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sql":
		return "sql"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
