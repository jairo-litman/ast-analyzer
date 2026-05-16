// Package store persists a graph.Project to a SQLite database so
// downstream stages can skip re-parsing the codebase. Source text
// isn't persisted; consumers re-derive it from byte ranges.
package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Store is a handle on a SQLite-backed project graph.
type Store struct {
	db *sql.DB
}

// Open opens (and bootstraps) the SQLite database at path. Pass
// ":memory:" for an ephemeral in-process DB.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}

	// FK enforcement is opt-in per connection in SQLite, and the
	// `files` cascade depends on it.
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("bootstrap schema: %w", err)
	}

	// Additive migration for pre-type_refs indexes. SQLite has no
	// IF NOT EXISTS for ADD COLUMN; the duplicate-column error from a
	// fresh schema is the expected no-op.
	_, _ = db.Exec(`ALTER TABLE symbols ADD COLUMN type_refs TEXT NOT NULL DEFAULT ''`)

	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS project_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    path         TEXT    NOT NULL UNIQUE,
    content_hash TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS symbols (
    id                TEXT    PRIMARY KEY,
    kind              TEXT    NOT NULL,
    name              TEXT    NOT NULL,
    file_id           INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    start_byte        INTEGER NOT NULL,
    end_byte          INTEGER NOT NULL,
    body_start_byte   INTEGER NOT NULL DEFAULT 0,
    details           TEXT    NOT NULL DEFAULT '',
    is_default_export INTEGER NOT NULL DEFAULT 0,
    type_refs         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_symbols_name_file ON symbols(file_id, name);

CREATE TABLE IF NOT EXISTS calls (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    caller_id      TEXT    NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    callee         TEXT    NOT NULL,
    receiver       TEXT    NOT NULL DEFAULT '',
    expression     TEXT    NOT NULL,
    is_constructor INTEGER NOT NULL DEFAULT 0,
    file_id        INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    start_byte     INTEGER NOT NULL,
    end_byte       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_calls_caller ON calls(caller_id);

CREATE TABLE IF NOT EXISTS call_resolutions (
    call_id          INTEGER NOT NULL REFERENCES calls(id)    ON DELETE CASCADE,
    target_symbol_id TEXT    NOT NULL REFERENCES symbols(id)  ON DELETE CASCADE,
    PRIMARY KEY (call_id, target_symbol_id)
);

CREATE TABLE IF NOT EXISTS imports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL,
    resolved   TEXT    NOT NULL DEFAULT '',
    kind       TEXT    NOT NULL,
    namespace  TEXT    NOT NULL DEFAULT '',
    start_byte INTEGER NOT NULL DEFAULT 0,
    end_byte   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS import_identifiers (
    import_id    INTEGER NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
    local_name   TEXT    NOT NULL,
    remote_name  TEXT    NOT NULL,
    is_type_only INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS re_exports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL,
    resolved   TEXT    NOT NULL DEFAULT '',
    kind       TEXT    NOT NULL,
    namespace  TEXT    NOT NULL DEFAULT '',
    start_byte INTEGER NOT NULL DEFAULT 0,
    end_byte   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS re_export_bindings (
    re_export_id INTEGER NOT NULL REFERENCES re_exports(id) ON DELETE CASCADE,
    local_name   TEXT    NOT NULL,
    remote_name  TEXT    NOT NULL,
    is_type_only INTEGER NOT NULL DEFAULT 0
);
`
