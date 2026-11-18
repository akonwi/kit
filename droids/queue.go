package droids

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	maxQueuedPrompts       = 64
	maxQueuedMetadataBytes = 16 << 10
	queueAdmissionKind     = "queue_admission"
	queuedPromptKind       = "queued_prompt"
	// maxStartedPrompts bounds unacknowledged started prompts; the queue
	// waits while it is reached.
	maxStartedPrompts       = 2 * maxQueuedPrompts
	queueAdmissionKeyLimit  = 256
	queuedPromptIDPrefix    = "queued_"
	queueBlockedReasonLimit = 1 << 10
)

// QueuedPromptID identifies one prompt admitted to a droid's queue.
type QueuedPromptID string

// QueueOptions configures Enqueue.
type QueueOptions struct {
	// AdmissionKey makes queuing idempotent for an application crossing its own
	// durable boundary. Reuse with different input or metadata returns
	// ErrConflict, including after the prompt has left the queue.
	AdmissionKey string
	// Metadata is bounded application JSON stored with the prompt and returned
	// with it, so an application can restore the prompt as the person wrote it.
	// Droids does not interpret it.
	Metadata json.RawMessage
}

// QueuedPrompt is one prompt waiting to start a turn.
type QueuedPrompt struct {
	ID       QueuedPromptID
	Input    Input
	Metadata json.RawMessage
	QueuedAt time.Time
}

// QueueAdmission reports the result of Enqueue.
type QueueAdmission struct {
	// Prompt is the admitted prompt. A replayed admission whose prompt has left
	// the queue reports only its ID.
	Prompt QueuedPrompt
	// Execution is the turn the prompt started, when it started at once or, for
	// a replayed admission, when its turn is the droid's current turn. It is nil
	// while the prompt waits in the queue.
	Execution ExecutionHandle
	// Replayed reports that AdmissionKey matched an earlier admission.
	Replayed bool
}

// QueueSnapshot reports the prompts waiting to start turns, in order.
type QueueSnapshot struct {
	Prompts []QueuedPrompt
	// Held reports that the queue waits for StartQueue or Enqueue before it
	// starts its next prompt: it was restored by Spawn, or a turn ended in a
	// provider policy stop.
	Held bool
	// Blocked explains why the first prompt could not start. The queue waits
	// until that prompt is withdrawn or promoted.
	Blocked string
}

