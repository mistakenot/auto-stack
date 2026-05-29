package cochange

import (
	"database/sql"
	"fmt"
)

// LargeCommitCutoff is the files_changed threshold above which a commit is
// dropped entirely before aggregation (AC-3b). It lives here because the
// aggregation passes apply it; Phase 4 surfaces it in params_used.
const LargeCommitCutoff = 50

// AggregateResult is the full output of one co-change aggregation over the
// loaded repo for a single input file A. All weighted quantities (Wa, Wn, and
// each candidate's Wab/Wb) are precomputed-weight SUMs; Phase 4 turns them into
// the final scalar scores (confidence, lift, score).
type AggregateResult struct {
	// CanonicalA is the input file's canonical (latest) path after folding all
	// rename edges. It is the empty string if A never appears in history.
	CanonicalA string
	// Wa is Σ weight over commits touching any lineage path of A.
	Wa float64
	// CommitsA is the raw count of commits touching A (post large-commit filter).
	CommitsA int
	// Wn is Σ weight over all loaded (filtered) commits.
	Wn float64
	// Candidates are the co-changing files, one row per canonical candidate path.
	Candidates []Candidate
}

// Candidate holds the per-candidate aggregates for one canonical path B.
type Candidate struct {
	Path         string
	Wab          float64 // Σ weight over commits touching both A and B
	Wb           float64 // Σ weight over ALL commits touching B (independent of A)
	CoCommits    int     // raw count of commits touching both A and B
	CommitsB     int     // raw count of commits touching B
	LastCoChange int64   // author_date (unix ms) of the most recent co-change commit
	TopAuthors   []AuthorCount
	TopSessions  []string       // session ids, recency order, up to 5
	SampleCommit []SampleCommit // up to 3, most recent
}

// AuthorCount is a single {name, count} entry.
type AuthorCount struct {
	Name  string
	Count int
}

// SampleCommit is a single representative commit for a candidate.
type SampleCommit struct {
	SHA     string // short_id
	Date    int64  // author_date (unix ms)
	Subject string // message_truncated
}

// RefTip is a branch/tag whose ref tip coincides with a commit touching A.
type RefTip struct {
	RefName   string
	RefType   string
	IsDefault bool
}

// Aggregate runs all SQL passes for input file A and returns the combined
// result. inputPath is matched against canonical paths after rename folding, so
// passing either a historical or current path of A resolves to the same lineage.
//
// The input path is always bound as a SQL argument (never concatenated), per
// AC-12.
func Aggregate(db *DB, inputPath string) (*AggregateResult, error) {
	sdb := db.SQL()

	canonA, err := canonicalPath(sdb, inputPath)
	if err != nil {
		return nil, err
	}

	res := &AggregateResult{CanonicalA: canonA}

	if err := sdb.QueryRow(`SELECT COALESCE(SUM(weight), 0) FROM c`).Scan(&res.Wn); err != nil {
		return nil, fmt.Errorf("compute Wn: %w", err)
	}

	if canonA == "" {
		// A never appears in history: no lineage, no candidates. Phase 4 emits a
		// metadata-only payload (AC-9).
		return res, nil
	}

	if err := perPathTotalForA(sdb, canonA, res); err != nil {
		return nil, err
	}

	candidates, err := coOccurrence(sdb, canonA)
	if err != nil {
		return nil, err
	}
	if err := fillCandidateTotals(sdb, candidates); err != nil {
		return nil, err
	}
	for i := range candidates {
		if err := fillCandidateDetail(sdb, canonA, &candidates[i]); err != nil {
			return nil, err
		}
	}
	res.Candidates = candidates
	return res, nil
}

