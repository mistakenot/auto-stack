// Package cochange implements the engine behind `auto search co-change`: it
// loads a repo's git parquet (commits, commit_files, git_refs) into an
// ephemeral in-memory SQLite database and aggregates temporal coupling in SQL.
//
// The engine is split across two files:
//   - load.go  — read projected parquet rows, compute the per-commit decay/size
//     weight in Go, and INSERT into the in-memory DB.
//   - query.go — run the rename-canonicalisation CTE, per-path and co-occurrence
//     aggregations, top-N windows, and the ref-tip join.
//
// Final scalar scoring (confidence, lift, score) and JSON assembly live in
// Phase 4 (score.go / cochange.go) and consume the structs this package exposes.
package cochange

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/mistakenot/auto-search/internal/etlscan"

	// modernc.org/sqlite registers the pure-Go "sqlite" driver (no cgo).
	_ "modernc.org/sqlite"
)

// LoadParams controls how per-commit weights are computed at load time.
type LoadParams struct {
	// RepoID is the repository whose rows are loaded; all other repos' rows are
	// filtered out at load time.
	RepoID string
	// TauDays is the exponential time-decay constant in days (default 90).
	TauDays float64
	// NoDecay disables time decay (weight depends only on commit size).
	NoDecay bool
}

// DB wraps the ephemeral in-memory SQLite database holding one repo's git data.
// Callers MUST Close it when done.
type DB struct {
	repoID string
	sql    *sql.DB
}

