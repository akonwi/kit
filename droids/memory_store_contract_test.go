package droids_test

import (
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/droidstest"
)

func TestMemoryStoreContract(t *testing.T) {
	droidstest.RunStoreContract(t, func(*testing.T) droids.Store {
		return droids.NewMemoryStore()
	})
}
