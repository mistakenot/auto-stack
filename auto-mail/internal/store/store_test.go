package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-mail/internal/store"
)

// TestOpenMigrateIdempotent: opening a fresh store creates its parent
// directory, and migrating twice is a no-op — the alpha store has exactly one
// schema and `CREATE ... IF NOT EXISTS` must stay re-runnable (G10).
func TestOpenMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail", "alpha-store.db")

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	for i := range 2 {
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migrate #%d: %v", i+1, err)
		}
	}

	if got := st.Path(); got != path {
		t.Errorf("Path() = %q, want %q", got, path)
	}

	// Every table the final shape declares must exist after migration, so
	// phase 2 fills write paths rather than adding a migration.
	want := []string{"schema_migrations", "events", "mail", "subscriptions", "deliveries", "bindings"}
	rows, err := st.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, table := range want {
		if !have[table] {
			t.Errorf("table %q missing after migrate; have %v", table, have)
		}
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestWithTxRollsBackOnError: a failing operation leaves no partial state, so
// the append-and-project pair phase 2 adds is genuinely atomic (D-1).
func TestWithTxRollsBackOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha-store.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	boom := errors.New("boom")
	err = st.WithTx(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx,
			`INSERT INTO events (id, type, version, occurred_at, payload) VALUES (?, ?, ?, ?, ?)`,
			"01TEST", "alpha.mail.sent", 1, "2026-08-25T10:14:02Z", "{}"); execErr != nil {
			return execErr
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx error = %v, want %v", err, boom)
	}

	var count int
	row := st.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Errorf("events after rollback = %d, want 0", count)
	}
}
