package tui

import (
	"strings"

	"github.com/akonwi/kit/internal/auth"
)

const anthropicOAuthOptionID = "anthropic-oauth"

type authProviderOption struct {
	ID           string
	ProviderID   string
	Name         string
	Method       string
	DefaultModel string
}

var authProviderOptions = []authProviderOption{
	{
		ID: auth.OpenAICodexProviderID, ProviderID: auth.OpenAICodexProviderID, Name: "OpenAI Codex",
		Method: "ChatGPT plan · device code", DefaultModel: codexDefaultModel,
	},
	{
		ID: auth.AnthropicProviderID, ProviderID: auth.AnthropicProviderID, Name: "Anthropic",
		Method: "API key", DefaultModel: "anthropic/claude-sonnet-4-6",
	},
	{
		ID: auth.OpenAIProviderID, ProviderID: auth.OpenAIProviderID, Name: "OpenAI",
		Method: "API key", DefaultModel: "openai/gpt-5.6-sol",
	},
	{
		ID: anthropicOAuthOptionID, ProviderID: auth.AnthropicProviderID, Name: "Claude",
		Method: "Pro or Max plan · browser", DefaultModel: "anthropic/claude-sonnet-4-6",
	},
}

func filteredAuthProviders(query string) []authProviderOption {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]authProviderOption(nil), authProviderOptions...)
	}
	result := make([]authProviderOption, 0, len(authProviderOptions))
	for _, provider := range authProviderOptions {
		haystack := strings.ToLower(provider.Name + " " + provider.Method)
		if strings.Contains(haystack, query) {
			result = append(result, provider)
		}
	}
	return result
}

func authProviderByID(providerID string) (authProviderOption, bool) {
	for _, provider := range authProviderOptions {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return authProviderOption{}, false
}
