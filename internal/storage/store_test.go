package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenAppliesMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	version, err := store.CurrentMigration(ctx)
	if err != nil {
		t.Fatalf("CurrentMigration() error = %v", err)
	}
	if version != 2 {
		t.Fatalf("migration version = %d, want 2", version)
	}

	for _, table := range []string{
		"sessions",
		"turns",
		"messages",
		"parent_runs",
		"subagent_conversations",
		"subagent_runs",
		"subagent_messages",
		"session_mailbox",
		"session_streams",
		"session_events",
	} {
		var count int
		err := store.db.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("inspect table %q: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %q count = %d, want 1", table, count)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kit.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close()

	var applied int
	if err := second.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM schema_migrations",
	).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 2 {
		t.Fatalf("migration rows = %d, want 2", applied)
	}
}

func TestOpenRejectsReservedPathCharactersAsDSNSyntax(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "kit#literal.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database was not created at literal path %q: %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "kit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SQLite interpreted path as a DSN and created %q: %v", filepath.Join(directory, "kit"), err)
	}
}

func TestSQLiteDSNEscapesURIControlCharacters(t *testing.T) {
	t.Parallel()

	dsn := sqliteDSN(filepath.Join(t.TempDir(), "kit?mode=memory#fragment.db"))
	if !strings.Contains(dsn, "%3F") || !strings.Contains(dsn, "%23") {
		t.Fatalf("sqliteDSN() = %q, want escaped question mark and fragment", dsn)
	}
}

func TestOpenRejectsUnknownFutureMigration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO schema_migrations(version, name, checksum, applied_at)
		VALUES (999, 'future', 'future-checksum', '2026-01-01T00:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert future migration: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if reopened, err := Open(ctx, path); err == nil {
		reopened.Close()
		t.Fatal("Open() accepted a database with a future migration")
	}
}

func TestOpenRejectsMigrationChecksumDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kit.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE schema_migrations SET checksum = 'changed' WHERE version = 1"); err != nil {
		t.Fatalf("change checksum: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if reopened, err := Open(ctx, path); err == nil {
		reopened.Close()
		t.Fatal("Open() accepted modified migration history")
	}
}

func TestMigrationFailureRollsBackOnlyFailingVersion(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	firstSQL := "CREATE TABLE first_table (id INTEGER PRIMARY KEY);"
	broken := fstest.MapFS{
		"migrations/0001_first.sql":  &fstest.MapFile{Data: []byte(firstSQL)},
		"migrations/0002_broken.sql": &fstest.MapFile{Data: []byte("CREATE TABLE rolled_back (id INTEGER); THIS IS INVALID;")},
	}
	if err := migrateFS(ctx, db, broken); err == nil {
		t.Fatal("migrateFS() accepted invalid SQL")
	}

	assertTableCount(t, db, "first_table", 1)
	assertTableCount(t, db, "rolled_back", 0)
	var applied int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied migration count = %d, want 1", applied)
	}

	fixed := fstest.MapFS{
		"migrations/0001_first.sql": &fstest.MapFile{Data: []byte(firstSQL)},
		"migrations/0002_fixed.sql": &fstest.MapFile{Data: []byte("CREATE TABLE second_table (id INTEGER PRIMARY KEY);")},
	}
	if err := migrateFS(ctx, db, fixed); err != nil {
		t.Fatalf("migrateFS() after correction error = %v", err)
	}
	assertTableCount(t, db, "second_table", 1)
}

func TestOpenEnforcesForeignKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO turns(id, session_id, sequence, status, created_at)
		VALUES ('turn-1', 'missing-session', 0, 'pending', '2026-01-01T00:00:00Z')
	`)
	if err == nil {
		t.Fatal("insert with missing session succeeded; foreign keys are disabled")
	}
}

func TestForeignKeysSurviveCanceledConnection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)

	cancelContext, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	var result int
	err = store.db.QueryRowContext(cancelContext, `
		WITH RECURSIVE counter(value) AS (
			VALUES(0)
			UNION ALL
			SELECT value + 1 FROM counter WHERE value < 100000000
		)
		SELECT SUM(value) FROM counter
	`).Scan(&result)
	if err == nil {
		t.Fatal("long query unexpectedly completed before cancellation")
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO turns(id, session_id, sequence, status, created_at)
		VALUES ('turn-after-cancel', 'missing-session', 0, 'pending', '2026-01-01T00:00:00Z')
	`)
	if err == nil {
		t.Fatal("foreign keys were disabled on a replacement connection")
	}
}

