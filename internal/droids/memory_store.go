package droids

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	defaultStorePageSize = 100
	maxStorePageSize     = 1000
)

type recordKey struct {
	kind string
	id   string
}

// MemoryStore is a concurrency-safe, process-local Store with the same record,
// revision, and outbox semantics as the SQLite adapter.
type MemoryStore struct {
	mu sync.Mutex

	opened             bool
	id                 ConversationID
	revision           uint64
	lastRecordSequence uint64
	lastEventSequence  EventSequence
	records            map[recordKey]EncodedRecord
	events             []StoredEvent
}

// MemoryStoreOption configures a MemoryStore. It is reserved for deterministic
// test options; the initial implementation has no public options.
type MemoryStoreOption func(*MemoryStore)

// NewMemoryStore returns an empty Store dedicated to one droid conversation.
func NewMemoryStore(options ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{records: make(map[recordKey]EncodedRecord)}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

// Open binds an empty store to the conversation or returns its current state.
func (s *MemoryStore) Open(ctx context.Context, request OpenConversation) (OpenConversationResult, error) {
	if err := contextError(ctx); err != nil {
		return OpenConversationResult{}, err
	}
	if request.ID == "" {
		return OpenConversationResult{}, fmt.Errorf("droids: conversation id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened {
		if s.id != request.ID {
			return OpenConversationResult{}, fmt.Errorf("droids: store is already initialized")
		}
		return OpenConversationResult{Conversation: s.stateLocked()}, nil
	}

	records := make(map[recordKey]EncodedRecord, len(request.InitialRecords))
	var lastRecord uint64
	for _, input := range request.InitialRecords {
		record, next, err := prepareInitialRecord(input, lastRecord)
		if err != nil {
			return OpenConversationResult{}, err
		}
		key := recordKey{kind: record.Kind, id: record.ID}
		if _, exists := records[key]; exists {
			return OpenConversationResult{}, fmt.Errorf("droids: duplicate initial record %s/%s", record.Kind, record.ID)
		}
		records[key] = record
		lastRecord = next
	}

	events := make([]StoredEvent, 0, len(request.InitialEvents))
	for _, input := range request.InitialEvents {
		if err := validateEncodedEvent(input); err != nil {
			return OpenConversationResult{}, err
		}
		sequence := EventSequence(len(events) + 1)
		events = append(events, storedEvent(input, sequence, 1))
	}

	s.opened = true
	s.id = request.ID
	s.revision = 1
	s.lastRecordSequence = lastRecord
	s.lastEventSequence = EventSequence(len(events))
	s.records = records
	s.events = events
	return OpenConversationResult{Conversation: s.stateLocked(), Created: true}, nil
}

// Commit applies a complete state-plus-outbox transition.
func (s *MemoryStore) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	if err := contextError(ctx); err != nil {
		return CommitResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return CommitResult{}, fmt.Errorf("droids: store is not open")
	}
	if request.ExpectedRevision != s.revision {
		return CommitResult{}, ErrConflict
	}

	records := cloneRecordMap(s.records)
	lastRecord := s.lastRecordSequence
	nextRevision := s.revision + 1
	for _, mutation := range request.Mutations {
		var err error
		lastRecord, err = applyMemoryMutation(records, lastRecord, nextRevision, mutation)
		if err != nil {
			return CommitResult{}, err
		}
	}

	events := append([]StoredEvent(nil), s.events...)
	lastEvent := s.lastEventSequence
	for _, input := range request.Events {
		if err := validateEncodedEvent(input); err != nil {
			return CommitResult{}, err
		}
		lastEvent++
		events = append(events, storedEvent(input, lastEvent, nextRevision))
	}

	s.records = records
	s.events = events
	s.revision = nextRevision
	s.lastRecordSequence = lastRecord
	s.lastEventSequence = lastEvent
	return CommitResult{Revision: nextRevision, LastEvent: lastEvent}, nil
}

// State returns a deep copy of the bounded runtime state.
func (s *MemoryStore) State(ctx context.Context) (StoredConversation, error) {
	if err := contextError(ctx); err != nil {
		return StoredConversation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return StoredConversation{}, fmt.Errorf("droids: store is not open")
	}
	return s.stateLocked(), nil
}

// Records pages immutable history in ascending sequence order.
func (s *MemoryStore) Records(ctx context.Context, query RecordQuery) (RecordPage, error) {
	if err := contextError(ctx); err != nil {
		return RecordPage{}, err
	}
	limit, err := storePageSize(query.Limit)
	if err != nil {
		return RecordPage{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return RecordPage{}, fmt.Errorf("droids: store is not open")
	}
	var records []EncodedRecord
	for _, record := range s.records {
		if record.Scope != RecordHistory || record.Sequence <= query.After {
			continue
		}
		if query.Before > 0 && record.Sequence >= query.Before {
			continue
		}
		if query.Kind != "" && record.Kind != query.Kind {
			continue
		}
		records = append(records, cloneEncodedRecord(record))
	}
	sort.Slice(records, func(i, j int) bool {
		if query.Descending {
			return records[i].Sequence > records[j].Sequence
		}
		return records[i].Sequence < records[j].Sequence
	})
	page := RecordPage{}
	if len(records) > limit {
		page.HasMore = true
		records = records[:limit]
	}
	page.Records = records
	if len(records) > 0 {
		page.Next = records[len(records)-1].Sequence
	}
	return page, nil
}

// Events pages durable events in ascending sequence order.
func (s *MemoryStore) Events(ctx context.Context, query EventQuery) (EventPage, error) {
	if err := contextError(ctx); err != nil {
		return EventPage{}, err
	}
	limit, err := storePageSize(query.Limit)
	if err != nil {
		return EventPage{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return EventPage{}, fmt.Errorf("droids: store is not open")
	}
	page := EventPage{}
	for _, event := range s.events {
		if event.Sequence <= query.After {
			continue
		}
		if len(page.Events) == limit {
			page.HasMore = true
			break
		}
		page.Events = append(page.Events, cloneStoredEvent(event))
	}
	if len(page.Events) > 0 {
		page.Next = page.Events[len(page.Events)-1].Sequence
	}
	return page, nil
}

func (s *MemoryStore) stateLocked() StoredConversation {
	state := StoredConversation{ID: s.id, Revision: s.revision, LastEvent: s.lastEventSequence}
	for _, record := range s.records {
		if record.Scope == RecordRuntime {
			state.RuntimeState = append(state.RuntimeState, cloneEncodedRecord(record))
		}
	}
	sort.Slice(state.RuntimeState, func(i, j int) bool {
		if state.RuntimeState[i].Kind == state.RuntimeState[j].Kind {
			return state.RuntimeState[i].ID < state.RuntimeState[j].ID
		}
		return state.RuntimeState[i].Kind < state.RuntimeState[j].Kind
	})
	return state
}

func prepareInitialRecord(input EncodedRecord, lastSequence uint64) (EncodedRecord, uint64, error) {
	if err := validateRecord(input.Kind, input.ID, input.Scope, input.Version, input.Payload); err != nil {
		return EncodedRecord{}, lastSequence, err
	}
	record := cloneEncodedRecord(input)
	switch record.Scope {
	case RecordRuntime:
		record.Sequence = 0
	case RecordHistory:
		lastSequence++
		record.Sequence = lastSequence
	}
	return record, lastSequence, nil
}

func applyMemoryMutation(records map[recordKey]EncodedRecord, lastSequence, _ uint64, mutation EncodedMutation) (uint64, error) {
	key := recordKey{kind: mutation.RecordKind, id: mutation.RecordID}
	existing, exists := records[key]
	if mutation.Operation == MutationDelete {
		if !exists {
			return lastSequence, nil
		}
		if existing.Scope == RecordHistory {
			return lastSequence, fmt.Errorf("droids: historical record %s/%s is immutable", key.kind, key.id)
		}
		delete(records, key)
		return lastSequence, nil
	}
	if err := validateRecord(mutation.RecordKind, mutation.RecordID, mutation.Scope, mutation.Version, mutation.Payload); err != nil {
		return lastSequence, err
	}
	if mutation.Operation == MutationAssertAbsent && exists {
		return lastSequence, fmt.Errorf("droids: record %s/%s already exists", key.kind, key.id)
	}
	if mutation.Operation != MutationPut && mutation.Operation != MutationAssertAbsent {
		return lastSequence, fmt.Errorf("droids: unsupported mutation operation %q", mutation.Operation)
	}
	if exists && existing.Scope == RecordHistory {
		return lastSequence, fmt.Errorf("droids: historical record %s/%s is immutable", key.kind, key.id)
	}
	record := EncodedRecord{
		Kind: mutation.RecordKind, ID: mutation.RecordID, Scope: mutation.Scope,
		Version: mutation.Version, Payload: append([]byte(nil), mutation.Payload...),
	}
	if record.Scope == RecordHistory {
		lastSequence++
		record.Sequence = lastSequence
	}
	records[key] = record
	return lastSequence, nil
}

func validateRecord(kind, id string, scope RecordScope, version uint16, payload []byte) error {
	if kind == "" || id == "" {
		return fmt.Errorf("droids: record kind and id are required")
	}
	if scope != RecordRuntime && scope != RecordHistory {
		return fmt.Errorf("droids: unsupported record scope %q", scope)
	}
	if version == 0 {
		return fmt.Errorf("droids: record version is required")
	}
	if len(payload) == 0 {
		return fmt.Errorf("droids: record payload is required")
	}
	return nil
}

func validateEncodedEvent(event EncodedDurableEvent) error {
	if event.Kind == "" || event.Version == 0 || len(event.Payload) == 0 {
		return fmt.Errorf("droids: event kind, version, and payload are required")
	}
	return nil
}

func storedEvent(input EncodedDurableEvent, sequence EventSequence, revision uint64) StoredEvent {
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return StoredEvent{
		Sequence: sequence, Revision: revision, Kind: input.Kind, Version: input.Version,
		Payload: append([]byte(nil), input.Payload...), OccurredAt: occurredAt,
	}
}

func cloneRecordMap(input map[recordKey]EncodedRecord) map[recordKey]EncodedRecord {
	out := make(map[recordKey]EncodedRecord, len(input))
	for key, record := range input {
		out[key] = cloneEncodedRecord(record)
	}
	return out
}

func cloneEncodedRecord(record EncodedRecord) EncodedRecord {
	record.Payload = append([]byte(nil), record.Payload...)
	return record
}

func cloneStoredEvent(event StoredEvent) StoredEvent {
	event.Payload = append([]byte(nil), event.Payload...)
	return event
}

func storePageSize(requested int) (int, error) {
	if requested < 0 || requested > maxStorePageSize {
		return 0, fmt.Errorf("droids: page limit must be between 0 and %d", maxStorePageSize)
	}
	if requested == 0 {
		return defaultStorePageSize, nil
	}
	return requested, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
