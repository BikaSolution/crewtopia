// Package db opens the single SQLite file the app uses and applies the
// embedded schema on startup. No migration framework: schema.sql is written
// with idempotent CREATE TABLE IF NOT EXISTS statements.
package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// Open opens (creating if necessary) the SQLite file at path and applies the
// embedded schema.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite allows only one writer at a time; at this app's scale a single
	// shared connection avoids "database is locked" errors entirely rather
	// than needing retry/backoff logic.
	sqlDB.SetMaxOpenConns(1)

	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	if _, err := sqlDB.Exec(string(schema)); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return sqlDB, nil
}
