package droids_test

import "github.com/akonwi/kit/internal/droids"

func resolvedTestModel(providers droids.Providers, selector string) droids.Model {
	model, err := providers.Resolve(selector)
	if err != nil {
		panic(err)
	}
	return model
}
