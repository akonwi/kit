package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/akonwi/kit/internal/session"
)

func TestSessionConfigurationUpdateIsAtomicRevisionGuardedAndMonotonic(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_config", ScratchpadOwnerID: "session_config", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "small", ThinkingLevel: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ConfigurationRevision != 1 {
		t.Fatalf("initial configuration revision = %d, want 1", created.ConfigurationRevision)
	}
	futureActivity := time.Now().UTC().Add(time.Hour)
	if err := store.TouchSession(t.Context(), created.ID, futureActivity); err != nil {
		t.Fatal(err)
	}

	update := session.ConfigurationUpdate{
		SessionID: created.ID, ExpectedRevision: 1,
		ModelProvider: "test", ModelID: "large", ThinkingLevel: "high",
	}
	configured, err := store.UpdateSessionConfiguration(t.Context(), update)
	if err != nil {
		t.Fatal(err)
	}
	if configured.ModelProvider != "test" || configured.ModelID != "large" || configured.ThinkingLevel != "high" ||
		configured.ConfigurationRevision != 2 || configured.UpdatedAt.Before(futureActivity) {
		t.Fatalf("configured session = %+v", configured)
	}
	persisted, err := store.GetSession(t.Context(), created.ID)
	if err != nil || persisted != configured {
		t.Fatalf("persisted session = %+v, %v; want %+v", persisted, err, configured)
	}

	stale := update
	stale.ModelID = "other"
	if _, err := store.UpdateSessionConfiguration(t.Context(), stale); !errors.Is(err, session.ErrConfigurationConflict) {
		t.Fatalf("stale configuration error = %v", err)
	} else {
		var conflict *session.ConfigurationConflictError
		if !errors.As(err, &conflict) || conflict.Expected != 1 || conflict.Actual != 2 {
			t.Fatalf("configuration conflict = %+v", conflict)
		}
	}
	unchangedAfterConflict, err := store.GetSession(t.Context(), created.ID)
	if err != nil || unchangedAfterConflict != configured {
		t.Fatalf("stale update changed session = %+v, %v", unchangedAfterConflict, err)
	}

	noChange := session.ConfigurationUpdate{
		SessionID: created.ID, ExpectedRevision: 2,
		ModelProvider: "test", ModelID: "large", ThinkingLevel: "high",
	}
	unchanged, err := store.UpdateSessionConfiguration(t.Context(), noChange)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ConfigurationRevision != 2 || unchanged.UpdatedAt.Before(configured.UpdatedAt) {
		t.Fatalf("no-op configuration = %+v", unchanged)
	}
	next := noChange
	next.ModelID = "largest"
	nextConfigured, err := store.UpdateSessionConfiguration(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	if nextConfigured.ConfigurationRevision != 3 || nextConfigured.ModelID != "largest" {
		t.Fatalf("configuration after no-op = %+v", nextConfigured)
	}

	missing := update
	missing.SessionID = "session_missing"
	if _, err := store.UpdateSessionConfiguration(t.Context(), missing); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing configuration error = %v", err)
	}
}

func TestAmbiguousConfigurationResponseResynchronizesFromRegistry(t *testing.T) {
	t.Parallel()

	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_config_resync", ScratchpadOwnerID: "session_config_resync", CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "small", ThinkingLevel: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Treat the successful return as if the transport lost it after commit.
	_, err = store.UpdateSessionConfiguration(t.Context(), session.ConfigurationUpdate{
		SessionID: created.ID, ExpectedRevision: 1,
		ModelProvider: "test", ModelID: "large", ThinkingLevel: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	resynchronized, err := store.GetSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resynchronized.ConfigurationRevision != 2 || resynchronized.ModelID != "large" || resynchronized.ThinkingLevel != "high" {
		t.Fatalf("resynchronized session = %+v", resynchronized)
	}
}

func TestConfigurationRevisionMigrationUpgradesExistingSessionsAtRevisionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.db")
	legacyDB, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	prefix := fstest.MapFS{}
	for _, name := range []string{"0001_initial.sql", "0002_session_cwd_mutations.sql"} {
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		prefix["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	if err := migrateFS(t.Context(), legacyDB, prefix); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	now := formatTimestamp(time.Now())
	if _, err := legacyDB.ExecContext(t.Context(), `
		INSERT INTO sessions(id, cwd, persistent, model_provider, model_id, thinking_level, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?)
	`, "session_legacy_config", t.TempDir(), "test", "legacy", "medium", now, now); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if _, err := legacyDB.ExecContext(t.Context(), `
		INSERT INTO sessions(id, cwd, persistent, parent_session_id, model_provider, model_id, thinking_level, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?)
	`, "session_legacy_child", t.TempDir(), "session_legacy_config", "test", "legacy", "medium", now, now); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	version, err := upgraded.CurrentMigration(t.Context())
	if err != nil || version != 13 {
		t.Fatalf("migration version = %d, %v; want 13", version, err)
	}
	record, err := upgraded.GetSession(t.Context(), "session_legacy_config")
	if err != nil {
		t.Fatal(err)
	}
	if record.ConfigurationRevision != 1 || record.ModelID != "legacy" || record.ThinkingLevel != "medium" || record.ScratchpadOwnerID != record.ID {
		t.Fatalf("upgraded session = %+v", record)
	}
	child, err := upgraded.GetSession(t.Context(), "session_legacy_child")
	if err != nil || child.ScratchpadOwnerID != child.ID {
		t.Fatalf("upgraded child = %+v, %v", child, err)
	}
	scratch, err := upgraded.Get(t.Context(), record.ID)
	if err != nil || scratch.OwnerSessionID != record.ID || scratch.Revision != 1 || scratch.Content != "" {
		t.Fatalf("upgraded scratchpad = %+v, %v", scratch, err)
	}
}

func TestScratchpadMigrationRejectsLegacyTemporaryRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.db")
	legacyDB, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	prefix := fstest.MapFS{}
	body, err := migrationFiles.ReadFile("migrations/0001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	prefix["migrations/0001_initial.sql"] = &fstest.MapFile{Data: body}
	if err := migrateFS(t.Context(), legacyDB, prefix); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	now := formatTimestamp(time.Now())
	if _, err := legacyDB.ExecContext(t.Context(), `
		INSERT INTO sessions(id, cwd, persistent, model_provider, model_id, created_at, updated_at)
		VALUES (?, ?, 0, ?, ?, ?, ?)
	`, "session_legacy_temporary", t.TempDir(), "test", "model", now, now); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(t.Context(), path); err == nil {
		_ = store.Close()
		t.Fatal("migration accepted a durable temporary session")
	}
}

func TestConfigurationUpdateRejectsInvalidBoundaryValues(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, update := range []session.ConfigurationUpdate{
		{},
		{SessionID: "session", ExpectedRevision: 0, ModelProvider: "test", ModelID: "model"},
		{SessionID: "session", ExpectedRevision: 1, ModelProvider: "test", ModelID: string([]byte{'m', 0})},
	} {
		if _, err := store.UpdateSessionConfiguration(t.Context(), update); err == nil {
			t.Fatalf("accepted invalid update: %+v", update)
		}
	}
}
