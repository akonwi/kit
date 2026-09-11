package subagent

import (
	"context"
	"errors"
	"testing"
)

func TestResolveChildModelCanonicalizesAvailableSelector(t *testing.T) {
	t.Parallel()
	model, thinking, warning, err := resolveChildConfiguration(t.Context(), "reviewer", "gpt-5", "anthropic/claude", "high", func(_ context.Context, selector, thinking string) (string, string, error) {
		if selector == "gpt-5" {
			return "openai/gpt-5", thinking, nil
		}
		return "", "", errors.New("unknown model")
	})
	if err != nil || model != "openai/gpt-5" || thinking != "high" || warning != "" {
		t.Fatalf("resolved model = %q, thinking = %q, warning = %q, err = %v", model, thinking, warning, err)
	}
}

func TestResolveChildModelFallsBackToActiveConfiguration(t *testing.T) {
	t.Parallel()
	model, thinking, warning, err := resolveChildConfiguration(t.Context(), "reviewer", "production/reviewer", "anthropic/claude", "high", func(_ context.Context, selector, _ string) (string, string, error) {
		if selector == "anthropic/claude" {
			return selector, "medium", nil
		}
		return "", "", errors.New("unknown model")
	})
	if err != nil || model != "anthropic/claude" || thinking != "medium" {
		t.Fatalf("resolved model = %q, thinking = %q, err = %v", model, thinking, err)
	}
	want := `Subagent "reviewer" requested unavailable model "production/reviewer"; using active model "anthropic/claude".`
	if warning != want {
		t.Fatalf("warning = %q, want %q", warning, want)
	}
}

func TestResolveChildModelRejectsUnavailableActiveConfiguration(t *testing.T) {
	t.Parallel()
	_, _, _, err := resolveChildConfiguration(t.Context(), "reviewer", "production/reviewer", "missing/active", "high", func(context.Context, string, string) (string, string, error) {
		return "", "", errors.New("unknown model")
	})
	if err == nil {
		t.Fatal("unavailable requested and active models were accepted")
	}
}
