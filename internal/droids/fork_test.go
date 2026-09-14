package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestForkDefersProviderReplayValidationUntilChildPrompt(t *testing.T) {
	providers := newForkTestProviders()
	source, err := Spawn(t.Context(), "conversation_deferred_source", Config{
		Providers: providers, Model: "test/fork-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	handle, err := source.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "seed"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if outcome, err := handle.Wait(ctx); err != nil || outcome.Status != ExecutionCompleted {
		t.Fatalf("source outcome = %+v, %v", outcome, err)
	}
	beforeFork := providers.provider.validations
	providers.provider.rejectReplay = true
	forked, err := source.Fork(t.Context(), "conversation_deferred_child", ForkOptions{})
	if err != nil {
		t.Fatalf("Fork with incompatible replay context: %v", err)
	}
	t.Cleanup(func() { _ = forked.Droid.Close() })
	if providers.provider.validations != beforeFork {
		t.Fatalf("Fork performed provider replay validation")
	}
	childHandle, err := forked.Droid.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "continue"}}}, PromptOptions{})
	if err != nil {
		t.Fatalf("child Prompt admission: %v", err)
	}
	outcome, err := childHandle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ExecutionFailed || providers.provider.validations != beforeFork+1 {
		t.Fatalf("child outcome = %+v, replay validations=%d", outcome, providers.provider.validations)
	}
}

