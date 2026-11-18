package droids_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
)

// interactionProviders answers each request with the next scripted assistant
// message, then with "done", and records the tool results the model saw.
type interactionProviders struct {
	*readProviders
	mu       sync.Mutex
	script   []droids.AssistantMessage
	seen     []droids.ToolResultMessage
	requests []droids.Request
}

func newInteractionProviders(script ...droids.AssistantMessage) *interactionProviders {
	return &interactionProviders{readProviders: newReadProviders(), script: script}
}

func (p *interactionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *interactionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	p.seen = nil
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok {
			p.seen = append(p.seen, result)
		}
	}
	if len(p.script) > 0 {
		next := p.script[0]
		p.script = p.script[1:]
		return sdkStaticStream(next)
	}
	return sdkStaticStream(droids.AssistantMessage{Provider: "test", Model: "read", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}})
}

// results returns the text of each tool result the model last saw, by the
// call ID the model gave.
func (p *interactionProviders) results() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	texts := make(map[string]string, len(p.seen))
	for _, result := range p.seen {
		for _, part := range result.Content {
			if text, ok := part.(droids.TextContent); ok {
				texts[result.ProviderCallID] += text.Text
			}
		}
	}
	return texts
}

func callTools(calls ...droids.ToolCall) droids.AssistantMessage {
	content := make([]droids.AssistantContent, 0, len(calls))
	for _, call := range calls {
		content = append(content, call)
	}
	return droids.AssistantMessage{Provider: "test", Model: "read", StopReason: droids.StopReasonToolUse, Content: content}
}

func toolCall(id, name, arguments string) droids.ToolCall {
	return droids.ToolCall{ID: droids.ToolCallID(id), Name: name, Arguments: json.RawMessage(arguments)}
}

type dangerArgs struct {
	Target string `json:"target"`
}

// countingTool runs by recording its target.
func countingTool(name string, runs *atomic.Int32, configure func(*droids.Tool[dangerArgs])) droids.AnyTool {
	tool := droids.Tool[dangerArgs]{Name: name, Execute: func(_ context.Context, _ droids.ToolContext, args dangerArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
		runs.Add(1)
		return droids.ToolText("ran on " + args.Target), nil
	}}
	if configure != nil {
		configure(&tool)
	}
	return droids.MustTool(tool)
}

func spawnInteractionDroid(t *testing.T, id droids.ConversationID, store droids.Store, providers *interactionProviders, tools ...droids.AnyTool) *droids.Droid {
	t.Helper()
	droid, err := droids.Spawn(t.Context(), id, droids.Config{Store: store, Model: resolvedTestModel(providers, "test/read"), Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	return droid
}

func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitPending waits for count pending interactions and returns them.
func waitPending(t *testing.T, droid *droids.Droid, count int) []droids.Interaction {
	t.Helper()
	var pending []droids.Interaction
	waitUntil(t, "interactions are pending", func() bool {
		var err error
		pending, err = droid.Interactions(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return len(pending) == count
	})
	return pending
}

func activeStatus(t *testing.T, droid *droids.Droid) droids.ExecutionStatus {
	t.Helper()
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Active == nil {
		return ""
	}
	return snapshot.Active.Status
}

func settle(t *testing.T, handle droids.ExecutionHandle) droids.Outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func yes() *bool { value := true; return &value }
func no() *bool  { value := false; return &value }

func TestRequiredConfirmationRunsTheToolOnlyAfterApproval(t *testing.T) {
	providers := newInteractionProviders(callTools(toolCall("call_1", "danger", `{"target":"prod"}`)))
	var runs atomic.Int32
	droid := spawnInteractionDroid(t, "confirm_approve", droids.NewMemoryStore(), providers,
		countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) { tool.RequireConfirmation = true }))
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	want := droids.Question{Kind: droids.QuestionConfirm, Title: "Allow danger?", Detail: "{\n  \"target\": \"prod\"\n}"}
	if got := pending[0]; got.Live || got.ToolCallID == "" || got.TurnID != handle.TurnID() || !reflectEqual(got.Question, want) {
		t.Fatalf("pending interaction = %+v; want the default confirmation for call_1", got)
	}
	waitUntil(t, "the turn waits", func() bool { return activeStatus(t, droid) == droids.ExecutionWaiting })
	if runs.Load() != 0 {
		t.Fatal("the tool ran before approval")
	}

	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	if outcome := settle(t, handle); outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if runs.Load() != 1 || providers.results()["call_1"] != "ran on prod" {
		t.Fatalf("runs = %d, results = %v; want one run on prod", runs.Load(), providers.results())
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); !errors.Is(err, droids.ErrInteractionNotFound) {
		t.Fatalf("answering again = %v, want not found", err)
	}
}

func TestDeniedConfirmationTellsTheModelWithoutRunningTheTool(t *testing.T) {
	for name, answer := range map[string]droids.Answer{"declined": {Declined: true}, "no": {Confirmed: no()}} {
		t.Run(name, func(t *testing.T) {
			providers := newInteractionProviders(callTools(toolCall("call_1", "danger", `{"target":"prod"}`)))
			var runs atomic.Int32
			droid := spawnInteractionDroid(t, droids.ConversationID("confirm_deny_"+name), droids.NewMemoryStore(), providers,
				countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) { tool.RequireConfirmation = true }))
			handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
			if err != nil {
				t.Fatal(err)
			}
			pending := waitPending(t, droid, 1)
			if err := droid.Respond(t.Context(), pending[0].ID, answer); err != nil {
				t.Fatal(err)
			}
			if outcome := settle(t, handle); outcome.Status != droids.ExecutionCompleted {
				t.Fatalf("outcome = %+v", outcome)
			}
			if runs.Load() != 0 || providers.results()["call_1"] != "Not run: the user declined." {
				t.Fatalf("runs = %d, results = %v", runs.Load(), providers.results())
			}
		})
	}
}

