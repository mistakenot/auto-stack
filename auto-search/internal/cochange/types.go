package cochange

// JSON output structs for `auto search co-change`. Field names and snake_case
// tags match requirements AC-4 (per-related-file) and AC-5 (metadata header)
// exactly. Dates are emitted as ISO date strings (YYYY-MM-DD, UTC) — see
// isoDate in cochange.go for the conversion from the engine's unix-ms int64s.

// Result is the top-level JSON payload, using the shared `{_meta, ...}` envelope
// convention (see cli/stats.go, cli/search.go).
type Result struct {
	Meta         Meta          `json:"_meta"`
	Metadata     Metadata      `json:"metadata"`
	RelatedFiles []RelatedFile `json:"related_files"`
}

// Meta is the response envelope metadata.
type Meta struct {
	RequestID string `json:"request_id,omitempty"`
	Command   string `json:"command"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

// Metadata is the AC-5 metadata header summarising the input file's history.
type Metadata struct {
	File                    string            `json:"file"`
	ResolvedPath            string            `json:"resolved_path"`
	ExistsInWorkspace       bool              `json:"exists_in_workspace"`
	Language                string            `json:"language"`
	Repo                    string            `json:"repo"`
	TotalCommits            int               `json:"total_commits"`
	FirstTouched            string            `json:"first_touched"`
	LastTouched             string            `json:"last_touched"`
	TopAuthors              []AuthorCountJSON `json:"top_authors"`
	TopSessions             []string          `json:"top_sessions"`
	AvgFilesPerCommit       float64           `json:"avg_files_per_commit"`
	RenamedFrom             []RenamedFromJSON `json:"renamed_from"`
	RefTipsAtTouchedCommits []RefTipJSON      `json:"ref_tips_at_touched_commits"`
	RelatedFilesFound       int               `json:"related_files_found"`
	ParamsUsed              ParamsUsed        `json:"params_used"`
	Warning                 string            `json:"warning,omitempty"`
}

// RelatedFile is one AC-4 related-file entry.
type RelatedFile struct {
	Path           string             `json:"path"`
	Score          float64            `json:"score"`
	CoCommits      int                `json:"co_commits"`
	ConfidenceAtoB float64            `json:"confidence_a_to_b"`
	ConfidenceBtoA float64            `json:"confidence_b_to_a"`
	Lift           float64            `json:"lift"`
	LastCoChange   string             `json:"last_co_change"`
	TopAuthors     []AuthorCountJSON  `json:"top_authors"`
	TopSessions    []string           `json:"top_sessions"`
	SampleCommits  []SampleCommitJSON `json:"sample_commits"`
}

// AuthorCountJSON is a {name, count} author entry.
type AuthorCountJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RenamedFromJSON is a prior name in A's rename lineage.
type RenamedFromJSON struct {
	Path      string `json:"path"`
	UntilDate string `json:"until_date"`
}

// RefTipJSON is a branch/tag whose ref tip coincides with a commit touching A.
type RefTipJSON struct {
	RefName   string `json:"ref_name"`
	RefType   string `json:"ref_type"`
	IsDefault bool   `json:"is_default"`
}

// SampleCommitJSON is a representative co-change commit for a related file.
type SampleCommitJSON struct {
	SHA     string `json:"sha"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// ParamsUsed records the scoring parameters in effect for the query (AC-5).
type ParamsUsed struct {
	DecayTauDays float64 `json:"decay_tau_days"`
	MinCoCommits int     `json:"min_co_commits"`
	MinCommitsA  int     `json:"min_commits_a"`
}
