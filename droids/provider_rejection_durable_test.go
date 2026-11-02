package droids_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/sqlitestore"
)

// A provider's reason for rejecting a request is stored, made safe to
// render, with the failed response and survives reopening the conversation.
func TestProviderRejectionReasonIsDurable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":{"message":"Upstream request failed:\n\tModel is\u202e unavailable.","type":"upstream_error"}}`)
	}))
	defer server.Close()
	providers, err := droids.NewProviders(droids.OpenCodeGo{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	var model droids.Model
	for _, candidate := range providers.Models() {
		if candidate.API == droids.ModelAPIOpenAIResponses {
			model = resolvedTestModel(providers, candidate.Provider+"/"+candidate.ID)
			break
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "droid.db")
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), "conversation_provider_rejection", droids.Config{Store: store, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "hello"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	const explanation = "Provider rejected the request: Upstream request failed: Model is unavailable. (HTTP 400)"
	if err != nil || outcome.Status != droids.ExecutionFailed || outcome.Error == nil || outcome.Error.Message != explanation {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	turnID := handle.TurnID()
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedStore.Close() })
	reopened, err := droids.Spawn(t.Context(), "conversation_provider_rejection", droids.Config{Store: reopenedStore, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	page, err := reopened.History(t.Context(), droids.HistoryQuery{Descending: true, Limit: 1})
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("History = %+v, %v", page, err)
	}
	assistant, ok := page.Messages[0].Message.(droids.AssistantMessage)
	if !ok || assistant.Error == nil {
		t.Fatalf("last message = %#v", page.Messages[0].Message)
	}
	want := droids.ProviderError{
		Kind: droids.ProviderInvalidRequest, Code: "upstream_error", Message: "Provider rejected the request", Status: http.StatusBadRequest,
		Detail: "Upstream request failed: Model is unavailable.",
	}
	if *assistant.Error != want || assistant.ErrorMessage != want.Message {
		t.Fatalf("stored error = %q, %+v\nwant %+v", assistant.ErrorMessage, *assistant.Error, want)
	}
	if got := assistant.ErrorExplanation(); got != explanation {
		t.Fatalf("ErrorExplanation() = %q", got)
	}
	turn, err := reopened.Turn(t.Context(), turnID)
	if err != nil || turn.Error == nil || turn.Error.Kind != droids.DroidErrorProvider || turn.Error.Message != explanation {
		t.Fatalf("reopened Turn = %+v, %v", turn, err)
	}
}
