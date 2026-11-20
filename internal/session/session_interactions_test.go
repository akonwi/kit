package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

const interactionTestSession = "session_0123456789abcdef0123456789abcdef"

var interactionTestOwner = PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}

type interactionTestStream struct{ message droids.AssistantMessage }

func (s interactionTestStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s interactionTestStream) Result() droids.AssistantMessage { return s.message }

// interactionHarness is a session's droid and interactions, whose model calls
// the probe tool once per prompt.
type interactionHarness struct {
	droid        *droids.Droid
	interactions *sessionInteractions
	mu           sync.Mutex
	events       []NewEvent
}

func newInteractionHarness(t *testing.T, probe func(context.Context, droids.ToolContext) string, hook droids.BeforeToolCallHook) *interactionHarness {
	t.Helper()
	harness := &interactionHarness{}
	harness.interactions = newSessionInteractions(interactionTestSession, func(event NewEvent) {
		harness.mu.Lock()
		defer harness.mu.Unlock()
		harness.events = append(harness.events, event)
	})
	baseModel := droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
	model, err := droids.BindModel(droids.AdaptProvider("test", []droids.Model{baseModel}, func(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
		message := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
		if _, ok := request.Messages[len(request.Messages)-1].(droids.UserMessage); ok {
			message.StopReason = droids.StopReasonToolUse
			message.Content = []droids.AssistantContent{droids.ToolCall{ID: "probe_call", Name: "probe", Arguments: json.RawMessage(`{}`)}}
		}
		return interactionTestStream{message: message}
	}), baseModel)
	if err != nil {
		t.Fatal(err)
	}
	tool := droids.MustTool(droids.Tool[struct{}]{Name: "probe", Execute: func(ctx context.Context, call droids.ToolContext, _ struct{}, _ droids.ToolUpdate) (droids.ToolResult, error) {
		return droids.ToolText(probe(ctx, call)), nil
	}})
	config := droids.Config{Store: droids.NewMemoryStore(), Model: model, Tools: []droids.AnyTool{tool}}
	if hook != nil {
		config.BeforeToolCall = hook
		config.BeforeToolCallIdentity = func() string { return "policy" }
	}
	harness.droid, err = droids.Spawn(t.Context(), droids.ConversationID(interactionTestSession), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = harness.droid.Close() })
	harness.interactions.setDroid(harness.droid)
	return harness
}

func (h *interactionHarness) prompt(t *testing.T, text string, steer bool) droids.ExecutionHandle {
	t.Helper()
	handle, err := h.droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: text}}}, droids.PromptOptions{Steer: steer})
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func (h *interactionHarness) waitPending(t *testing.T) InteractionRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, err := h.interactions.pending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 {
			return pending[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending = %#v", pending)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *interactionHarness) respond(t *testing.T, response InteractionResponse) {
	t.Helper()
	if err := respondDroidInteraction(t.Context(), h.interactions, response); err != nil {
		t.Fatal(err)
	}
}

func (h *interactionHarness) published() []NewEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]NewEvent(nil), h.events...)
}

func TestMCPAuthorizationIsAQuestionOnTheToolCall(t *testing.T) {
	type outcome struct {
		value string
		err   error
	}
	results := make(chan outcome, 1)
	harness := &interactionHarness{}
	harness = newInteractionHarness(t, func(ctx context.Context, call droids.ToolContext) string {
		value, err := mcpAuthorizer(harness.interactions)(ctx, call, testAuthorizationRequest())
		results <- outcome{value, err}
		return "authorized"
	}, nil)

	handle := harness.prompt(t, "connect", false)
	pending := harness.waitPending(t)
	want := InteractionRequest{ID: pending.ID, SessionID: interactionTestSession, RunID: string(handle.TurnID()), ToolCallID: pending.ToolCallID,
		Kind: InteractionAuthorize, Title: "Authorize the docs MCP server",
		URL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://127.0.0.1:53173/mcp/callback", CreatedAt: pending.CreatedAt}
	if pending.ToolCallID == "" || !reflect.DeepEqual(pending, want) {
		t.Fatalf("pending = %#v\nwant %#v", pending, want)
	}
	redirect := "http://127.0.0.1:53173/mcp/callback?code=c&state=s"
	harness.respond(t, InteractionResponse{RequestID: pending.ID, Value: &redirect})
	if got := <-results; got.err != nil || got.value != redirect {
		t.Fatalf("authorize = %q, %v", got.value, got.err)
	}
	if _, err := handle.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}

	harness.prompt(t, "connect again", false)
	pending = harness.waitPending(t)
	harness.respond(t, InteractionResponse{RequestID: pending.ID, Cancelled: true})
	if got := <-results; !errors.Is(got.err, ErrAuthorizationCancelled) {
		t.Fatalf("declined authorize error = %v", got.err)
	}
	// The run announces a question on its tool call; nothing is published
	// outside it.
	if published := harness.published(); len(published) != 0 {
		t.Fatalf("published = %#v", published)
	}
}

