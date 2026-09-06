package droids_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func TestSDKMemoryAndSQLite(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		store := droids.NewMemoryStore()
		exerciseDroid(t, store, nil)
	})
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "droid.sqlite")
		store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		var toolRuns atomic.Int32
		exerciseDroid(t, store, &toolRuns)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		reopenedStore, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reopenedStore.Close() })
		providers := newReadProviders()
		droid, err := droids.Open(t.Context(), "conversation_integration", droids.Config{
			Store: reopenedStore, Providers: providers, Model: "test/read",
			Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
		})
		if err != nil {
			t.Fatalf("reopen droid: %v", err)
		}
		t.Cleanup(func() { _ = droid.Close() })
		if err := droid.Resume(t.Context()); err != nil {
			t.Fatalf("Resume settled droid: %v", err)
		}
		history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 10})
		if err != nil {
			t.Fatalf("History after reopen: %v", err)
		}
		if len(history.Messages) != 4 {
			t.Fatalf("history messages = %d, want 4", len(history.Messages))
		}
		if got := toolRuns.Load(); got != 1 {
			t.Fatalf("tool executions after reopen = %d, want 1", got)
		}
	})
}

func exerciseDroid(t *testing.T, store droids.Store, externalRuns *atomic.Int32) {
	t.Helper()
	var localRuns atomic.Int32
	runs := externalRuns
	if runs == nil {
		runs = &localRuns
	}
	providers := newReadProviders()
	droid, err := droids.Open(t.Context(), "conversation_integration", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(runs)},
	})
	if err != nil {
		t.Fatalf("Open droid: %v", err)
	}
	t.Cleanup(func() { _ = droid.Close() })

	handle, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "Read the fixture."}},
	}, droids.PromptOptions{})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("status = %q, error = %+v", outcome.Status, outcome.Error)
	}
	if outcome.FinalMessage == nil || outcome.FinalMessage.Message.(droids.AssistantMessage).Text() != "Read complete." {
		t.Fatalf("final message = %+v", outcome.FinalMessage)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("tool executions = %d, want 1", got)
	}
	history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history.Messages) != 4 {
		t.Fatalf("history messages = %d, want 4", len(history.Messages))
	}
	call := history.Messages[1].Message.(droids.AssistantMessage).ToolCalls()[0]
	result := history.Messages[2].Message.(droids.ToolResultMessage)
	if call.ID == "" || call.ID != result.ToolCallID || call.ProviderCallID != "call_read_fixture" || result.ProviderCallID != call.ProviderCallID {
		t.Fatalf("canonical call = %+v, result = %+v", call, result)
	}
	events, err := store.Events(t.Context(), droids.EventQuery{Limit: 100})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events.Events) < 8 {
		t.Fatalf("durable events = %d, want at least 8", len(events.Events))
	}
}

func TestSDKBoundaryIDRemainsIdempotentAcrossReopen(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := newReadProviders()
	droid, err := droids.Open(t.Context(), "conversation_boundary_receipt", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
	})
	if err != nil {
		t.Fatal(err)
	}
	receipts := make([]string, 65)
	for index := range receipts {
		receipts[index] = fmt.Sprintf("bash_%032x", index)
	}
	boundary := droids.BoundaryMessage{
		ID: "bash_batch_0123456789abcdef0123456789abcdef", ReceiptIDs: receipts,
		Kind: "bash", Source: "test",
		Content: []droids.InputContent{droids.TextInput{Text: "durable boundary batch"}},
	}
	if err := droid.Inform(t.Context(), boundary); err != nil {
		t.Fatal(err)
	}
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := droids.Open(t.Context(), "conversation_boundary_receipt", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Inform(t.Context(), boundary); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending.Boundary != 1 {
		t.Fatalf("pending boundaries = %d, want one idempotent delivery", snapshot.Pending.Boundary)
	}
}

