//go:build live

package droids

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveOpenAIResponses(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	modelID := os.Getenv("OPENAI_MODEL")
	if modelID == "" {
		modelID = "gpt-4.1-mini"
	}

	t.Run("direct", func(t *testing.T) {
		testLiveOpenAIResponses(t, OpenAI{APIKey: apiKey}, modelID)
	})

	t.Run("cloudflare gateway", func(t *testing.T) {
		accountID := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		gatewayID := os.Getenv("CLOUDFLARE_AI_GATEWAY_ID")
		if accountID == "" || gatewayID == "" {
			t.Skip("CLOUDFLARE_ACCOUNT_ID or CLOUDFLARE_AI_GATEWAY_ID is not set")
		}
		gateway := CloudflareGateway{
			AccountID: accountID,
			GatewayID: gatewayID,
			Token:     os.Getenv("CLOUDFLARE_AI_GATEWAY_TOKEN"),
		}
		testLiveOpenAIResponses(t, gateway.OpenAI(OpenAI{APIKey: apiKey}), modelID)
	})
}

func testLiveOpenAIResponses(t *testing.T, config OpenAI, modelID string) {
	t.Helper()
	providers, err := NewProviders(config)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model(modelID)
	if !ok {
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := providers.RefreshModels(refreshCtx)
		refreshCancel()
		if err != nil {
			t.Fatalf("refresh models: %v", err)
		}
		model, ok = providers.Model(modelID)
	}
	if !ok {
		t.Fatalf("model %q did not resolve", modelID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stream := providers.Stream(ctx, model, Request{Messages: []Message{
		UserMessage{Content: []Content{
			TextContent{Text: "Read the attached note and reply with exactly its code word."},
			NewFileData("note.txt", "text/plain", []byte("cobalt")),
		}},
	}})
	for range stream.Events() {
	}
	message := stream.Result()
	if message.StopReason == StopReasonError || message.StopReason == StopReasonAborted {
		t.Fatalf("response failed: %s", message.ErrorMessage)
	}
	answer := strings.Trim(strings.ToLower(message.Text()), " \t\r\n.\"'")
	if answer != "cobalt" {
		t.Fatalf("response did not consume attachment: %q", message.Text())
	}
}
