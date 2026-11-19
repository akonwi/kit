package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

// questionProviders has the model ask confirm_from_user once, then reply, and
// records the tool result the model saw.
type questionProviders struct {
	mu     sync.Mutex
	asked  bool
	result string
}

func (p *questionProviders) ID() string             { return "test" }
func (p *questionProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *questionProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}
func (p *questionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (*questionProviders) RefreshModels(context.Context) error { return nil }
func (*questionProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *questionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok {
			p.result = result.Content[0].(droids.TextContent).Text
		}
	}
	if !p.asked {
		p.asked = true
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{ID: "call_ask", Name: session.ConfirmInteractionToolName, Arguments: []byte(`{"title":"Ship it?"}`)}},
		}}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
	}}
}
func (p *questionProviders) seen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}
func (*questionProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

func openQuestionManager(t *testing.T, root string, store *storage.Store, providers *questionProviders) *session.Manager {
	t.Helper()
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

// waitForQuestion waits until the session's snapshot lists one pending
// interaction and returns the snapshot.
func waitForQuestion(t *testing.T, manager *session.Manager, sessionID string) session.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.PendingInteractions) == 1 {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("no question was asked: %+v", snapshot.PendingInteractions)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForIdle(t *testing.T, manager *session.Manager, sessionID string) session.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveRunID == "" {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatal("the run did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheModelsQuestionIsProjectedAndAnsweredThroughTheSession(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &questionProviders{}
	manager := openQuestionManager(t, root, store, providers)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), record.ID, "ship?")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := waitForQuestion(t, manager, record.ID)
	request := snapshot.PendingInteractions[0]
	if request.Kind != session.InteractionConfirm || request.Title != "Ship it?" || request.RunID != reservation.RunID || request.ToolCallID == "" || request.Plugin != nil {
		t.Fatalf("pending interaction = %+v", request)
	}
	if snapshot.ActiveRunID != reservation.RunID {
		t.Fatalf("active run while waiting = %q, want %q", snapshot.ActiveRunID, reservation.RunID)
	}

	invalid := "yes"
	if err := manager.RespondInteraction(t.Context(), record.ID, session.InteractionResponse{RequestID: request.ID, Value: &invalid}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("an answer of the wrong kind = %v, want invalid input", err)
	}
	confirmed := true
	if err := manager.RespondInteraction(t.Context(), record.ID, session.InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RespondInteraction(t.Context(), record.ID, session.InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); !errors.Is(err, session.ErrInteractionSettled) {
		t.Fatalf("answering again at once = %v, want settled", err)
	}
	after := waitForIdle(t, manager, record.ID)
	if len(after.PendingInteractions) != 0 || providers.seen() != `{"confirmed":true}` {
		t.Fatalf("after answering: pending = %+v, result = %q", after.PendingInteractions, providers.seen())
	}
	if err := manager.RespondInteraction(t.Context(), record.ID, session.InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); !errors.Is(err, session.ErrInteractionSettled) {
		t.Fatalf("answering again = %v, want settled", err)
	}

	// Each run starts a new event stream; read the run's from its start.
	page, err := manager.Events(t.Context(), record.ID, snapshot.EventStreamID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var requested, resolved bool
	for _, event := range page.Events {
		switch event.Kind {
		case session.EventInteractionRequested:
			requested = requested || event.Interaction != nil && event.Interaction.ID == request.ID && event.RunID == reservation.RunID
		case session.EventInteractionResolved:
			resolved = resolved || event.InteractionID == request.ID && event.InteractionResolution == "answered"
		}
	}
	if !requested || !resolved {
		t.Fatalf("interaction events: requested=%v resolved=%v resync=%v in %+v", requested, resolved, page.ResyncRequired, page.Events)
	}
}

func TestAQuestionTheModelAskedSurvivesARestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &questionProviders{}
	first := openQuestionManager(t, root, store, providers)
	record, err := first.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := first.StartPrompt(t.Context(), record.ID, "ship?")
	if err != nil {
		t.Fatal(err)
	}
	asked := waitForQuestion(t, first, record.ID).PendingInteractions[0]
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := first.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}

	second := openQuestionManager(t, root, store, providers)
	snapshot := waitForQuestion(t, second, record.ID)
	if restored := snapshot.PendingInteractions[0]; restored.ID != asked.ID || restored.Title != "Ship it?" || restored.RunID != reservation.RunID {
		t.Fatalf("restored interaction = %+v; want %+v", restored, asked)
	}
	if snapshot.ActiveRunID != reservation.RunID {
		t.Fatalf("active run after restart = %q, want the waiting turn %q", snapshot.ActiveRunID, reservation.RunID)
	}
	confirmed := false
	if err := second.RespondInteraction(t.Context(), record.ID, session.InteractionResponse{RequestID: asked.ID, Confirmed: &confirmed}); err != nil {
		t.Fatal(err)
	}
	waitForIdle(t, second, record.ID)
	if got := providers.seen(); got != `{"confirmed":false}` {
		t.Fatalf("result after restart = %q", got)
	}
}