func TestSDKAbortPendingHookProducesReplayableTerminalContext(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := newReadProviders()
	var toolRuns atomic.Int32
	hookStarted := make(chan struct{})
	droid, err := droids.Open(t.Context(), "conversation_abort_hook", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
		BeforeToolCall: func(ctx context.Context, _ droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
			close(hookStarted)
			<-ctx.Done()
			return droids.BeforeToolResult{}, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "read"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-hookStarted
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Status != droids.ExecutionAborted {
		t.Fatalf("outcome = %+v, error = %v", outcome, err)
	}
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := droids.Open(t.Context(), "conversation_abort_hook", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatalf("reopen aborted droid: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Resume(t.Context()); err != nil {
		t.Fatalf("resume terminal droid: %v", err)
	}
	if toolRuns.Load() != 0 {
		t.Fatalf("aborted tool ran %d times", toolRuns.Load())
	}
}

func TestSDKDurableHookResumesAfterReopen(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := newReadProviders()
	var toolRuns atomic.Int32
	hookStarted := make(chan struct{})
	var signalOnce sync.Once
	var firstContext droids.ToolContext
	first, err := droids.Open(t.Context(), "conversation_hook", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
		BeforeToolCall: func(ctx context.Context, call droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
			firstContext = call
			signalOnce.Do(func() { close(hookStarted) })
			<-ctx.Done()
			return droids.BeforeToolResult{}, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "Read the fixture."}},
	}, droids.PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hookStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("before hook did not start")
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := toolRuns.Load(); got != 0 {
		t.Fatalf("tool ran before approval: %d", got)
	}

	var resumedContext droids.ToolContext
	second, err := droids.Open(t.Context(), "conversation_hook", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
		BeforeToolCall: func(_ context.Context, call droids.ToolContext, _ droids.ToolCall) (droids.BeforeToolResult, error) {
			resumedContext = call
			return droids.BeforeToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.Resume(t.Context()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	state, err := second.WaitQuiescent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != droids.QuiescentSettled {
		t.Fatalf("quiescent state = %q", state.Kind)
	}
	if got := toolRuns.Load(); got != 1 {
		t.Fatalf("tool executions = %d, want 1", got)
	}
	if resumedContext != firstContext || resumedContext.ToolCallID == "" {
		t.Fatalf("resumed tool context = %+v, want %+v", resumedContext, firstContext)
	}
}

func TestSDKPauseWinsRaceWithFinalResponse(t *testing.T) {
	providers := newSteeringProviders()
	droid, err := droids.Open(t.Context(), "conversation_pause_race", droids.Config{
		Providers: providers, Model: "test/steer",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "initial"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if err := droid.Pause(t.Context(), "operator requested"); err != nil {
		t.Fatal(err)
	}
	close(providers.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionPaused {
		t.Fatalf("status = %q, want paused", outcome.Status)
	}
}

func TestSDKAbortWinsRaceWithFinalResponse(t *testing.T) {
	providers := newSteeringProviders()
	droid, err := droids.Open(t.Context(), "conversation_abort_race", droids.Config{
		Providers: providers, Model: "test/steer",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "initial"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(providers.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionAborted {
		t.Fatalf("status = %q, want aborted", outcome.Status)
	}
}

func TestSDKSteeringKeepsNoToolResponseAlive(t *testing.T) {
	providers := newSteeringProviders()
	droid, err := droids.Open(t.Context(), "conversation_steer", droids.Config{
		Providers: providers, Model: "test/steer",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "initial"}},
	}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if _, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "busy"}},
	}, droids.PromptOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("default busy prompt error = %v", err)
	}
	steered, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "steering"}},
	}, droids.PromptOptions{Steer: true})
	if err != nil {
		t.Fatalf("steer: %v", err)
	}
	if steered.TurnID() != handle.TurnID() {
		t.Fatalf("steered turn = %q, want %q", steered.TurnID(), handle.TurnID())
	}
	close(providers.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("status = %q", outcome.Status)
	}
	requests := providers.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	lastUser := requests[1].Messages[len(requests[1].Messages)-1].(droids.UserMessage)
	if text := lastUser.Content[0].(droids.TextInput).Text; text != "steering" {
		t.Fatalf("steering text = %q", text)
	}
}

func TestSDKLengthTruncatedToolCallGetsSyntheticResult(t *testing.T) {
	providers := &truncatedToolProviders{}
	var toolRuns atomic.Int32
	droid, err := droids.Open(t.Context(), "conversation_truncated", droids.Config{
		Providers: providers, Model: "test/truncated",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "read"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v, droid error = %+v, wait error = %v", outcome, outcome.Error, err)
	}
	if toolRuns.Load() != 0 {
		t.Fatalf("truncated tool executed %d times", toolRuns.Load())
	}
	requests := providers.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	result := requests[1].Messages[len(requests[1].Messages)-1].(droids.ToolResultMessage)
	if !result.IsError || !strings.Contains(result.Content[0].(droids.TextContent).Text, "not executed") {
		t.Fatalf("synthetic result = %+v", result)
	}
}

func TestSDKCycleBudgetPausesAndResumeRenewsIt(t *testing.T) {
	providers := newReadProviders()
	var toolRuns atomic.Int32
	droid, err := droids.Open(t.Context(), "conversation_budget", droids.Config{
		Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
		Execution: &droids.ExecutionPolicy{
			Budget:        droids.ExecutionBudget{MaxModelCycles: 1},
			ToolExecution: droids.ModeParallel, MaxParallelTools: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "Read the fixture."}},
	}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	paused, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != droids.ExecutionPaused {
		t.Fatalf("first outcome = %q, want paused", paused.Status)
	}
	if err := droid.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := droid.WaitQuiescent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != droids.QuiescentSettled {
		t.Fatalf("quiescent state = %q", state.Kind)
	}
	if got := toolRuns.Load(); got != 1 {
		t.Fatalf("tool executions = %d, want 1", got)
	}
}

func TestSDKRunsMoreThanOneHundredModelCycles(t *testing.T) {
	providers := &longLoopProviders{toolCycles: 105}
	var toolRuns atomic.Int32
	droid, err := droids.Open(t.Context(), "conversation_long", droids.Config{
		Providers: providers, Model: "test/long",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "continue until done"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("status = %q: %+v", outcome.Status, outcome.Error)
	}
	if providers.calls.Load() != 106 || toolRuns.Load() != 105 {
		t.Fatalf("provider calls=%d tool calls=%d", providers.calls.Load(), toolRuns.Load())
	}
}

func TestSDKRetriesTransientProviderFailure(t *testing.T) {
	providers := &retryProviders{}
	droid, err := droids.Open(t.Context(), "conversation_retry", droids.Config{
		Providers: providers, Model: "test/retry",
		Retry: &droids.RetryPolicy{
			Enabled: true, MaxRetries: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	handle, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: "retry"}},
	}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("status = %q, error = %+v", outcome.Status, outcome.Error)
	}
	if providers.calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", providers.calls.Load())
	}
}

func TestSDKSettlementFailureReturnsWaitAndShutdownErrors(t *testing.T) {
	store := &failSettlementStore{Store: droids.NewMemoryStore()}
	providers := &retryProviders{alwaysSucceed: true}
	droid, err := droids.Open(t.Context(), "conversation_settlement_failure", droids.Config{
		Store: store, Providers: providers, Model: "test/retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	store.failNext.Store(true)
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "complete"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := handle.Wait(ctx); err == nil {
		t.Fatalf("Wait error = %v", err)
	}
	if err := droid.Shutdown(ctx); err == nil {
		t.Fatalf("Shutdown error = %v", err)
	}
}

func TestSDKRawToolResultFailureBlocksContinuation(t *testing.T) {
	store := &failRawResultStore{Store: droids.NewMemoryStore()}
	providers := newReadProviders()
	var toolRuns atomic.Int32
	droid, err := droids.Open(t.Context(), "conversation_raw_failure", droids.Config{
		Store: store, Providers: providers, Model: "test/read",
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	store.failNext.Store(true)
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "read"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionInterrupted {
		t.Fatalf("status = %q, want interrupted", outcome.Status)
	}
	if toolRuns.Load() != 1 {
		t.Fatalf("tool executions = %d, want 1", toolRuns.Load())
	}
	if err := droid.Resume(t.Context()); !errors.Is(err, droids.ErrUnsafeContinuation) {
		t.Fatalf("Resume error = %v, want ErrUnsafeContinuation", err)
	}
	if _, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "new work"},
	}}, droids.PromptOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("Prompt error = %v, want ErrBusy", err)
	}
	if err := droid.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSDKReconcilesAmbiguousCommit(t *testing.T) {
	store := &ambiguousStore{Store: droids.NewMemoryStore()}
	providers := &retryProviders{alwaysSucceed: true}
	droid, err := droids.Open(t.Context(), "conversation_ambiguous", droids.Config{
		Store: store, Providers: providers, Model: "test/retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	store.failNext.Store(true)
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "complete"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatalf("Prompt after committed error: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v, error = %v", outcome, err)
	}
}

func TestSDKSubscriptionReplaysAndFollowsDurableEvents(t *testing.T) {
	providers := &retryProviders{alwaysSucceed: true}
	droid, err := droids.Open(t.Context(), "conversation_subscription", droids.Config{
		Providers: providers, Model: "test/retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	subscription, err := droid.Subscribe(t.Context(), droids.SubscribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	first := <-subscription.Events()
	if !first.Durable || first.Sequence != 1 || first.Event.(droids.LifecycleEvent).Kind != "conversation.created" {
		t.Fatalf("first event = %+v", first)
	}
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "complete"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := handle.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	foundSettled := false
	for !foundSettled {
		select {
		case envelope := <-subscription.Events():
			if event, ok := envelope.Event.(droids.LifecycleEvent); ok && event.Kind == "execution.settled" {
				foundSettled = true
			}
		case <-ctx.Done():
			t.Fatal("settlement event was not delivered")
		}
	}
}

func TestSDKAutomaticCompactionPreservesHistory(t *testing.T) {
	providers := &compactionProviders{}
	droid, err := droids.Open(t.Context(), "conversation_compaction", droids.Config{
		Providers: providers, Model: "test/compact",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	for index := range 4 {
		handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
			droids.TextInput{Text: strings.Repeat(string(rune('a'+index)), 450)},
		}}, droids.PromptOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		outcome, waitErr := handle.Wait(ctx)
		cancel()
		if waitErr != nil {
			t.Fatal(waitErr)
		}
		if outcome.Status != droids.ExecutionCompleted {
			t.Fatalf("prompt %d status = %q: %+v", index, outcome.Status, outcome.Error)
		}
	}
	if providers.compactions.Load() == 0 {
		t.Fatal("automatic compaction did not run")
	}
	history, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Messages) != 8 {
		t.Fatalf("diagnostic messages = %d, want 8", len(history.Messages))
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Context.CheckpointID == "" || snapshot.Context.Messages >= len(history.Messages) {
		t.Fatalf("compacted context = %+v, history=%d", snapshot.Context, len(history.Messages))
	}
}

type failSettlementStore struct {
	droids.Store
	failNext atomic.Bool
}

func (s *failSettlementStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	for _, event := range request.Events {
		if event.Kind == "execution.settled" && s.failNext.CompareAndSwap(true, false) {
			return droids.CommitResult{}, errors.New("simulated settlement persistence failure")
		}
	}
	return s.Store.Commit(ctx, request)
}

type failRawResultStore struct {
	droids.Store
	failNext atomic.Bool
}

func (s *failRawResultStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	for _, event := range request.Events {
		if event.Kind == "tool.raw_result" && s.failNext.CompareAndSwap(true, false) {
			return droids.CommitResult{}, errors.New("simulated raw result persistence failure")
		}
	}
	return s.Store.Commit(ctx, request)
}

type ambiguousStore struct {
	droids.Store
	failNext atomic.Bool
}

func (s *ambiguousStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	result, err := s.Store.Commit(ctx, request)
	if err == nil && s.failNext.CompareAndSwap(true, false) {
		return droids.CommitResult{}, errors.New("simulated response loss after commit")
	}
	return result, err
}

type readArgs struct {
	Path string `json:"path" jsonschema:"required"`
}

func readOnlyTool(runs *atomic.Int32) droids.AnyTool {
	return droids.MustTool(droids.Tool[readArgs]{
		Name: "read_fixture", Description: "Read a fixture without modifying it.",
		Execute: func(context.Context, droids.ToolContext, readArgs, droids.ToolUpdate) (droids.ToolResult, error) {
			runs.Add(1)
			return droids.ToolText("fixture contents"), nil
		},
	})
}

type readProviders struct {
	mu       sync.Mutex
	requests []droids.Request
}

func newReadProviders() *readProviders { return &readProviders{} }

func (p *readProviders) ID() string             { return "test" }
func (p *readProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *readProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *readProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *readProviders) Model(id string) (droids.Model, bool) {
	if id == "test/read" || id == "read" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *readProviders) RefreshModels(context.Context) error { return nil }
func (p *readProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "read", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{
				ID: "call_read_fixture", Name: "read_fixture", Arguments: []byte(`{"path":"fixture.txt"}`),
			}},
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "read", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "Read complete."}},
	})
}
func (p *readProviders) model() droids.Model {
	return droids.Model{
		ID: "read", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}

type readStream struct {
	message droids.AssistantMessage
}

func sdkStaticStream(message droids.AssistantMessage) droids.Stream {
	return &readStream{message: message}
}
func (s *readStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: s.message.Provider, Model: s.message.Model}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s *readStream) Result() droids.AssistantMessage { return s.message }

type steeringProviders struct {
	mu       sync.Mutex
	requests []droids.Request
	started  chan struct{}
	release  chan struct{}
}

func newSteeringProviders() *steeringProviders {
	return &steeringProviders{started: make(chan struct{}), release: make(chan struct{})}
}
func (p *steeringProviders) ID() string             { return "test" }
func (p *steeringProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *steeringProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *steeringProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *steeringProviders) Model(id string) (droids.Model, bool) {
	if id == "test/steer" || id == "steer" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *steeringProviders) RefreshModels(context.Context) error { return nil }
func (p *steeringProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	message := droids.AssistantMessage{
		Provider: "test", Model: "steer", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "complete"}},
	}
	if call == 1 {
		return &blockingReadStream{message: message, started: p.started, release: p.release}
	}
	return sdkStaticStream(message)
}
func (p *steeringProviders) model() droids.Model {
	return droids.Model{
		ID: "steer", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}
func (p *steeringProviders) Requests() []droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]droids.Request(nil), p.requests...)
}

