package droids

// OpenAI Codex is served through ChatGPT rather than the public OpenAI API.
// Its model availability is account-dependent and is not represented by the
// models.dev OpenAI API catalog, so this deliberately reviewed snapshot is not
// changed by Providers.RefreshModels.

const defaultOpenAICodexBaseURL = "https://chatgpt.com/backend-api"

var builtinOpenAICodexModels = []Model{
	{
		ID: "gpt-5.3-codex-spark", Name: "GPT-5.3 Codex Spark",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh"},
		Input: []string{"text"}, ContextWindow: 128_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.4", Name: "GPT-5.4",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.4-mini", Name: "GPT-5.4 mini",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.5", Name: "GPT-5.5",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
	{
		ID: "gpt-6-astra", Name: "GPT-6 Astra",
		Provider: "openai-codex", API: ModelAPIOpenAICodexResponses, BaseURL: defaultOpenAICodexBaseURL,
		Reasoning: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"},
		Input: []string{"text", "image"}, ContextWindow: 272_000, MaxOutputTokens: 128_000, OutputLimitMode: OutputLimitProviderControlled,
	},
}

// OpenAICodexModels returns a fresh copy of the reviewed OpenAI Codex model
// catalog. Availability still depends on the authenticated ChatGPT account.
func OpenAICodexModels() []Model {
	models := make([]Model, len(builtinOpenAICodexModels))
	for i, model := range builtinOpenAICodexModels {
		models[i] = cloneModel(model)
	}
	return models
}

// OpenAICodexModel returns one model from the reviewed OpenAI Codex catalog.
func OpenAICodexModel(id string) (Model, bool) {
	for _, model := range builtinOpenAICodexModels {
		if model.ID == id {
			return cloneModel(model), true
		}
	}
	return Model{}, false
}
