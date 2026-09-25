package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRedactInternalIdentities(t *testing.T) {
	t.Parallel()
	text := "child subagent_0123456789abcdef0123456789abcdef failed task_abcdef0123456789abcdef0123456789 at turn_11111111111111111111111111111111"
	redacted := RedactInternalIdentities(text)
	for _, prefix := range []string{"subagent_", "task_", "turn_"} {
		if strings.Contains(redacted, prefix) {
			t.Fatalf("redacted text retained %q: %q", prefix, redacted)
		}
	}
	if !strings.Contains(redacted, "child <internal> failed <internal> at <internal>") {
		t.Fatalf("redacted text = %q", redacted)
	}
}

func TestModelToolUsesAgentNameWithoutStorageIdentities(t *testing.T) {
	t.Parallel()
	properties := modelToolParameters()["properties"].(map[string]any)
	if len(properties) != 3 || properties["action"] == nil || properties["agent"] == nil || properties["message"] == nil {
		t.Fatalf("model tool properties = %#v", properties)
	}
	projected := projectModelConversation(Conversation{
		ID: "subagent_0123456789abcdef0123456789abcdef", Agent: Definition{Name: "reviewer"},
		Model: "test/model", State: ConversationRunning,
		ActiveTaskID: "task_0123456789abcdef0123456789abcdef", LastCompletedTaskID: "task_abcdef0123456789abcdef0123456789",
	})
	raw, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"subagent_", "task_", "conversationId", "taskId", "activeTaskId"} {
		if strings.Contains(string(raw), identity) {
			t.Fatalf("model projection retained %q: %s", identity, raw)
		}
	}
}

func TestToolServiceMergesAndRemovesLivePluginCatalog(t *testing.T) {
	base, err := NewCatalog(Definition{Name: "base", Description: "Base", Instructions: "Base work", Source: Source{Kind: SourceUser, Path: "/base.md"}})
	if err != nil {
		t.Fatal(err)
	}
	pluginCatalog, err := NewCatalog(Definition{Name: "demo.reviewer", Description: "Reviews", Instructions: "Review", Source: Source{Kind: SourcePlugin, PluginID: "demo", Path: "/plugin.json"}})
	if err != nil {
		t.Fatal(err)
	}
	service := &ToolService{}
	cleanup, err := service.RegisterPluginCatalogProvider("session-one", func() (Catalog, error) { return pluginCatalog, nil })
	if err != nil {
		t.Fatal(err)
	}
	merged, err := service.effectiveCatalog("session-one", base)
	if err != nil || len(merged.Definitions()) != 2 {
		t.Fatalf("merged catalog = %#v, %v", merged.Definitions(), err)
	}
	cleanup()
	merged, err = service.effectiveCatalog("session-one", base)
	if err != nil || len(merged.Definitions()) != 1 || merged.Definitions()[0].Name != "base" {
		t.Fatalf("cleared catalog = %#v, %v", merged.Definitions(), err)
	}
	oldCleanup, err := service.RegisterPluginCatalogProvider("session-one", func() (Catalog, error) { return Catalog{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	newCleanup, err := service.RegisterPluginCatalogProvider("session-one", func() (Catalog, error) { return pluginCatalog, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer newCleanup()
	oldCleanup()
	merged, err = service.effectiveCatalog("session-one", base)
	if err != nil || len(merged.Definitions()) != 2 {
		t.Fatalf("old cleanup removed replacement provider: %#v, %v", merged.Definitions(), err)
	}
}

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
