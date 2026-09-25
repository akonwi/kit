package server

import (
	"fmt"
	"slices"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

// resolveSubagentModel permits bare frontmatter model IDs to choose the first
// available match in registration order without changing global model resolution.
func resolveSubagentModel(providers droids.Providers, selector string, available []string) (droids.Model, error) {
	if !strings.Contains(selector, "/") {
		for _, providerID := range droids.ProviderIDs(providers) {
			if !slices.Contains(available, providerID) {
				continue
			}
			model, err := providers.Resolve(providerID + "/" + selector)
			if err == nil {
				return model, nil
			}
		}
		return droids.Model{}, fmt.Errorf("no available provider supports model %q", selector)
	}
	model, err := providers.Resolve(selector)
	if err != nil {
		return droids.Model{}, err
	}
	if !slices.Contains(available, model.Provider) {
		return droids.Model{}, fmt.Errorf("provider %q is unavailable", model.Provider)
	}
	return model, nil
}
