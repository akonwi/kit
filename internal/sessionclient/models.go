package sessionclient

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
)

// ResolveAvailableModel selects an authoritative available model. An explicit
// unavailable selection fails; an implicit stale preference falls back within
// its provider before using the first available catalog entry.
func ResolveAvailableModel(catalog protocol.ModelCatalog, preferred string, explicit bool) (string, error) {
	for _, model := range catalog.Models {
		if model.ID == preferred && model.Available {
			return model.ID, nil
		}
	}
	if explicit {
		return "", fmt.Errorf("model %q is not available", preferred)
	}
	preferredProvider, _, _ := strings.Cut(preferred, "/")
	for _, model := range catalog.Models {
		if model.Provider == preferredProvider && model.Available {
			return model.ID, nil
		}
	}
	for _, model := range catalog.Models {
		if model.Available {
			return model.ID, nil
		}
	}
	return "", fmt.Errorf("no authenticated model is available")
}