func TestConfirmDecidesPerCallAndFillsDefaults(t *testing.T) {
	providers := newInteractionProviders(callTools(
		toolCall("call_safe", "danger", `{"target":"dev"}`),
		toolCall("call_risky", "danger", `{"target":"prod"}`),
		toolCall("call_refused", "danger", `{"target":"nowhere"}`),
	))
	var runs atomic.Int32
	droid := spawnInteractionDroid(t, "confirm_callback", droids.NewMemoryStore(), providers,
		countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) {
			tool.Confirm = func(_ context.Context, _ droids.ToolContext, args dangerArgs) (*droids.Confirmation, error) {
				switch args.Target {
				case "dev":
					return nil, nil
				case "prod":
					return &droids.Confirmation{Detail: "Production has 3 live sessions.", ConfirmLabel: "Deploy"}, nil
				default:
					return nil, errors.New("unknown target nowhere")
				}
			}
		}))
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	want := droids.Question{Kind: droids.QuestionConfirm, Title: "Allow danger?", Detail: "Production has 3 live sessions.", ConfirmLabel: "Deploy"}
	if !reflectEqual(pending[0].Question, want) {
		t.Fatalf("pending = %+v; want only call_risky's confirmation", pending[0])
	}
	waitUntil(t, "the safe call ran", func() bool { return runs.Load() == 1 })
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	settle(t, handle)
	results := providers.results()
	if runs.Load() != 2 || results["call_safe"] != "ran on dev" || results["call_risky"] != "ran on prod" || results["call_refused"] != "unknown target nowhere" {
		t.Fatalf("runs = %d, results = %v", runs.Load(), results)
	}
}

func TestSequentialCallsAfterAnAwaitingCallWaitForIt(t *testing.T) {
	providers := newInteractionProviders(callTools(
		toolCall("call_first", "danger", `{"target":"first"}`),
		toolCall("call_second", "after", `{"target":"second"}`),
	))
	var dangerRuns, afterRuns atomic.Int32
	droid := spawnInteractionDroid(t, "confirm_sequential", droids.NewMemoryStore(), providers,
		countingTool("danger", &dangerRuns, func(tool *droids.Tool[dangerArgs]) {
			tool.RequireConfirmation = true
			tool.Mode = droids.ModeSequential
		}),
		countingTool("after", &afterRuns, nil))
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	waitUntil(t, "the turn waits", func() bool { return activeStatus(t, droid) == droids.ExecutionWaiting })
	if afterRuns.Load() != 0 {
		t.Fatal("a later sequential call ran before the earlier call was approved")
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	settle(t, handle)
	if dangerRuns.Load() != 1 || afterRuns.Load() != 1 {
		t.Fatalf("runs = %d, %d; want each call once", dangerRuns.Load(), afterRuns.Load())
	}
}

func TestAWaitingTurnSurvivesARestartAndContinuesWhenAnswered(t *testing.T) {
	providers := newInteractionProviders(callTools(toolCall("call_1", "danger", `{"target":"prod"}`)))
	store := droids.NewMemoryStore()
	var runs atomic.Int32
	tool := countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) { tool.RequireConfirmation = true })
	first := spawnInteractionDroid(t, "confirm_restart", store, providers, tool)
	handle, err := first.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitPending(t, first, 1)
	waitUntil(t, "the turn waits", func() bool { return activeStatus(t, first) == droids.ExecutionWaiting })
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	second := spawnInteractionDroid(t, "confirm_restart", store, providers, tool)
	if status := activeStatus(t, second); status != droids.ExecutionWaiting {
		t.Fatalf("restored status = %q, want waiting", status)
	}
	pending := waitPending(t, second, 1)
	if pending[0].Live || pending[0].ToolCallID == "" {
		t.Fatalf("restored interaction = %+v", pending[0])
	}
	resumed, err := second.Execution(t.Context(), handle.TurnID())
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	if outcome := settle(t, resumed); outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if runs.Load() != 1 || providers.results()["call_1"] != "ran on prod" {
		t.Fatalf("runs = %d, results = %v", runs.Load(), providers.results())
	}
}

