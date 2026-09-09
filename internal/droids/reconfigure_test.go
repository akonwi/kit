package droids

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestReconfigureAppliesToNextProviderRequestWithoutInterruptingInflightRequest(t *testing.T) {
	providers := newReconfigureProviders()
	droid, err := Open(t.Context(), "conversation_reconfigure", Config{
		Providers: providers, Model: "test/reconfigure", SystemPrompt: "before", Reasoning: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })

	first, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "first"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if err := droid.Reconfigure(RequestConfiguration{SystemPrompt: "after", Reasoning: "high"}); err != nil {
		t.Fatal(err)
	}
	close(providers.release)
	if _, err := first.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}

	second, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "second"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	requests := providers.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	if requests[0].SystemPrompt != "before" || requests[0].Reasoning != "low" {
		t.Fatalf("in-flight request configuration = %+v", requests[0])
	}
	if requests[1].SystemPrompt != "after" || requests[1].Reasoning != "high" {
		t.Fatalf("next request configuration = %+v", requests[1])
	}
}

type reconfigureProviders struct {
	mu       sync.Mutex
	requests []Request
	started  chan struct{}
	release  chan struct{}
}

func newReconfigureProviders() *reconfigureProviders {
	return &reconfigureProviders{started: make(chan struct{}), release: make(chan struct{})}
}

func (p *reconfigureProviders) ID() string                                             { return "test" }
func (p *reconfigureProviders) Models() []Model                                        { return []Model{p.model()} }
func (p *reconfigureProviders) RefreshModels(context.Context) error                    { return nil }
func (p *reconfigureProviders) ValidateReplay(context.Context, Model, []Message) error { return nil }
func (p *reconfigureProviders) Model(id string) (Model, bool) {
	if id == "reconfigure" || id == "test/reconfigure" {
		return p.model(), true
	}
	return Model{}, false
}
func (p *reconfigureProviders) Resolve(id string) (Provider, Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, Model{}, errors.New("unknown model")
	}
	return AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *reconfigureProviders) Stream(ctx context.Context, _ Model, request Request) Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	return reconfigureStream{message: AssistantMessage{
		Provider: "test", Model: "reconfigure", StopReason: StopReasonStop,
		Content: []AssistantContent{TextContent{Text: "ok"}},
	}}
}
func (p *reconfigureProviders) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.requests...)
}
func (*reconfigureProviders) model() Model {
	return Model{
		ID: "reconfigure", Provider: "test", API: ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
		Reasoning: true, ReasoningLevels: []string{"low", "high"},
	}
}

type reconfigureStream struct{ message AssistantMessage }

func (s reconfigureStream) Events() <-chan StreamEvent {
	events := make(chan StreamEvent, 2)
	events <- StreamStart{Partial: AssistantMessage{Provider: s.message.Provider, Model: s.message.Model}}
	events <- StreamDone{Message: s.message}
	close(events)
	return events
}
func (s reconfigureStream) Result() AssistantMessage { return s.message }
func (reconfigureStream) Close() error               { return nil }