func TestMCPAuthorizationEndsWithItsContext(t *testing.T) {
	errs := make(chan error, 1)
	harness := &interactionHarness{}
	harness = newInteractionHarness(t, func(ctx context.Context, call droids.ToolContext) string {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		_, err := mcpAuthorizer(harness.interactions)(ctx, call, testAuthorizationRequest())
		errs <- err
		return "timed out"
	}, nil)
	harness.prompt(t, "connect", false)
	if err := <-errs; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out error = %v", err)
	}
	if pending, err := harness.interactions.pending(t.Context()); err != nil || len(pending) != 0 {
		t.Fatalf("pending after timeout = %#v, %v", pending, err)
	}
}

func TestAPluginDialogOutsideATurnIsAnnouncedAndMapsOptionsToTheirIndex(t *testing.T) {
	harness := newInteractionHarness(t, func(context.Context, droids.ToolContext) string { return "" }, nil)
	result := make(chan PluginInteractionResult, 1)
	go func() {
		answer, err := harness.interactions.askPlugin(t.Context(), PluginInteractionInput{Owner: interactionTestOwner, Kind: InteractionSelect, Title: "Select", Options: []PluginInteractionOption{{Label: "First"}, {Label: "Second"}}}, func() bool { return true })
		if err != nil {
			t.Error(err)
		}
		result <- answer
	}()
	request := harness.waitPending(t)
	if request.Plugin == nil || *request.Plugin != interactionTestOwner || request.RunID != "" || request.ToolCallID != "" || len(request.Options) != 2 {
		t.Fatalf("request = %#v", request)
	}
	harness.respond(t, InteractionResponse{RequestID: request.ID, SelectedOptionID: request.Options[1].ID})
	if answer := <-result; answer.OptionIndex != 1 || answer.Cancelled {
		t.Fatalf("selected answer = %#v", answer)
	}
	published := harness.published()
	if len(published) != 2 || published[0].Kind != EventInteractionRequested || published[0].Interaction.ID != request.ID ||
		published[1].Kind != EventInteractionResolved || published[1].InteractionID != request.ID || published[1].InteractionResolution != "answered" {
		t.Fatalf("published = %#v", published)
	}
	for _, event := range published {
		if err := event.Validate(); err != nil {
			t.Fatalf("event %s is invalid: %v", event.Kind, err)
		}
	}
	if err := respondDroidInteraction(t.Context(), harness.interactions, InteractionResponse{RequestID: request.ID, SelectedOptionID: request.Options[0].ID}); !errors.Is(err, ErrInteractionSettled) {
		t.Fatalf("second answer = %v", err)
	}
}

func TestAPluginDialogIsHiddenAndRefusedOnceItsGenerationIsGone(t *testing.T) {
	harness := newInteractionHarness(t, func(context.Context, droids.ToolContext) string { return "" }, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var available sync.Mutex
	active := true
	errs := make(chan error, 1)
	go func() {
		_, err := harness.interactions.askPlugin(ctx, PluginInteractionInput{Owner: interactionTestOwner, Kind: InteractionConfirm, Title: "Continue?"}, func() bool {
			available.Lock()
			defer available.Unlock()
			return active
		})
		errs <- err
	}()
	request := harness.waitPending(t)
	available.Lock()
	active = false
	available.Unlock()
	if pending, err := harness.interactions.pending(t.Context()); err != nil || len(pending) != 0 {
		t.Fatalf("revoked pending = %#v, %v", pending, err)
	}
	confirmed := true
	if err := respondDroidInteraction(t.Context(), harness.interactions, InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); !errors.Is(err, ErrInteractionSettled) {
		t.Fatalf("revoked answer = %v", err)
	}
	cancel() // Revoking the generation also cancels its request.
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("revoked dialog error = %v", err)
	}
	published := harness.published()
	if last := published[len(published)-1]; last.Kind != EventInteractionResolved || last.InteractionResolution != "unavailable" {
		t.Fatalf("published = %#v", published)
	}
}

func TestAHookDialogBelongsToTheTurnAndSteeringDeclinesIt(t *testing.T) {
	results := make(chan PluginInteractionResult, 1)
	harness := &interactionHarness{}
	harness = newInteractionHarness(t, func(context.Context, droids.ToolContext) string { return "ran" },
		func(ctx context.Context, call droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
			answer, err := harness.interactions.askPlugin(withHookCall(ctx, call), PluginInteractionInput{Owner: interactionTestOwner, Kind: InteractionConfirm, Title: "Allow probe?"}, func() bool { return true })
			if err != nil {
				return droids.BeforeToolResult{}, err
			}
			results <- answer
			return droids.BeforeToolResult{Reject: !answer.Confirmed, Reason: "the plugin refused"}, nil
		})
	harness.prompt(t, "go", false)
	request := harness.waitPending(t)
	// On the wire it is the plugin's dialog, as a dialog outside a turn is.
	if request.Plugin == nil || request.RunID != "" || request.ToolCallID != "" {
		t.Fatalf("request = %#v", request)
	}
	interactions, err := harness.droid.Interactions(t.Context())
	if err != nil || len(interactions) != 1 || interactions[0].TurnID == "" || interactions[0].ToolCallID == "" {
		t.Fatalf("droid interactions = %#v, %v; want the dialog on the turn", interactions, err)
	}
	harness.prompt(t, "never mind", true)
	if answer := <-results; !answer.Cancelled {
		t.Fatalf("steered answer = %#v, want declined", answer)
	}
	if published := harness.published(); len(published) != 0 {
		t.Fatalf("published = %#v; the run announces the turn's dialogs", published)
	}
}