func TestSteeringDeclinesTheTurnsPendingQuestions(t *testing.T) {
	providers := newInteractionProviders(callTools(toolCall("call_1", "danger", `{"target":"prod"}`)))
	var runs atomic.Int32
	droid := spawnInteractionDroid(t, "confirm_steer", droids.NewMemoryStore(), providers,
		countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) { tool.RequireConfirmation = true }))
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitPending(t, droid, 1)
	waitUntil(t, "the turn waits", func() bool { return activeStatus(t, droid) == droids.ExecutionWaiting })
	if _, err := droid.Prompt(t.Context(), textInput("use staging instead"), droids.PromptOptions{Steer: true}); err != nil {
		t.Fatal(err)
	}
	if outcome := settle(t, handle); outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if runs.Load() != 0 || providers.results()["call_1"] != "Not run: the user sent a message instead of answering." {
		t.Fatalf("runs = %d, results = %v", runs.Load(), providers.results())
	}
	if got := lastUserText(providers.requestsSeen()[len(providers.requestsSeen())-1]); got != "use staging instead" {
		t.Fatalf("last user message = %q, want the steering", got)
	}
	if pending, _ := droid.Interactions(t.Context()); len(pending) != 0 {
		t.Fatalf("pending after steering = %+v", pending)
	}
}

func TestAbortingAWaitingTurnCancelsItsQuestions(t *testing.T) {
	providers := newInteractionProviders(callTools(toolCall("call_1", "danger", `{"target":"prod"}`)))
	var runs atomic.Int32
	droid := spawnInteractionDroid(t, "confirm_abort", droids.NewMemoryStore(), providers,
		countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) { tool.RequireConfirmation = true }))
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	waitUntil(t, "the turn waits", func() bool { return activeStatus(t, droid) == droids.ExecutionWaiting })
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	if outcome := settle(t, handle); outcome.Status != droids.ExecutionAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if remaining, _ := droid.Interactions(t.Context()); len(remaining) != 0 || runs.Load() != 0 {
		t.Fatalf("after abort: pending = %+v, runs = %d", remaining, runs.Load())
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); !errors.Is(err, droids.ErrInteractionNotFound) {
		t.Fatalf("answering a cancelled question = %v", err)
	}
}

func TestAToolAsksALiveQuestionWhileItRuns(t *testing.T) {
	providers := newInteractionProviders(callTools(toolCall("call_1", "login", `{}`)))
	login := droids.MustTool(droids.Tool[struct{}]{Name: "login", Execute: func(ctx context.Context, _ droids.ToolContext, _ struct{}, _ droids.ToolUpdate) (droids.ToolResult, error) {
		settlement, err := droids.Ask(ctx, droids.Question{Kind: droids.QuestionInput, Title: "Paste the code"}, json.RawMessage(`{"server":"remote"}`))
		if err != nil {
			return droids.ToolResult{}, err
		}
		if settlement.Status != droids.InteractionAnswered {
			return droids.ToolText("no code: " + settlement.Reason), nil
		}
		return droids.ToolText("code " + *settlement.Answer.Value), nil
	}})
	droid := spawnInteractionDroid(t, "live_ask", droids.NewMemoryStore(), providers, login)
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	if got := pending[0]; !got.Live || got.ToolCallID == "" || got.TurnID != handle.TurnID() || string(got.Metadata) != `{"server":"remote"}` {
		t.Fatalf("live interaction = %+v", got)
	}
	if status := activeStatus(t, droid); status != droids.ExecutionRunning {
		t.Fatalf("status while a tool asks = %q, want running", status)
	}
	code := "1234"
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Value: &code}); err != nil {
		t.Fatal(err)
	}
	settle(t, handle)
	if got := providers.results()["call_1"]; got != "code 1234" {
		t.Fatalf("result = %q", got)
	}
}

