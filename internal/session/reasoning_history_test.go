package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestSessionReasoningHistoryReopenAndModelSwitch(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := &reasoningSessionProviders{}
	open := func() *session.Manager {
		m, err := session.NewManager(store, provider, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		return m
	}
	manager := open()
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Name: "Reasoning history test", Model: "test/gpt-6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	configure := func(model, effort string) {
		t.Helper()
		result, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: record.ConfigurationRevision, Model: model, ThinkingLevel: &effort})
		if err != nil {
			t.Fatal(err)
		}
		record = result.Session
	}
	prompt := func(text string, want *droids.ReasoningHistory) {
		t.Helper()
		if _, err := manager.RunPrompt(t.Context(), record.ID, text); err != nil {
			t.Fatal(err)
		}
		got := provider.last()
		if !reflect.DeepEqual(got.ReasoningHistory, want) {
			t.Fatalf("%s history=%+v, want %+v", text, got.ReasoningHistory, want)
		}
	}
	configure("test/gpt-6-sol", "low")
	prompt("first", &droids.ReasoningHistory{Baseline: "low", Effective: "low"})
	configure("test/gpt-6-sol", "high")
	manager.Close()
	manager = open()
	prompt("second", &droids.ReasoningHistory{Baseline: "low", Effective: "high", Updates: []droids.ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}}})
	configure("test/gpt-6-sol", "low")
	prompt("third", &droids.ReasoningHistory{Baseline: "low", Effective: "low", Updates: []droids.ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}, {BeforeMessage: 4, Effort: "low"}}})
	configure("test/gpt-6-astra", "medium")
	prompt("fourth", &droids.ReasoningHistory{Baseline: "medium", Effective: "medium"})
	configure("test/gpt-5-mini", "low")
	prompt("unsupported", nil)
	configure("test/gpt-6-sol", "high")
	manager.Close()
	manager = open()
	prompt("returned", &droids.ReasoningHistory{Baseline: "high", Effective: "high"})
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcriptIDs(snapshot)) != 12 {
		t.Fatalf("transcript message count=%d, want 12", len(transcriptIDs(snapshot)))
	}
}

type reasoningSessionProviders struct {
	mu       sync.Mutex
	requests []droids.Request
}

func (*reasoningSessionProviders) Models() []droids.Model {
	var models []droids.Model
	for _, id := range []string{"gpt-6-sol", "gpt-6-astra", "gpt-5-mini"} {
		model, _ := droids.OpenAIModel(id)
		model.Provider = "test"
		models = append(models, model)
	}
	return models
}
func (p *reasoningSessionProviders) Model(id string) (droids.Model, bool) {
	for _, model := range p.Models() {
		if id == model.ID || id == model.Provider+"/"+model.ID {
			return model, true
		}
	}
	return droids.Model{}, false
}
func (p *reasoningSessionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %s", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (*reasoningSessionProviders) RefreshModels(context.Context) error { return nil }
func (p *reasoningSessionProviders) Stream(_ context.Context, model droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	p.mu.Unlock()
	return &configurationStream{message: droids.AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "ok"}}}}
}
func (p *reasoningSessionProviders) last() droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}
