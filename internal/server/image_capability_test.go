package server

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	kitsession "github.com/akonwi/kit/internal/session"
)

func TestInspectImageFollowsToolResultImagePolicy(t *testing.T) {
	t.Parallel()
	providers, err := droids.NewProviders(droids.Anthropic{}, droids.OpenAI{}, droids.OpenAICodex{Credentials: droids.OpenAICodexCredentials{AccessToken: "test-token", AccountID: "test-account"}}, droids.OpenCodeGo{})
	if err != nil {
		t.Fatal(err)
	}
	enabled := inspectImageEnabled(providers)
	for _, test := range []struct {
		selector string
		want     bool
	}{
		{"anthropic/claude-opus-5-5", true},
		{"anthropic/claude-haiku-4-5", true},
		{"openai/gpt-5.4", true},
		{"openai-codex/gpt-5.4", true},
		{"openai-codex/gpt-5.3-codex-spark", false}, // text-only
		{"opencode-go/minimax-m3", true},
		{"opencode-go/gpt-6-luna", true},
		{"opencode-go/grok-4.7", false}, // user images only
		{"opencode-go/kimi-k2.6", false},
		{"opencode-go/glm-5.3", false}, // text-only
		{"unknown/model", false},
	} {
		provider, id, _ := strings.Cut(test.selector, "/")
		if got := enabled(kitsession.SessionRecord{ModelProvider: provider, ModelID: id}); got != test.want {
			t.Errorf("inspect_image enabled for %s = %v, want %v", test.selector, got, test.want)
		}
	}
}
