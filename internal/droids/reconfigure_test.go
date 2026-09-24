package droids

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReconfigureAppliesToNextProviderRequestWithoutInterruptingInflightRequest(t *testing.T) {
	providers := newReconfigureProviders()
	model := resolvedTestModel(providers, "test/reconfigure").WithContextWindow(1_000_000)
	droid, err := Spawn(t.Context(), "conversation_reconfigure", Config{
		Model: model, SystemPrompt: "before", Reasoning: "low",
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
	configuration := droid.sdk.currentRequestConfiguration()
	if configuration.contextWindow != 0 {
		t.Fatalf("omitted context-window override = %d, want zero", configuration.contextWindow)
	}
	if got := configuredContextModel(droid.model, configuration).ContextWindow; got != 1_000_000 {
		t.Fatalf("effective context window after reconfigure = %d, want 1000000", got)
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
	toolName string
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
func (p *reconfigureProviders) Resolve(id string) (Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return Model{}, errors.New("unknown model")
	}
	return BindModel(AdaptProvider("test", p.Models(), p.Stream), model)
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
	if call == 1 && p.toolName != "" {
		return reconfigureStream{message: AssistantMessage{Provider: "test", Model: "reconfigure", StopReason: StopReasonToolUse, Content: []AssistantContent{ToolCall{ID: "captured_call", Name: p.toolName, Arguments: []byte(`{}`)}}}}
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

func TestAdditionalToolsAreIndependentOfBaseConfigurationAndCapturedRequests(t *testing.T) {
	model := resolvedTestModel(newReconfigureProviders(), "test/reconfigure")
	base := MustTool(Tool[struct{}]{Name: "base", Execute: testNoopTool[struct{}]})
	plugin := MustTool(Tool[struct{}]{Name: "demo__echo", Mode: ModeSequential, Execute: testNoopTool[struct{}]})
	droid, err := Spawn(t.Context(), "conversation_additional", Config{Model: model, SystemPrompt: "base", Tools: []AnyTool{base}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	captured := droid.sdk.currentRequestConfiguration()
	if err := droid.SetAdditionalTools([]AnyTool{plugin}, "\nplugin guidance"); err != nil {
		t.Fatal(err)
	}
	enriched := droid.sdk.currentRequestConfiguration()
	if len(captured.toolSchemas) != 1 || captured.systemPrompt != "base" || len(enriched.toolSchemas) != 2 || enriched.systemPrompt != "base\nplugin guidance" {
		t.Fatalf("captured/new config = %#v / %#v", captured, enriched)
	}
	if err := droid.Reconfigure(RequestConfiguration{SystemPrompt: "changed", Reasoning: "high", Tools: []AnyTool{base}}); err != nil {
		t.Fatal(err)
	}
	current := droid.sdk.currentRequestConfiguration()
	if current.systemPrompt != "changed\nplugin guidance" || current.reasoning != "high" || len(current.toolSchemas) != 2 {
		t.Fatalf("reconfigured = %#v", current)
	}
	if err := droid.SetAdditionalTools([]AnyTool{base}, ""); err == nil {
		t.Fatal("additional tool shadowed base tool")
	}
	if droid.sdk.currentRequestConfiguration() != current {
		t.Fatal("failed catalog replaced current config")
	}
	if err := droid.SetAdditionalTools(nil, ""); err != nil {
		t.Fatal(err)
	}
	cleared := droid.sdk.currentRequestConfiguration()
	if len(cleared.toolSchemas) != 1 || cleared.systemPrompt != "changed" || len(enriched.toolSchemas) != 2 {
		t.Fatalf("clear altered captured config: %#v", cleared)
	}
}

func TestAdditionalToolsPublishAtNextModelRequestBoundary(t *testing.T) {
	providers := newReconfigureProviders()
	droid, err := Spawn(t.Context(), "conversation_live_tools", Config{Model: resolvedTestModel(providers, "test/reconfigure")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	first, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "first"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	tool := MustTool(Tool[struct{}]{Name: "demo__live", Execute: testNoopTool[struct{}]})
	if err := droid.SetAdditionalTools([]AnyTool{tool}, "tool guidance"); err != nil {
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
	if len(requests) != 2 || len(requests[0].Tools) != 0 || len(requests[1].Tools) != 1 || requests[1].Tools[0].Name != "demo__live" || requests[1].SystemPrompt != "tool guidance" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestInFlightToolResponseCannotRetargetReplacementRegistration(t *testing.T) {
	providers := newReconfigureProviders()
	providers.toolName = "demo__owned"
	droid, err := Spawn(t.Context(), "conversation_owned_tools", Config{Model: resolvedTestModel(providers, "test/reconfigure")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	var oldCalls, replacementCalls atomic.Int32
	var record durableTool
	old := MustTool(Tool[struct{}]{Name: "demo__owned", RegistrationID: "old", Execute: func(context.Context, ToolContext, struct{}, ToolUpdate) (ToolResult, error) {
		oldCalls.Add(1)
		droid.sdk.mu.Lock()
		for _, tool := range droid.sdk.state.Tools {
			if tool.Call.Name == "demo__owned" {
				record = tool
				break
			}
		}
		droid.sdk.mu.Unlock()
		return ToolResult{}, errors.New("owner revoked")
	}})
	replacement := MustTool(Tool[struct{}]{Name: "demo__owned", RegistrationID: "replacement", Execute: func(context.Context, ToolContext, struct{}, ToolUpdate) (ToolResult, error) {
		replacementCalls.Add(1)
		return ToolText("wrong callback"), nil
	}})
	if err := droid.SetAdditionalTools([]AnyTool{old}, ""); err != nil {
		t.Fatal(err)
	}
	turn, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "invoke captured tool"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if err := droid.SetAdditionalTools([]AnyTool{replacement}, ""); err != nil {
		t.Fatal(err)
	}
	close(providers.release)
	if _, err := turn.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if oldCalls.Load() != 1 || replacementCalls.Load() != 0 {
		t.Fatalf("callbacks old=%d replacement=%d", oldCalls.Load(), replacementCalls.Load())
	}
	droid.sdk.mu.Lock()
	droid.sdk.state.Tools[record.ID] = record
	// Simulate recovery: no live model-request callback snapshot survives.
	droid.sdk.modelRequestConfig = nil
	droid.sdk.mu.Unlock()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var recovered durableTool
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.RegistrationID != "old" {
		t.Fatalf("durable owner = %s", encoded)
	}
	result := droid.sdk.invokeTool(t.Context(), ToolContext{ToolCallID: record.ID}, ToolCall{ID: record.ID, Name: "demo__owned", Arguments: []byte(`{}`)})
	if !result.IsError || replacementCalls.Load() != 0 {
		t.Fatalf("recovered call retargeted: %#v", result)
	}
}
