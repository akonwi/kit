package droids

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAutomaticAndManualCompactionShareRequestAndKeepHistory(t *testing.T) {
	var summaryRequests []Request
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			provider := newCompactionTestProvider(90_000)
			droid := spawnCompactionTestDroid(t, provider, nil)
			source := seedCompactionContext(t, droid, longToolCompactionContext())
			history := compactionHistory(t, droid)
			if automatic {
				handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "continue"}}}, PromptOptions{})
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := handle.Wait(t.Context())
				if err != nil || outcome.Status != ExecutionCompleted {
					t.Fatalf("automatic compaction outcome = %+v, %v", outcome, err)
				}
			} else {
				if _, err := droid.CompactContext(t.Context(), CompactContextOptions{
					OperationID: "manual_equivalence", Target: ContextTarget{Model: droid.model}, Force: true,
				}); err != nil {
					t.Fatal(err)
				}
			}
			requests := provider.summaryRequests()
			if len(requests) != 1 {
				t.Fatalf("summary requests = %d, want 1", len(requests))
			}
			summaryRequests = append(summaryRequests, requests[0])
			compactionRequestText(t, requests[0])
			assertCompactionHistoryPrefix(t, droid, history)

			droid.sdk.mu.Lock()
			contextWire := cloneWireContext(droid.sdk.state.Context)
			checkpointID := droid.sdk.state.CheckpointID
			droid.sdk.mu.Unlock()
			// The last two complete tool exchanges exceed the 20K target together;
			// the entire exchange crossing that boundary is kept, not split.
			wantTail := source[7:]
			if !reflect.DeepEqual(contextWire[1:1+len(wantTail)], wantTail) {
				t.Fatal("retained suffix changed original envelopes or tool output")
			}
			if contextWire[0].TurnID != source[6].TurnID {
				t.Fatalf("summary provenance = %q, want %q", contextWire[0].TurnID, source[6].TurnID)
			}
			checkpoint, err := droid.sdk.store.Record(t.Context(), checkpointKind, string(checkpointID))
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				RetainedFrom string              `json:"retained_from"`
				Summary      wireMessageEnvelope `json:"summary_message"`
			}
			if err := json.Unmarshal(checkpoint.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.RetainedFrom != string(source[7].ID) || payload.Summary.ID != contextWire[0].ID {
				t.Fatalf("checkpoint boundary = %+v", payload)
			}
		})
	}
	if len(summaryRequests) != 2 || !reflect.DeepEqual(summaryRequests[0], summaryRequests[1]) {
		t.Fatal("automatic and manual compaction produced different summary requests")
	}
}

func TestRepeatedCompactionUsesCheckpointAndPreviouslyRetainedMessages(t *testing.T) {
	provider := newCompactionTestProvider(90_000)
	store := NewMemoryStore()
	droid := spawnCompactionTestDroid(t, provider, store)
	seedCompactionContext(t, droid, longToolCompactionContext())
	first, err := droid.CompactContext(t.Context(), CompactContextOptions{
		OperationID: "first_checkpoint", Target: ContextTarget{Model: droid.model}, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstCheckpoint, err := store.Record(t.Context(), checkpointKind, string(first.CheckpointID))
	if err != nil {
		t.Fatal(err)
	}
	// Advance the recent window so all formerly retained messages enter the
	// next prefix. Their timestamps/IDs predate the first checkpoint.
	newMessages := []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "new work"}}},
		AssistantMessage{Content: []AssistantContent{TextContent{Text: strings.Repeat("new progress ", 4_000)}}},
	}
	newWire := seedCompactionContext(t, droid, newMessages)
	history := compactionHistory(t, droid)
	second, err := droid.CompactContext(t.Context(), CompactContextOptions{
		OperationID: "second_checkpoint", Target: ContextTarget{Model: droid.model}, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requests := provider.summaryRequests()
	if len(requests) != 2 {
		t.Fatalf("summary requests = %d, want 2", len(requests))
	}
	text := compactionRequestText(t, requests[1])
	want := "<conversation>\n" +
		"[Assistant tool call]\nread({\"path\":\"file-3.go\"})\n\n[Tool result: read; completed; output omitted]\n\n" +
		"[Assistant tool call]\nread({\"path\":\"file-4.go\"})\n\n[Tool result: read; completed; output omitted]\n\n" +
		"[User]\nnew work\n</conversation>\n\n<previous-summary>\ncheckpoint memory\n</previous-summary>\n\n" +
		compactionUpdateInstructions + compactionSummaryInstructions
	if text != want {
		t.Fatalf("second summary request:\n%s\nwant:\n%s", text, want)
	}
	assertCompactionHistoryPrefix(t, droid, history)
	unchanged, err := store.Record(t.Context(), checkpointKind, string(first.CheckpointID))
	if err != nil || !bytes.Equal(firstCheckpoint.Payload, unchanged.Payload) {
		t.Fatalf("prior checkpoint changed: %v", err)
	}
	checkpoint, err := store.Record(t.Context(), checkpointKind, string(second.CheckpointID))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Source CheckpointID `json:"source_checkpoint_id"`
	}
	if err := json.Unmarshal(checkpoint.Payload, &metadata); err != nil || metadata.Source != first.CheckpointID {
		t.Fatalf("checkpoint ancestry = %+v, %v", metadata, err)
	}
	if !reflect.DeepEqual(droid.sdk.state.Context[1:], newWire[1:]) {
		t.Fatal("second checkpoint did not retain the original newest message")
	}
	wantContext := cloneWireContext(droid.sdk.state.Context)
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := spawnCompactionTestDroid(t, provider, store)
	if reopened.sdk.state.CheckpointID != second.CheckpointID || !reflect.DeepEqual(reopened.sdk.state.Context, wantContext) {
		t.Fatal("reopen changed the checkpointed provider context")
	}
	assertCompactionHistoryPrefix(t, reopened, history)
}

