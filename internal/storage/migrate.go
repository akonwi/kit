package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

type appliedMigration struct {
	version  int
	name     string
	checksum string
}

func migrate(ctx context.Context, db *sql.DB) error {
	return migrateFS(ctx, db, migrationFiles)
}

func migrateFS(ctx context.Context, db *sql.DB, files fs.FS) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	migrations, err := loadMigrations(files)
	if err != nil {
		return err
	}
	applied, err := loadAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	if len(applied) > len(migrations) {
		return fmt.Errorf(
			"database schema version %d is newer than this Kit build (latest known version %d)",
			applied[len(applied)-1].version,
			migrations[len(migrations)-1].version,
		)
	}
	for index, record := range applied {
		expected := migrations[index]
		if record.version != expected.version {
			return fmt.Errorf(
				"migration history is not a known prefix: found version %d at position %d, expected %d",
				record.version,
				index+1,
				expected.version,
			)
		}
		if record.name != expected.name {
			return fmt.Errorf(
				"migration %d was applied as %q, current name is %q",
				record.version,
				record.name,
				expected.name,
			)
		}
		if record.checksum != expected.checksum {
			return fmt.Errorf("migration %d (%s) checksum does not match this Kit build", record.version, record.name)
		}
	}

	for _, next := range migrations[len(applied):] {
		if err := applyMigration(ctx, db, next); err != nil {
			return err
		}
	}
	return nil
}

func loadAppliedMigrations(ctx context.Context, db *sql.DB) ([]appliedMigration, error) {
	rows, err := db.QueryContext(
		ctx,
		"SELECT version, name, checksum FROM schema_migrations ORDER BY version",
	)
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	defer rows.Close()

	var applied []appliedMigration
	for rows.Next() {
		var record appliedMigration
		if err := rows.Scan(&record.version, &record.name, &record.checksum); err != nil {
			return nil, fmt.Errorf("decode migration history: %w", err)
		}
		applied = append(applied, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, next migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", next.version, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, next.sql); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", next.version, next.name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations(version, name, checksum, applied_at)
		 VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		next.version,
		next.name,
		next.checksum,
	); err != nil {
		return fmt.Errorf("record migration %d: %w", next.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", next.version, err)
	}
	return nil
}

func loadMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	migrations := make([]migration, 0, len(entries))
	lastVersion := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration filename %q must begin with <version>_", entry.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration filename %q has invalid version", entry.Name())
		}
		if version <= lastVersion {
			return nil, fmt.Errorf("migration version %d is duplicated or out of order", version)
		}
		body, err := fs.ReadFile(files, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(body)
		migrations = append(migrations, migration{
			version:  version,
			name:     strings.TrimSuffix(parts[1], ".sql"),
			checksum: hex.EncodeToString(digest[:]),
			sql:      string(body),
		})
		lastVersion = version
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no embedded migrations")
	}
	return migrations, nil
}