// Close releases the underlying in-memory database.
func (d *DB) Close() error {
	if d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

// RepoID returns the repository these rows were loaded for.
func (d *DB) RepoID() string { return d.repoID }

// SQL exposes the underlying *sql.DB for query.go and tests.
func (d *DB) SQL() *sql.DB { return d.sql }

// Load reads the named parquet sources, filters them to params.RepoID, computes
// per-commit weights in Go, and loads everything into a fresh in-memory SQLite
// database. The returned DB is ready for Aggregate.
//
// sources are the ParquetSource entries discovered via
// etlscan.DiscoverDatasets(inputRoot, {"commits","commit_files","git_refs"}).
// (git_repositories is used for repo resolution, not loaded here.)
func Load(sources []etlscan.ParquetSource, params LoadParams) (*DB, error) {
	commits, commitFiles, refs, err := readSources(sources)
	if err != nil {
		return nil, err
	}
	return LoadRows(commits, commitFiles, refs, params)
}

// LoadRows loads already-read slim rows into a fresh in-memory SQLite database.
// It is the seam the unit tests use to feed synthetic data through the exact
// same load path as production.
func LoadRows(
	commits []etlscan.CommitSlim,
	commitFiles []etlscan.CommitFileSlim,
	refs []etlscan.GitRefSlim,
	params LoadParams,
) (*DB, error) {
	tau := params.TauDays
	if tau <= 0 {
		tau = 90
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open in-memory sqlite: %w", err)
	}
	// A :memory: database is private to a single connection; a pool with more
	// than one connection would observe empty tables on a different conn.
	db.SetMaxOpenConns(1)

	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Reference time for decay: the most recent commit's author date among the
	// loaded (filtered) commits. Using the newest commit (rather than wall-clock
	// now) keeps weights stable across runs and independent of when the query
	// is executed. Δt is each commit's age relative to this reference; the newest
	// commit therefore has decay == 1.
	var refTimeMs int64
	for i := range commits {
		if commits[i].RepoID != params.RepoID {
			continue
		}
		if commits[i].AuthorDate > refTimeMs {
			refTimeMs = commits[i].AuthorDate
		}
	}

	if err := insertCommits(db, commits, params, tau, refTimeMs); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := insertCommitFiles(db, commitFiles, params.RepoID); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := insertRefs(db, refs, params.RepoID); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &DB{repoID: params.RepoID, sql: db}, nil
}

// readSources reads each discovered parquet source into its slim row type.
func readSources(sources []etlscan.ParquetSource) (
	commits []etlscan.CommitSlim,
	commitFiles []etlscan.CommitFileSlim,
	refs []etlscan.GitRefSlim,
	err error,
) {
	for _, s := range sources {
		switch s.Dataset {
		case "commits":
			rows, e := etlscan.ReadCommitsSlim(s.Path)
			if e != nil {
				return nil, nil, nil, fmt.Errorf("read commits %s: %w", s.Path, e)
			}
			commits = append(commits, rows...)
		case "commit_files":
			rows, e := etlscan.ReadCommitFilesSlim(s.Path)
			if e != nil {
				return nil, nil, nil, fmt.Errorf("read commit_files %s: %w", s.Path, e)
			}
			commitFiles = append(commitFiles, rows...)
		case "git_refs":
			rows, e := etlscan.ReadGitRefs(s.Path)
			if e != nil {
				return nil, nil, nil, fmt.Errorf("read git_refs %s: %w", s.Path, e)
			}
			refs = append(refs, rows...)
		}
	}
	return commits, commitFiles, refs, nil
}

func createSchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE c (
	commit_id         TEXT PRIMARY KEY,
	author_date       INTEGER,
	files_changed     INTEGER,
	author_name       TEXT,
	author_email      TEXT,
	session_id        TEXT,
	short_id          TEXT,
	message_truncated TEXT,
	weight            REAL
);
CREATE TABLE cf (
	commit_id   TEXT,
	file_path   TEXT,
	change_type TEXT,
	old_path    TEXT
);
CREATE TABLE refs (
	ref_name   TEXT,
	ref_type   TEXT,
	commit_id  TEXT,
	is_default INTEGER
);
CREATE INDEX idx_cf_commit ON cf(commit_id);
CREATE INDEX idx_cf_path   ON cf(file_path);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// commitWeight computes the per-commit weight stored on c.weight. It is the
// authoritative scoring weight from solution.md:
//
//	filesWeight = 1 / log1p(max(1, files_changed))
//	decay       = noDecay ? 1 : exp(-ageSeconds / (tauDays * 86400))
//	weight      = filesWeight * decay
//
// ageSeconds is the commit's age relative to refTimeMs (the newest loaded
// commit). Computing this in Go means SQL only ever SUMs a precomputed column.
func commitWeight(filesChanged int32, authorDateMs, refTimeMs int64, noDecay bool, tauDays float64) float64 {
	files := math.Max(1, float64(filesChanged))
	filesWeight := 1.0 / math.Log1p(files)
	decay := 1.0
	if !noDecay {
		ageSeconds := float64(refTimeMs-authorDateMs) / 1000.0
		if ageSeconds < 0 {
			ageSeconds = 0
		}
		decay = math.Exp(-ageSeconds / (tauDays * 86400.0))
	}
	return filesWeight * decay
}

func insertCommits(db *sql.DB, commits []etlscan.CommitSlim, params LoadParams, tau float64, refTimeMs int64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin commits tx: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO c
		(commit_id, author_date, files_changed, author_name, author_email, session_id, short_id, message_truncated, weight)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare commits insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range commits {
		c := &commits[i]
		if c.RepoID != params.RepoID {
			continue
		}
		w := commitWeight(c.FilesChanged, c.AuthorDate, refTimeMs, params.NoDecay, tau)
		if _, err := stmt.Exec(
			c.ID, c.AuthorDate, c.FilesChanged, c.AuthorName, c.AuthorEmail,
			c.SessionID, c.ShortID, c.MessageTruncated, w,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert commit %s: %w", c.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit commits tx: %w", err)
	}
	return nil
}

func insertCommitFiles(db *sql.DB, files []etlscan.CommitFileSlim, repoID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin commit_files tx: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO cf (commit_id, file_path, change_type, old_path) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare commit_files insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range files {
		f := &files[i]
		if f.RepoID != repoID {
			continue
		}
		if _, err := stmt.Exec(f.CommitID, f.FilePath, f.ChangeType, f.OldPath); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert commit_file %s/%s: %w", f.CommitID, f.FilePath, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit commit_files tx: %w", err)
	}
	return nil
}

func insertRefs(db *sql.DB, refs []etlscan.GitRefSlim, repoID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin refs tx: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO refs (ref_name, ref_type, commit_id, is_default) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare refs insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range refs {
		r := &refs[i]
		if r.RepoID != repoID {
			continue
		}
		isDefault := 0
		if r.IsDefault {
			isDefault = 1
		}
		if _, err := stmt.Exec(r.RefName, r.RefType, r.CommitID, isDefault); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert ref %s: %w", r.RefName, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit refs tx: %w", err)
	}
	return nil
}
