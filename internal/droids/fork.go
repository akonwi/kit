package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	forkRecordPageSize      = 1000
	forkReconciliationLimit = 30 * time.Second
)

// Fork creates an independent ready conversation from this droid's settled
// state. The destination Store must be uninitialized and dedicated to the
// returned child.
func (d *Droid) Fork(ctx context.Context, id ConversationID, options ForkOptions) (ForkResult, error) {
	if d == nil || d.sdk == nil {
		return ForkResult{}, fmt.Errorf("droids: Fork requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ForkResult{}, fmt.Errorf("droids: fork conversation id is required")
	}
	sourceID := d.sdk.conversation
	if id == sourceID {
		return ForkResult{}, fmt.Errorf("droids: fork conversation id must differ from its source")
	}
	destination := options.Store
	if destination == nil {
		destination = NewMemoryStore()
	}

	if lineage, initialized, err := inspectForkDestination(ctx, destination, id); err != nil {
		return ForkResult{}, err
	} else if initialized {
		if lineage != nil && lineage.Point.ConversationID == sourceID {
			return ForkResult{Point: lineage.Point}, &ForkAlreadyInitializedError{Point: lineage.Point}
		}
		return ForkResult{}, ErrForkDestinationExists
	}

	point, source, records, config, err := d.captureFork(ctx)
	if err != nil {
		return ForkResult{}, err
	}
	childState, err := forkRuntimeState(source, sourceID, id, point)
	if err != nil {
		return ForkResult{}, err
	}
	forkedRecords := make([]EncodedRecord, 0, len(records)+2)
	for _, record := range records {
		forked, transformErr := forkHistoryRecord(record, sourceID, id)
		if transformErr != nil {
			return ForkResult{}, transformErr
		}
		forkedRecords = append(forkedRecords, forked)
	}

	operationID, err := newID("fork_")
	if err != nil {
		return ForkResult{}, err
	}
	lineage := durableForkLineage{Point: point, OperationID: operationID}
	runtimeRecord, err := runtimeEncodedRecord(childState)
	if err != nil {
		return ForkResult{}, err
	}
	lineageRecord, err := lineageEncodedRecord(lineage)
	if err != nil {
		return ForkResult{}, err
	}
	forkedRecords = append(forkedRecords, runtimeRecord, lineageRecord)
	createdEvent, err := lifecycleEvent("conversation.created", "", "", nil)
	if err != nil {
		return ForkResult{}, err
	}
	forkedEvent, err := lifecycleEvent("conversation.forked", "", "", map[string]any{
		"parent_conversation_id": point.ConversationID,
		"parent_revision":        point.Revision,
		"parent_last_event":      point.LastEvent,
	})
	if err != nil {
		return ForkResult{}, err
	}

	opened, openErr := destination.Open(ctx, OpenConversation{
		ID: id, InitialRecords: forkedRecords,
		InitialEvents: []EncodedDurableEvent{createdEvent, forkedEvent},
	})
	reconcileContext, reconcileCancel := context.WithTimeout(context.Background(), forkReconciliationLimit)
	defer reconcileCancel()
	if openErr != nil {
		stored, inspectErr := destination.State(reconcileContext)
		if inspectErr != nil {
			return ForkResult{}, errors.Join(openErr, inspectErr)
		}
		persisted, inspectErr := matchingForkLineage(stored, id, lineage)
		if inspectErr != nil {
			return ForkResult{}, errors.Join(openErr, inspectErr)
		}
		if !persisted {
			result, existingErr := existingForkResult(stored, id, sourceID)
			return result, errors.Join(openErr, existingErr)
		}
	} else if !opened.Created {
		return existingForkResult(opened.Conversation, id, sourceID)
	}

	config.Store = destination
	child, err := Open(reconcileContext, id, config)
	if err != nil {
		return ForkResult{}, fmt.Errorf("droids: open forked conversation: %w", err)
	}
	return ForkResult{Droid: child, Point: point}, nil
}

func (d *Droid) captureFork(ctx context.Context) (
	ForkPoint,
	durableRuntime,
	[]EncodedRecord,
	Config,
	error,
) {
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ForkPoint{}, durableRuntime{}, nil, Config{}, ErrClosed
	}
	if rt.contextFlight != nil {
		return ForkPoint{}, durableRuntime{}, nil, Config{}, ErrBusy
	}
	if !forkableStatus(rt.state.Status) {
		return ForkPoint{}, durableRuntime{}, nil, Config{}, ErrBusy
	}
	if rt.state.AttemptOpen || len(rt.state.PendingSteering) != 0 || len(rt.state.Tools) != 0 {
		return ForkPoint{}, durableRuntime{}, nil, Config{}, fmt.Errorf("droids: settled fork source contains active execution state")
	}
	state, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return ForkPoint{}, durableRuntime{}, nil, Config{}, err
	}
	point := ForkPoint{
		ConversationID: rt.conversation,
		Revision:       rt.revision,
		LastEvent:      rt.lastEvent,
	}
	var records []EncodedRecord
	var after uint64
	for {
		page, err := rt.store.Records(ctx, RecordQuery{After: after, Limit: forkRecordPageSize})
		if err != nil {
			return ForkPoint{}, durableRuntime{}, nil, Config{}, fmt.Errorf("droids: read fork history: %w", err)
		}
		for _, record := range page.Records {
			if record.Scope != RecordHistory || record.Sequence <= after {
				return ForkPoint{}, durableRuntime{}, nil, Config{}, fmt.Errorf("droids: fork history is not strictly ordered")
			}
			after = record.Sequence
			record.Payload = append(json.RawMessage(nil), record.Payload...)
			records = append(records, record)
		}
		if !page.HasMore {
			break
		}
		if len(page.Records) == 0 || page.Next != after {
			return ForkPoint{}, durableRuntime{}, nil, Config{}, fmt.Errorf("droids: fork history pagination did not advance")
		}
	}
	config := cloneForkConfig(rt.config)
	return point, state, records, config, nil
}

