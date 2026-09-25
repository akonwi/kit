package server

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestResolveSubagentModel(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		configs := []droids.ProviderConfig{droids.OpenAI{}, droids.OpenAICodex{Credentials: droids.OpenAICodexCredentials{AccessToken: "test-token", AccountID: "test-account"}}}
		first := "openai"
		if reverse {
			configs[0], configs[1] = configs[1], configs[0]
			first = "openai-codex"
		}
		providers, err := droids.NewProviders(configs...)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name, selector string
			available      []string
			want           string
		}{
			{"ambiguous uses registration order", "gpt-5.6-sol", []string{"openai-codex", "openai"}, first},
			{"skips unavailable", "gpt-5.6-sol", []string{"openai-codex"}, "openai-codex"},
			{"skips nonmatching", "gpt-4o-mini", []string{"openai", "openai-codex"}, "openai"},
			{"explicit openai", "openai/gpt-5.6-sol", []string{"openai", "openai-codex"}, "openai"},
			{"explicit codex", "openai-codex/gpt-5.6-sol", []string{"openai", "openai-codex"}, "openai-codex"},
			{"explicit unavailable never reroutes", "openai/gpt-5.6-sol", []string{"openai-codex"}, ""},
			{"unknown model", "missing-model", []string{"openai", "openai-codex"}, ""},
			{"unknown provider", "missing/gpt-5.6-sol", []string{"openai", "openai-codex"}, ""},
			{"no available providers", "gpt-5.6-sol", nil, ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				model, err := resolveSubagentModel(providers, tc.selector, tc.available)
				if tc.want == "" {
					if err == nil {
						t.Fatal("expected resolution failure")
					}
					return
				}
				if err != nil || model.Provider != tc.want {
					t.Fatalf("provider=%q err=%v; want %q", model.Provider, err, tc.want)
				}
			})
		}
		if _, err := providers.Resolve("gpt-5.6-sol"); err == nil {
			t.Fatal("global resolution should still reject ambiguity")
		}
	}
}

func TestProviderRegistrationOrderIsACopy(t *testing.T) {
	providers, err := droids.NewProviders(droids.OpenAICodex{Credentials: droids.OpenAICodexCredentials{AccessToken: "test-token", AccountID: "test-account"}}, droids.OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	ids := droids.ProviderIDs(providers)
	if len(ids) != 2 || ids[0] != "openai-codex" || ids[1] != "openai" {
		t.Fatalf("registration order: %v", ids)
	}
	ids[0] = "changed"
	if droids.ProviderIDs(providers)[0] != "openai-codex" {
		t.Fatal("caller mutated registry order")
	}
}

type orderedModelCatalog struct{ rejectingProviders }

func (orderedModelCatalog) Models() []droids.Model {
	return []droids.Model{
		{Provider: "second", ID: "one"},
		{Provider: "first", ID: "two"},
		{Provider: "second", ID: "three"},
	}
}

func TestProviderOrderForCustomCatalog(t *testing.T) {
	ids := droids.ProviderIDs(orderedModelCatalog{})
	if len(ids) != 2 || ids[0] != "second" || ids[1] != "first" {
		t.Fatalf("custom provider order = %v", ids)
	}
}
