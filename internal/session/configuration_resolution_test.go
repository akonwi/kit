package session

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestResolveSavedThinkingFallsBackToOffOrLowestAvailable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		levels []string
		want   string
	}{
		{name: "off", levels: []string{"none", "low", "high"}, want: "off"},
		{name: "lowest", levels: []string{"low", "high"}, want: "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := droids.Model{ID: "model", Provider: "test", API: droids.ModelAPIOpenAIResponses, Reasoning: true, ReasoningLevels: test.levels}
			got, adjusted, err := resolveSavedThinkingLevel(model, "medium")
			if err != nil || !adjusted || got != test.want {
				t.Fatalf("resolveSavedThinkingLevel() = %q, %t, %v; want %q, true, nil", got, adjusted, err, test.want)
			}
		})
	}
}
