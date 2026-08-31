// Package store owns the auto-mail event log and its projections. It is
// internal on purpose: nothing outside this module can import it, which makes
// G11 ("nothing outside the mail package reads the store") a compile-time
// property rather than a convention (D-062-8).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store is the single write connection this process holds on the mail store.
//
// Concurrency discipline (D-11): WAL journaling plus a 5s busy_timeout — the
// exact pragma set auto-watch/internal/store/store.go already uses — and one
// connection per process, with every write inside one BEGIN IMMEDIATE
// transaction. Immediate is requested through the driver's `_txlock` DSN
// parameter, so database/sql's own BeginTx opens the write lock up front
// instead of upgrading mid-transaction (which is what produces SQLITE_BUSY
// under concurrent writers).
type Store struct {
	db   *sql.DB
	path string
}

// Open creates the parent directory if needed and opens the store with the
// house pragma set.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create mail store directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open mail store: %w", err)
	}
	// One write connection per process. The pragmas below are per-connection,
	// so pinning the pool to a single connection is what makes them (and the
	// BEGIN IMMEDIATE discipline) deterministic.
	db.SetMaxOpenConns(1)
	// busy_timeout is set **first**, and the order is load-bearing. Switching a
	// database into WAL takes a brief exclusive lock, so two processes opening
	// one store at the same moment race for it — and with no timeout in force
	// yet, the loser gets SQLITE_BUSY immediately instead of waiting. D-11 makes
	// simultaneous opens the ordinary case, so the connection has to be willing
	// to wait before it asks for anything contended.
	for _, stmt := range []string{
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply sqlite pragma %q: %w", stmt, err)
		}
	}
	return &Store{db: db, path: path}, nil
}

// Path is the store file this handle is bound to.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close releases the store handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithTx runs fn inside one immediate transaction, committing on success and
// rolling back on any error. Every mail operation appends its event and updates
// its projections inside a single call (D-1).
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mail transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mail transaction: %w", err)
	}
	return nil
}

// QueryContext exposes read-only querying to the projection code in this
// package. Kept unexported to callers outside the module by the internal path.
func (s *Store) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRowContext exposes single-row reads to this package's projection code.
func (s *Store) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}
