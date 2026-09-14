//go:build live

package droids_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func TestLiveSDKMemoryAndSQLiteReadOnlyTool(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4.1-mini"
	}
	providers, err := droids.NewProviders(droids.OpenAI{APIKey: apiKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers.Model(model); !ok {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		err := providers.RefreshModels(ctx)
		cancel()
		if err != nil {
			t.Fatalf("refresh models: %v", err)
		}
	}

	t.Run("memory", func(t *testing.T) {
		exerciseLiveSDKStore(t, droids.NewMemoryStore(), providers, model)
	})
	t.Run("sqlite", func(t *testing.T) {
		store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{
			Path: filepath.Join(t.TempDir(), "droid.sqlite"),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		exerciseLiveSDKStore(t, store, providers, model)
	})
}

func exerciseLiveSDKStore(t *testing.T, store droids.Store, providers droids.Providers, model string) {
	t.Helper()
	var calls atomic.Int32
	read := droids.MustTool(droids.Tool[struct {
		Name string `json:"name" jsonschema:"required"`
	}]{
		Name:        "read_fixture",
		Description: "Read a named immutable test fixture. This tool cannot modify anything.",
		Execute: func(_ context.Context, _ droids.ToolContext, args struct {
			Name string `json:"name" jsonschema:"required"`
		}, _ droids.ToolUpdate) (droids.ToolResult, error) {
			calls.Add(1)
			return droids.ToolText("The fixture code word is cobalt."), nil
		},
	})
	droid, err := droids.Spawn(t.Context(), droids.ConversationID("live_"+strings.ReplaceAll(t.Name(), "/", "_")), droids.Config{
		Store: store, Model: model, Tools: []droids.AnyTool{read},
		SystemPrompt: "Use the available read-only tool when asked to read a fixture.",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "Call read_fixture for fixture.txt, then reply with exactly the code word from the fixture."},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionCompleted || outcome.FinalMessage == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
	answer := strings.Trim(strings.ToLower(outcome.FinalMessage.Message.(droids.AssistantMessage).Text()), " \t\r\n.\"'")
	if answer != "cobalt" {
		t.Fatalf("answer = %q, want cobalt", answer)
	}
	if calls.Load() != 1 {
		t.Fatalf("read-only tool calls = %d, want 1", calls.Load())
	}
}
