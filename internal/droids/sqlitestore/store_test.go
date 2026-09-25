package sqlitestore_test

import (
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/droidstest"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

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