type blockingReadStream struct {
	message droids.AssistantMessage
	started chan struct{}
	release chan struct{}
}

func (s *blockingReadStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	go func() {
		events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: s.message.Provider, Model: s.message.Model}}
		close(s.started)
		<-s.release
		events <- droids.StreamDone{Message: s.message}
		close(events)
	}()
	return events
}
func (s *blockingReadStream) Result() droids.AssistantMessage { return s.message }

type truncatedToolProviders struct {
	mu       sync.Mutex
	requests []droids.Request
}

func (p *truncatedToolProviders) ID() string             { return "test" }
func (p *truncatedToolProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *truncatedToolProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *truncatedToolProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *truncatedToolProviders) Model(id string) (droids.Model, bool) {
	if id == "test/truncated" || id == "truncated" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *truncatedToolProviders) RefreshModels(context.Context) error { return nil }
func (p *truncatedToolProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "truncated", StopReason: droids.StopReasonLength,
			Content: []droids.AssistantContent{droids.ToolCall{
				Name: "read_fixture", Arguments: []byte(`{"path":"fix`),
			}},
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "truncated", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "recovered"}},
	})
}
func (p *truncatedToolProviders) model() droids.Model {
	return droids.Model{
		ID: "truncated", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}
func (p *truncatedToolProviders) Requests() []droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]droids.Request(nil), p.requests...)
}