func TestALiveQuestionOutsideATurnEndsUnavailableAcrossARestart(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := newInteractionProviders()
	first := spawnInteractionDroid(t, "live_restart", store, providers)
	asked := make(chan droids.Settlement, 1)
	go func() {
		settlement, _ := first.Ask(context.Background(), droids.Question{Kind: droids.QuestionConfirm, Title: "Install the plugin?"}, droids.AskOptions{})
		asked <- settlement
	}()
	pending := waitPending(t, first, 1)
	if pending[0].TurnID != "" || !pending[0].Live {
		t.Fatalf("question outside a turn = %+v", pending[0])
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case settlement := <-asked:
		if settlement.Status != droids.InteractionUnavailable {
			t.Fatalf("settlement at shutdown = %+v", settlement)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ask did not return at shutdown")
	}
	second := spawnInteractionDroid(t, "live_restart", store, providers)
	if remaining, err := second.Interactions(t.Context()); err != nil || len(remaining) != 0 {
		t.Fatalf("pending after restart = %+v, %v", remaining, err)
	}
}

func TestRespondChecksTheAnswerAgainstTheQuestion(t *testing.T) {
	droid := spawnInteractionDroid(t, "respond_validation", droids.NewMemoryStore(), newInteractionProviders())
	asked := make(chan droids.Settlement, 1)
	question := droids.Question{Kind: droids.QuestionSelect, Title: "Which branch?", Options: []droids.Option{{ID: "a", Label: "main", Value: "main"}, {ID: "b", Label: "dev", Value: "dev"}}}
	go func() {
		settlement, _ := droid.Ask(context.Background(), question, droids.AskOptions{})
		asked <- settlement
	}()
	pending := waitPending(t, droid, 1)
	text := "main"
	for name, answer := range map[string]droids.Answer{
		"unknown option": {OptionID: "c"},
		"wrong kind":     {Value: &text},
		"nothing":        {},
		"declined+value": {Declined: true, OptionID: "a"},
	} {
		if err := droid.Respond(t.Context(), pending[0].ID, answer); !errors.Is(err, droids.ErrInvalidInteraction) {
			t.Fatalf("%s: Respond = %v, want invalid", name, err)
		}
	}
	if err := droid.Respond(t.Context(), "interaction_00000000000000000000000000000000", droids.Answer{OptionID: "a"}); !errors.Is(err, droids.ErrInteractionNotFound) {
		t.Fatalf("unknown interaction = %v", err)
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{OptionID: "b"}); err != nil {
		t.Fatal(err)
	}
	if settlement := <-asked; settlement.Status != droids.InteractionAnswered || settlement.Answer.OptionID != "b" {
		t.Fatalf("settlement = %+v", settlement)
	}
}

func (p *interactionProviders) requestsSeen() []droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.requests)
}

func reflectEqual(a, b droids.Question) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func TestABeforeToolHookDecidesBeforeTheToolsConfirmationAndCanAsk(t *testing.T) {
	providers := newInteractionProviders(callTools(
		toolCall("call_blocked", "danger", `{"target":"blocked"}`),
		toolCall("call_allowed", "danger", `{"target":"allowed"}`),
	))
	var runs atomic.Int32
	hook := func(ctx context.Context, _ droids.ToolContext, call droids.ToolCall) (droids.BeforeToolResult, error) {
		var args dangerArgs
		_ = json.Unmarshal(call.Arguments, &args)
		if args.Target == "blocked" {
			return droids.BeforeToolResult{Reject: true, Reason: "The plugin blocked it."}, nil
		}
		settlement, err := droids.Ask(ctx, droids.Question{Kind: droids.QuestionConfirm, Title: "Plugin: allow?"}, nil)
		if err != nil || settlement.Status != droids.InteractionAnswered || !*settlement.Answer.Confirmed {
			return droids.BeforeToolResult{Reject: true, Reason: "The plugin's question was declined."}, err
		}
		return droids.BeforeToolResult{}, nil
	}
	droid, err := droids.Spawn(t.Context(), "confirm_hook", droids.Config{
		Store: droids.NewMemoryStore(), Model: resolvedTestModel(providers, "test/read"), BeforeToolCall: hook,
		Tools: []droids.AnyTool{countingTool("danger", &runs, func(tool *droids.Tool[dangerArgs]) {
			tool.RequireConfirmation = true
			tool.Mode = droids.ModeSequential
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), textInput("go"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The blocked call never asks; the allowed call's hook asks live first.
	pending := waitPending(t, droid, 1)
	if !pending[0].Live || pending[0].Question.Title != "Plugin: allow?" {
		t.Fatalf("first question = %+v; want the hook's live question", pending[0])
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	// Then the tool's own confirmation.
	waitUntil(t, "the tool's confirmation is asked", func() bool {
		pending, _ = droid.Interactions(t.Context())
		return len(pending) == 1 && !pending[0].Live
	})
	if pending[0].Question.Title != "Allow danger?" {
		t.Fatalf("second question = %+v", pending[0])
	}
	if err := droid.Respond(t.Context(), pending[0].ID, droids.Answer{Confirmed: yes()}); err != nil {
		t.Fatal(err)
	}
	settle(t, handle)
	results := providers.results()
	if runs.Load() != 1 || results["call_blocked"] != "The plugin blocked it." || results["call_allowed"] != "ran on allowed" {
		t.Fatalf("runs = %d, results = %v", runs.Load(), results)
	}
}
