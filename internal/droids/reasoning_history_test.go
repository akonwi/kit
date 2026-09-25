package droids

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func reasoningTestDroid(t *testing.T, initial string) (*Droid, chan map[string]any, Config) {
	t.Helper()
	requests := make(chan map[string]any, 32)
	server := newResponsesServer(t, requests, `{"type":"response.completed","sequence_number":1,"response":{"id":"resp_fixture","object":"response","created_at":1,"model":"gpt-6-sol","status":"completed","output":[{"id":"msg_fixture","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Done","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}}`)
	t.Cleanup(server.Close)
	providers, err := NewProviders(OpenAI{APIKey: "test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("gpt-6-sol")
	if err != nil {
		t.Fatal(err)
	}
	model.SupportsReasoningConfigurationUpdates = true // Fixture emulates the public endpoint.
	config := Config{Store: NewMemoryStore(), Model: model, Reasoning: initial}
	d, err := Spawn(t.Context(), "reasoning_test", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, requests, config
}

func reasoningPrompt(t *testing.T, d *Droid, text string) {
	t.Helper()
	handle, err := d.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: text}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != ExecutionCompleted {
		t.Fatalf("prompt outcome: %+v, %v", outcome, err)
	}
}

func reasoningSelect(t *testing.T, d *Droid, effort string) {
	t.Helper()
	if err := d.ReconfigureContext(t.Context(), RequestConfiguration{Reasoning: effort}); err != nil {
		t.Fatal(err)
	}
}

func assertReasoningWire(t *testing.T, body map[string]any, baseline string, positions []int, efforts []string) {
	t.Helper()
	wantReasoning := map[string]any{}
	if baseline != "" {
		wantReasoning["effort"] = baseline
	}
	if baseline != "none" {
		wantReasoning["summary"] = "auto"
	}
	if !reflect.DeepEqual(body["reasoning"], wantReasoning) {
		t.Fatalf("reasoning = %#v, want %#v", body["reasoning"], wantReasoning)
	}
	input := body["input"].([]any)
	var actualPositions []int
	var actualEfforts []string
	for i, item := range input {
		value := item.(map[string]any)
		if value["type"] == "configuration_update" {
			actualPositions = append(actualPositions, i)
			actualEfforts = append(actualEfforts, value["reasoning"].(map[string]any)["effort"].(string))
			want := map[string]any{"type": "configuration_update", "reasoning": map[string]any{"effort": efforts[len(actualEfforts)-1]}}
			if !reflect.DeepEqual(value, want) {
				t.Fatalf("update = %#v, want %#v", value, want)
			}
			if i+1 >= len(input) || input[i+1].(map[string]any)["role"] != "user" {
				t.Fatalf("update %d not immediately before user: %#v", i, input)
			}
		}
	}
	if !reflect.DeepEqual(actualPositions, positions) || !reflect.DeepEqual(actualEfforts, efforts) {
		t.Fatalf("updates = %v / %v, want %v / %v; body=%#v", actualPositions, actualEfforts, positions, efforts, body)
	}
}

func TestReasoningHistoryRoundTripAndCoalescing(t *testing.T) {
	for _, baseline := range []string{"low", ""} {
		t.Run("baseline_"+baseline, func(t *testing.T) {
			d, requests, _ := reasoningTestDroid(t, baseline)
			reasoningPrompt(t, d, "first")
			first := <-requests
			assertReasoningWire(t, first, baseline, nil, nil)
			reasoningSelect(t, d, "medium")
			reasoningSelect(t, d, "high")
			reasoningSelect(t, d, "high")
			reasoningPrompt(t, d, "second")
			second := <-requests
			assertReasoningWire(t, second, baseline, []int{2}, []string{"high"})
			if !reflect.DeepEqual(first["input"], second["input"].([]any)[:1]) {
				t.Fatal("original prefix changed")
			}
			reasoningSelect(t, d, "low")
			reasoningPrompt(t, d, "third")
			third := <-requests
			assertReasoningWire(t, third, baseline, []int{2, 5}, []string{"high", "low"})
			if !reflect.DeepEqual(second["input"], third["input"].([]any)[:4]) {
				t.Fatal("dispatched prefix changed")
			}
			reasoningSelect(t, d, "low")
			reasoningPrompt(t, d, "fourth")
			assertReasoningWire(t, <-requests, baseline, []int{2, 5}, []string{"high", "low"})
		})
	}
}

func TestReasoningHistoryFirstSelectionAndRevertedPendingAreNoUpdates(t *testing.T) {
	d, requests, _ := reasoningTestDroid(t, "low")
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "first")
	assertReasoningWire(t, <-requests, "high", nil, nil)
	reasoningSelect(t, d, "low")
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	assertReasoningWire(t, <-requests, "high", nil, nil)
}

func TestReasoningHistoryRestartAndForkPreservePendingIntent(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	reasoningSelect(t, d, "low")
	fork, err := d.Fork(t.Context(), "reasoning_fork", ForkOptions{Store: NewMemoryStore()})
	if err != nil {
		t.Fatal(err)
	}
	defer fork.Droid.Close()
	reasoningPrompt(t, fork.Droid, "fork third")
	assertReasoningWire(t, <-requests, "low", []int{2, 5}, []string{"high", "low"})
	reasoningSelect(t, d, "medium")
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := config.Store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Spawn(t.Context(), "reasoning_test", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := config.Store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != after.Revision {
		t.Fatalf("Spawn wrote state: %d -> %d", before.Revision, after.Revision)
	}
	reasoningPrompt(t, reopened, "source third")
	assertReasoningWire(t, <-requests, "low", []int{2, 5}, []string{"high", "medium"})
}

func TestReasoningHistoryNewEpochRetainsAuditHistory(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	before, err := config.Store.Records(t.Context(), RecordQuery{Kind: reasoningHistoryRecordKind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileReasoning(t.Context(), "medium", ResetReasoningEpoch); err != nil {
		t.Fatal(err)
	}
	reasoningPrompt(t, d, "third")
	assertReasoningWire(t, <-requests, "medium", nil, nil)
	after, err := config.Store.Records(t.Context(), RecordQuery{Kind: reasoningHistoryRecordKind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Records) != len(before.Records)+1 {
		t.Fatalf("audit record count %d -> %d", len(before.Records), len(after.Records))
	}
	for i, record := range before.Records {
		if !reflect.DeepEqual(record, after.Records[i]) {
			t.Fatal("audit record rewritten")
		}
	}
}

func TestOpenAIReasoningHistoryTemperatureUsesEffectiveEffort(t *testing.T) {
	_, model := testOpenAIProvider(t, "http://unused")
	model.ID = "gpt-6-sol"
	model.SupportsReasoningConfigurationUpdates = true
	model.TemperaturePolicy = TemperatureReasoningOff
	model.ReasoningLevels = []string{"none", "low", "high"}
	temp := 0.3
	for _, test := range []struct {
		baseline, effective string
		temperature         bool
	}{{"none", "high", false}, {"high", "none", true}} {
		request := Request{Reasoning: test.effective, Temperature: &temp, Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}}, ReasoningHistory: &ReasoningHistory{Baseline: test.baseline, Effective: test.effective, Updates: []ReasoningUpdate{{BeforeMessage: 0, Effort: test.effective}}}}
		params, err := buildOpenAIResponseParams(model, request)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		_, present := body["temperature"]
		if present != test.temperature {
			t.Fatalf("%s -> %s temperature = %#v", test.baseline, test.effective, body["temperature"])
		}
		assertReasoningWire(t, body, test.baseline, []int{0}, []string{test.effective})
	}
}

func TestReasoningHistoryWaitsForUserAfterInflightToolContinuation(t *testing.T) {
	provider := newReconfigureProviders()
	provider.toolName = "read"
	model, _ := OpenAIModel("gpt-6-sol")
	model.Provider = "test"
	model, err := BindModel(AdaptProvider("test", []Model{model}, func(ctx context.Context, model Model, request Request) Stream {
		s := provider.Stream(ctx, model, request)
		message := s.Result()
		message.Provider = model.Provider
		message.Model = model.ID
		return reconfigureStream{message: message}
	}), model)
	if err != nil {
		t.Fatal(err)
	}
	tool := MustTool(Tool[struct{}]{Name: "read", Execute: testNoopTool[struct{}]})
	d, err := Spawn(t.Context(), "reasoning_inflight", Config{Store: NewMemoryStore(), Model: model, Reasoning: "low", Tools: []AnyTool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	execution, err := d.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "first"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-provider.started
	if err := d.ReconfigureContext(t.Context(), RequestConfiguration{Reasoning: "high", Tools: []AnyTool{tool}}); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	if outcome, err := execution.Wait(t.Context()); err != nil || outcome.Status != ExecutionCompleted {
		t.Fatalf("outcome: %+v, %v, error=%+v", outcome, err, outcome.Error)
	}
	requests := provider.Requests()
	want := &ReasoningHistory{Baseline: "low", Effective: "low"}
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for i, request := range requests {
		if request.Reasoning != "low" || !reflect.DeepEqual(request.ReasoningHistory, want) {
			t.Fatalf("request %d mutated or prematurely updated: %+v", i, request)
		}
	}
	reasoningPrompt(t, d, "second")
	requests = provider.Requests()
	want = &ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 4, Effort: "high"}}}
	if requests[2].Reasoning != "high" || !reflect.DeepEqual(requests[2].ReasoningHistory, want) {
		t.Fatalf("next user request = %+v", requests[2])
	}
	if !reflect.DeepEqual(requests[0].ReasoningHistory, &ReasoningHistory{Baseline: "low", Effective: "low"}) {
		t.Fatal("captured initial history changed")
	}
}

func TestReasoningHistoryCompactionEstablishesFreshBaseline(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	reasoningSelect(t, d, "medium")
	result, err := d.CompactContext(t.Context(), CompactContextOptions{OperationID: "reasoning_compact", Target: ContextTarget{Model: config.Model, Reasoning: "medium"}, Force: true})
	if err != nil || !result.Compacted {
		t.Fatalf("compaction: %+v, %v", result, err)
	}
	for len(requests) > 0 {
		summary := <-requests
		assertReasoningWire(t, summary, "", nil, nil)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Spawn(t.Context(), "reasoning_test", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reasoningPrompt(t, reopened, "third")
	assertReasoningWire(t, <-requests, "medium", nil, nil)
	records, err := config.Store.Records(t.Context(), RecordQuery{Kind: reasoningHistoryRecordKind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 3 {
		t.Fatalf("audit records = %d, want initial baseline, high update, new baseline", len(records.Records))
	}
}

func TestReasoningHistoryCapabilityGate(t *testing.T) {
	for _, test := range []struct {
		id, base string
		want     bool
	}{{"", "", true}, {"company-openai", "", true}, {"", "https://api.openai.com/v1/", true}, {"", "https://gateway.invalid/v1", false}} {
		providers, err := NewProviders(OpenAI{ID: test.id, APIKey: "test", BaseURL: test.base})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5-mini"} {
			model, err := providers.Resolve(id)
			if err != nil {
				t.Fatal(err)
			}
			want := test.want && id != "gpt-5-mini"
			if got := supportsOpenAIReasoningHistory(model); got != want {
				t.Fatalf("%s / %s / %s capability=%v, want %v", test.id, test.base, id, got, want)
			}
		}
	}
	for _, model := range OpenAICodexModels() {
		if supportsOpenAIReasoningHistory(model) {
			t.Fatalf("Codex enabled for %s", model.ID)
		}
		if _, err := buildOpenAICodexResponseParams(model, Request{ReasoningHistory: &ReasoningHistory{}}); err == nil {
			t.Fatal("Codex accepted public history")
		}
	}
	for _, model := range AnthropicModels() {
		if supportsOpenAIReasoningHistory(model) {
			t.Fatalf("Anthropic enabled for %s", model.ID)
		}
	}
}

func TestReasoningHistoryRejectsCorruptDurablePositions(t *testing.T) {
	d, requests, _ := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	state, err := cloneDurableRuntime(d.sdk.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOpenedRuntime(state); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*durableRuntime)
	}{
		{"empty context", func(s *durableRuntime) {
			s.Context = nil
			s.ReasoningHistory.Updates = nil
			s.ReasoningHistory.Effective = s.ReasoningHistory.Baseline
		}},
		{"unspecified requested", func(s *durableRuntime) { s.ReasoningHistory.Requested = "" }},
		{"cursor", func(s *durableRuntime) { s.ReasoningHistory.DispatchedThrough = "missing" }},
		{"effort", func(s *durableRuntime) { s.ReasoningHistory.Requested = "unknown" }},
		{"model", func(s *durableRuntime) { s.ReasoningHistory.Model = "openai/unknown" }},
		{"effective", func(s *durableRuntime) { s.ReasoningHistory.Effective = "low" }},
		{"unstarted", func(s *durableRuntime) { s.ReasoningHistory.Started = false }},
		{"missing", func(s *durableRuntime) { s.ReasoningHistory.Updates[0].BeforeMessage = "missing" }},
		{"assistant", func(s *durableRuntime) { s.ReasoningHistory.Updates[0].BeforeMessage = s.Context[1].ID }},
		{"duplicate", func(s *durableRuntime) {
			s.ReasoningHistory.Updates = append(s.ReasoningHistory.Updates, s.ReasoningHistory.Updates[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy, err := cloneDurableRuntime(state)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&copy)
			if err := validateOpenedRuntime(copy); err == nil {
				t.Fatal("accepted corrupted history")
			}
		})
	}
}

func TestReasoningHistoryLegacyBootstrapDoesNotCompactOrDeleteContext(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	// Emulate a pre-capability conversation with no durable effort metadata.
	d.model.SupportsReasoningConfigurationUpdates = false
	reasoningPrompt(t, d, "legacy first")
	first := <-requests
	assertReasoningWire(t, first, "low", nil, nil)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	config.Reasoning = "high"
	reopened, err := Spawn(t.Context(), "reasoning_test", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reasoningPrompt(t, reopened, "new second")
	second := <-requests
	assertReasoningWire(t, second, "high", nil, nil)
	if len(second["input"].([]any)) != 3 || !reflect.DeepEqual(first["input"], second["input"].([]any)[:1]) {
		t.Fatalf("legacy context not preserved: %#v", second)
	}
}

func TestReasoningHistoryCanceledSelectionRollsBackDurableAndLiveIntent(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.ReconfigureContext(ctx, RequestConfiguration{Reasoning: "high"}); err == nil {
		t.Fatal("canceled selection succeeded")
	}
	reasoningPrompt(t, d, "second")
	assertReasoningWire(t, <-requests, "low", nil, nil)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Spawn(t.Context(), "reasoning_test", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reasoningPrompt(t, reopened, "third")
	assertReasoningWire(t, <-requests, "low", nil, nil)
}

func TestOpenAIReasoningHistoryRejectsMalformedProjection(t *testing.T) {
	model, _ := OpenAIModel("gpt-6-sol")
	user := UserMessage{Content: []InputContent{TextInput{Text: "hello"}}}
	for _, test := range []struct {
		name     string
		history  ReasoningHistory
		messages []Message
	}{
		{"missing anchor", ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 1, Effort: "high"}}}, []Message{user}},
		{"adjacent updates", ReasoningHistory{Baseline: "low", Effective: "low", Updates: []ReasoningUpdate{{BeforeMessage: 0, Effort: "high"}, {BeforeMessage: 0, Effort: "low"}}}, []Message{user}},
		{"assistant anchor", ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 0, Effort: "high"}}}, []Message{AssistantMessage{Content: []AssistantContent{TextContent{Text: "hello"}}}}},
		{"unspecified update", ReasoningHistory{Baseline: "low", Updates: []ReasoningUpdate{{BeforeMessage: 0}}}, []Message{user}},
		{"noncanonical update", ReasoningHistory{Baseline: "low", Effective: "off", Updates: []ReasoningUpdate{{BeforeMessage: 0, Effort: "off"}}}, []Message{user}},
		{"effective mismatch", ReasoningHistory{Baseline: "low", Effective: "high"}, []Message{user}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildOpenAIResponseParams(model, Request{Messages: test.messages, Reasoning: test.history.Effective, ReasoningHistory: &test.history}); err == nil {
				t.Fatal("malformed history accepted")
			}
		})
	}
}