// pathCanonCTE is the recursive CTE that folds rename edges (change_type='R':
// old_path -> file_path) FORWARD to each observed path's latest canonical path,
// over ALL rename edges in the repo. It exposes one row per observed path
// (observed -> canonical). Non-renamed paths map to themselves.
//
// Rename edges are deduplicated and ordered by the commit author_date so the
// "latest" rename for a given old_path wins when a path was renamed more than
// once from the same source. We seed the recursion from every observed path
// (file_path and old_path) and walk forward; the terminal path (one that is
// never an old_path of a later rename) is the canonical path.
const pathCanonCTE = `
WITH RECURSIVE
rename_edge AS (
	SELECT cf.old_path AS src, cf.file_path AS dst, c.author_date AS at
	FROM cf JOIN c ON c.commit_id = cf.commit_id
	WHERE cf.change_type = 'R'
	  AND cf.old_path <> '' AND cf.file_path <> '' AND cf.old_path <> cf.file_path
	  AND c.files_changed <= ` + cutoffStr + `
),
-- latest forward edge per source path
edge AS (
	SELECT src, dst FROM (
		SELECT src, dst, ROW_NUMBER() OVER (PARTITION BY src ORDER BY at DESC) AS rn
		FROM rename_edge
	) WHERE rn = 1
),
observed AS (
	SELECT DISTINCT file_path AS p FROM cf
	UNION
	SELECT DISTINCT old_path AS p FROM cf WHERE old_path <> ''
),
canon(p, cur, depth) AS (
	SELECT p, p, 0 FROM observed
	UNION ALL
	SELECT canon.p, edge.dst, canon.depth + 1
	FROM canon JOIN edge ON edge.src = canon.cur
	WHERE canon.depth < 64
),
path_canon AS (
	SELECT p AS observed_path, cur AS canonical_path FROM (
		SELECT p, cur, ROW_NUMBER() OVER (PARTITION BY p ORDER BY depth DESC) AS rn
		FROM canon
	) WHERE rn = 1
)
`

// cutoffStr is the large-commit cutoff inlined into static SQL. It is a
// compile-time constant integer, never user input, so inlining is safe.
const cutoffStr = "50"

