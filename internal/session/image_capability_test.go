package session

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestModelSupportsToolResultImageRequiresCompatibleAPI(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		model droids.Model
		want  bool
	}{
		{name: "openai responses", model: droids.Model{API: droids.ModelAPIOpenAIResponses, Input: []string{"text", "image"}}, want: true},
		{name: "codex responses", model: droids.Model{API: droids.ModelAPIOpenAICodexResponses, Input: []string{"text", "image"}}, want: true},
		{name: "chat completions", model: droids.Model{API: droids.ModelAPIOpenAIChat, Input: []string{"text", "image"}}},
		{name: "anthropic", model: droids.Model{API: droids.ModelAPIAnthropicMessages, Input: []string{"text", "image"}}},
		{name: "text only", model: droids.Model{API: droids.ModelAPIOpenAIResponses, Input: []string{"text"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ModelSupportsToolResultImage(test.model); got != test.want {
				t.Fatalf("ModelSupportsToolResultImage() = %v, want %v", got, test.want)
			}
		})
	}
}
