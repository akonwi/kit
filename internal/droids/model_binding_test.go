package droids

func resolvedTestModel(providers Providers, selector string) Model {
	model, err := providers.Resolve(selector)
	if err != nil {
		panic(err)
	}
	return model
}
