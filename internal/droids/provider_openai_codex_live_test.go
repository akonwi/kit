//go:build live

package droids

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	codexoauth "github.com/akonwi/kit/internal/droids/openaicodex"
)

func TestLiveOpenAICodexResponses(t *testing.T) {
	accessToken := os.Getenv("OPENAI_CODEX_ACCESS_TOKEN")
	if accessToken == "" {
		t.Skip("OPENAI_CODEX_ACCESS_TOKEN is not set")
	}
	accountID := os.Getenv("OPENAI_CODEX_ACCOUNT_ID")
	metadata, metadataErr := codexoauth.ParseAccountMetadata(accessToken)
	if accountID == "" {
		if metadataErr != nil {
			t.Fatal(metadataErr)
		}
		accountID = metadata.ID
	}
	fedRAMP := metadataErr == nil && metadata.FedRAMP
	if idToken := os.Getenv("OPENAI_CODEX_ID_TOKEN"); idToken != "" {
		idMetadata, err := codexoauth.ParseAccountMetadata(idToken)
		if err != nil {
			t.Fatal(err)
		}
		if idMetadata.ID != accountID {
			t.Fatal("OpenAI Codex access and ID token accounts do not match")
		}
		fedRAMP = fedRAMP || idMetadata.FedRAMP
	}
	if raw := os.Getenv("OPENAI_CODEX_FEDRAMP"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			t.Fatalf("parse OPENAI_CODEX_FEDRAMP: %v", err)
		}
		fedRAMP = fedRAMP || enabled
	}
	modelID := os.Getenv("OPENAI_CODEX_MODEL")
	if modelID == "" {
		modelID = "gpt-5.6-sol"
	}
	providers, err := NewProviders(OpenAICodex{
		Originator: "droids-live-test",
		Credentials: OpenAICodexCredentials{
			AccessToken: accessToken, AccountID: accountID, FedRAMP: fedRAMP,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/" + modelID)
	if !ok {
		t.Fatalf("OpenAI Codex model %q is not in the reviewed catalog", modelID)
	}

	t.Run("text", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		stream := providers.Stream(ctx, model, Request{
			SystemPrompt: "Follow the user's output-format instruction exactly.",
			Messages: []Message{UserMessage{Content: []Content{
				TextContent{Text: "Reply with exactly: codex-live-ok"},
			}}},
			Reasoning: "low",
		})
		for range stream.Events() {
		}
		message := stream.Result()
		assertLiveCodexSuccess(t, message)
		answer := strings.Trim(strings.ToLower(message.Text()), " \t\r\n.\"'")
		if answer != "codex-live-ok" {
			t.Fatalf("unexpected Codex response: %q", message.Text())
		}
	})

	t.Run("reasoning off", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		stream := providers.Stream(ctx, model, Request{
			SystemPrompt: "Follow the user's output-format instruction exactly.",
			Messages: []Message{UserMessage{Content: []Content{
				TextContent{Text: "Reply with exactly: reasoning-off-ok"},
			}}},
			Reasoning: "off",
		})
		for range stream.Events() {
		}
		message := stream.Result()
		assertLiveCodexSuccess(t, message)
		answer := strings.Trim(strings.ToLower(message.Text()), " \t\r\n.\"'")
		if answer != "reasoning-off-ok" {
			t.Fatalf("unexpected reasoning-off response: %q", message.Text())
		}
	})

	t.Run("encrypted reasoning replay", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		firstUser := UserMessage{Content: []Content{TextContent{Text: "Compute the last six digits of 123456789 raised to the 12345th power. Work it out carefully, then reply with only those six digits."}}}
		firstStream := providers.Stream(ctx, model, Request{
			SystemPrompt: "Solve the problem carefully and follow the output format exactly.",
			Messages:     []Message{firstUser},
			Reasoning:    "high",
		})
		for range firstStream.Events() {
		}
		first := firstStream.Result()
		assertLiveCodexSuccess(t, first)
		if strings.ReplaceAll(strings.TrimSpace(first.Text()), ",", "") != "338549" {
			t.Fatalf("unexpected first reasoning answer: %q", first.Text())
		}
		if !hasEncryptedReasoning(first) {
			t.Fatalf("first response lacks encrypted reasoning metadata: %#v", first)
		}

		secondStream := providers.Stream(ctx, model, Request{
			SystemPrompt: "Solve the problem carefully and follow the output format exactly.",
			Messages: []Message{
				firstUser,
				first,
				UserMessage{Content: []Content{TextContent{Text: "Add one to the previous result. Reply with only the integer."}}},
			},
			Reasoning: "high",
		})
		for range secondStream.Events() {
		}
		second := secondStream.Result()
		assertLiveCodexSuccess(t, second)
		if strings.ReplaceAll(strings.TrimSpace(second.Text()), ",", "") != "338550" {
			t.Fatalf("unexpected replay answer: %q", second.Text())
		}
	})

	t.Run("tool replay", func(t *testing.T) {
		store := NewMemoryStorage()
		reveal := NewTool(Tool[struct{}]{
			Name:        "reveal_code",
			Description: "Return the code that must be reported to the user.",
			Execute: func(context.Context, struct{}) (ToolResult, error) {
				return ToolText("tool-replay-ok"), nil
			},
		})
		droid, err := New(Options{
			Providers:    providers,
			Model:        "openai-codex/" + modelID,
			Session:      "codex-live-tool-replay",
			Storage:      store,
			SystemPrompt: "You must call reveal_code exactly once, then reply with exactly the code it returns.",
			Tools:        []AnyTool{reveal},
			Reasoning:    "low",
			MaxSteps:     4,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer droid.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		message, err := droid.Execute(ctx, "Get and report the code now.")
		if err != nil {
			t.Fatal(err)
		}
		answer := strings.Trim(strings.ToLower(message.Text()), " \t\r\n.\"'")
		if answer != "tool-replay-ok" {
			t.Fatalf("unexpected final tool answer: %q", message.Text())
		}
		transcript, err := store.Load(context.Background(), "codex-live-tool-replay")
		if err != nil {
			t.Fatal(err)
		}
		verifiedToolTurn := false
		for _, entry := range transcript {
			assistant, ok := entry.(AssistantMessage)
			if !ok || assistant.StopReason != StopReasonToolUse {
				continue
			}
			for _, content := range assistant.Content {
				if call, ok := content.(ToolCall); ok && call.Signature != "" {
					verifiedToolTurn = true
				}
			}
		}
		if !verifiedToolTurn {
			t.Fatalf("tool-use turn lacks a replay signature: %#v", transcript)
		}
	})
}

func hasEncryptedReasoning(message AssistantMessage) bool {
	for _, content := range message.Content {
		thinking, ok := content.(ThinkingContent)
		if !ok {
			continue
		}
		var signature struct {
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal([]byte(thinking.Signature), &signature) == nil && signature.EncryptedContent != "" {
			return true
		}
	}
	return false
}

func assertLiveCodexSuccess(t *testing.T, message AssistantMessage) {
	t.Helper()
	if message.StopReason == StopReasonError || message.StopReason == StopReasonAborted || message.StopReason == StopReasonContextWindow {
		t.Fatalf("Codex response failed (%s): %s", message.ErrorKind, message.ErrorMessage)
	}
}