type longLoopProviders struct {
	calls      atomic.Int32
	toolCycles int32
}

func (p *longLoopProviders) ID() string             { return "test" }
func (p *longLoopProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *longLoopProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *longLoopProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *longLoopProviders) Model(id string) (droids.Model, bool) {
	if id == "test/long" || id == "long" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *longLoopProviders) RefreshModels(context.Context) error { return nil }
func (p *longLoopProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	call := p.calls.Add(1)
	if call <= p.toolCycles {
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "long", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{
				ID: droids.ToolCallID(fmt.Sprintf("call_%03d", call)), Name: "read_fixture",
				Arguments: []byte(`{"path":"fixture.txt"}`),
			}},
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "long", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
	})
}
func (p *longLoopProviders) model() droids.Model {
	return droids.Model{
		ID: "long", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 1_000_000, MaxOutputTokens: 8_192,
	}
}

type retryProviders struct {
	calls         atomic.Int32
	alwaysSucceed bool
}

func (p *retryProviders) ID() string             { return "test" }
func (p *retryProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *retryProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *retryProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *retryProviders) Model(id string) (droids.Model, bool) {
	if id == "test/retry" || id == "retry" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *retryProviders) RefreshModels(context.Context) error { return nil }
func (p *retryProviders) Stream(context.Context, droids.Model, droids.Request) droids.Stream {
	if p.calls.Add(1) == 1 && !p.alwaysSucceed {
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "retry", StopReason: droids.StopReasonError,
			ErrorKind: droids.ErrorTransport, ErrorMessage: "temporary transport failure",
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "retry", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "recovered"}},
	})
}
func (p *retryProviders) model() droids.Model {
	return droids.Model{
		ID: "retry", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}

type compactionProviders struct {
	calls       atomic.Int32
	compactions atomic.Int32
}

func (p *compactionProviders) ID() string             { return "test" }
func (p *compactionProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *compactionProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *compactionProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *compactionProviders) Model(id string) (droids.Model, bool) {
	if id == "test/compact" || id == "compact" {
		return p.model(), true
	}
	return droids.Model{}, false
}
func (p *compactionProviders) RefreshModels(context.Context) error { return nil }
func (p *compactionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.calls.Add(1)
	if strings.Contains(request.SystemPrompt, "Summarize the supplied conversation") {
		p.compactions.Add(1)
		return sdkStaticStream(droids.AssistantMessage{
			Provider: "test", Model: "compact", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "Earlier prompts contained repeated test data."}},
		})
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: "compact", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "ok"}},
	})
}
func (p *compactionProviders) model() droids.Model {
	return droids.Model{
		ID: "compact", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 1_000, MaxInputTokens: 900, MaxOutputTokens: 64,
	}
}
