// Package storage owns Kit's SQLite database and schema migrations.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/akonwi/kit/internal/securefs"
	"modernc.org/sqlite"
)

// Store is the daemon-owned SQLite store.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens a SQLite database, configures it for Kit, and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := securefs.MakePrivateDir(filepath.Dir(absolutePath)); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// Protect the main file before SQLite can create WAL sidecars. The containing
	// Kit home is also private, so sidecars cannot be traversed by other users.
	file, err := os.OpenFile(absolutePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	if err := securefs.ProtectFile(absolutePath); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("protect database: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close database before opening: %w", err)
	}

	dsn := sqliteDSN(absolutePath)
	connector, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("configure database connector: %w", err)
	}
	db := sql.OpenDB(connector)
	// The daemon remains the only process owner. A small pool allows independent
	// session reads while SQLite WAL mode serializes short write transactions.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	closeOnError := func(openErr error) (*Store, error) {
		_ = db.Close()
		return nil, openErr
	}
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping database: %w", err))
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return closeOnError(fmt.Errorf("enable database WAL mode: %w", err))
	}
	var foreignKeys int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return closeOnError(fmt.Errorf("verify foreign key enforcement: %w", err))
	}
	if foreignKeys != 1 {
		return closeOnError(fmt.Errorf("foreign key enforcement is disabled"))
	}
	if err := migrate(ctx, db); err != nil {
		return closeOnError(err)
	}
	return &Store{db: db, path: absolutePath}, nil
}

func sqliteDSN(path string) string {
	slashPath := filepath.ToSlash(path)
	uri := &url.URL{Scheme: "file", Path: slashPath}
	query := uri.Query()
	// _pragma is applied whenever database/sql opens a replacement physical
	// connection, including after cancellation causes a connection to be dropped.
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

// DroidStoreDirectory returns the sibling directory for per-session droid databases.
func (s *Store) DroidStoreDirectory() string {
	if s == nil || s.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.path), "droids")
}

// Close closes the SQLite database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CurrentMigration returns the latest applied schema migration.
func (s *Store) CurrentMigration(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("store is closed")
	}
	var version int
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&version); err != nil {
		return 0, fmt.Errorf("read current migration: %w", err)
	}
	return version, nil
}

// Ready verifies that the database connection remains usable.
func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is closed")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database is not ready: %w", err)
	}
	return nil
}