func init() {
	if cutoffStr != itoa(LargeCommitCutoff) {
		panic("cutoffStr out of sync with LargeCommitCutoff")
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// canonicalPath resolves inputPath to its canonical (latest) path. If the path
// never appears in history (neither as file_path nor old_path), it returns "".
func canonicalPath(sdb *sql.DB, inputPath string) (string, error) {
	q := pathCanonCTE + `
SELECT canonical_path FROM path_canon WHERE observed_path = ? LIMIT 1`
	var canon string
	err := sdb.QueryRow(q, inputPath).Scan(&canon)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve canonical path: %w", err)
	}
	return canon, nil
}

// cfCanonCTE is appended after pathCanonCTE to expose `cfc`: each cf row with its
// canonical path and its commit weight, restricted to non-large commits.
const cfCanonCTE = `,
cfc AS (
	SELECT cf.commit_id AS commit_id,
	       pc.canonical_path AS path,
	       c.weight AS weight,
	       c.author_date AS author_date
	FROM cf
	JOIN path_canon pc ON pc.observed_path = cf.file_path
	JOIN c ON c.commit_id = cf.commit_id
	WHERE c.files_changed <= ` + cutoffStr + `
)
`

// perPathTotalForA fills Wa and CommitsA for A's canonical path.
func perPathTotalForA(sdb *sql.DB, canonA string, res *AggregateResult) error {
	q := pathCanonCTE + cfCanonCTE + `
SELECT COALESCE(SUM(weight), 0), COUNT(DISTINCT commit_id)
FROM cfc WHERE path = ?`
	err := sdb.QueryRow(q, canonA).Scan(&res.Wa, &res.CommitsA)
	if err != nil {
		return fmt.Errorf("per-path total for A: %w", err)
	}
	return nil
}

// coOccurrence runs the self-join of A's canonical path against every other
// canonical path on shared commit_id. It produces ONLY Wab, raw co_commits, and
// last_co_change per candidate — never Wb.
func coOccurrence(sdb *sql.DB, canonA string) ([]Candidate, error) {
	q := pathCanonCTE + cfCanonCTE + `
SELECT b.path,
       COALESCE(SUM(a.weight), 0) AS wab,
       COUNT(DISTINCT a.commit_id) AS co_commits,
       MAX(a.author_date) AS last_co_change
FROM cfc a
JOIN cfc b ON b.commit_id = a.commit_id AND b.path <> a.path
WHERE a.path = ?
GROUP BY b.path
ORDER BY b.path`
	rows, err := sdb.Query(q, canonA)
	if err != nil {
		return nil, fmt.Errorf("co-occurrence query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		var lastCo sql.NullInt64
		if err := rows.Scan(&c.Path, &c.Wab, &c.CoCommits, &lastCo); err != nil {
			return nil, fmt.Errorf("scan co-occurrence row: %w", err)
		}
		c.LastCoChange = lastCo.Int64
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate co-occurrence rows: %w", err)
	}
	return out, nil
}

// fillCandidateTotals fills Wb and CommitsB for each candidate via a per-path
// total over ALL filtered cf rows (independent of A), so Wb counts B's non-A
// commits.
func fillCandidateTotals(sdb *sql.DB, candidates []Candidate) error {
	if len(candidates) == 0 {
		return nil
	}
	q := pathCanonCTE + cfCanonCTE + `
SELECT path, COALESCE(SUM(weight), 0), COUNT(DISTINCT commit_id)
FROM cfc GROUP BY path`
	rows, err := sdb.Query(q)
	if err != nil {
		return fmt.Errorf("per-path totals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	totals := make(map[string]struct {
		w float64
		n int
	})
	for rows.Next() {
		var p string
		var w float64
		var n int
		if err := rows.Scan(&p, &w, &n); err != nil {
			return fmt.Errorf("scan per-path total: %w", err)
		}
		totals[p] = struct {
			w float64
			n int
		}{w, n}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate per-path totals: %w", err)
	}
	for i := range candidates {
		t := totals[candidates[i].Path]
		candidates[i].Wb = t.w
		candidates[i].CommitsB = t.n
	}
	return nil
}

// fillCandidateDetail populates top authors, top sessions and sample commits for
// one candidate, over the commits where A and the candidate co-occur.
func fillCandidateDetail(sdb *sql.DB, canonA string, cand *Candidate) error {
	// Common subselect: the set of commit_ids where A and this candidate
	// co-occur (post-canonicalisation, post large-commit filter).
	coCommitsCTE := pathCanonCTE + cfCanonCTE + `,
co AS (
	SELECT DISTINCT a.commit_id
	FROM cfc a JOIN cfc b ON b.commit_id = a.commit_id
	WHERE a.path = ? AND b.path = ?
)
`
	// Top authors.
	authorsQ := coCommitsCTE + `
SELECT c.author_name, COUNT(*) AS cnt
FROM c JOIN co ON co.commit_id = c.commit_id
GROUP BY c.author_name
ORDER BY cnt DESC, c.author_name ASC
LIMIT 5`
	rows, err := sdb.Query(authorsQ, canonA, cand.Path)
	if err != nil {
		return fmt.Errorf("top authors query: %w", err)
	}
	for rows.Next() {
		var a AuthorCount
		if err := rows.Scan(&a.Name, &a.Count); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan author: %w", err)
		}
		cand.TopAuthors = append(cand.TopAuthors, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate authors: %w", err)
	}
	_ = rows.Close()

	// Top sessions, recency order (most recent co-change first). Skip empty
	// session ids (commits with no linked session).
	sessionsQ := coCommitsCTE + `
SELECT c.session_id, MAX(c.author_date) AS recent
FROM c JOIN co ON co.commit_id = c.commit_id
WHERE c.session_id <> ''
GROUP BY c.session_id
ORDER BY recent DESC
LIMIT 5`
	rows, err = sdb.Query(sessionsQ, canonA, cand.Path)
	if err != nil {
		return fmt.Errorf("top sessions query: %w", err)
	}
	for rows.Next() {
		var sid string
		var recent int64
		if err := rows.Scan(&sid, &recent); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan session: %w", err)
		}
		cand.TopSessions = append(cand.TopSessions, sid)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate sessions: %w", err)
	}
	_ = rows.Close()

	// Sample commits, most recent first.
	samplesQ := coCommitsCTE + `
SELECT c.short_id, c.author_date, c.message_truncated
FROM c JOIN co ON co.commit_id = c.commit_id
ORDER BY c.author_date DESC
LIMIT 3`
	rows, err = sdb.Query(samplesQ, canonA, cand.Path)
	if err != nil {
		return fmt.Errorf("sample commits query: %w", err)
	}
	for rows.Next() {
		var s SampleCommit
		if err := rows.Scan(&s.SHA, &s.Date, &s.Subject); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan sample commit: %w", err)
		}
		cand.SampleCommit = append(cand.SampleCommit, s)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate sample commits: %w", err)
	}
	_ = rows.Close()

	return nil
}

