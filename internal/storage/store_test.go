package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
)

func TestInitialSchemaContainsOnlySessionRegistry(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rows, err := store.db.QueryContext(t.Context(), `
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	want := []string{"schema_migrations", "sessions"}
	if len(tables) != len(want) || tables[0] != want[0] || tables[1] != want[1] {
		t.Fatalf("tables = %v, want %v", tables, want)
	}
	for _, obsolete := range []string{"turns", "parent_runs", "messages", "session_streams", "session_events"} {
		var name string
		err := store.db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, obsolete).Scan(&name)
		if err != sql.ErrNoRows {
			t.Fatalf("table %q exists: name=%q err=%v", obsolete, name, err)
		}
	}
}

func TestSessionRegistryTracksDroidInitialization(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	record, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_test", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.DroidInitializedAt != nil {
		t.Fatalf("new session initialized at %v", record.DroidInitializedAt)
	}
	now := time.Now().UTC()
	if err := store.MarkDroidInitialized(t.Context(), record.ID, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetSession(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DroidInitializedAt == nil {
		t.Fatal("droid initialization marker was not persisted")
	}
}
