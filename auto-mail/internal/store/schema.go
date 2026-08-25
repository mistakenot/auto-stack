package store

import (
	"context"
	"fmt"
)

// schemaVersion is the only schema this alpha store ever has. G10 makes the
// store disposable — no upcasters, no migrations, `auto mail reset` is a
// supported operation — so the version exists to record shape, not to migrate.
const schemaVersion = 1

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
	`CREATE TABLE IF NOT EXISTS mail (
		id TEXT PRIMARY KEY,
		to_address TEXT NOT NULL,
		envelope TEXT NOT NULL,
		body TEXT NOT NULL,
		sent_at DATETIME NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_mail_to_address_id ON mail (to_address, id);`,
	`CREATE TABLE IF NOT EXISTS subscriptions (
		id TEXT PRIMARY KEY,
		address TEXT NOT NULL,
		name TEXT,
		from_cursor TEXT NOT NULL,
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
func (s *Store) Migrate(ctx context.Context) error {
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