// RefTips returns the branches/tags whose ref tip coincides with a commit
// touching A's canonical path. The join strips the "<repoID>-" prefix from the
// commit id (c.commit_id) because refs.commit_id is the RAW sha (AC-5): a naive
// ON commit_id join matches nothing.
//
// repoID is passed as a bound argument used only to compute the substring
// offset (length(repoID)+2: one for the 1-based index, one for the '-'), so no
// user value is concatenated into the SQL.
func RefTips(db *DB, canonA string) ([]RefTip, error) {
	if canonA == "" {
		return nil, nil
	}
	sdb := db.SQL()
	q := pathCanonCTE + cfCanonCTE + `
SELECT DISTINCT r.ref_name, r.ref_type, r.is_default
FROM refs r
JOIN c ON r.commit_id = substr(c.commit_id, length(?) + 2)
JOIN cfc ON cfc.commit_id = c.commit_id
WHERE cfc.path = ?
ORDER BY r.is_default DESC, r.ref_name ASC`
	rows, err := sdb.Query(q, db.RepoID(), canonA)
	if err != nil {
		return nil, fmt.Errorf("ref-tip query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RefTip
	for rows.Next() {
		var t RefTip
		var isDefault int
		if err := rows.Scan(&t.RefName, &t.RefType, &isDefault); err != nil {
			return nil, fmt.Errorf("scan ref tip: %w", err)
		}
		t.IsDefault = isDefault != 0
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ref tips: %w", err)
	}
	return out, nil
}

// RenamedFrom returns the prior paths of A's lineage (each rename edge along the
// chain ending at canonA), most recent rename first, as {path, until_date}
// pairs. until_date is the author_date (unix ms) of the rename commit.
func RenamedFrom(db *DB, canonA string) ([]RenameStep, error) {
	if canonA == "" {
		return nil, nil
	}
	sdb := db.SQL()
	// Walk rename edges backward from canonA: any edge whose canonical
	// destination equals canonA contributes its old_path as a prior name.
	q := pathCanonCTE + `
SELECT cf.old_path, c.author_date
FROM cf
JOIN c ON c.commit_id = cf.commit_id
JOIN path_canon pc ON pc.observed_path = cf.file_path
WHERE cf.change_type = 'R'
  AND cf.old_path <> '' AND cf.old_path <> cf.file_path
  AND pc.canonical_path = ?
  AND c.files_changed <= ` + cutoffStr + `
ORDER BY c.author_date DESC`
	rows, err := sdb.Query(q, canonA)
	if err != nil {
		return nil, fmt.Errorf("renamed-from query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := map[string]bool{}
	var out []RenameStep
	for rows.Next() {
		var step RenameStep
		if err := rows.Scan(&step.Path, &step.UntilDate); err != nil {
			return nil, fmt.Errorf("scan rename step: %w", err)
		}
		if seen[step.Path] {
			continue
		}
		seen[step.Path] = true
		out = append(out, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rename steps: %w", err)
	}
	return out, nil
}

// RenameStep is one prior name in A's rename lineage.
type RenameStep struct {
	Path      string
	UntilDate int64 // author_date (unix ms) of the rename commit
}
