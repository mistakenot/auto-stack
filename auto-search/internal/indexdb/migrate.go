package indexdb

import (
	"database/sql"
	"fmt"
	"os"
)

// NeedsRebuild checks whether the existing index at path requires a full
// rebuild rather than an incremental update. Returns true when:
//   - the file does not exist
//   - the stored schema version differs from SchemaVersion
//   - the schema_info table is missing or empty
func NeedsRebuild(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return true, nil // can't open → rebuild
	}
	defer func() { _ = db.Close() }()

	if err := configurePragmas(db); err != nil {
		return true, nil
	}

	version, err := ReadSchemaVersion(db)
	if err != nil {
		return true, nil
	}
	return version != SchemaVersion, nil
}

// OpenOrCreate opens the index at path if it exists and the schema is current.
// If the database is missing or has a stale schema version, it performs a full
// rebuild by creating a fresh database. Returns the open *sql.DB and a boolean
// indicating whether a full rebuild was performed.
func OpenOrCreate(path string) (*sql.DB, bool, error) {
	rebuild, err := NeedsRebuild(path)
	if err != nil {
		return nil, false, err
	}
	if rebuild {
		db, err := Create(path)
		if err != nil {
			return nil, false, err
		}
		return db, true, nil
	}
	db, err := Open(path)
	if err != nil {
		return nil, false, err
	}
	return db, false, nil
}