func forkableStatus(status ExecutionStatus) bool {
	switch status {
	case ExecutionReady, ExecutionCompleted, ExecutionFailed, ExecutionAborted:
		return true
	default:
		return false
	}
}

func cloneForkConfig(config Config) Config {
	config.Tools = append([]AnyTool(nil), config.Tools...)
	if config.Retry != nil {
		policy := *config.Retry
		config.Retry = &policy
	}
	if config.Execution != nil {
		policy := *config.Execution
		config.Execution = &policy
	}
	return config
}

func forkRuntimeState(source durableRuntime, sourceID, destinationID ConversationID, point ForkPoint) (durableRuntime, error) {
	child := newDurableRuntime()
	child.Context = append([]wireMessageEnvelope(nil), source.Context...)
	for index := range child.Context {
		if err := rewriteForkEnvelope(&child.Context[index], sourceID, destinationID); err != nil {
			return durableRuntime{}, err
		}
	}
	child.PendingBoundaries = append([]durableBoundary(nil), source.PendingBoundaries...)
	child.CheckpointID = source.CheckpointID
	child.SessionUsage = source.SessionUsage
	child.SessionUsageInitialized = source.SessionUsageInitialized
	if point.ConversationID != sourceID {
		return durableRuntime{}, fmt.Errorf("droids: fork point does not identify its source")
	}
	return child, nil
}

func forkHistoryRecord(record EncodedRecord, sourceID, destinationID ConversationID) (EncodedRecord, error) {
	if record.Scope != RecordHistory {
		return EncodedRecord{}, fmt.Errorf("droids: fork source returned non-historical record %s/%s", record.Kind, record.ID)
	}
	forked := cloneEncodedRecord(record)
	if record.Version != recordVersion {
		return EncodedRecord{}, fmt.Errorf("droids: cannot fork %s record version %d", record.Kind, record.Version)
	}
	switch record.Kind {
	case messageRecordKind:
		var envelope wireMessageEnvelope
		if err := json.Unmarshal(record.Payload, &envelope); err != nil {
			return EncodedRecord{}, fmt.Errorf("droids: decode fork message %q: %w", record.ID, err)
		}
		if err := rewriteForkEnvelope(&envelope, sourceID, destinationID); err != nil {
			return EncodedRecord{}, fmt.Errorf("droids: rewrite fork message %q: %w", record.ID, err)
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			return EncodedRecord{}, err
		}
		forked.Payload = payload
	case turnRecordKind:
		payload, err := forkTurnPayload(record.Payload, sourceID, destinationID)
		if err != nil {
			return EncodedRecord{}, fmt.Errorf("droids: rewrite fork turn %q: %w", record.ID, err)
		}
		forked.Payload = payload
	case checkpointKind:
		payload, err := forkCheckpointPayload(record.Payload, sourceID, destinationID)
		if err != nil {
			return EncodedRecord{}, fmt.Errorf("droids: rewrite fork checkpoint %q: %w", record.ID, err)
		}
		forked.Payload = payload
	case attemptRecordKind, toolRecordKind, boundaryReceiptKind, compactionIntentKind, compactionReceiptKind, usageContributionKind:
		if !json.Valid(record.Payload) {
			return EncodedRecord{}, fmt.Errorf("droids: %s record %q is not valid JSON", record.Kind, record.ID)
		}
	default:
		return EncodedRecord{}, fmt.Errorf("droids: cannot fork unknown history record kind %q", record.Kind)
	}
	return forked, nil
}

