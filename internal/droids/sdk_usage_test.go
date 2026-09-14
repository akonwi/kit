package droids_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestSDKCumulativeUsageCountsRetriesAbortsForksAndLegacyRebuild(t *testing.T) {
	first := droids.Usage{Input: 2, Output: 3, CacheRead: 1, Reasoning: 1, TotalTokens: 5}
	second := droids.Usage{Input: 5, Output: 7, CacheWrite: 2, Reasoning: 2, TotalTokens: 12}
	aborted := droids.Usage{Input: 11, Output: 1, TotalTokens: 12}
	later := droids.Usage{Input: 13, Output: 2, TotalTokens: 15}
	providers := &usageProviders{
		responses: []droids.AssistantMessage{
			usageMessage(droids.StopReasonError, first),
			usageMessage(droids.StopReasonStop, second),
			usageMessage(droids.StopReasonAborted, aborted),
		},
		fallback: later,
	}
	store := droids.NewMemoryStore()
	droid, err := droids.Spawn(t.Context(), "conversation_usage", droids.Config{
		Store: store, Model: resolvedTestModel(providers, "test/usage"),
		Retry: &droids.RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := droid.Subscribe(t.Context(), droids.SubscribeOptions{After: initial.LastEvent, Buffer: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	firstHandle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "retry"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstOutcome, err := firstHandle.Wait(t.Context())
	if err != nil || firstOutcome.Status != droids.ExecutionCompleted {
		t.Fatalf("first outcome = %+v, %v", firstOutcome, err)
	}
	wantFirst := pricedUsage(mergeExpectedUsage(first, second))
	updates := awaitUsageUpdates(t, subscription, 2)
	if !usageEqual(updates[0], pricedUsage(first)) || !usageEqual(updates[1], wantFirst) {
		t.Fatalf("retry usage updates = %+v, want first=%+v final=%+v", updates, pricedUsage(first), wantFirst)
	}
	if !usageEqual(firstOutcome.Usage, wantFirst) {
		t.Fatalf("first turn usage = %+v, want %+v", firstOutcome.Usage, wantFirst)
	}
	turn, err := droid.Turn(t.Context(), firstHandle.TurnID())
	if err != nil || !usageEqual(turn.Usage, wantFirst) {
		t.Fatalf("Turn() usage = %+v, %v; want %+v", turn.Usage, err, wantFirst)
	}

	abortedHandle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "abort"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	abortedOutcome, err := abortedHandle.Wait(t.Context())
	if err != nil || abortedOutcome.Status != droids.ExecutionInterrupted {
		t.Fatalf("aborted outcome = %+v, %v", abortedOutcome, err)
	}
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	wantInherited := pricedUsage(mergeExpectedUsage(first, second, aborted))
	abortedUpdates := awaitUsageUpdates(t, subscription, 1)
	if !usageEqual(abortedUpdates[0], wantInherited) {
		t.Fatalf("aborted usage update = %+v, want %+v", abortedUpdates[0], wantInherited)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 20})
	if err != nil || !usageEqual(snapshot.Usage, wantInherited) {
		t.Fatalf("cumulative usage = %+v, %v; want %+v", snapshot.Usage, err, wantInherited)
	}

	forked, err := droid.Fork(t.Context(), "conversation_usage_fork", droids.ForkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parentHandle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "parent"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parentHandle.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	childHandle, err := forked.Droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "child"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := childHandle.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	childSnapshot, err := forked.Droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	wantFork := pricedUsage(mergeExpectedUsage(first, second, aborted, later))
	if !usageEqual(childSnapshot.Usage, wantFork) {
		t.Fatalf("fork usage = %+v, want %+v", childSnapshot.Usage, wantFork)
	}
	secondParent, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "parent again"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondParent.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	unchangedChild, err := forked.Droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil || !usageEqual(unchangedChild.Usage, wantFork) {
		t.Fatalf("child usage after later parent work = %+v, %v; want %+v", unchangedChild.Usage, err, wantFork)
	}
	if err := forked.Droid.Close(); err != nil {
		t.Fatal(err)
	}
	beforeRebuild, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var runtime droids.EncodedRecord
	for _, record := range stored.RuntimeState {
		if record.Kind == "runtime" && record.ID == "current" {
			runtime = record
			break
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(runtime.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "session_usage")
	delete(payload, "session_usage_initialized")
	runtimePayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(t.Context(), droids.CommitRequest{
		ExpectedRevision: stored.Revision,
		Mutations: []droids.EncodedMutation{{
			Operation: droids.MutationPut, RecordKind: "runtime", RecordID: "current",
			Scope: droids.RecordRuntime, Version: runtime.Version, Payload: runtimePayload,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := droids.Spawn(t.Context(), "conversation_usage", droids.Config{
		Store: store, Model: resolvedTestModel(providers, "test/usage"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	rebuilt, err := reopened.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil || !usageEqual(rebuilt.Usage, beforeRebuild.Usage) {
		t.Fatalf("rebuilt usage = %+v, %v; want %+v", rebuilt.Usage, err, beforeRebuild.Usage)
	}
}

func TestSDKCumulativeUsageIncludesEveryToolLoopModelCycle(t *testing.T) {
	first := droids.Usage{Input: 4, Output: 2, Reasoning: 1, TotalTokens: 6}
	second := droids.Usage{Input: 8, Output: 3, TotalTokens: 11}
	toolCall := usageMessage(droids.StopReasonToolUse, first)
	toolCall.Content = []droids.AssistantContent{droids.ToolCall{ID: "provider_call", Name: "usage_noop", Arguments: json.RawMessage(`{}`)}}
	providers := &usageProviders{responses: []droids.AssistantMessage{toolCall, usageMessage(droids.StopReasonStop, second)}}
	tool := droids.MustTool(droids.Tool[usageToolArgs]{
		Name: "usage_noop", Description: "Complete one usage-accounting tool cycle.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		Execute: func(context.Context, droids.ToolContext, usageToolArgs, droids.ToolUpdate) (droids.ToolResult, error) {
			return droids.ToolText("ok"), nil
		},
	})
	droid, err := droids.Spawn(t.Context(), "conversation_usage_tools", droids.Config{
		Model: resolvedTestModel(providers, "test/usage"), Tools: []droids.AnyTool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "use tool"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	want := pricedUsage(mergeExpectedUsage(first, second))
	if err != nil || outcome.Status != droids.ExecutionCompleted || !usageEqual(outcome.Usage, want) {
		t.Fatalf("tool-loop outcome = %+v, %v; want usage %+v", outcome, err, want)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 10})
	if err != nil || !usageEqual(snapshot.Usage, want) {
		t.Fatalf("tool-loop cumulative usage = %+v, %v; want %+v", snapshot.Usage, err, want)
	}
}

type usageToolArgs struct{}

func TestSDKObservedTerminalUsagePersistsWhenAbortWinsBeforeAccounting(t *testing.T) {
	providers := &observedCancellationProviders{started: make(chan struct{}), release: make(chan struct{})}
	droid, err := droids.Spawn(t.Context(), "conversation_usage_abort_race", droids.Config{
		Model: resolvedTestModel(providers, "test/observed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "race abort"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider terminal result was not observed")
	}
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(providers.release)
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != droids.ExecutionAborted {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Usage.Input != 6 || snapshot.Usage.Output != 4 || snapshot.Usage.TotalTokens != 10 {
		t.Fatalf("usage after abort race = %+v", snapshot.Usage)
	}
}

type observedCancellationProviders struct {
	started chan struct{}
	release chan struct{}
}

func (p *observedCancellationProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *observedCancellationProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(&observedCancellationProvider{owner: p}, model)
}
func (p *observedCancellationProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "observed" || id == "test/observed"
}
func (*observedCancellationProviders) RefreshModels(context.Context) error { return nil }
func (p *observedCancellationProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	return sdkStaticStream(droids.AssistantMessage{})
}
func (*observedCancellationProviders) model() droids.Model {
	return droids.Model{ID: "observed", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 1_024}
}

type observedCancellationProvider struct {
	owner *observedCancellationProviders
}

func (*observedCancellationProvider) ID() string                      { return "test" }
func (provider *observedCancellationProvider) Models() []droids.Model { return provider.owner.Models() }
func (provider *observedCancellationProvider) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (provider *observedCancellationProvider) Stream(context.Context, droids.Model, droids.Request) (droids.AssistantStream, error) {
	return &observedCancellationStream{owner: provider.owner}, nil
}

type observedCancellationStream struct {
	owner *observedCancellationProviders
}

func (*observedCancellationStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent)
	close(events)
	return events
}
func (stream *observedCancellationStream) Result() (droids.AssistantMessage, error) {
	close(stream.owner.started)
	<-stream.owner.release
	return droids.AssistantMessage{
		Provider: "test", Model: "observed", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "observed"}},
		Usage:   droids.Usage{Input: 6, Output: 4, TotalTokens: 10},
	}, nil
}
func (*observedCancellationStream) Close() error { return nil }

func TestSDKUsageContributionReconcilesAmbiguousCommitOnce(t *testing.T) {
	providers := &usageProviders{fallback: droids.Usage{Input: 3, Output: 2, TotalTokens: 5}}
	store := &ambiguousUsageStore{Store: droids.NewMemoryStore()}
	droid, err := droids.Spawn(t.Context(), "conversation_usage_ambiguous", droids.Config{
		Store: store, Model: resolvedTestModel(providers, "test/usage"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	store.fail.Store(true)
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "once"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Usage.Input != 3 || snapshot.Usage.Output != 2 || snapshot.Usage.TotalTokens != 5 {
		t.Fatalf("usage after ambiguous commit = %+v", snapshot.Usage)
	}
	if providers.calls() != 1 {
		t.Fatalf("provider calls = %d, want 1", providers.calls())
	}
}

type ambiguousUsageStore struct {
	droids.Store
	fail atomic.Bool
}

func (store *ambiguousUsageStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	result, err := store.Store.Commit(ctx, request)
	if err != nil {
		return result, err
	}
	for _, event := range request.Events {
		if event.Kind == "usage.updated" && store.fail.CompareAndSwap(true, false) {
			return droids.CommitResult{}, errors.New("simulated response loss after usage commit")
		}
	}
	return result, nil
}

func TestSDKMalformedProviderUsageIsNotAggregated(t *testing.T) {
	providers := &usageProviders{responses: []droids.AssistantMessage{
		usageMessage(droids.StopReasonStop, droids.Usage{Input: -1, Output: 2, TotalTokens: 1}),
	}}
	droid, err := droids.Spawn(t.Context(), "conversation_invalid_usage", droids.Config{
		Model: resolvedTestModel(providers, "test/usage"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "invalid usage"}}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != droids.ExecutionFailed || !usageEqual(outcome.Usage, droids.Usage{}) {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !usageEqual(snapshot.Usage, droids.SessionUsage{}) || len(snapshot.Recent.Messages) != 2 {
		t.Fatalf("snapshot usage/history = %+v / %+v", snapshot.Usage, snapshot.Recent.Messages)
	}
}

func TestSDKFailedCompactionAddsObservedSummaryUsage(t *testing.T) {
	providers := newAdaptationProviders()
	providers.summaryUsage = droids.Usage{Input: 7, Output: 2, TotalTokens: 9}
	providers.summaryStopReason = droids.StopReasonError
	droid, err := droids.Spawn(t.Context(), "conversation_failed_compaction_usage", droids.Config{
		Model: resolvedTestModel(providers, "test/active"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 4)
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "usage_failed_compaction", Target: droids.ContextTarget{Model: resolvedTestModel(providers, "test/small"), Reasoning: "off"},
	}); err == nil {
		t.Fatal("CompactContext succeeded with a failed summary response")
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Usage.Input != 7 || snapshot.Usage.Output != 2 || snapshot.Usage.TotalTokens != 9 {
		t.Fatalf("usage after failed compaction = %+v", snapshot.Usage)
	}
}

func TestSDKExplicitCompactionAddsSummaryProviderUsage(t *testing.T) {
	providers := newAdaptationProviders()
	providers.normalUsage = droids.Usage{Input: 2, Output: 1, TotalTokens: 3}
	providers.summaryUsage = droids.Usage{Input: 7, Output: 2, TotalTokens: 9}
	droid, err := droids.Spawn(t.Context(), "conversation_compaction_usage", droids.Config{
		Model: resolvedTestModel(providers, "test/active"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 4)
	before, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if before.Usage.TotalTokens != 12 {
		t.Fatalf("usage before compaction = %+v", before.Usage)
	}
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "usage_compaction", Target: droids.ContextTarget{Model: resolvedTestModel(providers, "test/small"), Reasoning: "off"},
	}); err != nil {
		t.Fatal(err)
	}
	after, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if after.Usage.Input != 15 || after.Usage.Output != 6 || after.Usage.TotalTokens != 21 {
		t.Fatalf("usage after compaction = %+v", after.Usage)
	}
}

func awaitUsageUpdates(t *testing.T, subscription droids.Subscription, count int) []droids.SessionUsage {
	t.Helper()
	updates := make([]droids.SessionUsage, 0, count)
	deadline := time.After(5 * time.Second)
	for len(updates) < count {
		select {
		case envelope := <-subscription.Events():
			if update, ok := envelope.Event.(droids.UsageUpdated); ok {
				updates = append(updates, update.Usage)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %d usage updates; got %d", count, len(updates))
		}
	}
	return updates
}

func usageMessage(reason droids.StopReason, usage droids.Usage) droids.AssistantMessage {
	message := droids.AssistantMessage{
		Provider: "test", Model: "usage", StopReason: reason, Usage: usage,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
	}
	if reason == droids.StopReasonError {
		message.ErrorKind = droids.ProviderTransport
		message.ErrorMessage = "temporary"
		message.Error = &droids.ProviderError{Kind: droids.ProviderTransport, Message: "temporary", Retryable: true}
	}
	return message
}

type usageProviders struct {
	mu        sync.Mutex
	responses []droids.AssistantMessage
	fallback  droids.Usage
	callCount int
}

func (p *usageProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *usageProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p *usageProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "usage" || id == "test/usage"
}
func (*usageProviders) RefreshModels(context.Context) error { return nil }
func (p *usageProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount++
	if len(p.responses) > 0 {
		message := p.responses[0]
		p.responses = p.responses[1:]
		return sdkStaticStream(message)
	}
	return sdkStaticStream(usageMessage(droids.StopReasonStop, p.fallback))
}
func (p *usageProviders) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.callCount
}

func (*usageProviders) model() droids.Model {
	return droids.Model{
		ID: "usage", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 1_024,
		Cost: droids.Cost{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000},
	}
}

func mergeExpectedUsage(values ...droids.Usage) droids.Usage {
	var result droids.Usage
	for _, value := range values {
		result.Input += value.Input
		result.Output += value.Output
		result.CacheRead += value.CacheRead
		result.CacheWrite += value.CacheWrite
		result.Reasoning += value.Reasoning
		result.TotalTokens += value.TotalTokens
	}
	return result
}

func pricedUsage(usage droids.Usage) droids.Usage {
	usage.Cost.Input = float64(usage.Input)
	usage.Cost.Output = float64(usage.Output)
	usage.Cost.CacheRead = float64(usage.CacheRead)
	usage.Cost.CacheWrite = float64(usage.CacheWrite)
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
	return usage
}

func usageEqual(got, want droids.Usage) bool {
	return got.Input == want.Input && got.Output == want.Output && got.CacheRead == want.CacheRead &&
		got.CacheWrite == want.CacheWrite && got.Reasoning == want.Reasoning && got.TotalTokens == want.TotalTokens &&
		math.Abs(got.Cost.Input-want.Cost.Input) < 1e-9 && math.Abs(got.Cost.Output-want.Cost.Output) < 1e-9 &&
		math.Abs(got.Cost.CacheRead-want.Cost.CacheRead) < 1e-9 && math.Abs(got.Cost.CacheWrite-want.Cost.CacheWrite) < 1e-9 &&
		math.Abs(got.Cost.Total-want.Cost.Total) < 1e-9
}