func TestCompactionRejectsInvalidSummariesWithoutReplacingCheckpoint(t *testing.T) {
	for _, kind := range []string{"length", "empty", "tool-call", "provider-error"} {
		t.Run(kind, func(t *testing.T) {
			provider := newCompactionTestProvider(90_000)
			droid := spawnCompactionTestDroid(t, provider, nil)
			seedCompactionContext(t, droid, longToolCompactionContext())
			first, err := droid.CompactContext(t.Context(), CompactContextOptions{
				OperationID: "good_checkpoint", Target: ContextTarget{Model: droid.model}, Force: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			seedCompactionContext(t, droid, []Message{UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("new work ", 5_000)}}}})
			source := cloneWireContext(droid.sdk.state.Context)
			history := compactionHistory(t, droid)
			provider.summaryResponse = func(int) AssistantMessage {
				message := provider.summaryMessage()
				switch kind {
				case "length":
					message.StopReason = StopReasonLength
				case "empty":
					message.Content = []AssistantContent{TextContent{Text: " \n "}}
				case "tool-call":
					message.StopReason = StopReasonToolUse
					message.Content = []AssistantContent{ToolCall{ID: "unexpected_call", Name: "bash", Arguments: []byte(`{}`)}}
				case "provider-error":
					message.StopReason = StopReasonError
					message.ErrorKind = ProviderTransport
					message.ErrorMessage = "summary failed"
				}
				return message
			}
			_, err = droid.CompactContext(t.Context(), CompactContextOptions{
				OperationID: "bad_summary", Target: ContextTarget{Model: droid.model}, Force: true,
			})
			if err == nil {
				t.Fatal("accepted an invalid summary")
			}
			if !reflect.DeepEqual(droid.sdk.state.Context, source) || droid.sdk.state.CheckpointID != first.CheckpointID {
				t.Fatal("failed summary replaced the provider context")
			}
			assertCompactionHistoryPrefix(t, droid, history)
			if droid.sdk.state.SessionUsage.TotalTokens != 10 {
				t.Fatalf("failed summary usage = %+v", droid.sdk.state.SessionUsage)
			}
		})
	}
}

func TestAutomaticCompactionFailureKeepsOriginalContext(t *testing.T) {
	provider := newCompactionTestProvider(90_000)
	provider.summaryResponse = func(int) AssistantMessage {
		message := provider.summaryMessage()
		message.StopReason = StopReasonLength
		return message
	}
	droid := spawnCompactionTestDroid(t, provider, nil)
	source := seedCompactionContext(t, droid, longToolCompactionContext())
	history := compactionHistory(t, droid)
	handle, err := droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "continue"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(t.Context())
	if err != nil || outcome.Status != ExecutionFailed || outcome.Error == nil || outcome.Error.Kind != DroidErrorCompaction {
		t.Fatalf("failed compaction outcome = %+v, %v", outcome, err)
	}
	rt := droid.sdk
	rt.mu.Lock()
	state, err := cloneDurableRuntime(rt.state)
	rt.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if state.CheckpointID != "" || len(state.Context) != len(source)+1 || !reflect.DeepEqual(state.Context[:len(source)], source) {
		t.Fatal("failed automatic compaction discarded original context")
	}
	if state.SessionUsage.TotalTokens != 5 || state.Usage.TotalTokens != 5 {
		t.Fatalf("failed automatic summary usage = %+v / %+v", state.SessionUsage, state.Usage)
	}
	assertCompactionHistoryPrefix(t, droid, history)
}