func forkTurnPayload(payload json.RawMessage, sourceID, destinationID ConversationID) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, err
	}
	raw, exists := object["final"]
	if !exists || string(raw) == "null" {
		return json.Marshal(object)
	}
	var final wireMessageEnvelope
	if err := json.Unmarshal(raw, &final); err != nil {
		return nil, err
	}
	if err := rewriteForkEnvelope(&final, sourceID, destinationID); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(final)
	if err != nil {
		return nil, err
	}
	object["final"] = encoded
	return json.Marshal(object)
}

func forkCheckpointPayload(payload json.RawMessage, sourceID, destinationID ConversationID) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, err
	}
	found := false
	if raw, exists := object["messages"]; exists {
		var messages []wireMessageEnvelope
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, err
		}
		for index := range messages {
			if err := rewriteForkEnvelope(&messages[index], sourceID, destinationID); err != nil {
				return nil, err
			}
		}
		encoded, err := json.Marshal(messages)
		if err != nil {
			return nil, err
		}
		object["messages"] = encoded
		found = true
	}
	if raw, exists := object["summary_message"]; exists {
		var message wireMessageEnvelope
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, err
		}
		if err := rewriteForkEnvelope(&message, sourceID, destinationID); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		object["summary_message"] = encoded
		found = true
	}
	if !found {
		return nil, fmt.Errorf("checkpoint contains no context messages")
	}
	return json.Marshal(object)
}

func rewriteForkEnvelope(envelope *wireMessageEnvelope, sourceID, destinationID ConversationID) error {
	if envelope.ConversationID != sourceID {
		return fmt.Errorf("message %q belongs to conversation %q", envelope.ID, envelope.ConversationID)
	}
	envelope.ConversationID = destinationID
	return nil
}

func inspectForkDestination(
	ctx context.Context,
	store Store,
	destinationID ConversationID,
) (*durableForkLineage, bool, error) {
	state, err := store.State(ctx)
	if errors.Is(err, ErrStoreUninitialized) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("droids: inspect fork destination: %w", err)
	}
	if state.ID != destinationID {
		return nil, true, nil
	}
	lineage, err := decodeLineage(state.RuntimeState)
	if err != nil {
		return nil, true, err
	}
	return lineage, true, nil
}

func existingForkResult(state StoredConversation, id, sourceID ConversationID) (ForkResult, error) {
	if state.ID != id {
		return ForkResult{}, ErrForkDestinationExists
	}
	lineage, err := decodeLineage(state.RuntimeState)
	if err != nil {
		return ForkResult{}, err
	}
	if lineage != nil && lineage.Point.ConversationID == sourceID {
		return ForkResult{Point: lineage.Point}, &ForkAlreadyInitializedError{Point: lineage.Point}
	}
	return ForkResult{}, ErrForkDestinationExists
}

func matchingForkLineage(state StoredConversation, id ConversationID, expected durableForkLineage) (bool, error) {
	if state.ID != id {
		return false, nil
	}
	lineage, err := decodeLineage(state.RuntimeState)
	if err != nil {
		return false, err
	}
	return lineage != nil && lineage.OperationID == expected.OperationID && lineage.Point == expected.Point, nil
}
