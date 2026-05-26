package contextpack

// Pack is the top-level context pack model containing selected files,
// relationships, guidance, and budget accounting for a set of seed files.
type Pack struct {
	ProjectRoot       string             `json:"project_root"`
	TokenLimit        int                `json:"token_limit"`
	EstimatedTokens   int                `json:"estimated_tokens"`
	OmittedTokens     int                `json:"omitted_tokens"`
	SeedFiles         []string           `json:"seed_files"`
	ReadingOrder      []ReadingOrderItem `json:"reading_order"`
	Files             []FileEntry        `json:"files"`
	Relationships     []Relationship     `json:"relationships"`
	Guidance          Guidance           `json:"guidance"`
	OmittedCandidates []OmittedCandidate `json:"omitted_candidates"`
}

// ReadingOrderItem specifies a file to read and why.
type ReadingOrderItem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// FileEntry represents an included file with its content and metadata.
type FileEntry struct {
	Path            string   `json:"path"`
	Role            string   `json:"role"`
	Reason          string   `json:"reason"`
	EstimatedTokens int      `json:"estimated_tokens"`
	Flags           []string `json:"flags,omitempty"`
	Content         string   `json:"content"`
}

// Relationship captures an import relationship between two files.
type Relationship struct {
	Source            string   `json:"source"`
	Target            string   `json:"target"`
	Direction         string   `json:"direction"`
	PrimaryImportKind string   `json:"primary_import_kind"`
	ImportKinds       []string `json:"import_kinds"`
	Distance          int      `json:"distance"`
	Reason            string   `json:"reason"`
}

// Guidance contains concise algorithmic guidance derived from graph facts.
type Guidance struct {
	Watch []string `json:"watch,omitempty"`
}

// OmittedCandidate records a file that was considered but excluded from
// the pack due to budget constraints.
type OmittedCandidate struct {
	Path            string `json:"path"`
	Role            string `json:"role"`
	Reason          string `json:"reason"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

// ValidationError is a structured error returned when seed path validation fails.
type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

func (e *ValidationError) Error() string {
	return e.Message
}

// ValidationErrors collects multiple validation failures.
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

func (e *ValidationErrors) Error() string {
	if len(e.Errors) == 0 {
		return "validation failed"
	}
	return e.Errors[0].Message
}

// SeedBudgetExceededError is returned when seed files cannot fit within
// the token budget for the selected output format.
type SeedBudgetExceededError struct {
	MinimumBudget int `json:"minimum_budget"`
	TokenLimit    int `json:"token_limit"`
}

func (e *SeedBudgetExceededError) Error() string {
	return "seed files exceed token budget"
}