func TestSchemaRejectsSessionLineageCycles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO sessions(id, cwd, parent_session_id, created_at, updated_at)
		VALUES ('self', '/tmp', 'self', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
	`)
	if err == nil {
		t.Fatal("session accepted itself as parent")
	}
	for _, id := range []string{"session-a", "session-b"} {
		_, err := store.db.ExecContext(ctx, `
			INSERT INTO sessions(id, cwd, created_at, updated_at)
			VALUES (?, '/tmp', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
		`, id)
		if err != nil {
			t.Fatalf("insert session %q: %v", id, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE sessions SET parent_session_id = 'session-b' WHERE id = 'session-a'"); err != nil {
		t.Fatalf("create acyclic parent relation: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE sessions SET parent_session_id = 'session-a' WHERE id = 'session-b'"); err == nil {
		t.Fatal("session lineage accepted a two-node update cycle")
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO sessions(id, cwd, parent_session_id, created_at, updated_at) VALUES
		('bulk-a', '/tmp', 'bulk-b', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		('bulk-b', '/tmp', 'bulk-a', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
	`)
	if err == nil {
		t.Fatal("session lineage accepted a two-node bulk-insert cycle")
	}
}

func TestSchemaRejectsCrossSessionRelationships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	for _, id := range []string{"session-a", "session-b"} {
		_, err := store.db.ExecContext(ctx, `
			INSERT INTO sessions(id, cwd, created_at, updated_at)
			VALUES (?, '/tmp', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
		`, id)
		if err != nil {
			t.Fatalf("insert session %q: %v", id, err)
		}
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO turns(id, session_id, sequence, status, created_at)
		VALUES ('turn-a', 'session-a', 0, 'pending', '2026-01-01T00:00:00Z')
	`)
	if err != nil {
		t.Fatalf("insert turn: %v", err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO messages(id, session_id, turn_id, sequence, role, payload_json, created_at)
		VALUES ('message-b', 'session-b', 'turn-a', 0, 'user', '{}', '2026-01-01T00:00:00Z')
	`)
	if err == nil {
		t.Fatal("message in session-b referenced a turn in session-a")
	}
}

func TestDeletingSessionCascadesOwnedRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	statements := []string{
		`INSERT INTO sessions(id, cwd, created_at, updated_at) VALUES ('session', '/tmp', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO turns(id, session_id, sequence, status, created_at) VALUES ('turn', 'session', 0, 'completed', '2026-01-01T00:00:00Z')`,
		`INSERT INTO messages(id, session_id, turn_id, sequence, role, payload_json, created_at) VALUES ('message', 'session', 'turn', 0, 'user', '{}', '2026-01-01T00:00:00Z')`,
		`INSERT INTO parent_runs(id, session_id, turn_id, status, created_at) VALUES ('parent-run', 'session', 'turn', 'completed', '2026-01-01T00:00:00Z')`,
		`INSERT INTO subagent_conversations(id, parent_session_id, agent_name, status, last_activity_at, created_at) VALUES ('conversation', 'session', 'scout', 'completed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO subagent_runs(id, parent_session_id, conversation_id, parent_run_id, status, prompt_json, created_at) VALUES ('subagent-run', 'session', 'conversation', 'parent-run', 'completed', '{}', '2026-01-01T00:00:00Z')`,
		`INSERT INTO subagent_messages(id, conversation_id, run_id, sequence, role, payload_json, created_at) VALUES ('subagent-message', 'conversation', 'subagent-run', 0, 'assistant', '{}', '2026-01-01T00:00:00Z')`,
		`INSERT INTO session_mailbox(id, parent_session_id, subagent_run_id, kind, payload_json, created_at) VALUES ('mail', 'session', 'subagent-run', 'subagent.completed', '{}', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed owned record with %q: %v", statement, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = 'session'"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	for _, table := range []string{"sessions", "turns", "messages", "parent_runs", "subagent_conversations", "subagent_runs", "subagent_messages", "session_mailbox"} {
		var count int
		if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s retained %d rows after session deletion", table, count)
		}
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	if strings.ContainsAny(table, "'\"") {
		t.Fatalf("unsafe test table name %q", table)
	}
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		table,
	).Scan(&count)
	if err != nil {
		t.Fatalf("inspect table %q: %v", table, err)
	}
	if count != want {
		t.Fatalf("table %q count = %d, want %d", table, count, want)
	}
}
