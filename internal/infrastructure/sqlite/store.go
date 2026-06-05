// Package sqlite is the embedded store for config/KB/prices/media-meta plus the
// webhook-dedup table (docs/03, docs/04). It uses a PURE-GO driver
// (modernc.org/sqlite) so the binary builds with CGO_ENABLED=0.
package sqlite

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"

	"github.com/yessaliyev/xpayment-crm/migrations"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite file at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection avoids SQLITE_BUSY under the brain's low concurrency.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for adapters that need it (e.g. dedup).
func (s *Store) DB() *sql.DB { return s.db }

// migrate applies every embedded *.sql file in lexical order. Files are written
// idempotently (CREATE TABLE IF NOT EXISTS / INSERT … WHERE NOT EXISTS), so
// re-running on an existing DB is safe without a versions table.
func (s *Store) migrate() error {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".sql" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := s.db.Exec(string(b)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}