func TestHistoryRejectsPersistedMessageIdentityMismatch(t *testing.T) {
	store := NewMemoryStore()
	runtimeRecord, err := runtimeEncodedRecord(newDurableRuntime())
	if err != nil {
		t.Fatal(err)
	}
	envelope := MessageEnvelope{
		ID: "message_payload", ConversationID: "conversation_history", TurnID: "turn_history",
		CreatedAt: time.Now().UTC(), Message: UserMessage{Content: []InputContent{TextInput{Text: "hello"}}},
	}
	payload, err := encodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	created, _ := lifecycleEvent("conversation.created", "", "", nil)
	if _, err := store.Open(t.Context(), OpenConversation{
		ID: "conversation_history",
		InitialRecords: []EncodedRecord{
			runtimeRecord,
			{Kind: messageRecordKind, ID: "message_record", Scope: RecordHistory, Version: recordVersion, Payload: payload},
		},
		InitialEvents: []EncodedDurableEvent{created},
	}); err != nil {
		t.Fatal(err)
	}
	droid, err := Spawn(t.Context(), "conversation_history", Config{
		Store: store, Providers: newForkTestProviders(), Model: "test/fork-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	if _, err := droid.History(t.Context(), HistoryQuery{Limit: 10}); err == nil {
		t.Fatal("History accepted mismatched record and payload message IDs")
	}
}

func TestForkCapturesHistoryAcrossStorePages(t *testing.T) {
	const count = forkRecordPageSize + 1
	sourceStore := NewMemoryStore()
	runtimeRecord, err := runtimeEncodedRecord(newDurableRuntime())
	if err != nil {
		t.Fatal(err)
	}
	records := make([]EncodedRecord, 0, count+1)
	records = append(records, runtimeRecord)
	for index := range count {
		envelope := MessageEnvelope{
			ID:             MessageID(fmt.Sprintf("message_%04d", index)),
			ConversationID: "conversation_paged_source", TurnID: "turn_ancestral",
			CreatedAt: time.Unix(int64(index+1), 0).UTC(),
			Message:   UserMessage{Content: []InputContent{TextInput{Text: "history"}}},
		}
		payload, err := encodeMessageEnvelope(envelope)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, EncodedRecord{
			Kind: messageRecordKind, ID: string(envelope.ID), Scope: RecordHistory,
			Version: recordVersion, Payload: payload,
		})
	}
	created, err := lifecycleEvent("conversation.created", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceStore.Open(t.Context(), OpenConversation{
		ID: "conversation_paged_source", InitialRecords: records,
		InitialEvents: []EncodedDurableEvent{created},
	}); err != nil {
		t.Fatal(err)
	}
	providers := newForkTestProviders()
	source, err := Spawn(t.Context(), "conversation_paged_source", Config{
		Store: sourceStore, Providers: providers, Model: "test/fork-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	destination := NewMemoryStore()
	forked, err := source.Fork(t.Context(), "conversation_paged_child", ForkOptions{Store: destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forked.Droid.Close() })
	first, err := destination.Records(t.Context(), RecordQuery{Limit: forkRecordPageSize})
	if err != nil {
		t.Fatal(err)
	}
	second, err := destination.Records(t.Context(), RecordQuery{After: first.Next, Limit: forkRecordPageSize})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != forkRecordPageSize || !first.HasMore || len(second.Records) != 1 || second.HasMore {
		t.Fatalf("forked record pages = %d/%d, hasMore=%v/%v", len(first.Records), len(second.Records), first.HasMore, second.HasMore)
	}
	if first.Records[0].ID != "message_0000" || second.Records[0].ID != fmt.Sprintf("message_%04d", count-1) {
		t.Fatalf("forked record bounds = %q..%q", first.Records[0].ID, second.Records[0].ID)
	}
}

func TestForkCheckpointPayloadRewritesOnlyConversationIdentity(t *testing.T) {
	envelope := wireMessageEnvelope{
		ID: "message_ancestral", ConversationID: "conversation_source",
		TurnID: "turn_ancestral", CreatedAt: time.Unix(1, 0).UTC(),
		Message: wireMessage{Role: RoleUser, Content: []wireContent{{Type: "text", Text: "conversation_source is user content"}}},
	}
	for _, test := range []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{
			name: "pause checkpoint",
			payload: map[string]any{
				"checkpoint_id": "checkpoint_ancestral",
				"turn_id":       "turn_ancestral",
				"messages":      []wireMessageEnvelope{envelope},
			},
			field: "messages",
		},
		{
			name: "compaction checkpoint",
			payload: map[string]any{
				"checkpoint_id":   "checkpoint_ancestral",
				"turn_id":         "turn_ancestral",
				"summary_message": envelope,
			},
			field: "summary_message",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatal(err)
			}
			forked, err := forkCheckpointPayload(encoded, "conversation_source", "conversation_child")
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(forked, &object); err != nil {
				t.Fatal(err)
			}
			var got wireMessageEnvelope
			if test.field == "messages" {
				var messages []wireMessageEnvelope
				if err := json.Unmarshal(object[test.field], &messages); err != nil {
					t.Fatal(err)
				}
				got = messages[0]
			} else if err := json.Unmarshal(object[test.field], &got); err != nil {
				t.Fatal(err)
			}
			if got.ConversationID != "conversation_child" || got.ID != envelope.ID || got.TurnID != envelope.TurnID {
				t.Fatalf("forked envelope = %+v", got)
			}
			if got.Message.Content[0].Text != "conversation_source is user content" {
				t.Fatalf("user content was rewritten: %+v", got.Message.Content)
			}
		})
	}
}

func TestForkTurnRecordRewritesFinalConversationIdentity(t *testing.T) {
	state := newDurableRuntime()
	state.TurnID = "turn_ancestral"
	state.Final = &wireMessageEnvelope{
		ID: "message_final", ConversationID: "conversation_source",
		TurnID: state.TurnID, CreatedAt: time.Unix(1, 0).UTC(),
		Message: wireMessage{Role: RoleAssistant, Content: []wireContent{{Type: "text", Text: "done"}}},
	}
	mutation, err := turnHistoryMutation(state, ExecutionCompleted)
	if err != nil {
		t.Fatal(err)
	}
	record := EncodedRecord{
		Kind: mutation.RecordKind, ID: mutation.RecordID, Scope: mutation.Scope,
		Version: mutation.Version, Payload: mutation.Payload,
	}
	forked, err := forkHistoryRecord(record, "conversation_source", "conversation_child")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Final wireMessageEnvelope `json:"final"`
	}
	if err := json.Unmarshal(forked.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Final.ConversationID != "conversation_child" || payload.Final.ID != "message_final" || payload.Final.TurnID != state.TurnID {
		t.Fatalf("forked turn final = %+v", payload.Final)
	}
}

