package droids

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutomaticCompactionProtectsToolOutputNotYetConsumedByModel(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	provider.normalResponse = func(int) AssistantMessage {
		return AssistantMessage{
			Provider: "test", Model: "checkpoint", StopReason: StopReasonToolUse,
			Content: []AssistantContent{ToolCall{ID: "read_evidence", Name: "read", Arguments: []byte(`{}`)}},
		}
	}
	var calls atomic.Int32
	evidence := "unique evidence: " + strings.Repeat("large result ", 400)
	tool := MustTool(Tool[struct{}]{
		Name: "read", Description: "Read evidence",
		Execute: func(context.Context, ToolContext, struct{}, ToolUpdate) (ToolResult, error) {
			calls.Add(1)
			return ToolResult{Content: []ResultContent{TextContent{Text: evidence}}}, nil
		},
	})
	droid := spawnCompactionTestDroid(t, provider, nil)
	if err := droid.Reconfigure(RequestConfiguration{SystemPrompt: "normal system", Tools: []AnyTool{tool}}); err != nil {
		t.Fatal(err)
	}
	handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "Read the evidence and report it"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != ExecutionFailed || outcome.Error == nil || outcome.Error.Kind != DroidErrorCompaction {
		t.Fatalf("protected-tail outcome = %+v, %v", outcome, err)
	}
	droid.sdk.mu.Lock()
	state, err := cloneDurableRuntime(droid.sdk.state)
	droid.sdk.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messagesFromWireContext(state.Context)
	if err != nil || len(messages) != 3 || state.CheckpointID != "" {
		t.Fatalf("protected context = %+v, %v", state, err)
	}
	result, ok := messages[2].(ToolResultMessage)
	if !ok || !reflect.DeepEqual(result.Content, []ResultContent{TextContent{Text: evidence}}) {
		t.Fatalf("unconsumed result = %#v", messages[2])
	}
	if calls.Load() != 1 || len(provider.summaryRequests()) != 0 {
		t.Fatalf("tool calls = %d, summary calls = %d", calls.Load(), len(provider.summaryRequests()))
	}
}

func TestCompactionRejectsConcurrentRequestConfigurationChange(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			provider := newCompactionTestProvider(90_000)
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			provider.summaryResponse = func(int) AssistantMessage {
				close(started)
				<-release
				return provider.summaryMessage()
			}
			droid := spawnCompactionTestDroid(t, provider, nil)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			source := seedCompactionContext(t, droid, longToolCompactionContext())
			result := make(chan error, 1)
			go func() {
				if !automatic {
					_, err := droid.CompactContext(t.Context(), CompactContextOptions{
						OperationID: "configuration_race", Target: ContextTarget{Model: droid.model}, Force: true,
					})
					result <- err
					return
				}
				handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "continue"}}}, PromptOptions{})
				if err != nil {
					result <- err
					return
				}
				outcome, err := handle.Wait(t.Context())
				if err == nil && outcome.Status == ExecutionFailed && outcome.Error != nil && outcome.Error.Kind == DroidErrorCompaction {
					result <- ErrConflict
				} else {
					result <- fmt.Errorf("unexpected outcome %+v: %v", outcome, err)
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("summary generation did not start")
			}
			if err := droid.Reconfigure(RequestConfiguration{SystemPrompt: "normal system", ContextWindow: 1_000}); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-result:
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("configuration race error = %v, want conflict", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("compaction did not finish")
			}
			droid.sdk.mu.Lock()
			state, err := cloneDurableRuntime(droid.sdk.state)
			droid.sdk.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if state.CheckpointID != "" || !reflect.DeepEqual(state.Context[:len(source)], source) {
				t.Fatal("compaction installed a checkpoint validated against obsolete configuration")
			}
			if state.SessionUsage.TotalTokens != 5 {
				t.Fatalf("observed summary usage = %+v", state.SessionUsage)
			}
		})
	}
}

func TestCompactionCarriesGeneratedSummaryForwardInsteadOfRepeatingPrefix(t *testing.T) {
	for _, padding := range []int{69, 9} {
		t.Run(fmt.Sprintf("padding=%d", padding), func(t *testing.T) {
			provider := newCompactionTestProvider(1_000)
			summary := strings.Repeat("word ", 50)
			provider.summaryResponse = func(int) AssistantMessage {
				message := provider.summaryMessage()
				// The conservative estimator can count more text tokens than the
				// provider, even though the provider stayed within its output allowance.
				message.Content = []AssistantContent{TextContent{Text: summary}}
				message.Usage = Usage{Input: 3, Output: 64, TotalTokens: 67}
				return message
			}
			droid := spawnCompactionTestDroid(t, provider, nil)
			var messages []Message
			for i := range 20 {
				messages = append(messages, UserMessage{Content: []InputContent{TextInput{Text: fmt.Sprintf("message-%02d ", i) + strings.Repeat("x", padding)}}})
			}
			seedCompactionContext(t, droid, messages)
			result, err := droid.CompactContext(t.Context(), CompactContextOptions{
				OperationID: "bounded_summary_candidates", Target: ContextTarget{Model: droid.model}, Force: true,
			})
			if err != nil || !result.Compacted {
				t.Fatalf("compaction = %+v, %v", result, err)
			}
			requests := provider.summaryRequests()
			if len(requests) != 2 {
				t.Fatalf("summary requests = %d, want 2 bounded incremental candidates", len(requests))
			}
			second := compactionRequestText(t, requests[1])
			if !strings.Contains(second, "<previous-summary>\n"+summary+"\n</previous-summary>") || strings.Contains(second, "message-00") {
				t.Fatalf("candidate did not reuse generated memory:\n%s", second)
			}
			if droid.sdk.state.SessionUsage.TotalTokens != 134 {
				t.Fatalf("summary usage = %+v", droid.sdk.state.SessionUsage)
			}
		})
	}
}