func TestCompactionCheckpointCommitFailureKeepsContextAndCanRetry(t *testing.T) {
	provider := newCompactionTestProvider(90_000)
	store := &rejectCompactionStore{Store: NewMemoryStore(), reject: true}
	droid := spawnCompactionTestDroid(t, provider, store)
	source := seedCompactionContext(t, droid, longToolCompactionContext())
	history := compactionHistory(t, droid)
	options := CompactContextOptions{OperationID: "checkpoint_write_failure", Target: ContextTarget{Model: droid.model}, Force: true}
	if _, err := droid.CompactContext(t.Context(), options); err == nil {
		t.Fatal("compaction succeeded despite checkpoint persistence failure")
	}
	if !reflect.DeepEqual(droid.sdk.state.Context, source) || droid.sdk.state.CheckpointID != "" {
		t.Fatal("failed checkpoint commit installed memory-only context")
	}
	if _, err := store.Record(t.Context(), compactionReceiptKind, options.OperationID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("receipt after failed commit = %v", err)
	}
	assertCompactionHistoryPrefix(t, droid, history)
	store.reject = false
	result, err := droid.CompactContext(t.Context(), options)
	if err != nil || !result.Compacted || result.CheckpointID == "" {
		t.Fatalf("retry = %+v, %v", result, err)
	}
	if droid.sdk.state.SessionUsage.TotalTokens != 10 {
		t.Fatalf("observed summary usage = %+v", droid.sdk.state.SessionUsage)
	}
	assertCompactionHistoryPrefix(t, droid, history)
}

type rejectCompactionStore struct {
	Store
	reject bool
}

func (store *rejectCompactionStore) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	if store.reject {
		for _, mutation := range request.Mutations {
			if mutation.RecordKind == checkpointKind {
				return CommitResult{}, errors.New("injected checkpoint write failure")
			}
		}
	}
	return store.Store.Commit(ctx, request)
}

func longToolCompactionContext() []Message {
	messages := []Message{UserMessage{Content: []InputContent{TextInput{Text: "Original goal: " + strings.Repeat("work ", 2_000)}}}}
	for i := range 5 {
		id := ToolCallID(fmt.Sprintf("call_%d", i))
		messages = append(messages,
			AssistantMessage{Provider: "test", Model: "checkpoint", StopReason: StopReasonToolUse, Content: []AssistantContent{
				ToolCall{ID: id, Name: "read", Arguments: []byte(fmt.Sprintf(`{"path":"file-%d.go"}`, i))},
			}},
			ToolResultMessage{ToolCallID: id, ToolName: "read", Content: []ResultContent{TextContent{Text: strings.Repeat("tool output ", 2_333)}}},
		)
	}
	return messages
}

// Materialize acknowledged canonical context without making provider calls;
// this also gives the tests exact historical payloads to compare after compaction.
func seedCompactionContext(t *testing.T, droid *Droid, messages []Message) []wireMessageEnvelope {
	t.Helper()
	rt := droid.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var wire []wireMessageEnvelope
	var mutations []EncodedMutation
	for _, message := range messages {
		id, err := newMessageID()
		if err != nil {
			t.Fatal(err)
		}
		envelope := MessageEnvelope{
			ID: id, ConversationID: rt.conversation, TurnID: "turn_original", CreatedAt: time.Unix(1, 0).UTC(), Message: message,
		}
		mutation, err := messageHistoryMutation(envelope)
		if err != nil {
			t.Fatal(err)
		}
		mutations = append(mutations, mutation)
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			t.Fatal(err)
		}
		wire = append(wire, rt.state.Context[len(rt.state.Context)-1])
	}
	if err := rt.commitLocked(t.Context(), mutations, nil); err != nil {
		t.Fatal(err)
	}
	return wire
}

func compactionHistory(t *testing.T, droid *Droid) []EncodedRecord {
	t.Helper()
	page, err := droid.sdk.store.Records(t.Context(), RecordQuery{Kind: messageRecordKind, Limit: 100})
	if err != nil || page.HasMore {
		t.Fatalf("history = %+v, %v", page, err)
	}
	return page.Records
}

func assertCompactionHistoryPrefix(t *testing.T, droid *Droid, before []EncodedRecord) {
	t.Helper()
	after := compactionHistory(t, droid)
	if len(after) < len(before) || !reflect.DeepEqual(after[:len(before)], before) {
		t.Fatal("compaction changed historical message IDs, sequences, or payloads")
	}
}
