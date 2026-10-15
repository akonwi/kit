package server

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/settings"
)

// catalogProviders is a fixed model catalog.
type catalogProviders struct {
	rejectingProviders
	models []droids.Model
}

func (p catalogProviders) Models() []droids.Model { return p.models }
func (p catalogProviders) Model(selector string) (droids.Model, bool) {
	for _, model := range p.models {
		if model.Provider+"/"+model.ID == selector {
			return model, true
		}
	}
	return droids.Model{}, false
}

func TestSessionDefaultsChooseAModel(t *testing.T) {
	catalog := catalogProviders{models: []droids.Model{
		{Provider: "opencode-go", ID: "kimi"},
		{Provider: "anthropic", ID: "claude-opus-4-7"},
		{Provider: "anthropic", ID: "claude-sonnet-4-6"},
		{Provider: "openai-codex", ID: "gpt-5.5"},
	}}
	for _, test := range []struct {
		name       string
		configured string
		available  []string
		want       string
	}{
		{name: "the configured default", configured: "anthropic/claude-opus-4-7", available: []string{"anthropic", "openai-codex"}, want: "anthropic/claude-opus-4-7"},
		{name: "a fallback model", available: []string{"anthropic", "openai-codex"}, want: "anthropic/claude-sonnet-4-6"},
		{name: "a default whose provider has no credentials", configured: "opencode-go/kimi", available: []string{"anthropic"}, want: "anthropic/claude-sonnet-4-6"},
		// The fallback model is gone from the catalog, so another model of
		// its provider is preferred to catalog order.
		{name: "another model of a fallback provider", available: []string{"opencode-go", "openai-codex"}, want: "openai-codex/gpt-5.5"},
		{name: "any model with credentials", available: []string{"opencode-go"}, want: "opencode-go/kimi"},
	} {
		defaults := sessionDefaults{
			providers:          catalog,
			availableProviders: func(context.Context) []string { return test.available },
			settings:           func() (settings.Settings, error) { return settings.Settings{DefaultModel: test.configured}, nil },
		}
		if got, err := defaults.model(t.Context()); err != nil || got != test.want {
			t.Errorf("%s: model() = %q, %v; want %q", test.name, got, err, test.want)
		}
	}

	none := sessionDefaults{providers: catalog, availableProviders: func(context.Context) []string { return nil }}
	var failure *httpapi.APIError
	if _, err := none.model(t.Context()); !errors.As(err, &failure) || failure.Message != "no model is available; log in to a provider" {
		t.Fatalf("model() without credentials = %v", err)
	}
	if model, level := none.newSession(t.Context()); model != "" || level != "" {
		t.Fatalf("newSession() without credentials = %q, %q", model, level)
	}
}

// A new session starts at its model's default thinking level, and
// unreadable settings leave the default model unset.
func TestSessionDefaultsReportTheModelsDefaultThinkingLevel(t *testing.T) {
	reasoning := droids.Model{Provider: "test", ID: "thinker", API: droids.ModelAPIOpenAIResponses, Reasoning: true, ReasoningLevels: []string{"low", "medium", "high"}}
	plain := droids.Model{Provider: "test", ID: "plain", API: droids.ModelAPIOpenAIResponses}
	catalog := catalogProviders{models: []droids.Model{reasoning, plain}}
	configured := sessionDefaults{providers: catalog, settings: func() (settings.Settings, error) {
		return settings.Settings{DefaultModel: "test/plain"}, nil
	}}
	if model, level := configured.newSession(t.Context()); model != "test/plain" || level != "off" {
		t.Fatalf("newSession() = %q, %q", model, level)
	}
	broken := sessionDefaults{providers: catalog, settings: func() (settings.Settings, error) { return settings.Settings{}, errors.New("malformed") }}
	if model, level := broken.newSession(t.Context()); model != "test/thinker" || level != "medium" {
		t.Fatalf("newSession() with unreadable settings = %q, %q", model, level)
	}
}
