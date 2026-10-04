package sqlitestore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/droidstest"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func TestReadOnlyOpenRequiresExistingStoreAndPreservesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "droid.sqlite")
	if _, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path, ReadOnly: true}); err == nil {
		t.Fatal("read-only open created a missing store")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing read-only store was created: %v", err)
	}

	store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(t.Context(), droids.OpenConversation{ID: "conversation_read_only"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.State(t.Context())
	if err != nil || state.ID != "conversation_read_only" {
		t.Fatalf("read-only state = %#v, %v", state, err)
	}
}

func TestStoreContract(t *testing.T) {
	droidstest.RunStoreContract(t, func(t *testing.T) droids.Store {
		store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{
			Path: filepath.Join(t.TempDir(), "droid.sqlite"),
		})
		if err != nil {
			t.Fatalf("Open sqlite store: %v", err)
		}
		t.Cleanup(func() {
			if err := store.Close(); err != nil {
				t.Errorf("Close sqlite store: %v", err)
			}
		})
		return store
	})
}