type durableQueuedPrompt struct {
	ID       QueuedPromptID  `json:"id"`
	Message  wireMessage     `json:"message"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	QueuedAt time.Time       `json:"queued_at"`
}

type queueAdmissionReceipt struct {
	Key      string         `json:"key"`
	Hash     string         `json:"hash"`
	QueuedID QueuedPromptID `json:"queued_id"`
}

// Enqueue durably admits input to start the droid's next turn. Queued prompts
// start in admission order, each after the previous turn settles. When the
// droid is idle and nothing is queued, the prompt starts a turn at once.
// Enqueue also releases a held queue.
//
// A turn that is paused or interrupted holds the queue until it settles. It
// returns ErrBusy when the queue is full.
func (d *Droid) Enqueue(ctx context.Context, input Input, options QueueOptions) (QueueAdmission, error) {
	if d == nil || d.sdk == nil {
		return QueueAdmission{}, fmt.Errorf("droids: Enqueue requires a droid opened with droids.Spawn")
	}
	message, err := inputToMessage(input)
	if err != nil {
		return QueueAdmission{}, err
	}
	wire, err := messageToWire(message)
	if err != nil {
		return QueueAdmission{}, err
	}
	if len(options.Metadata) > 0 && !json.Valid(options.Metadata) {
		return QueueAdmission{}, fmt.Errorf("droids: queued prompt metadata is not valid JSON")
	}
	if len(options.Metadata) > maxQueuedMetadataBytes {
		return QueueAdmission{}, fmt.Errorf("droids: queued prompt metadata exceeds %d bytes", maxQueuedMetadataBytes)
	}
	admissionHash := ""
	if options.AdmissionKey != "" {
		if !validBoundedContextValue(options.AdmissionKey, queueAdmissionKeyLimit) {
			return QueueAdmission{}, fmt.Errorf("droids: queue admission key is invalid")
		}
		admissionHash, err = queueAdmissionHash(wire, options.Metadata)
		if err != nil {
			return QueueAdmission{}, err
		}
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return QueueAdmission{}, ErrClosed
	}
	if options.AdmissionKey != "" {
		admission, found, err := rt.replayQueueAdmissionLocked(ctx, options.AdmissionKey, admissionHash)
		if err != nil || found {
			return admission, err
		}
	}
	if len(rt.state.Queue) >= maxQueuedPrompts {
		return QueueAdmission{}, fmt.Errorf("%w: queue capacity reached", ErrBusy)
	}
	id, err := newID(queuedPromptIDPrefix)
	if err != nil {
		return QueueAdmission{}, err
	}
	prompt := durableQueuedPrompt{
		ID: QueuedPromptID(id), Message: wire,
		Metadata: append(json.RawMessage(nil), options.Metadata...), QueuedAt: time.Now().UTC(),
	}
	var mutations []EncodedMutation
	if options.AdmissionKey != "" {
		receipt, err := queueAdmissionMutation(queueAdmissionReceipt{Key: options.AdmissionKey, Hash: admissionHash, QueuedID: prompt.ID})
		if err != nil {
			return QueueAdmission{}, err
		}
		mutations = append(mutations, receipt)
	}
	body, err := queuedPromptMutation(prompt)
	if err != nil {
		return QueueAdmission{}, err
	}
	mutations = append(mutations, body)
	rt.queueHeld = false
	if rt.queueIdleLocked() && len(rt.state.Queue) == 0 {
		accepted, _ := lifecycleEvent("queue.accepted", "", "", map[string]any{"queued_id": prompt.ID})
		handle, err := rt.startTurnLocked(ctx, &message, "", "", mutations, []EncodedDurableEvent{accepted}, prompt.ID)
		if err != nil {
			return QueueAdmission{}, err
		}
		rt.queued[prompt.ID] = prompt
		return QueueAdmission{Prompt: queuedPromptFromDurable(prompt, message), Execution: handle}, nil
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return QueueAdmission{}, err
	}
	rt.state.Queue = append(rt.state.Queue, prompt.ID)
	accepted, _ := lifecycleEvent("queue.accepted", "", "", map[string]any{"queued_id": prompt.ID})
	if err := rt.commitLocked(ctx, mutations, []EncodedDurableEvent{accepted}); err != nil {
		rt.state = before
		return QueueAdmission{}, err
	}
	rt.queued[prompt.ID] = prompt
	admission := QueueAdmission{Prompt: queuedPromptFromDurable(prompt, message)}
	rt.advanceQueueLocked(ctx)
	if rt.state.QueuedPromptID == prompt.ID && rt.handle != nil && rt.handle.turnID == rt.state.TurnID {
		admission.Execution = rt.handle
	}
	return admission, nil
}

// Queue returns the prompts waiting to start turns, in order.
func (d *Droid) Queue(ctx context.Context) (QueueSnapshot, error) {
	if d == nil || d.sdk == nil {
		return QueueSnapshot{}, fmt.Errorf("droids: Queue requires a droid opened with droids.Spawn")
	}
	if err := contextError(ctx); err != nil {
		return QueueSnapshot{}, err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	prompts, err := queuedPromptsFromDurable(rt.queuedLocked())
	if err != nil {
		return QueueSnapshot{}, err
	}
	return QueueSnapshot{Prompts: prompts, Held: rt.queueHeld && len(prompts) > 0, Blocked: rt.state.QueueBlocked}, nil
}

// StartQueue releases a held queue. When the droid is idle, its next queued
// prompt starts a turn, whose handle is returned; otherwise the queue advances
// when the current turn settles and the handle is nil.
func (d *Droid) StartQueue(ctx context.Context) (ExecutionHandle, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: StartQueue requires a droid opened with droids.Spawn")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrClosed
	}
	rt.queueHeld = false
	if len(rt.state.Queue) == 0 || !rt.queueIdleLocked() {
		return nil, nil
	}
	turnBefore := rt.state.TurnID
	rt.advanceQueueLocked(ctx)
	if rt.state.TurnID != turnBefore && rt.handle != nil && rt.handle.turnID == rt.state.TurnID {
		return rt.handle, nil
	}
	if rt.state.QueueBlocked != "" {
		return nil, fmt.Errorf("droids: queued prompt could not start: %s", rt.state.QueueBlocked)
	}
	return nil, nil
}

// Withdraw removes the identified prompts from the queue and returns them in
// queue order. Identifiers that are no longer queued are skipped. With no
// identifiers it withdraws every queued prompt.
func (d *Droid) Withdraw(ctx context.Context, ids ...QueuedPromptID) ([]QueuedPrompt, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Withdraw requires a droid opened with droids.Spawn")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrClosed
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return nil, err
	}
	removed, kept := partitionQueue(rt.state.Queue, ids)
	if len(removed) == 0 {
		return nil, nil
	}
	prompts, err := queuedPromptsFromDurable(rt.queuedBodiesLocked(removed))
	if err != nil {
		return nil, err
	}
	if rt.state.QueueBlocked != "" && removed[0] == rt.state.Queue[0] {
		rt.state.QueueBlocked = ""
	}
	rt.state.Queue = kept
	event, _ := lifecycleEvent("queue.withdrawn", "", "", map[string]any{"queued_ids": removed})
	if err := rt.commitLocked(ctx, deleteQueuedPromptMutations(removed), []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return nil, err
	}
	rt.forgetQueuedLocked(removed)
	rt.advanceQueueLocked(ctx)
	return prompts, nil
}

// Promote moves the identified queued prompts, in queue order, into the
// running turn as steering. Identifiers that are no longer queued are skipped.
// With no identifiers it promotes every queued prompt. Like steering, it
// returns ErrConflict when no turn is running and ErrBusy when the turn cannot
// accept steering or its steering capacity would be exceeded.
func (d *Droid) Promote(ctx context.Context, ids ...QueuedPromptID) ([]QueuedPrompt, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Promote requires a droid opened with droids.Spawn")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrClosed
	}
	if rt.contextFlight != nil {
		return nil, ErrBusy
	}
	if !isOccupied(rt.state.Status) {
		return nil, ErrConflict
	}
	if rt.state.Status == ExecutionInterrupted || rt.state.Status == ExecutionAborting {
		return nil, ErrBusy
	}
	removed, kept := partitionQueue(rt.state.Queue, ids)
	if len(removed) == 0 {
		return nil, nil
	}
	if len(rt.state.PendingSteering)+len(removed) > maxPendingSteering {
		return nil, fmt.Errorf("%w: pending steering capacity reached", ErrBusy)
	}
	prompts, err := queuedPromptsFromDurable(rt.queuedBodiesLocked(removed))
	if err != nil {
		return nil, err
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return nil, err
	}
	mutations := deleteQueuedPromptMutations(removed)
	var events []EncodedDurableEvent
	receipts := make(map[string]MessageID)
	messageIDs := make([]MessageID, 0, len(removed))
	for index, prompt := range prompts {
		message := UserMessage{Content: prompt.Input.Content, Timestamp: time.Now().UnixMilli()}
		messageID, err := newMessageID()
		if err != nil {
			rt.state = before
			return nil, err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: rt.state.TurnID,
			CreatedAt: time.Now().UTC(), Message: message,
		}
		wire, err := messageEnvelopeToWire(envelope)
		if err != nil {
			rt.state = before
			return nil, err
		}
		rt.state.PendingSteering = append(rt.state.PendingSteering, wire)
		receipt, err := annotationSubmissionReceiptMutation(message, messageID)
		if err != nil {
			rt.state = before
			return nil, err
		}
		if receipt != nil {
			mutations = append(mutations, *receipt)
			receipts[receipt.RecordID] = messageID
		}
		event, _ := lifecycleEvent("steering.accepted", rt.state.TurnID, rt.state.AttemptID, map[string]any{
			"message_id": messageID, "queued_id": removed[index],
		})
		events = append(events, event)
		messageIDs = append(messageIDs, messageID)
	}
	if rt.state.QueueBlocked != "" && removed[0] == rt.state.Queue[0] {
		rt.state.QueueBlocked = ""
	}
	rt.state.Queue = kept
	promoted, _ := lifecycleEvent("queue.promoted", rt.state.TurnID, rt.state.AttemptID, map[string]any{
		"queued_ids": removed, "message_ids": messageIDs,
	})
	events = append(events, promoted)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return nil, err
	}
	rt.forgetQueuedLocked(removed)
	for submission, messageID := range receipts {
		rt.annotationSubmissions[submission] = messageID
	}
	return prompts, nil
}

// queueIdleLocked reports whether a queued prompt could start a turn now.
func (rt *sdkRuntime) queueIdleLocked() bool {
	return !rt.closed && !isOccupied(rt.state.Status) && rt.contextFlight == nil && rt.resumeFlight == nil && len(rt.state.Started) < maxStartedPrompts
}

// forgetUntrackedStartedLocked drops started prompts of settled turns when the
// application does not track them, as a conversation reopened without
// Config.TrackStartedPrompts after running with it. The caller holds rt.mu.
func (rt *sdkRuntime) forgetUntrackedStartedLocked(ctx context.Context) error {
	if rt.config.TrackStartedPrompts || len(rt.state.Started) == 0 {
		return nil
	}
	unsettled := rt.handle != nil
	before := rt.state
	var forgotten []QueuedPromptID
	var kept []startedPromptRef
	for _, ref := range rt.state.Started {
		if unsettled && ref.TurnID == rt.state.TurnID {
			kept = append(kept, ref)
		} else {
			forgotten = append(forgotten, ref.ID)
		}
	}
	if len(forgotten) == 0 {
		return nil
	}
	rt.state.Started = kept
	if err := rt.commitLocked(ctx, deleteQueuedPromptMutations(forgotten), nil); err != nil {
		rt.state = before
		return err
	}
	rt.forgetQueuedLocked(forgotten)
	return nil
}

// acknowledgeSettledLocked removes the started prompt of the settling turn
// when the application does not track started prompts, and returns it. The
// caller holds rt.mu and commits the removal with the settlement.
func (rt *sdkRuntime) acknowledgeSettledLocked() []QueuedPromptID {
	if rt.config.TrackStartedPrompts {
		return nil
	}
	var acknowledged []QueuedPromptID
	kept := rt.state.Started[:0:0]
	for _, ref := range rt.state.Started {
		if ref.TurnID == rt.state.TurnID {
			acknowledged = append(acknowledged, ref.ID)
		} else {
			kept = append(kept, ref)
		}
	}
	rt.state.Started = kept
	return acknowledged
}

// advanceQueueLocked starts the next queued prompt when the droid is idle and
// the queue is neither held nor blocked. A prompt that cannot start blocks the
// queue with the reason. The caller holds rt.mu.
func (rt *sdkRuntime) advanceQueueLocked(ctx context.Context) {
	if rt.queueHeld || rt.state.QueueBlocked != "" || len(rt.state.Queue) == 0 || !rt.queueIdleLocked() {
		return
	}
	ctx = context.WithoutCancel(ctx)
	head := rt.queued[rt.state.Queue[0]]
	decoded, err := messageFromWire(head.Message)
	message, ok := decoded.(UserMessage)
	if err == nil && !ok {
		err = fmt.Errorf("droids: queued prompt %q is not a user message", head.ID)
	}
	if err == nil {
		message.Timestamp = time.Now().UnixMilli()
		before := rt.state.Queue
		rt.state.Queue = slices.Clone(before[1:])
		// The prompt's body stays until its turn settles, so the application
		// can still read the prompt that started the turn after a restart.
		_, err = rt.startTurnLocked(ctx, &message, "", "", nil, nil, head.ID)
		if err != nil {
			rt.state.Queue = before
		}
	}
	if err == nil {
		return
	}
	reason := truncateUTF8(err.Error(), queueBlockedReasonLimit)
	rt.state.QueueBlocked = reason
	event, _ := lifecycleEvent("queue.blocked", "", "", map[string]any{"queued_id": head.ID, "reason": reason})
	// The blocked reason stays in memory if it cannot be recorded; the queue
	// waits either way.
	_ = rt.commitLocked(ctx, nil, []EncodedDurableEvent{event})
}

// replayQueueAdmissionLocked resolves an earlier admission with key. The caller
// holds rt.mu.
func (rt *sdkRuntime) replayQueueAdmissionLocked(ctx context.Context, key, hash string) (QueueAdmission, bool, error) {
	record, err := rt.store.Record(ctx, queueAdmissionKind, key)
	if errors.Is(err, ErrRecordNotFound) {
		return QueueAdmission{}, false, nil
	}
	if err != nil {
		return QueueAdmission{}, false, fmt.Errorf("droids: load queue admission: %w", err)
	}
	var receipt queueAdmissionReceipt
	if record.Scope != RecordHistory || record.Version != recordVersion || json.Unmarshal(record.Payload, &receipt) != nil || receipt.Key != key || !validID(string(receipt.QueuedID), queuedPromptIDPrefix) {
		return QueueAdmission{}, false, fmt.Errorf("droids: queue admission receipt is invalid")
	}
	if receipt.Hash != hash {
		return QueueAdmission{}, false, ErrConflict
	}
	admission := QueueAdmission{Prompt: QueuedPrompt{ID: receipt.QueuedID}, Replayed: true}
	if queued, ok := rt.queued[receipt.QueuedID]; ok && slices.Contains(rt.state.Queue, receipt.QueuedID) {
		prompts, err := queuedPromptsFromDurable([]durableQueuedPrompt{queued})
		if err != nil {
			return QueueAdmission{}, false, err
		}
		admission.Prompt = prompts[0]
		return admission, true, nil
	}
	if rt.state.QueuedPromptID == receipt.QueuedID {
		handle := rt.ensureHandleLocked()
		if isTerminalStatus(rt.state.Status) {
			handle.complete(outcomeFromState(rt.conversation, rt.state))
		}
		admission.Execution = handle
	}
	return admission, true, nil
}

func queueAdmissionHash(message wireMessage, metadata json.RawMessage) (string, error) {
	encoded, err := json.Marshal(struct {
		Message  wireMessage     `json:"message"`
		Metadata json.RawMessage `json:"metadata,omitempty"`
	}{message, metadata})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func queueAdmissionMutation(receipt queueAdmissionReceipt) (EncodedMutation, error) {
	payload, err := json.Marshal(receipt)
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: queueAdmissionKind, RecordID: receipt.Key,
		Scope: RecordHistory, Version: recordVersion, Payload: payload,
	}, nil
}

func partitionQueue(queue []QueuedPromptID, ids []QueuedPromptID) (removed, kept []QueuedPromptID) {
	for _, id := range queue {
		if len(ids) == 0 || slices.Contains(ids, id) {
			removed = append(removed, id)
		} else {
			kept = append(kept, id)
		}
	}
	return removed, kept
}

// queuedLocked returns the queued prompt bodies in queue order. The caller
// holds rt.mu.
func (rt *sdkRuntime) queuedLocked() []durableQueuedPrompt {
	return rt.queuedBodiesLocked(rt.state.Queue)
}

func (rt *sdkRuntime) queuedBodiesLocked(ids []QueuedPromptID) []durableQueuedPrompt {
	prompts := make([]durableQueuedPrompt, 0, len(ids))
	for _, id := range ids {
		prompts = append(prompts, rt.queued[id])
	}
	return prompts
}

func (rt *sdkRuntime) forgetQueuedLocked(ids []QueuedPromptID) {
	for _, id := range ids {
		delete(rt.queued, id)
	}
}

func queuedPromptMutation(prompt durableQueuedPrompt) (EncodedMutation, error) {
	payload, err := json.Marshal(prompt)
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: queuedPromptKind, RecordID: string(prompt.ID),
		Scope: RecordRuntime, Version: recordVersion, Payload: payload,
	}, nil
}

func deleteQueuedPromptMutations(ids []QueuedPromptID) []EncodedMutation {
	mutations := make([]EncodedMutation, 0, len(ids))
	for _, id := range ids {
		mutations = append(mutations, EncodedMutation{
			Operation: MutationDelete, RecordKind: queuedPromptKind, RecordID: string(id),
			Scope: RecordRuntime, Version: recordVersion,
		})
	}
	return mutations
}

func queuedPromptFromDurable(prompt durableQueuedPrompt, message UserMessage) QueuedPrompt {
	return QueuedPrompt{
		ID: prompt.ID, Input: Input{Content: slices.Clone(message.Content)},
		Metadata: append(json.RawMessage(nil), prompt.Metadata...), QueuedAt: prompt.QueuedAt,
	}
}

func queuedPromptsFromDurable(queue []durableQueuedPrompt) ([]QueuedPrompt, error) {
	prompts := make([]QueuedPrompt, 0, len(queue))
	for _, prompt := range queue {
		decoded, err := messageFromWire(prompt.Message)
		if err != nil {
			return nil, fmt.Errorf("droids: decode queued prompt %q: %w", prompt.ID, err)
		}
		message, ok := decoded.(UserMessage)
		if !ok {
			return nil, fmt.Errorf("droids: queued prompt %q is not a user message", prompt.ID)
		}
		prompts = append(prompts, queuedPromptFromDurable(prompt, message))
	}
	return prompts, nil
}

// loadQueuedPrompts loads and validates the bodies of the queued prompts and
// of the unacknowledged started prompts from a conversation's runtime records.
func loadQueuedPrompts(records []EncodedRecord, queue []QueuedPromptID, started []startedPromptRef) (map[QueuedPromptID]durableQueuedPrompt, error) {
	if len(queue) > maxQueuedPrompts || len(started) > maxStartedPrompts {
		return nil, fmt.Errorf("droids: persisted queue is too long")
	}
	bodies := make(map[QueuedPromptID]durableQueuedPrompt)
	for _, record := range records {
		if record.Kind != queuedPromptKind {
			continue
		}
		var prompt durableQueuedPrompt
		if record.Scope != RecordRuntime || record.Version != recordVersion || json.Unmarshal(record.Payload, &prompt) != nil || string(prompt.ID) != record.ID {
			return nil, fmt.Errorf("droids: persisted queued prompt %q is invalid", record.ID)
		}
		bodies[prompt.ID] = prompt
	}
	ids := slices.Clone(queue)
	for _, ref := range started {
		if ref.TurnID == "" {
			return nil, fmt.Errorf("droids: persisted started prompt %q has no turn", ref.ID)
		}
		ids = append(ids, ref.ID)
	}
	queued := make(map[QueuedPromptID]durableQueuedPrompt, len(ids))
	for _, id := range ids {
		prompt, ok := bodies[id]
		if !validID(string(id), queuedPromptIDPrefix) || !ok {
			return nil, fmt.Errorf("droids: persisted queued prompt %q is missing", id)
		}
		if _, duplicate := queued[id]; duplicate {
			return nil, fmt.Errorf("droids: persisted queued prompt %q is duplicated", id)
		}
		if len(prompt.Metadata) > maxQueuedMetadataBytes || len(prompt.Metadata) > 0 && !json.Valid(prompt.Metadata) {
			return nil, fmt.Errorf("droids: persisted queued prompt %q metadata is invalid", id)
		}
		queued[id] = prompt
	}
	if _, err := queuedPromptsFromDurable(slices.Collect(maps.Values(queued))); err != nil {
		return nil, err
	}
	return queued, nil
}

// truncateUTF8 bounds value to limit bytes without splitting a character.
func truncateUTF8(value string, limit int) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8Start(value[end]) {
		end--
	}
	return value[:end]
}

// Execution returns the handle of turn id: the current turn, or a settled
// earlier one, whose handle is already complete. An application uses it to
// follow a turn the droid started from its queue. It returns ErrTurnNotFound
// for an unknown turn.
func (d *Droid) Execution(ctx context.Context, id TurnID) (ExecutionHandle, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Execution requires a droid opened with droids.Spawn")
	}
	if id == "" {
		return nil, fmt.Errorf("droids: turn id is required")
	}
	rt := d.sdk
	rt.mu.Lock()
	if rt.state.TurnID == id {
		handle := rt.ensureHandleLocked()
		if isTerminalStatus(rt.state.Status) || rt.state.Status == ExecutionPaused || rt.state.Status == ExecutionInterrupted {
			handle.complete(outcomeFromState(rt.conversation, rt.state))
		}
		rt.mu.Unlock()
		return handle, nil
	}
	rt.mu.Unlock()
	turn, err := d.Turn(ctx, id)
	if err != nil {
		return nil, err
	}
	handle := newSDKExecution(rt, id)
	handle.complete(Outcome{
		ConversationID: rt.conversation, TurnID: id, QueuedPromptID: turn.QueuedPromptID,
		Status: turn.Status, FinalMessage: turn.FinalMessage, Error: turn.Error, Usage: turn.Usage,
	})
	return handle, nil
}

// StartedPrompt is a queued prompt that started a turn.
type StartedPrompt struct {
	Prompt QueuedPrompt
	TurnID TurnID
}

// StartedPrompts returns, oldest first, the queued prompts that started turns
// and that the application has not acknowledged. With
// Config.TrackStartedPrompts they survive restarts until acknowledged, so an
// application can finish following turns its queue started, even turns that
// settled, before it stopped. Otherwise each is forgotten when its turn
// settles.
func (d *Droid) StartedPrompts(ctx context.Context) ([]StartedPrompt, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: StartedPrompts requires a droid opened with droids.Spawn")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	started := make([]StartedPrompt, 0, len(rt.state.Started))
	for _, ref := range rt.state.Started {
		prompts, err := queuedPromptsFromDurable([]durableQueuedPrompt{rt.queued[ref.ID]})
		if err != nil {
			return nil, err
		}
		started = append(started, StartedPrompt{Prompt: prompts[0], TurnID: ref.TurnID})
	}
	return started, nil
}

// AcknowledgeStarted forgets the identified started prompts once the
// application has followed their turns. Unknown identifiers are skipped.
func (d *Droid) AcknowledgeStarted(ctx context.Context, ids ...QueuedPromptID) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: AcknowledgeStarted requires a droid opened with droids.Spawn")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	var acknowledged []QueuedPromptID
	kept := make([]startedPromptRef, 0, len(rt.state.Started))
	for _, ref := range rt.state.Started {
		if slices.Contains(ids, ref.ID) {
			acknowledged = append(acknowledged, ref.ID)
		} else {
			kept = append(kept, ref)
		}
	}
	if len(acknowledged) == 0 {
		return nil
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	rt.state.Started = kept
	if err := rt.commitLocked(ctx, deleteQueuedPromptMutations(acknowledged), nil); err != nil {
		rt.state = before
		return err
	}
	rt.forgetQueuedLocked(acknowledged)
	// A queue that waited on unacknowledged prompts can start again.
	rt.advanceQueueLocked(ctx)
	return nil
}