func TestForkHistoryRecordRejectsUnsupportedRecord(t *testing.T) {
	for _, record := range []EncodedRecord{
		{Kind: messageRecordKind, ID: "message_new", Scope: RecordHistory, Version: recordVersion + 1, Payload: json.RawMessage(`{}`)},
		{Kind: "future", ID: "future_1", Scope: RecordHistory, Version: recordVersion, Payload: json.RawMessage(`{}`)},
	} {
		if _, err := forkHistoryRecord(record, "conversation_source", "conversation_child"); err == nil {
			t.Fatalf("fork accepted unsupported record %+v", record)
		}
	}
}

func TestForkHistoryRecordRejectsForeignConversation(t *testing.T) {
	record, err := messageHistoryMutation(MessageEnvelope{
		ID: "message_foreign", ConversationID: "conversation_other", TurnID: "turn_foreign",
		CreatedAt: time.Unix(1, 0).UTC(), Message: UserMessage{Content: []InputContent{TextInput{Text: "hello"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodedRecord{
		Kind: record.RecordKind, ID: record.RecordID, Scope: record.Scope,
		Version: record.Version, Payload: record.Payload,
	}
	if _, err := forkHistoryRecord(encoded, "conversation_source", "conversation_child"); err == nil {
		t.Fatal("fork accepted a message from another conversation")
	}
}

type forkTestProviders struct {
	provider *forkTestProvider
	model    Model
}

func newForkTestProviders() *forkTestProviders {
	model := Model{
		Provider: "test", ID: "fork-test", API: ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
	return &forkTestProviders{provider: &forkTestProvider{model: model}, model: model}
}

func (p *forkTestProviders) Models() []Model { return []Model{p.model} }
func (p *forkTestProviders) Resolve(selector string) (Provider, Model, error) {
	if selector != "test/fork-test" && selector != "fork-test" {
		return nil, Model{}, errors.New("unknown model")
	}
	return p.provider, p.model, nil
}
func (p *forkTestProviders) Model(selector string) (Model, bool) {
	if selector == "test/fork-test" || selector == "fork-test" {
		return p.model, true
	}
	return Model{}, false
}
func (*forkTestProviders) RefreshModels(context.Context) error { return nil }
func (*forkTestProviders) Stream(context.Context, Model, Request) Stream {
	return nil
}

type forkTestProvider struct {
	model        Model
	rejectReplay bool
	validations  int
}

func (*forkTestProvider) ID() string { return "test" }
func (p *forkTestProvider) Models() []Model {
	return []Model{p.model}
}
func (p *forkTestProvider) Stream(context.Context, Model, Request) (AssistantStream, error) {
	message := AssistantMessage{
		Provider: "test", Model: "fork-test", StopReason: StopReasonStop,
		Content: []AssistantContent{TextContent{Text: "done"}},
	}
	return newForkTestStream(message), nil
}
func (p *forkTestProvider) ValidateReplay(context.Context, Model, []Message) error {
	p.validations++
	if p.rejectReplay {
		return errors.New("replay rejected")
	}
	return nil
}

type forkTestStream struct {
	events  chan StreamEvent
	message AssistantMessage
}

func newForkTestStream(message AssistantMessage) *forkTestStream {
	events := make(chan StreamEvent, 2)
	events <- StreamStart{Partial: AssistantMessage{Provider: message.Provider, Model: message.Model}}
	events <- StreamDone{Message: message}
	close(events)
	return &forkTestStream{events: events, message: message}
}

func (s *forkTestStream) Events() <-chan StreamEvent { return s.events }
func (s *forkTestStream) Result() (AssistantMessage, error) {
	return s.message, nil
}
func (*forkTestStream) Close() error { return nil }
