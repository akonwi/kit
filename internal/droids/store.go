package droids

import (
	"context"
	"encoding/json"
	"time"
)

// ConversationID identifies one droid conversation. Store instances are bound
// to exactly one conversation after Open.
type ConversationID string

// TurnID identifies one durable user-facing turn.
type TurnID string

// AttemptID identifies one provider/tool-loop attempt within a turn.
type AttemptID string

// MessageID identifies one canonical message.
type MessageID string

// ToolCallID identifies one provider-issued tool call.
type ToolCallID string

// CheckpointID identifies one active-context checkpoint.
type CheckpointID string

// EventSequence is a monotonically increasing durable event cursor.
type EventSequence uint64

// RecordScope separates bounded mutable runtime state from immutable diagnostic
// history.
type RecordScope string

const (
	RecordRuntime RecordScope = "runtime"
	RecordHistory RecordScope = "history"
)

// MutationOperation describes one atomic record mutation.
type MutationOperation string

const (
	MutationPut          MutationOperation = "put"
	MutationDelete       MutationOperation = "delete"
	MutationAssertAbsent MutationOperation = "assert_absent"
)

// EncodedRecord is a versioned droids-domain record. Payload is canonical JSON
// encoded by droids and opaque to Store implementations.
type EncodedRecord struct {
	Kind     string
	ID       string
	Scope    RecordScope
	Sequence uint64
	Version  uint16
	Payload  json.RawMessage
}

// EncodedMutation mutates one record. AssertAbsent inserts the supplied record
// only when its stable kind/id does not already exist.
type EncodedMutation struct {
	Operation  MutationOperation
	RecordKind string
	RecordID   string
	Scope      RecordScope
	Version    uint16
	Payload    json.RawMessage
}

// EncodedDurableEvent is appended to the Store outbox in the same transaction
// as its corresponding state transition.
type EncodedDurableEvent struct {
	Kind       string
	Version    uint16
	Payload    json.RawMessage
	OccurredAt time.Time
}

// StoredEvent is one sequenced durable outbox event.
type StoredEvent struct {
	Sequence   EventSequence
	Revision   uint64
	Kind       string
	Version    uint16
	Payload    json.RawMessage
	OccurredAt time.Time
}

// StoredConversation is the bounded persisted state needed to run a droid.
type StoredConversation struct {
	ID           ConversationID
	Revision     uint64
	RuntimeState []EncodedRecord
	LastEvent    EventSequence
}

// OpenConversation initializes a new Store when it is empty.
type OpenConversation struct {
	ID             ConversationID
	InitialRecords []EncodedRecord
	InitialEvents  []EncodedDurableEvent
}

// OpenConversationResult reports whether Store.Open initialized the Store.
type OpenConversationResult struct {
	Conversation StoredConversation
	Created      bool
}

// CommitRequest atomically applies record mutations and appends events when the
// expected conversation revision matches.
type CommitRequest struct {
	ExpectedRevision uint64
	Mutations        []EncodedMutation
	Events           []EncodedDurableEvent
}

// CommitResult identifies the committed state and outbox high-water mark.
type CommitResult struct {
	Revision  uint64
	LastEvent EventSequence
}

// RecordQuery pages immutable history in ascending sequence order.
type RecordQuery struct {
	After      uint64
	Before     uint64
	Limit      int
	Kind       string
	Descending bool
}

// RecordPage is one page of immutable diagnostic records.
type RecordPage struct {
	Records []EncodedRecord
	Next    uint64
	HasMore bool
}

// EventQuery pages durable events in ascending sequence order.
type EventQuery struct {
	After EventSequence
	Limit int
}

// EventPage is one page of durable outbox events.
type EventPage struct {
	Events  []StoredEvent
	Next    EventSequence
	HasMore bool
}

// Store persists exactly one droid conversation. Implementations must apply a
// Commit's mutations, revision update, and events atomically.
type Store interface {
	Open(context.Context, OpenConversation) (OpenConversationResult, error)
	Commit(context.Context, CommitRequest) (CommitResult, error)
	State(context.Context) (StoredConversation, error)
	Records(context.Context, RecordQuery) (RecordPage, error)
	Events(context.Context, EventQuery) (EventPage, error)
}
