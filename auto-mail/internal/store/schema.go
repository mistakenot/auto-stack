package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// schemaVersion is the shape this alpha store expects. G10 makes the store
// disposable — no upcasters, no migrations, `auto mail reset` is a supported
// operation — so the version exists to *detect* a different shape and say so,
// not to migrate one into another.
const schemaVersion = 2

// ErrSchemaMismatch means the store on disk was written by a different alpha
// schema. There is no migration path by design (G10); the remediation is
// `auto mail reset --yes`, which is why this is a typed error a command can
// branch on rather than a raw SQL failure at the first insert.
var ErrSchemaMismatch = errors.New("the alpha mail store has a different schema")

// statements is the full table shape for the walking skeleton. The write paths
// arrive with the projection (phase 2), but the tables are created in their
// final shape now so no migration is needed inside this task.
//
// `events` is the authority (D-1); `mail` is an immutable projection;
// `deliveries` and `bindings` are mutable projections. Consumer state
// (read_at / acked_at) lives only on `deliveries`, keyed by
// (subscription_id, mail_id) — never on the mail row (G2).
var statements = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	);`,
	`CREATE TABLE IF NOT EXISTS events (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		id TEXT NOT NULL UNIQUE,
		type TEXT NOT NULL,
		version INTEGER NOT NULL,
		occurred_at DATETIME NOT NULL,
		payload TEXT NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_events_type_seq ON events (type, seq);`,
	// G1, enforced by the database rather than by this package remembering it:
	// the log is append-only, so a state change is a new event, never a rewrite
	// of an old one. Append() is the only writer of this table; these triggers
	// are what make that true for anything holding a handle on the file.
	`CREATE TRIGGER IF NOT EXISTS events_no_update
		BEFORE UPDATE ON events
		BEGIN
			SELECT RAISE(ABORT, 'G1: the mail event log is append-only — append a new event instead of updating one');
		END;`,
	`CREATE TRIGGER IF NOT EXISTS events_no_delete
		BEFORE DELETE ON events
		BEGIN
			SELECT RAISE(ABORT, 'G1: the mail event log is append-only — events are never deleted; ` + "`auto mail reset`" + ` wipes the whole alpha store');
		END;`,
	// `seq` is the store's own ordering of mail: the `seq` of the row's
	// `alpha.mail.sent` event, assigned by SQLite inside the same transaction.
	// It is what every cursor comparison uses, and it is deliberately *not* the
	// id: the id is minted by the sending process, and D-11 makes every send a
	// separate process against one file, so two ids drawn in the same
	// millisecond by different processes can sort either way. Only the log can
	// decide "after this point".
	`CREATE TABLE IF NOT EXISTS mail (
		id TEXT PRIMARY KEY,
		seq INTEGER NOT NULL UNIQUE,
		to_address TEXT NOT NULL,
		envelope TEXT NOT NULL,
		body TEXT NOT NULL,
		sent_at DATETIME NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_mail_to_address_seq ON mail (to_address, seq);`,
	// A mail row is an immutable projection of its alpha.mail.sent event (G1).
	// Consumer state — read_at, acked_at — lives on `deliveries`, keyed by
	// (subscription_id, mail_id), never on the mail row (G2), so nothing ever
	// needs to update one.
	`CREATE TRIGGER IF NOT EXISTS mail_no_update
		BEFORE UPDATE ON mail
		BEGIN
			SELECT RAISE(ABORT, 'G1: a mail row is immutable — consumer state belongs on deliveries, keyed by (subscription_id, mail_id)');
		END;`,
	`CREATE TRIGGER IF NOT EXISTS mail_no_delete
		BEFORE DELETE ON mail
		BEGIN
			SELECT RAISE(ABORT, 'G1: a mail row is immutable — it is never deleted; ` + "`auto mail reset`" + ` wipes the whole alpha store');
		END;`,
	// `from_cursor` is a mail `seq`, not a mail id: 0 means "from the beginning
	// of the log", and a --from-now cursor is the store's own high-water mark
	// read under the same write lock that stores it (D-10). `seq` orders the
	// subscriptions themselves for the same reason mail carries one — "the
	// first subscription this agent created" has to be the log's answer, not a
	// comparison of two process-minted ids.
	`CREATE TABLE IF NOT EXISTS subscriptions (
		id TEXT PRIMARY KEY,
		seq INTEGER NOT NULL UNIQUE,
		address TEXT NOT NULL,
		name TEXT,
		from_cursor INTEGER NOT NULL,
		created_at DATETIME NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_subscriptions_address ON subscriptions (address);`,
	`CREATE TABLE IF NOT EXISTS deliveries (
		subscription_id TEXT NOT NULL REFERENCES subscriptions (id),
		mail_id TEXT NOT NULL REFERENCES mail (id),
		first_seen_at DATETIME NOT NULL,
		read_at DATETIME,
		acked_at DATETIME,
		acked_by TEXT,
		PRIMARY KEY (subscription_id, mail_id)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_unacked
		ON deliveries (subscription_id, acked_at);`,
	`CREATE TABLE IF NOT EXISTS bindings (
		subscription_id TEXT PRIMARY KEY REFERENCES subscriptions (id),
		manager TEXT NOT NULL,
		target TEXT NOT NULL,
		session TEXT,
		last_seen DATETIME NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_bindings_pair ON bindings (manager, target);`,
}

// Migrate creates the schema. It is idempotent: every statement is
// CREATE ... IF NOT EXISTS, so opening an existing store is a no-op.
//
// A store written by a *different* alpha schema is refused up front with
// ErrSchemaMismatch. `CREATE TABLE IF NOT EXISTS` would otherwise leave the old
// tables untouched and the mismatch would surface as an unexplained SQL error
// at the first insert — the version row exists precisely so it surfaces here,
// with a remediation, instead.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.checkSchemaVersion(ctx); err != nil {
		return err
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply mail schema statement: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations (version) VALUES (?);`, schemaVersion); err != nil {
		return fmt.Errorf("record mail schema version: %w", err)
	}
	return nil
}

// checkSchemaVersion reports a store written by a different alpha schema. A
// store with no schema_migrations table at all is a fresh file, not a mismatch.
func (s *Store) checkSchemaVersion(ctx context.Context) error {
	var found int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&found); err != nil {
		return fmt.Errorf("inspect the mail store: %w", err)
	}
	if found == 0 {
		return nil
	}
	// COALESCE, not a bare MAX: an aggregate over an empty table yields one row
	// holding NULL, and 0 is the same answer as "nothing recorded yet".
	var version int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read the recorded mail schema version: %w", err)
	}
	if version != 0 && version != schemaVersion {
		return fmt.Errorf("%w: it is version %d, this build expects version %d",
			ErrSchemaMismatch, version, schemaVersion)
	}
	return nil
}

// RecordSchemaVersion replaces the store's recorded schema version.
//
// The guard above is only worth having if something covers it, and covering it
// means producing a store this build refuses. The alternatives are to open the
// mail store from a package that G11 does not allow to, or to keep a binary
// fixture of a schema nobody maintains. This is the third option: the one
// package that may open the store offers the one write the guard reads. It is
// unimportable outside this module, so it can only ever be reached by
// auto-mail's own tests.
func RecordSchemaVersion(path string, version int) error {
	st, err := Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	return st.WithTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `DELETE FROM schema_migrations`); err != nil {
			return fmt.Errorf("clear the recorded schema version: %w", err)
		}
		if _, err := tx.ExecContext(context.Background(),
			`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return fmt.Errorf("record schema version %d: %w", version, err)
		}
		return nil
	})
}