func reasoningScriptModel(t *testing.T, callback func(context.Context, Model, Request) Stream) Model {
	t.Helper()
	model, _ := OpenAIModel("gpt-6-sol")
	model.Provider = "test"
	bound, err := BindModel(AdaptProvider("test", []Model{model}, callback), model)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func TestReasoningHistoryCrashAfterDispatchReplaysAnchoredUpdate(t *testing.T) {
	requests := make(chan Request, 8)
	var calls atomic.Int32
	model := reasoningScriptModel(t, func(ctx context.Context, model Model, request Request) Stream {
		requests <- request
		if calls.Add(1) == 2 {
			<-ctx.Done()
		}
		return reconfigureStream{message: AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "Done"}}}}
	})
	recorder := &replayRecordingProvider{Provider: model.boundProvider()}
	bound, err := BindModel(recorder, model)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Store: NewMemoryStore(), Model: bound, Reasoning: "low"}
	d, err := Spawn(t.Context(), "reasoning_crash", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	if _, err := d.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "second"}}}, PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	dispatched := <-requests
	d.sdk.mu.Lock()
	crash, err := cloneDurableRuntime(d.sdk.state)
	d.sdk.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if crash.CyclePhase != cycleModelStarted {
		t.Fatalf("phase = %s", crash.CyclePhase)
	}
	// Snapshot all records and outbox state before shutdown can settle the turn.
	// The copy models the store left by an abruptly terminated process.
	source := config.Store.(*MemoryStore)
	source.mu.Lock()
	snapshot := NewMemoryStore()
	snapshot.opened, snapshot.id, snapshot.revision = source.opened, source.id, source.revision
	snapshot.lastRecordSequence, snapshot.lastEventSequence = source.lastRecordSequence, source.lastEventSequence
	snapshot.records = cloneRecordMap(source.records)
	for _, event := range source.events {
		snapshot.events = append(snapshot.events, cloneStoredEvent(event))
	}
	source.mu.Unlock()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	config.Store = snapshot
	reopened, err := Spawn(t.Context(), "reasoning_crash", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	validatedBefore, _ := recorder.recorded()
	if err := reopened.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	validatedAfter, _ := recorder.recorded()
	if len(validatedAfter) <= len(validatedBefore) {
		t.Fatal("resume did not validate the interrupted request")
	}
	if !reflect.DeepEqual(validatedAfter[len(validatedBefore)], dispatched) {
		t.Fatalf("resume projection = %#v, want dispatched %#v", validatedAfter[len(validatedBefore)], dispatched)
	}
	if _, err := reopened.WaitQuiescent(t.Context()); err != nil {
		t.Fatal(err)
	}
	retried := <-requests
	if !reflect.DeepEqual(retried.ReasoningHistory, dispatched.ReasoningHistory) || !reflect.DeepEqual(retried.Messages, dispatched.Messages) {
		t.Fatalf("retry changed request: %#v / %#v", dispatched, retried)
	}
	want := &ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}}}
	if !reflect.DeepEqual(retried.ReasoningHistory, want) {
		t.Fatalf("replayed history = %#v", retried.ReasoningHistory)
	}
	records, err := config.Store.Records(t.Context(), RecordQuery{Kind: reasoningHistoryRecordKind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 2 {
		t.Fatalf("audit records = %d, want baseline and one update", len(records.Records))
	}
}

func TestReasoningHistoryAutomaticOverflowCompaction(t *testing.T) {
	requests := make(chan Request, 32)
	var calls atomic.Int32
	model := reasoningScriptModel(t, func(_ context.Context, model Model, request Request) Stream {
		requests <- request
		stop := StopReasonStop
		if calls.Add(1) == 3 {
			stop = StopReasonContextWindow
		}
		return reconfigureStream{message: AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: stop, Content: []AssistantContent{TextContent{Text: "Summary or answer"}}}}
	})
	config := Config{Store: NewMemoryStore(), Model: model, Reasoning: "low"}
	d, err := Spawn(t.Context(), "reasoning_overflow", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	reasoningSelect(t, d, "medium")
	reasoningPrompt(t, d, "triggering third user")
	overflow := <-requests
	if len(overflow.ReasoningHistory.Updates) != 2 {
		t.Fatalf("overflow history = %#v", overflow.ReasoningHistory)
	}
	var retry Request
	for len(requests) > 0 {
		retry = <-requests
	}
	want := &ReasoningHistory{Baseline: "medium", Effective: "medium"}
	if retry.ReasoningHistory == nil || retry.ReasoningHistory.Baseline != want.Baseline || retry.ReasoningHistory.Effective != want.Effective || len(retry.ReasoningHistory.Updates) != 0 {
		t.Fatalf("compacted history = %#v", retry.ReasoningHistory)
	}
	last, ok := retry.Messages[len(retry.Messages)-1].(UserMessage)
	if !ok || !reflect.DeepEqual(last.Content, []InputContent{TextInput{Text: "triggering third user"}}) {
		t.Fatalf("compacted tail = %#v", last)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Spawn(t.Context(), "reasoning_overflow", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reasoningPrompt(t, reopened, "after restart")
	replayed := <-requests
	if replayed.ReasoningHistory.Baseline != "medium" || replayed.ReasoningHistory.Effective != "medium" || len(replayed.ReasoningHistory.Updates) != 0 {
		t.Fatalf("restarted history = %#v", replayed.ReasoningHistory)
	}
	if !reflect.DeepEqual(replayed.Messages[:len(retry.Messages)], retry.Messages) {
		t.Fatal("restart changed compacted prefix")
	}
}

func TestReasoningHistoryCapabilitySurvivesCatalogRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"openai":{"models":{"gpt-6-sol":{"id":"gpt-6-sol","name":"Refreshed Sol","family":"gpt-sol","tool_call":true,"reasoning":true,"modalities":{"input":["text"],"output":["text"]},"limit":{"context":400000,"output":128000}}}}}`))
	}))
	defer server.Close()
	for _, test := range []struct {
		name, base string
		want       bool
	}{{"custom-public", "", true}, {"gateway", "https://gateway.invalid/v1", false}} {
		t.Run(test.name, func(t *testing.T) {
			providers, err := NewProviders(OpenAI{ID: test.name, BaseURL: test.base})
			if err != nil {
				t.Fatal(err)
			}
			if err := providers.(*registry).refreshModels(t.Context(), server.URL); err != nil {
				t.Fatal(err)
			}
			model, err := providers.Resolve(test.name + "/gpt-6-sol")
			if err != nil {
				t.Fatal(err)
			}
			if model.Name != "Refreshed Sol" || model.SupportsReasoningConfigurationUpdates != test.want {
				t.Fatalf("refreshed model = %#v", model)
			}
		})
	}
}

// replayRecordingProvider records every replay assessment it receives through
// the request-aware contract.
type replayRecordingProvider struct {
	Provider
	mu       sync.Mutex
	requests []Request
	messages [][]Message
}

func (p *replayRecordingProvider) ValidateReplay(ctx context.Context, model Model, messages []Message) error {
	p.mu.Lock()
	p.messages = append(p.messages, messages)
	p.mu.Unlock()
	return p.Provider.ValidateReplay(ctx, model, messages)
}

func (p *replayRecordingProvider) ValidateRequestReplay(_ context.Context, _ Model, request Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	return nil
}

func (p *replayRecordingProvider) recorded() ([]Request, [][]Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.requests...), append([][]Message(nil), p.messages...)
}

func TestReasoningHistoryTokenEstimateIncludesConfigurationUpdates(t *testing.T) {
	messages := []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "first"}}},
		AssistantMessage{StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "answer"}}},
		UserMessage{Content: []InputContent{TextInput{Text: "second"}}},
	}
	model, _ := OpenAIModel("gpt-6-sol")
	plain := estimateContextUsage("system", nil, model, "low", 1024, messages, nil)
	history := &ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}}}
	withUpdate := estimateContextUsage("system", nil, model, "high", 1024, messages, history)
	// The item length is even for "high", so it shifts the estimate exactly.
	item := len(`{"type":"configuration_update","reasoning":{"effort":""}}`) + len("high")
	if want := plain.EstimatedInput + item/2; withUpdate.EstimatedInput != want {
		t.Fatalf("estimated input = %d, want %d", withUpdate.EstimatedInput, want)
	}
	if withUpdate.Remaining != plain.Remaining-(withUpdate.EstimatedInput-plain.EstimatedInput) {
		t.Fatalf("remaining = %d, plain = %d", withUpdate.Remaining, plain.Remaining)
	}
	if plain.EstimatedInput != estimateContextUsage("system", nil, model, "low", 1024, messages, &ReasoningHistory{Baseline: "low", Effective: "low"}).EstimatedInput {
		t.Fatal("an epoch without updates changed the estimate")
	}
}

func TestReasoningHistorySnapshotUsageIncludesDispatchedUpdates(t *testing.T) {
	d, requests, _ := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	before, err := d.Snapshot(t.Context(), SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reasoningSelect(t, d, "high")
	pending, err := d.Snapshot(t.Context(), SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Context.Usage.EstimatedInput != before.Context.Usage.EstimatedInput {
		t.Fatalf("pending selection changed usage: %d -> %d", before.Context.Usage.EstimatedInput, pending.Context.Usage.EstimatedInput)
	}
	reasoningPrompt(t, d, "second")
	<-requests
	dispatched, err := d.Snapshot(t.Context(), SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item := (len(`{"type":"configuration_update","reasoning":{"effort":""}}`) + len("high")) / 2
	messages := plainMessagesForTest(t, d)
	want := d.contextUsage(messages, nil).EstimatedInput + item
	if dispatched.Context.Usage.EstimatedInput != want {
		t.Fatalf("dispatched usage = %d, want %d", dispatched.Context.Usage.EstimatedInput, want)
	}
}

func plainMessagesForTest(t *testing.T, d *Droid) []Message {
	t.Helper()
	d.sdk.mu.Lock()
	defer d.sdk.mu.Unlock()
	envelopes, err := runtimeMessageEnvelopes(d.sdk.state)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]Message, 0, len(envelopes))
	for _, envelope := range envelopes {
		messages = append(messages, envelope.Message)
	}
	return messages
}

func TestReasoningHistoryReplayAssessmentSeesConfigurationUpdates(t *testing.T) {
	requests := make(chan Request, 8)
	model := reasoningScriptModel(t, func(_ context.Context, model Model, request Request) Stream {
		requests <- request
		return reconfigureStream{message: AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "Done"}}}}
	})
	recorder := &replayRecordingProvider{Provider: model.boundProvider()}
	bound, err := BindModel(recorder, model)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Store: NewMemoryStore(), Model: bound, Reasoning: "low"}
	d, err := Spawn(t.Context(), "reasoning_replay", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Spawn(t.Context(), "reasoning_replay", config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	recordedRequests, recordedMessages := recorder.recorded()
	if len(recordedMessages) != 0 {
		t.Fatalf("message-only replay assessments = %d, want none", len(recordedMessages))
	}
	if len(recordedRequests) == 0 {
		t.Fatal("no request-aware replay assessment")
	}
	want := &ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}}}
	last := recordedRequests[len(recordedRequests)-1]
	if !reflect.DeepEqual(last.ReasoningHistory, want) || last.Reasoning != "high" {
		t.Fatalf("resume assessment = %#v (reasoning %q)", last.ReasoningHistory, last.Reasoning)
	}
	for _, request := range recordedRequests {
		if request.ReasoningHistory == nil {
			continue
		}
		if err := validateOpenAIReasoningHistory(bound, request); err != nil {
			t.Fatalf("assessed request is not a valid projection: %v", err)
		}
	}
}

// measuringReplayProvider records exact context measurements as well as replay
// assessments.
type measuringReplayProvider struct {
	*replayRecordingProvider
	usage ContextUsage
}

func (p *measuringReplayProvider) MeasureContext(_ context.Context, _ Model, request Request) (ContextUsage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	return p.usage, nil
}

func TestReasoningHistoryExactMeasurementExcludesPendingSelection(t *testing.T) {
	requests := make(chan Request, 8)
	model := reasoningScriptModel(t, func(_ context.Context, model Model, request Request) Stream {
		requests <- request
		return reconfigureStream{message: AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "Done"}}}}
	})
	measurer := &measuringReplayProvider{
		replayRecordingProvider: &replayRecordingProvider{Provider: model.boundProvider()},
		usage:                   ContextUsage{EstimatedInput: 128, ReservedOutput: 1024, ContextWindow: 400_000, MaxInputTokens: 272_000, Remaining: 271_872, Exact: true},
	}
	bound, err := BindModel(measurer, model)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Spawn(t.Context(), "reasoning_measure", Config{Store: NewMemoryStore(), Model: bound, Reasoning: "low"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	reasoningSelect(t, d, "medium")
	earlier, _ := measurer.recorded()
	assessment, err := d.AssessContext(t.Context(), ContextTarget{Model: bound, Reasoning: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.Usage.Exact || assessment.Usage.EstimatedInput != 128 {
		t.Fatalf("assessment did not use the exact measurement: %+v", assessment.Usage)
	}
	want := &ReasoningHistory{Baseline: "low", Effective: "high", Updates: []ReasoningUpdate{{BeforeMessage: 2, Effort: "high"}}}
	recorded, _ := measurer.recorded()
	recorded = recorded[len(earlier):]
	if len(recorded) == 0 {
		t.Fatal("assessment measured no request")
	}
	for _, request := range recorded {
		if request.Reasoning != "high" || !reflect.DeepEqual(request.ReasoningHistory, want) {
			t.Fatalf("pending selection leaked into assessment: reasoning %q, history %#v", request.Reasoning, request.ReasoningHistory)
		}
	}
}

func TestReasoningHistoryContextAssessmentUsesDispatchedEpoch(t *testing.T) {
	d, requests, config := reasoningTestDroid(t, "low")
	reasoningPrompt(t, d, "first")
	<-requests
	reasoningSelect(t, d, "high")
	reasoningPrompt(t, d, "second")
	<-requests
	// A pending selection must not appear in assessment of the current context.
	reasoningSelect(t, d, "medium")
	assessment, err := d.AssessContext(t.Context(), ContextTarget{Model: config.Model, Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	messages := plainMessagesForTest(t, d)
	item := (len(`{"type":"configuration_update","reasoning":{"effort":""}}`) + len("high")) / 2
	if want := d.contextUsage(messages, nil).EstimatedInput + item; assessment.Usage.EstimatedInput != want {
		t.Fatalf("assessed input = %d, want %d", assessment.Usage.EstimatedInput, want)
	}
	if !assessment.ReplayCompatible {
		t.Fatal("dispatched epoch is not replay compatible")
	}
}
