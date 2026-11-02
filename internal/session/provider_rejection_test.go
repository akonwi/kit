package session_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

// A failed turn reports the provider's reason with Kit's description of the
// failure, live and after the session is reloaded by a restarted server.
func TestManagerReportsTheProviderRejectionReason(t *testing.T) {
	const explanation = "Provider rejected the request: Upstream request failed: Model is unavailable. (HTTP 400)"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, "Upstream request failed: Model is unavailable.")
	}))
	defer server.Close()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providers, err := droids.NewProviders(droids.OpenCodeGo{APIKey: "test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	open := func() *session.Manager {
		manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
		if err != nil {
			t.Fatal(err)
		}
		return manager
	}
	assertTranscript := func(manager *session.Manager, sessionID, turnID string) {
		t.Helper()
		snapshot, err := manager.Snapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		last := snapshot.Messages[len(snapshot.Messages)-1]
		if last.Role != "assistant" || last.TurnID != turnID || !last.IsError || last.ErrorMessage != explanation {
			t.Fatalf("failed reply = %+v", last)
		}
	}

	manager := open()
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "opencode-go/grok-4.6", Name: "rejected"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.RunPrompt(t.Context(), record.ID, "hello")
	if err != nil || result.Status != session.RunStatusFailed || result.ErrorMessage != explanation {
		t.Fatalf("RunPrompt() = %+v, %v", result, err)
	}
	if run, err := manager.GetRun(t.Context(), record.ID, result.RunID); err != nil || run.Error != explanation {
		t.Fatalf("GetRun() = %+v, %v", run, err)
	}
	assertTranscript(manager, record.ID, result.TurnID)
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	reopened := open()
	defer reopened.Close()
	assertTranscript(reopened, record.ID, result.TurnID)
	if run, err := reopened.GetRun(t.Context(), record.ID, result.RunID); err != nil || run.Status != session.RunStatusFailed || run.Error != explanation {
		t.Fatalf("reloaded GetRun() = %+v, %v", run, err)
	}
}
