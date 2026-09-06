// Package droidstest provides reusable conformance suites for droids adapters.
package droidstest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

// StoreFactory returns a fresh, empty Store for one test.
type StoreFactory func(t *testing.T) droids.Store

// RunStoreContract verifies the behavior required of every Store adapter.
func RunStoreContract(t *testing.T, factory StoreFactory) {
	t.Helper()
	t.Run("open and state", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		if !opened.Created {
			t.Fatal("first Open Created = false, want true")
		}
		if opened.Conversation.ID != "conversation_test" || opened.Conversation.Revision != 1 {
			t.Fatalf("initial conversation = %+v", opened.Conversation)
		}
		if opened.Conversation.LastEvent != 1 {
			t.Fatalf("initial LastEvent = %d, want 1", opened.Conversation.LastEvent)
		}
		assertRuntimeValue(t, opened.Conversation, "state", "current", `{"status":"ready"}`)

		again, err := store.Open(context.Background(), droids.OpenConversation{ID: "conversation_test"})
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		if again.Created || again.Conversation.Revision != 1 {
			t.Fatalf("second Open = %+v", again)
		}
	})

	t.Run("atomic commit and history", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		result, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: opened.Conversation.Revision,
			Mutations: []droids.EncodedMutation{
				mutation(droids.MutationPut, "state", "current", droids.RecordRuntime, `{"status":"running"}`),
				mutation(droids.MutationPut, "message", "message_1", droids.RecordHistory, `{"text":"hello"}`),
			},
			Events: []droids.EncodedDurableEvent{event("message.completed")},
		})
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if result.Revision != 2 || result.LastEvent != 2 {
			t.Fatalf("Commit result = %+v", result)
		}
		state, err := store.State(context.Background())
		if err != nil {
			t.Fatalf("State: %v", err)
		}
		assertRuntimeValue(t, state, "state", "current", `{"status":"running"}`)

		page, err := store.Records(context.Background(), droids.RecordQuery{Limit: 10})
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		if len(page.Records) != 2 { // initial history record + message_1
			t.Fatalf("history count = %d, want 2", len(page.Records))
		}
		if page.Records[0].Sequence != 1 || page.Records[1].Sequence != 2 {
			t.Fatalf("history sequences = %d, %d", page.Records[0].Sequence, page.Records[1].Sequence)
		}
	})

	t.Run("revision conflict is atomic", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		_, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: opened.Conversation.Revision + 1,
			Mutations: []droids.EncodedMutation{
				mutation(droids.MutationPut, "state", "current", droids.RecordRuntime, `{"status":"wrong"}`),
			},
			Events: []droids.EncodedDurableEvent{event("wrong")},
		})
		if !errors.Is(err, droids.ErrConflict) {
			t.Fatalf("Commit error = %v, want ErrConflict", err)
		}
		state, err := store.State(context.Background())
		if err != nil {
			t.Fatalf("State: %v", err)
		}
		if state.Revision != 1 || state.LastEvent != 1 {
			t.Fatalf("state changed after conflict: %+v", state)
		}
		assertRuntimeValue(t, state, "state", "current", `{"status":"ready"}`)
	})

	t.Run("assert absent survives history transition", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		admission := mutation(droids.MutationAssertAbsent, "tool", "call_1", droids.RecordRuntime, `{"phase":"before_hook"}`)
		first, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: opened.Conversation.Revision,
			Mutations:        []droids.EncodedMutation{admission},
		})
		if err != nil {
			t.Fatalf("admit tool: %v", err)
		}
		terminal := mutation(droids.MutationPut, "tool", "call_1", droids.RecordHistory, `{"phase":"completed"}`)
		second, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: first.Revision,
			Mutations:        []droids.EncodedMutation{terminal},
		})
		if err != nil {
			t.Fatalf("complete tool: %v", err)
		}
		_, err = store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: second.Revision,
			Mutations:        []droids.EncodedMutation{admission},
		})
		if err == nil {
			t.Fatal("duplicate tool admission succeeded")
		}
	})

	t.Run("historical records are immutable", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		_, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: opened.Conversation.Revision,
			Mutations: []droids.EncodedMutation{
				mutation(droids.MutationPut, "audit", "initial", droids.RecordHistory, `{"changed":true}`),
			},
		})
		if err == nil {
			t.Fatal("historical update succeeded")
		}
	})

	t.Run("pagination", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		mutations := make([]droids.EncodedMutation, 0, 3)
		events := make([]droids.EncodedDurableEvent, 0, 3)
		for i := range 3 {
			mutations = append(mutations, mutation(
				droids.MutationPut, "message", fmt.Sprintf("message_%d", i), droids.RecordHistory, fmt.Sprintf(`{"index":%d}`, i),
			))
			events = append(events, event(fmt.Sprintf("event.%d", i)))
		}
		if _, err := store.Commit(context.Background(), droids.CommitRequest{
			ExpectedRevision: opened.Conversation.Revision,
			Mutations:        mutations,
			Events:           events,
		}); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		firstRecords, err := store.Records(context.Background(), droids.RecordQuery{Limit: 2})
		if err != nil {
			t.Fatalf("first Records: %v", err)
		}
		if len(firstRecords.Records) != 2 || !firstRecords.HasMore {
			t.Fatalf("first record page = %+v", firstRecords)
		}
		secondRecords, err := store.Records(context.Background(), droids.RecordQuery{After: firstRecords.Next, Limit: 10})
		if err != nil {
			t.Fatalf("second Records: %v", err)
		}
		if len(secondRecords.Records) != 2 || secondRecords.HasMore {
			t.Fatalf("second record page = %+v", secondRecords)
		}

		firstEvents, err := store.Events(context.Background(), droids.EventQuery{Limit: 2})
		if err != nil {
			t.Fatalf("first Events: %v", err)
		}
		if len(firstEvents.Events) != 2 || !firstEvents.HasMore {
			t.Fatalf("first event page = %+v", firstEvents)
		}
		secondEvents, err := store.Events(context.Background(), droids.EventQuery{After: firstEvents.Next, Limit: 10})
		if err != nil {
			t.Fatalf("second Events: %v", err)
		}
		if len(secondEvents.Events) != 2 || secondEvents.HasMore {
			t.Fatalf("second event page = %+v", secondEvents)
		}
	})

	t.Run("concurrent compare and swap", func(t *testing.T) {
		store := factory(t)
		opened := openStore(t, store)
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := store.Commit(context.Background(), droids.CommitRequest{
					ExpectedRevision: opened.Conversation.Revision,
					Mutations: []droids.EncodedMutation{
						mutation(droids.MutationPut, "race", fmt.Sprintf("record_%d", i), droids.RecordRuntime, `{"ok":true}`),
					},
				})
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		var success, conflict int
		for err := range results {
			switch {
			case err == nil:
				success++
			case errors.Is(err, droids.ErrConflict):
				conflict++
			default:
				t.Fatalf("unexpected commit error: %v", err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d, want 1 each", success, conflict)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		store := factory(t)
		openStore(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := store.State(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("State error = %v, want context.Canceled", err)
		}
	})
}

func openStore(t *testing.T, store droids.Store) droids.OpenConversationResult {
	t.Helper()
	opened, err := store.Open(context.Background(), droids.OpenConversation{
		ID: "conversation_test",
		InitialRecords: []droids.EncodedRecord{
			record("state", "current", droids.RecordRuntime, `{"status":"ready"}`),
			record("audit", "initial", droids.RecordHistory, `{"created":true}`),
		},
		InitialEvents: []droids.EncodedDurableEvent{event("conversation.created")},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return opened
}

func record(kind, id string, scope droids.RecordScope, payload string) droids.EncodedRecord {
	return droids.EncodedRecord{Kind: kind, ID: id, Scope: scope, Version: 1, Payload: json.RawMessage(payload)}
}

func mutation(operation droids.MutationOperation, kind, id string, scope droids.RecordScope, payload string) droids.EncodedMutation {
	return droids.EncodedMutation{
		Operation: operation, RecordKind: kind, RecordID: id, Scope: scope,
		Version: 1, Payload: json.RawMessage(payload),
	}
}

func event(kind string) droids.EncodedDurableEvent {
	return droids.EncodedDurableEvent{
		Kind: kind, Version: 1, Payload: json.RawMessage(`{"ok":true}`),
		OccurredAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

func assertRuntimeValue(t *testing.T, state droids.StoredConversation, kind, id, payload string) {
	t.Helper()
	for _, record := range state.RuntimeState {
		if record.Kind == kind && record.ID == id {
			if string(record.Payload) != payload {
				t.Fatalf("record %s/%s payload = %s, want %s", kind, id, record.Payload, payload)
			}
			return
		}
	}
	t.Fatalf("runtime record %s/%s not found", kind, id)
}
