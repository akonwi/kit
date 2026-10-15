package contract

import "testing"

func TestServerSettingsValidation(t *testing.T) {
	t.Parallel()
	valid := func() ServerSettings {
		return ServerSettings{
			DefaultModel:         "openai/gpt-5.6-sol",
			ModelOverrides:       []ModelOverride{{Model: "anthropic/claude-sonnet-4-6", ContextWindow: 1_000_000}, {Model: "openai/gpt-5.6-sol", ContextWindow: 400_000}},
			PromptCacheRetention: PromptCacheLong, NewSessionModel: "openai/gpt-5.6-sol", NewSessionThinkingLevel: ThinkingHigh,
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ServerSettings){
		"unordered overrides": func(s *ServerSettings) {
			s.ModelOverrides[0], s.ModelOverrides[1] = s.ModelOverrides[1], s.ModelOverrides[0]
		},
		"a zero override":           func(s *ServerSettings) { s.ModelOverrides[0].ContextWindow = 0 },
		"a model without provider":  func(s *ServerSettings) { s.DefaultModel = "gpt-5.6-sol" },
		"an unknown thinking level": func(s *ServerSettings) { s.NewSessionThinkingLevel = "turbo" },
		"no retention":              func(s *ServerSettings) { s.PromptCacheRetention = "" },
		"a model without its level": func(s *ServerSettings) { s.NewSessionThinkingLevel = "" },
	} {
		settings := valid()
		change(&settings)
		if settings.Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestUpdateServerSettingsInputValidation(t *testing.T) {
	t.Parallel()
	empty, unknown := "", PromptCacheRetention("forever")
	for _, input := range []UpdateServerSettingsInput{
		{},
		{DefaultModel: &empty, CompactionModel: &empty},
		{ModelOverrides: []ModelOverride{{Model: "openai/gpt-5.6-sol", ContextWindow: 0}}},
	} {
		if err := input.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", input, err)
		}
	}
	for name, input := range map[string]UpdateServerSettingsInput{
		"unknown retention":  {PromptCacheRetention: &unknown},
		"repeated override":  {ModelOverrides: []ModelOverride{{Model: "a/b", ContextWindow: 1}, {Model: "a/b", ContextWindow: 0}}},
		"negative override":  {ModelOverrides: []ModelOverride{{Model: "a/b", ContextWindow: -1}}},
		"oversized override": {ModelOverrides: []ModelOverride{{Model: "a/b", ContextWindow: MaxContextWindow + 1}}},
		"untrimmed selector": {ModelOverrides: []ModelOverride{{Model: " a/b", ContextWindow: 1}}},
	} {
		if input.Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
