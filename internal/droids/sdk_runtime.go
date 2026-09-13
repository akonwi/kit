package droids

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultSubscriptionBuffer = 128
	maxSubscriptionBuffer     = 4096
	maxSubscriptionReplay     = 1000
	maxPendingSteering        = 64
	maxPendingBoundaries      = 64
	maxAutonomousReactions    = 8
)

type sdkRuntime struct {
	droid         *Droid
	config        Config
	store         Store
	provider      Provider
	conversation  ConversationID
	requestConfig atomic.Pointer[runtimeRequestConfiguration]

	mu                   sync.Mutex
	abortMu              sync.Mutex
	revision             uint64
	lastEvent            EventSequence
	state                durableRuntime
	forkedFrom           *ForkPoint
	boundaryReceipts     map[string]struct{}
	boundaryConsumptions map[string]TurnID
	changed              chan struct{}
	runCancel            context.CancelFunc
	runGeneration        uint64
	handle               *sdkExecution
	closed               bool
	shutdownStarted      bool
	shutdownDone         chan struct{}
	resumeCancel         context.CancelFunc
	resumeFlight         *resumeFlight
	contextFlight        *contextMaintenanceFlight
	abortPending         bool
	persistenceErr       error

	subsMu sync.Mutex
	subs   map[*sdkSubscription]struct{}
}

type resumeFlight struct {
	done chan struct{}
	err  error
}

type sdkExecution struct {
	runtime *sdkRuntime
	turnID  TurnID
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	outcome Outcome
	err     error
}

func (e *sdkExecution) TurnID() TurnID { return e.turnID }

func (e *sdkExecution) Wait(ctx context.Context) (Outcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-e.done:
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.outcome, e.err
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

func (e *sdkExecution) Snapshot(ctx context.Context) (ExecutionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return ExecutionSnapshot{}, err
	}
	e.runtime.mu.Lock()
	defer e.runtime.mu.Unlock()
	if e.runtime.state.TurnID != e.turnID {
		e.mu.Lock()
		defer e.mu.Unlock()
		return ExecutionSnapshot{
			TurnID: e.turnID, Status: e.outcome.Status,
			Reason: errorMessage(e.outcome.Error), Error: cloneDroidError(e.outcome.Error),
		}, nil
	}
	return executionSnapshot(e.runtime.state), nil
}

func (e *sdkExecution) complete(outcome Outcome) {
	e.completeWithError(outcome, nil)
}

func (e *sdkExecution) completeWithError(outcome Outcome, err error) {
	e.once.Do(func() {
		e.mu.Lock()
		e.outcome = outcome
		e.err = err
		e.mu.Unlock()
		close(e.done)
	})
}

type sdkSubscription struct {
	runtime   *sdkRuntime
	events    chan EventEnvelope
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	err       error
	transient bool
}

func (s *sdkSubscription) Events() <-chan EventEnvelope { return s.events }
func (s *sdkSubscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
func (s *sdkSubscription) Close() { s.close(nil) }
func (s *sdkSubscription) close(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
		s.runtime.removeSubscription(s)
		close(s.events)
	})
}

// Open finds or creates one autonomous droid conversation.
func Open(ctx context.Context, id ConversationID, config Config) (*Droid, error) {
	if id == "" {
		return nil, fmt.Errorf("droids: conversation id is required")
	}
	if config.Providers == nil {
		return nil, fmt.Errorf("droids: Config.Providers is required")
	}
	if config.Model == "" {
		return nil, fmt.Errorf("droids: Config.Model is required")
	}
	provider, model, err := config.Providers.Resolve(config.Model)
	if err != nil {
		return nil, err
	}
	if config.Store == nil {
		config.Store = NewMemoryStore()
	}
	execution := ExecutionPolicy{ToolExecution: ModeParallel, MaxParallelTools: 4}
	if config.Execution != nil {
		execution = *config.Execution
		if execution.ToolExecution == ModeDefault {
			execution.ToolExecution = ModeParallel
		}
		if execution.MaxParallelTools == 0 {
			execution.MaxParallelTools = 4
		}
	}
	if execution.MaxParallelTools < 1 {
		return nil, fmt.Errorf("droids: MaxParallelTools must be positive")
	}
	retry := DefaultRetryPolicy()
	if config.Retry != nil {
		retry = *config.Retry
	}
	if retry.MaxRetries < 0 || retry.BaseDelay < 0 || retry.MaxDelay < 0 {
		return nil, fmt.Errorf("droids: retry values must not be negative")
	}
	config.Execution = &execution
	config.Retry = &retry

	requestConfig, err := buildRuntimeRequestConfiguration(model, RequestConfiguration{
		SystemPrompt: config.SystemPrompt, Reasoning: config.Reasoning, Tools: config.Tools,
	})
	if err != nil {
		return nil, err
	}
	d := &Droid{providers: config.Providers, model: model}
	initial := newDurableRuntime()
	record, err := runtimeEncodedRecord(initial)
	if err != nil {
		return nil, err
	}
	createdEvent, err := lifecycleEvent("conversation.created", "", "", nil)
	if err != nil {
		return nil, err
	}
	opened, err := config.Store.Open(ctx, OpenConversation{
		ID: id, InitialRecords: []EncodedRecord{record},
		InitialEvents: []EncodedDurableEvent{createdEvent},
	})
	if err != nil {
		return nil, err
	}
	if opened.Conversation.ID != id {
		return nil, fmt.Errorf("droids: Store returned conversation %q for %q", opened.Conversation.ID, id)
	}
	state, err := decodeRuntime(opened.Conversation.RuntimeState)
	if err != nil {
		return nil, err
	}
	usageRebuilt := false
	if !state.SessionUsageInitialized {
		state.SessionUsage, err = rebuildSessionUsage(ctx, config.Store, id)
		if err != nil {
			return nil, err
		}
		state.SessionUsageInitialized = true
		usageRebuilt = true
	}
	lineage, err := decodeLineage(opened.Conversation.RuntimeState)
	if err != nil {
		return nil, err
	}
	var forkedFrom *ForkPoint
	if lineage != nil {
		point := lineage.Point
		if point.ConversationID == id {
			return nil, fmt.Errorf("droids: fork lineage refers to its own conversation")
		}
		forkedFrom = &point
	}
	if err := validateOpenedRuntime(state); err != nil {
		return nil, err
	}
	boundaryReceipts, err := loadBoundaryReceipts(ctx, config.Store)
	if err != nil {
		return nil, err
	}
	boundaryConsumptions, err := loadBoundaryConsumptions(ctx, config.Store)
	if err != nil {
		return nil, err
	}
	rt := &sdkRuntime{
		droid: d, config: config, store: config.Store, provider: provider, conversation: id,
		revision: opened.Conversation.Revision, lastEvent: opened.Conversation.LastEvent,
		state: state, forkedFrom: forkedFrom, boundaryReceipts: boundaryReceipts, boundaryConsumptions: boundaryConsumptions,
		changed: make(chan struct{}), shutdownDone: make(chan struct{}),
		subs: make(map[*sdkSubscription]struct{}),
	}
	d.sdk = rt
	rt.requestConfig.Store(requestConfig)

	rt.mu.Lock()
	if usageRebuilt {
		event, _ := lifecycleEvent("usage.rebuilt", "", "", map[string]any{"session_usage": rt.state.SessionUsage})
		if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
			rt.mu.Unlock()
			return nil, err
		}
	}
	if statusNeedsInterruption(rt.state.Status) {
		previous := rt.state.Status
		rt.state.AbortRequested = previous == ExecutionAborting || rt.state.AbortRequested
		failure := &DroidError{
			Kind: DroidErrorInternal, Message: "process interrupted while " + string(previous),
		}
		if err := rt.interruptLocked(ctx, failure); err != nil {
			rt.mu.Unlock()
			return nil, err
		}
	}
	if rt.state.Status == ExecutionPaused || rt.state.Status == ExecutionInterrupted {
		rt.handle = newSDKExecution(rt, rt.state.TurnID)
	}
	rt.mu.Unlock()
	return d, nil
}

func loadBoundaryReceipts(ctx context.Context, store Store) (map[string]struct{}, error) {
	receipts := make(map[string]struct{})
	var after uint64
	for {
		page, err := store.Records(ctx, RecordQuery{After: after, Limit: 1000, Kind: boundaryReceiptKind})
		if err != nil {
			return nil, fmt.Errorf("droids: load boundary receipts: %w", err)
		}
		for _, record := range page.Records {
			if record.ID == "" {
				return nil, fmt.Errorf("droids: boundary receipt has an empty id")
			}
			receipts[record.ID] = struct{}{}
		}
		after = page.Next
		if !page.HasMore {
			return receipts, nil
		}
	}
}

func loadBoundaryConsumptions(ctx context.Context, store Store) (map[string]TurnID, error) {
	consumptions := make(map[string]TurnID)
	var after uint64
	for {
		page, err := store.Records(ctx, RecordQuery{After: after, Limit: 1000, Kind: boundaryConsumptionKind})
		if err != nil {
			return nil, fmt.Errorf("droids: load boundary consumptions: %w", err)
		}
		for _, record := range page.Records {
			var value struct {
				ID     string `json:"id"`
				TurnID TurnID `json:"turn_id"`
			}
			if json.Unmarshal(record.Payload, &value) != nil || value.ID == "" || value.TurnID == "" || value.ID != record.ID {
				return nil, fmt.Errorf("droids: boundary consumption %q is invalid", record.ID)
			}
			consumptions[value.ID] = value.TurnID
		}
		after = page.Next
		if !page.HasMore {
			return consumptions, nil
		}
	}
}

func statusNeedsInterruption(status ExecutionStatus) bool {
	switch status {
	case ExecutionRunning, ExecutionRetrying, ExecutionPausing, ExecutionAborting:
		return true
	default:
		return false
	}
}

func newSDKExecution(runtime *sdkRuntime, turnID TurnID) *sdkExecution {
	return &sdkExecution{runtime: runtime, turnID: turnID, done: make(chan struct{})}
}

// Prompt durably starts a turn or steers the current turn.
func (d *Droid) Prompt(ctx context.Context, input Input, options PromptOptions) (ExecutionHandle, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Prompt requires a droid opened with droids.Open")
	}
	message, err := inputToMessage(input)
	if err != nil {
		return nil, err
	}
	if options.Steer && options.AdmissionKey != "" {
		return nil, fmt.Errorf("droids: steering does not accept an admission key")
	}
	admissionHash := ""
	if options.AdmissionKey != "" {
		if !validBoundedContextValue(options.AdmissionKey, 256) {
			return nil, fmt.Errorf("droids: prompt admission key is invalid")
		}
		admissionHash, err = promptAdmissionHash(message)
		if err != nil {
			return nil, err
		}
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
	if options.AdmissionKey != "" && rt.state.AdmissionKey == options.AdmissionKey {
		if rt.state.AdmissionHash != admissionHash {
			return nil, ErrConflict
		}
		handle := rt.ensureHandleLocked()
		if isTerminalStatus(rt.state.Status) {
			handle.complete(outcomeFromState(rt.conversation, rt.state))
		}
		return handle, nil
	}
	if isOccupied(rt.state.Status) {
		if !options.Steer || rt.state.Status == ExecutionInterrupted || rt.state.Status == ExecutionAborting {
			return nil, ErrBusy
		}
		if len(rt.state.PendingSteering) >= maxPendingSteering {
			return nil, fmt.Errorf("%w: pending steering capacity reached", ErrBusy)
		}
		messageID, err := newMessageID()
		if err != nil {
			return nil, err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: rt.state.TurnID,
			CreatedAt: time.Now().UTC(), Message: message,
		}
		wire, err := messageEnvelopeToWire(envelope)
		if err != nil {
			return nil, err
		}
		before, err := cloneDurableRuntime(rt.state)
		if err != nil {
			return nil, err
		}
		rt.state.PendingSteering = append(rt.state.PendingSteering, wire)
		event, err := lifecycleEvent("steering.accepted", rt.state.TurnID, rt.state.AttemptID, map[string]any{"message_id": messageID})
		if err != nil {
			rt.state = before
			return nil, err
		}
		if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
			rt.state = before
			return nil, err
		}
		return rt.ensureHandleLocked(), nil
	}
	if options.Steer {
		return nil, ErrConflict
	}

	return rt.startTurnLocked(ctx, &message, options.AdmissionKey, admissionHash)
}

// React durably starts a context-only turn from pending external boundaries.
// admissionKey makes retries return the originally admitted turn. The replayed
// result reports whether the handle belongs to an earlier admission.
func (d *Droid) React(ctx context.Context, admissionKey string) (handle ExecutionHandle, replayed bool, err error) {
	if d == nil || d.sdk == nil {
		return nil, false, fmt.Errorf("droids: React requires a droid opened with droids.Open")
	}
	if !validBoundedContextValue(admissionKey, 256) {
		return nil, false, fmt.Errorf("droids: reaction admission key is invalid")
	}
	digest := sha256.Sum256([]byte("boundary-reaction\x00" + admissionKey))
	admissionHash := hex.EncodeToString(digest[:])
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, false, ErrClosed
	}
	if rt.contextFlight != nil {
		return nil, false, ErrBusy
	}
	if rt.state.AdmissionKey == admissionKey {
		if rt.state.AdmissionHash != admissionHash {
			return nil, false, ErrConflict
		}
		handle := rt.ensureHandleLocked()
		if isTerminalStatus(rt.state.Status) {
			handle.complete(outcomeFromState(rt.conversation, rt.state))
		}
		return handle, true, nil
	}
	if isOccupied(rt.state.Status) {
		return nil, false, ErrBusy
	}
	if len(rt.state.PendingBoundaries) == 0 {
		return nil, false, ErrUnsafeContinuation
	}
	if rt.state.AutonomousReactions >= maxAutonomousReactions {
		return nil, false, ErrReactionLimit
	}
	handle, err = rt.startTurnLocked(ctx, nil, admissionKey, admissionHash)
	return handle, false, err
}

func (rt *sdkRuntime) startTurnLocked(ctx context.Context, message *UserMessage, admissionKey, admissionHash string) (ExecutionHandle, error) {
	turnID, err := newTurnID()
	if err != nil {
		return nil, err
	}
	attemptID, err := newAttemptID()
	if err != nil {
		return nil, err
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return nil, err
	}
	rt.state = newDurableRuntime()
	rt.state.Context = before.Context
	rt.state.PendingBoundaries = before.PendingBoundaries
	rt.state.CheckpointID = before.CheckpointID
	rt.state.SessionUsage = before.SessionUsage
	rt.state.SessionUsageInitialized = before.SessionUsageInitialized
	rt.state.AdmissionKey = admissionKey
	rt.state.AdmissionHash = admissionHash
	if message == nil {
		rt.state.AutonomousReactions = before.AutonomousReactions + 1
		rt.state.BoundaryReaction = true
	}
	rt.state.Status = ExecutionRunning
	rt.state.TurnID = turnID
	rt.state.AttemptID = attemptID
	rt.state.AttemptOpen = true
	var mutations []EncodedMutation
	var boundaryEvents []EncodedDurableEvent
	var consumedBoundaryIDs []string
	for _, pending := range rt.state.PendingBoundaries {
		content, err := inputFromWire(pending.Message.Content)
		if err != nil {
			rt.state = before
			return nil, err
		}
		prefix := fmt.Sprintf("[%s", pending.Message.Kind)
		if pending.Message.Source != "" {
			prefix += " from " + pending.Message.Source
		}
		prefix += "]"
		content = append([]InputContent{TextInput{Text: prefix}}, content...)
		boundaryID, err := newMessageID()
		if err != nil {
			rt.state = before
			return nil, err
		}
		boundary := MessageEnvelope{
			ID: boundaryID, ConversationID: rt.conversation, TurnID: turnID,
			CreatedAt: time.Now().UTC(),
			Message: ContextMessage{
				BoundaryID: pending.Message.ID, Kind: pending.Message.Kind,
				Source: pending.Message.Source, Content: content,
				Details: append(json.RawMessage(nil), pending.Message.Details...),
			},
		}
		if err := appendRuntimeEnvelope(&rt.state, boundary); err != nil {
			rt.state = before
			return nil, err
		}
		mutation, err := messageHistoryMutation(boundary)
		if err != nil {
			rt.state = before
			return nil, err
		}
		mutations = append(mutations, mutation)
		for _, receiptID := range boundaryReceiptIDs(pending.Message) {
			consumption, err := boundaryConsumptionMutation(receiptID, turnID)
			if err != nil {
				rt.state = before
				return nil, err
			}
			mutations = append(mutations, consumption)
			consumedBoundaryIDs = append(consumedBoundaryIDs, receiptID)
		}
		event, _ := lifecycleEvent("boundary.consumed", turnID, attemptID, map[string]any{"message_id": boundaryID, "kind": pending.Message.Kind})
		boundaryEvents = append(boundaryEvents, event)
	}
	rt.state.PendingBoundaries = nil
	admittedData := map[string]any{}
	if message != nil {
		messageID, err := newMessageID()
		if err != nil {
			rt.state = before
			return nil, err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
			CreatedAt: time.Now().UTC(), Message: *message,
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			rt.state = before
			return nil, err
		}
		messageMutation, err := messageHistoryMutation(envelope)
		if err != nil {
			rt.state = before
			return nil, err
		}
		mutations = append(mutations, messageMutation)
		admittedData["message_id"] = messageID
	}
	admitted, _ := lifecycleEvent("turn.admitted", turnID, attemptID, admittedData)
	started, _ := lifecycleEvent("execution.started", turnID, attemptID, nil)
	attemptStarted, _ := lifecycleEvent("attempt.started", turnID, attemptID, nil)
	events := append([]EncodedDurableEvent{admitted}, boundaryEvents...)
	events = append(events, started, attemptStarted)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return nil, err
	}
	for _, id := range consumedBoundaryIDs {
		rt.boundaryConsumptions[id] = turnID
	}
	handle := newSDKExecution(rt, turnID)
	rt.handle = handle
	rt.startRunLocked()
	return handle, nil
}

func boundaryReceiptIDs(message BoundaryMessageWire) []string {
	if len(message.ReceiptIDs) > 0 {
		return message.ReceiptIDs
	}
	if message.ID != "" {
		return []string{message.ID}
	}
	return nil
}

func boundaryConsumptionMutation(id string, turnID TurnID) (EncodedMutation, error) {
	payload, err := json.Marshal(map[string]any{"id": id, "turn_id": turnID, "consumed_at": time.Now().UTC()})
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationPut, RecordKind: boundaryConsumptionKind, RecordID: id,
		Scope: RecordHistory, Version: recordVersion, Payload: payload,
	}, nil
}

func promptAdmissionHash(message Message) (string, error) {
	wire, err := messageToWire(message)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func isOccupied(status ExecutionStatus) bool {
	switch status {
	case ExecutionRunning, ExecutionRetrying, ExecutionPausing, ExecutionPaused, ExecutionAborting, ExecutionInterrupted:
		return true
	default:
		return false
	}
}

func (rt *sdkRuntime) ensureHandleLocked() *sdkExecution {
	if rt.handle == nil || rt.handle.turnID != rt.state.TurnID || rt.handle.isDone() {
		rt.handle = newSDKExecution(rt, rt.state.TurnID)
	}
	return rt.handle
}

func (e *sdkExecution) isDone() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

func (rt *sdkRuntime) startRunLocked() {
	if rt.runCancel != nil || rt.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.runGeneration++
	generation := rt.runGeneration
	rt.runCancel = cancel
	turnID := rt.state.TurnID
	go rt.run(ctx, turnID, generation)
}

func (rt *sdkRuntime) releaseRunLocked() {
	rt.runGeneration++
	rt.runCancel = nil
	rt.signalChangedLocked()
}

// Inform durably records an external boundary message.
func (d *Droid) Inform(ctx context.Context, message BoundaryMessage) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: Inform requires a droid opened with droids.Open")
	}
	wire, err := boundaryToWire(message)
	if err != nil {
		return err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	receiptIDs := append([]string(nil), message.ReceiptIDs...)
	if len(receiptIDs) == 0 && message.ID != "" {
		receiptIDs = []string{message.ID}
	}
	wire.ReceiptIDs = append([]string(nil), receiptIDs...)
	seenReceipts := 0
	seen := make(map[string]struct{}, len(receiptIDs))
	for _, receiptID := range receiptIDs {
		if _, duplicate := seen[receiptID]; duplicate {
			return fmt.Errorf("droids: duplicate boundary receipt id %q", receiptID)
		}
		seen[receiptID] = struct{}{}
		if _, received := rt.boundaryReceipts[receiptID]; received {
			seenReceipts++
		}
	}
	if len(receiptIDs) > 0 && seenReceipts == len(receiptIDs) {
		return nil
	}
	if seenReceipts > 0 {
		return fmt.Errorf("droids: boundary receipt set was partially delivered")
	}

	alreadyPendingOrMaterialized := false
	if message.ID != "" {
		for _, pending := range rt.state.PendingBoundaries {
			if pending.Message.ID == message.ID {
				alreadyPendingOrMaterialized = true
				break
			}
		}
		if !alreadyPendingOrMaterialized {
			for _, raw := range rt.state.Context {
				envelope, err := messageEnvelopeFromWire(raw)
				if err != nil {
					return err
				}
				if boundary, ok := envelope.Message.(ContextMessage); ok && boundary.BoundaryID == message.ID {
					alreadyPendingOrMaterialized = true
					break
				}
			}
		}
	}
	if !alreadyPendingOrMaterialized && len(rt.state.PendingBoundaries) >= maxPendingBoundaries {
		return fmt.Errorf("%w: pending boundary capacity reached", ErrBusy)
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	if !alreadyPendingOrMaterialized {
		rt.state.PendingBoundaries = append(rt.state.PendingBoundaries, durableBoundary{Message: wire, Accepted: time.Now().UTC()})
	}
	var mutations []EncodedMutation
	for _, receiptID := range receiptIDs {
		payload, err := json.Marshal(map[string]any{"id": receiptID, "accepted_at": time.Now().UTC()})
		if err != nil {
			rt.state = before
			return err
		}
		mutations = append(mutations, EncodedMutation{
			Operation: MutationPut, RecordKind: boundaryReceiptKind, RecordID: receiptID,
			Scope: RecordHistory, Version: recordVersion, Payload: payload,
		})
	}
	event, _ := lifecycleEvent("boundary.accepted", rt.state.TurnID, rt.state.AttemptID, map[string]any{"id": message.ID, "kind": message.Kind, "source": message.Source})
	if err := rt.commitLocked(ctx, mutations, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	for _, receiptID := range receiptIDs {
		rt.boundaryReceipts[receiptID] = struct{}{}
	}
	return nil
}

// BoundaryReceived reports whether an idempotency receipt is durable.
func (d *Droid) BoundaryReceived(ctx context.Context, id string) (bool, error) {
	if d == nil || d.sdk == nil {
		return false, fmt.Errorf("droids: BoundaryReceived requires a droid opened with droids.Open")
	}
	if err := contextError(ctx); err != nil {
		return false, err
	}
	d.sdk.mu.Lock()
	defer d.sdk.mu.Unlock()
	if d.sdk.closed {
		return false, ErrClosed
	}
	_, received := d.sdk.boundaryReceipts[id]
	return received, nil
}

// BoundaryStatus reports whether a boundary receipt is accepted, still
// pending, or durably associated with a turn.
func (d *Droid) BoundaryStatus(ctx context.Context, id string) (BoundaryStatus, error) {
	if d == nil || d.sdk == nil {
		return BoundaryStatus{}, fmt.Errorf("droids: BoundaryStatus requires a droid opened with droids.Open")
	}
	if err := contextError(ctx); err != nil {
		return BoundaryStatus{}, err
	}
	d.sdk.mu.Lock()
	defer d.sdk.mu.Unlock()
	if d.sdk.closed {
		return BoundaryStatus{}, ErrClosed
	}
	status := BoundaryStatus{}
	_, status.Received = d.sdk.boundaryReceipts[id]
	status.TurnID = d.sdk.boundaryConsumptions[id]
	for _, pending := range d.sdk.state.PendingBoundaries {
		for _, receiptID := range boundaryReceiptIDs(pending.Message) {
			if receiptID == id {
				status.Pending = true
				break
			}
		}
		if status.Pending {
			break
		}
	}
	return status, nil
}

// Pause requests a durable pause at the next safe model boundary.
func (d *Droid) Pause(ctx context.Context, reason string) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: Pause requires a droid opened with droids.Open")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	if rt.state.Status == ExecutionPaused || rt.state.Status == ExecutionPausing {
		return nil
	}
	if rt.state.Status != ExecutionRunning && rt.state.Status != ExecutionRetrying {
		return ErrNoActiveExecution
	}
	before := rt.state
	rt.state.Status = ExecutionPausing
	rt.state.Reason = reason
	event, _ := lifecycleEvent("execution.pause_requested", rt.state.TurnID, rt.state.AttemptID, map[string]any{"reason": reason})
	if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	return nil
}

// Resume continues paused or safely recoverable work and otherwise does nothing.
func (d *Droid) Resume(ctx context.Context) (returnErr error) {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: Resume requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt := d.sdk
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return ErrClosed
	}
	switch rt.state.Status {
	case ExecutionReady, ExecutionCompleted, ExecutionFailed, ExecutionAborted,
		ExecutionRunning, ExecutionRetrying, ExecutionPausing, ExecutionAborting:
		rt.mu.Unlock()
		return nil
	case ExecutionPaused, ExecutionInterrupted:
	default:
		rt.mu.Unlock()
		return nil
	}
	if rt.abortPending {
		rt.mu.Unlock()
		return ErrBusy
	}
	if rt.resumeFlight != nil {
		flight := rt.resumeFlight
		rt.mu.Unlock()
		select {
		case <-flight.done:
			return flight.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if rt.state.AbortRequested {
		err := rt.settleLocked(ctx, ExecutionAborted, nil)
		rt.mu.Unlock()
		return err
	}
	if err := unsafeToolContinuation(rt.state, rt.config); err != nil {
		rt.mu.Unlock()
		return errors.Join(ErrUnsafeContinuation, err)
	}
	if err := validateRuntimeContinuation(rt.state); err != nil {
		rt.mu.Unlock()
		return errors.Join(ErrUnsafeContinuation, err)
	}
	messages, err := runtimeMessageEnvelopes(rt.state)
	if err != nil {
		rt.mu.Unlock()
		return errors.Join(ErrUnsafeContinuation, err)
	}
	plain := make([]Message, 0, len(messages))
	for _, message := range messages {
		plain = append(plain, message.Message)
	}
	revision := rt.revision
	status := rt.state.Status
	provider := rt.provider
	model := rt.droid.model
	validationCtx, cancelValidation := context.WithCancel(ctx)
	flight := &resumeFlight{done: make(chan struct{})}
	rt.resumeCancel = cancelValidation
	rt.resumeFlight = flight
	rt.mu.Unlock()

	validationErr := provider.ValidateReplay(validationCtx, model, plain)

	rt.mu.Lock()
	cancelValidation()
	rt.resumeCancel = nil
	defer func() {
		flight.err = returnErr
		if rt.resumeFlight == flight {
			rt.resumeFlight = nil
		}
		close(flight.done)
		rt.signalChangedLocked()
		rt.mu.Unlock()
	}()
	if validationErr != nil {
		if rt.closed {
			return ErrClosed
		}
		return errors.Join(ErrUnsafeContinuation, validationErr)
	}
	if rt.closed {
		return ErrClosed
	}
	if rt.abortPending {
		return ErrBusy
	}
	if rt.revision != revision || rt.state.Status != status {
		return ErrConflict
	}
	if !rt.state.RetryAt.IsZero() && !rt.state.AttemptOpen {
		before := rt.state
		rt.state.Status = ExecutionRetrying
		rt.state.Reason = ""
		rt.state.Error = nil
		event, _ := lifecycleEvent("execution.resumed", rt.state.TurnID, rt.state.AttemptID, map[string]any{"retry_at": rt.state.RetryAt})
		if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
			rt.state = before
			return err
		}
		rt.persistenceErr = nil
		rt.ensureHandleLocked()
		rt.startRunLocked()
		return nil
	}
	var mutations []EncodedMutation
	attemptWasOpen := rt.state.AttemptOpen
	if attemptWasOpen {
		attempt, err := attemptHistoryMutation(rt.state, ExecutionInterrupted)
		if err != nil {
			return err
		}
		mutations = append(mutations, attempt)
	}
	attemptID, err := newAttemptID()
	if err != nil {
		return err
	}
	before := rt.state
	var events []EncodedDurableEvent
	if rt.state.CyclePhase == cycleModelStarted {
		interrupted, _ := lifecycleEvent("model_cycle.interrupted", rt.state.TurnID, rt.state.AttemptID, nil)
		events = append(events, interrupted)
		rt.state.CyclePhase = cycleReady
	}
	if attemptWasOpen {
		settled, _ := lifecycleEvent("attempt.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"status": ExecutionInterrupted})
		events = append(events, settled)
	}
	rt.state.Status = ExecutionRunning
	rt.state.AttemptID = attemptID
	rt.state.AttemptOpen = true
	rt.state.ModelCycles = 0
	rt.state.Reason = ""
	rt.state.Error = nil
	attemptStarted, _ := lifecycleEvent("attempt.started", rt.state.TurnID, attemptID, map[string]any{"reason": "resume"})
	event, _ := lifecycleEvent("execution.resumed", rt.state.TurnID, attemptID, nil)
	events = append(events, attemptStarted, event)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	rt.persistenceErr = nil
	rt.ensureHandleLocked()
	rt.startRunLocked()
	return nil
}

func unsafeToolContinuation(state durableRuntime, config Config) error {
	for _, tool := range state.Tools {
		if tool.Phase == toolPhaseExecuting && tool.RawResult == nil {
			return fmt.Errorf("droids: tool execution may have started without a durable result")
		}
		if tool.Phase == toolPhaseBeforeHook && tool.RequiresBeforeHook && config.BeforeToolCall == nil {
			return fmt.Errorf("droids: required before-tool hook is unavailable")
		}
		if tool.Phase == toolPhaseAfterHook && tool.RequiresAfterHook && config.AfterToolCall == nil {
			return fmt.Errorf("droids: required after-tool hook is unavailable")
		}
	}
	return nil
}

func validateRuntimeContinuation(state durableRuntime) error {
	messages, err := runtimeMessageEnvelopes(state)
	if err != nil {
		return err
	}
	plain := make([]Message, 0, len(messages))
	for _, message := range messages {
		plain = append(plain, message.Message)
	}
	if len(state.Tools) == 0 {
		if (state.CyclePhase == cycleAssistantPersisted || state.CyclePhase == cycleSyntheticPending) && len(plain) > 0 {
			if assistant, ok := plain[len(plain)-1].(AssistantMessage); ok && len(assistant.ToolCalls()) > 0 {
				return validateMessageSequence(plain[:len(plain)-1])
			}
		}
		return validateMessageSequence(plain)
	}
	if len(plain) == 0 {
		return fmt.Errorf("droids: unresolved tools have no assistant message")
	}
	assistant, ok := plain[len(plain)-1].(AssistantMessage)
	if !ok || assistant.StopReason != StopReasonToolUse {
		return fmt.Errorf("droids: unresolved tools do not follow a tool-use assistant message")
	}
	if err := validateMessageSequence(plain[:len(plain)-1]); err != nil {
		return err
	}
	for _, call := range assistant.ToolCalls() {
		if _, ok := state.Tools[call.ID]; !ok {
			return fmt.Errorf("droids: tool call %q has no durable state", call.ID)
		}
	}
	return nil
}

// Abort cancels active work or settles paused/interrupted work as aborted.
func (d *Droid) Abort(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if d.sdk == nil {
		return ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt := d.sdk
	rt.abortMu.Lock()
	defer rt.abortMu.Unlock()
	rt.mu.Lock()
	rt.abortPending = true
	if rt.resumeFlight != nil {
		flight := rt.resumeFlight
		if rt.resumeCancel != nil {
			rt.resumeCancel()
		}
		rt.mu.Unlock()
		select {
		case <-flight.done:
		case <-ctx.Done():
			rt.mu.Lock()
			rt.abortPending = false
			rt.signalChangedLocked()
			rt.mu.Unlock()
			return ctx.Err()
		}
		rt.mu.Lock()
	}
	defer func() {
		rt.abortPending = false
		rt.signalChangedLocked()
		rt.mu.Unlock()
	}()
	if rt.closed {
		return ErrClosed
	}
	switch rt.state.Status {
	case ExecutionReady, ExecutionCompleted, ExecutionFailed, ExecutionAborted:
		return nil
	case ExecutionPaused, ExecutionInterrupted:
		return rt.settleLocked(ctx, ExecutionAborted, nil)
	case ExecutionAborting:
		return nil
	}
	before := rt.state
	rt.state.Status = ExecutionAborting
	rt.state.AbortRequested = true
	event, _ := lifecycleEvent("execution.abort_requested", rt.state.TurnID, rt.state.AttemptID, nil)
	if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	if rt.runCancel != nil {
		rt.runCancel()
	}
	return nil
}

// Shutdown stops admitted work safely and waits for runtime workers to quiesce.
func (d *Droid) Shutdown(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if d.sdk == nil {
		return d.Close()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt := d.sdk
	rt.mu.Lock()
	if !rt.shutdownStarted {
		rt.shutdownStarted = true
		rt.closed = true
		cancel := rt.runCancel
		resumeCancel := rt.resumeCancel
		var contextCancel context.CancelFunc
		if rt.contextFlight != nil {
			contextCancel = rt.contextFlight.cancel
		}
		rt.signalChangedLocked()
		go rt.finishShutdown(d, cancel, resumeCancel, contextCancel)
	}
	done := rt.shutdownDone
	rt.mu.Unlock()
	select {
	case <-done:
		rt.mu.Lock()
		err := rt.persistenceErr
		rt.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rt *sdkRuntime) finishShutdown(d *Droid, cancel, resumeCancel, contextCancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	if resumeCancel != nil {
		resumeCancel()
	}
	if contextCancel != nil {
		contextCancel()
	}
	for {
		rt.mu.Lock()
		running := rt.runCancel != nil || rt.resumeFlight != nil || rt.contextFlight != nil
		changed := rt.changed
		rt.mu.Unlock()
		if !running {
			break
		}
		<-changed
	}
	rt.subsMu.Lock()
	subs := make([]*sdkSubscription, 0, len(rt.subs))
	for sub := range rt.subs {
		subs = append(subs, sub)
	}
	rt.subsMu.Unlock()
	for _, sub := range subs {
		sub.close(nil)
	}
	close(rt.shutdownDone)
}

// WaitQuiescent waits until no model, retry, or tool work is runnable.
func (d *Droid) WaitQuiescent(ctx context.Context) (QuiescentState, error) {
	if d == nil || d.sdk == nil {
		return QuiescentState{}, fmt.Errorf("droids: WaitQuiescent requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt := d.sdk
	for {
		rt.mu.Lock()
		if rt.closed {
			rt.mu.Unlock()
			return QuiescentState{}, ErrClosed
		}
		if !statusRunsWork(rt.state.Status) && rt.contextFlight == nil {
			state := quiescentState(rt)
			rt.mu.Unlock()
			return state, nil
		}
		changed := rt.changed
		rt.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return QuiescentState{}, ctx.Err()
		}
	}
}

func statusRunsWork(status ExecutionStatus) bool {
	switch status {
	case ExecutionRunning, ExecutionRetrying, ExecutionPausing, ExecutionAborting:
		return true
	default:
		return false
	}
}

func quiescentState(rt *sdkRuntime) QuiescentState {
	result := QuiescentState{TurnID: rt.state.TurnID, LastEvent: rt.lastEvent}
	switch rt.state.Status {
	case ExecutionPaused:
		result.Kind = QuiescentPaused
		value := executionSnapshot(rt.state)
		result.Execution = &value
	case ExecutionInterrupted:
		result.Kind = QuiescentRecoverable
		value := executionSnapshot(rt.state)
		result.Execution = &value
	default:
		result.Kind = QuiescentSettled
		if rt.state.TurnID != "" {
			value := executionSnapshot(rt.state)
			result.Execution = &value
		}
	}
	return result
}

// Snapshot returns a bounded current droid projection.
func (d *Droid) Snapshot(ctx context.Context, options SnapshotOptions) (Snapshot, error) {
	if d == nil || d.sdk == nil {
		return Snapshot{}, fmt.Errorf("droids: Snapshot requires a droid opened with droids.Open")
	}
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	conversation := ConversationSnapshot{ID: rt.conversation, Revision: rt.revision}
	if rt.forkedFrom != nil {
		point := *rt.forkedFrom
		conversation.ForkedFrom = &point
	}
	pending := PendingInputSnapshot{
		Steering: len(rt.state.PendingSteering), Boundary: len(rt.state.PendingBoundaries),
		Boundaries: make([]PendingBoundarySnapshot, 0, len(rt.state.PendingBoundaries)),
	}
	for _, boundary := range rt.state.PendingBoundaries {
		content, err := inputFromWire(boundary.Message.Content)
		if err != nil {
			return Snapshot{}, fmt.Errorf("droids: decode pending boundary %q: %w", boundary.Message.ID, err)
		}
		pending.Boundaries = append(pending.Boundaries, PendingBoundarySnapshot{
			Message: BoundaryMessage{
				ID: boundary.Message.ID, ReceiptIDs: append([]string(nil), boundary.Message.ReceiptIDs...),
				Kind: boundary.Message.Kind, Source: boundary.Message.Source,
				Content: content, Details: append(json.RawMessage(nil), boundary.Message.Details...),
			},
			AcceptedAt: boundary.Accepted,
		})
	}
	result := Snapshot{
		Conversation: conversation,
		Pending:      pending,
		Usage:        rt.state.SessionUsage,
		Context: ContextSnapshot{
			CheckpointID: rt.state.CheckpointID, Messages: len(rt.state.Context),
		},
		LastEvent: rt.lastEvent,
	}
	if isOccupied(rt.state.Status) {
		value := executionSnapshot(rt.state)
		result.Active = &value
	}
	contextMessages, decodeErr := runtimeMessageEnvelopes(rt.state)
	if decodeErr != nil {
		return Snapshot{}, decodeErr
	}
	plainContext := make([]Message, 0, len(contextMessages))
	for _, message := range contextMessages {
		plainContext = append(plainContext, message.Message)
	}
	result.Context.Usage = d.contextUsage(plainContext)

	limit := options.RecentMessageLimit
	if limit == 0 {
		limit = 50
	}
	page, err := rt.store.Records(ctx, RecordQuery{Limit: limit, Kind: messageRecordKind, Descending: true})
	if err != nil {
		return Snapshot{}, err
	}
	result.Recent, err = messagePageFromRecords(page, true, rt.conversation)
	if err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// Turn returns the canonical terminal state of one settled turn.
func (d *Droid) Turn(ctx context.Context, id TurnID) (TurnSnapshot, error) {
	if d == nil || d.sdk == nil {
		return TurnSnapshot{}, fmt.Errorf("droids: Turn requires a droid opened with droids.Open")
	}
	if id == "" {
		return TurnSnapshot{}, fmt.Errorf("droids: turn id is required")
	}
	var after uint64
	for {
		page, err := d.sdk.store.Records(ctx, RecordQuery{After: after, Limit: 1000, Kind: turnRecordKind})
		if err != nil {
			return TurnSnapshot{}, err
		}
		for _, record := range page.Records {
			if record.ID != string(id) {
				continue
			}
			if record.Version != recordVersion {
				return TurnSnapshot{}, fmt.Errorf("droids: unsupported turn record version %d", record.Version)
			}
			var payload struct {
				TurnID TurnID             `json:"turn_id"`
				Status ExecutionStatus    `json:"status"`
				Error  *durableDroidError `json:"error"`
				Usage  Usage              `json:"usage"`
			}
			if err := json.Unmarshal(record.Payload, &payload); err != nil {
				return TurnSnapshot{}, fmt.Errorf("droids: decode turn %q: %w", id, err)
			}
			if payload.TurnID != id || !isTerminalStatus(payload.Status) {
				return TurnSnapshot{}, fmt.Errorf("droids: persisted turn %q is invalid", id)
			}
			if err := validateUsage(payload.Usage); err != nil {
				return TurnSnapshot{}, fmt.Errorf("droids: persisted turn %q usage is invalid: %w", id, err)
			}
			return TurnSnapshot{ID: id, Status: payload.Status, Error: expandDurableError(payload.Error), Usage: payload.Usage}, nil
		}
		after = page.Next
		if !page.HasMore {
			return TurnSnapshot{}, fmt.Errorf("droids: turn %q: %w", id, ErrTurnNotFound)
		}
	}
}

// History pages canonical diagnostic messages.
func (d *Droid) History(ctx context.Context, query HistoryQuery) (MessagePage, error) {
	if d == nil || d.sdk == nil {
		return MessagePage{}, fmt.Errorf("droids: History requires a droid opened with droids.Open")
	}
	page, err := d.sdk.store.Records(ctx, RecordQuery{
		After: query.After, Before: query.Before, Limit: query.Limit,
		Kind: messageRecordKind, Descending: query.Descending,
	})
	if err != nil {
		return MessagePage{}, err
	}
	return messagePageFromRecords(page, false, d.sdk.conversation)
}

func messagePageFromRecords(page RecordPage, reverse bool, conversation ConversationID) (MessagePage, error) {
	result := MessagePage{Next: page.Next, HasMore: page.HasMore}
	for index := range page.Records {
		if reverse {
			index = len(page.Records) - 1 - index
		}
		record := page.Records[index]
		if record.Kind != messageRecordKind || record.Version != recordVersion {
			return MessagePage{}, fmt.Errorf("droids: invalid message history record %s/%s", record.Kind, record.ID)
		}
		message, err := decodeMessageEnvelope(record.Payload)
		if err != nil {
			return MessagePage{}, err
		}
		if string(message.ID) != record.ID || message.ConversationID != conversation {
			return MessagePage{}, fmt.Errorf("droids: message history record %q identity mismatch", record.ID)
		}
		message.Sequence = record.Sequence
		result.Messages = append(result.Messages, message)
	}
	return result, nil
}

// Subscribe replays durable events then follows live durable/transient events.
func (d *Droid) Subscribe(ctx context.Context, options SubscribeOptions) (Subscription, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Subscribe requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrClosed
	}
	page, err := rt.store.Events(ctx, EventQuery{After: options.After, Limit: maxSubscriptionReplay})
	if err != nil {
		return nil, err
	}
	if page.HasMore {
		return nil, ErrSubscriberLagged
	}
	buffer := options.Buffer
	if buffer <= 0 {
		buffer = defaultSubscriptionBuffer
	}
	if buffer > maxSubscriptionBuffer {
		return nil, fmt.Errorf("droids: subscription buffer exceeds %d", maxSubscriptionBuffer)
	}
	if buffer < len(page.Events)+1 {
		buffer = len(page.Events) + 1
	}
	sub := &sdkSubscription{
		runtime: rt, events: make(chan EventEnvelope, buffer), done: make(chan struct{}),
		transient: options.IncludeTransient,
	}
	for _, event := range page.Events {
		envelope, err := decodeLifecycleEvent(event, rt.conversation)
		if err != nil {
			return nil, err
		}
		sub.events <- envelope
	}
	rt.subsMu.Lock()
	rt.subs[sub] = struct{}{}
	rt.subsMu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			sub.close(ctx.Err())
		case <-sub.done:
		}
	}()
	return sub, nil
}

func (rt *sdkRuntime) removeSubscription(sub *sdkSubscription) {
	rt.subsMu.Lock()
	delete(rt.subs, sub)
	rt.subsMu.Unlock()
}

func (rt *sdkRuntime) publish(event EventEnvelope) {
	rt.subsMu.Lock()
	var lagged []*sdkSubscription
	for sub := range rt.subs {
		if !event.Durable && !sub.transient {
			continue
		}
		select {
		case sub.events <- event:
		default:
			lagged = append(lagged, sub)
		}
	}
	rt.subsMu.Unlock()
	for _, sub := range lagged {
		sub.close(ErrSubscriberLagged)
	}
}

func (rt *sdkRuntime) commitLocked(ctx context.Context, mutations []EncodedMutation, events []EncodedDurableEvent) error {
	previousTransition := rt.state.LastTransitionID
	transitionID, err := newID("transition_")
	if err != nil {
		return err
	}
	rt.state.LastTransitionID = transitionID
	runtimeMutation, err := runtimeMutation(rt.state)
	if err != nil {
		rt.state.LastTransitionID = previousTransition
		return err
	}
	mutations = append([]EncodedMutation{runtimeMutation}, mutations...)
	previousEvent := rt.lastEvent
	previousRevision := rt.revision
	result, err := rt.store.Commit(ctx, CommitRequest{
		ExpectedRevision: previousRevision, Mutations: mutations, Events: events,
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			rt.state.LastTransitionID = previousTransition
			return err
		}
		if reconcileErr := rt.reconcileCommitLocked(previousRevision, previousEvent, transitionID, err); reconcileErr != nil {
			rt.state.LastTransitionID = previousTransition
			return reconcileErr
		}
		return nil
	}
	rt.revision = result.Revision
	rt.lastEvent = result.LastEvent
	rt.signalChangedLocked()
	for index, encoded := range events {
		sequence := previousEvent + EventSequence(index+1)
		stored := storedEvent(encoded, sequence, result.Revision)
		envelope, decodeErr := decodeLifecycleEvent(stored, rt.conversation)
		if decodeErr == nil {
			rt.publish(envelope)
		}
	}
	return nil
}

func (rt *sdkRuntime) reconcileCommitLocked(previousRevision uint64, previousEvent EventSequence, transitionID string, commitErr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	persisted, stateErr := rt.store.State(ctx)
	if stateErr != nil || persisted.Revision <= previousRevision {
		return errors.Join(commitErr, stateErr)
	}
	state, decodeErr := decodeRuntime(persisted.RuntimeState)
	if decodeErr != nil {
		return errors.Join(commitErr, decodeErr)
	}
	if state.LastTransitionID != transitionID {
		return errors.Join(commitErr, ErrConflict)
	}
	rt.state = state
	rt.revision = persisted.Revision
	rt.lastEvent = persisted.LastEvent
	rt.signalChangedLocked()
	page, eventsErr := rt.store.Events(ctx, EventQuery{After: previousEvent, Limit: maxSubscriptionReplay})
	if eventsErr != nil || page.HasMore {
		rt.lagAllSubscriptions()
		return nil
	}
	for _, event := range page.Events {
		envelope, err := decodeLifecycleEvent(event, rt.conversation)
		if err != nil {
			rt.lagAllSubscriptions()
			return nil
		}
		rt.publish(envelope)
	}
	return nil
}

func (rt *sdkRuntime) lagAllSubscriptions() {
	rt.subsMu.Lock()
	subs := make([]*sdkSubscription, 0, len(rt.subs))
	for sub := range rt.subs {
		subs = append(subs, sub)
	}
	rt.subsMu.Unlock()
	for _, sub := range subs {
		sub.close(ErrSubscriberLagged)
	}
}

func (rt *sdkRuntime) signalChangedLocked() {
	close(rt.changed)
	rt.changed = make(chan struct{})
}

func (rt *sdkRuntime) interruptLocked(ctx context.Context, failure *DroidError) error {
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	rt.state.Error = durableError(failure)
	rt.state.Reason = errorMessage(failure)
	var mutations []EncodedMutation
	attemptWasOpen := rt.state.AttemptOpen
	if attemptWasOpen {
		attempt, mutationErr := attemptHistoryMutation(rt.state, ExecutionInterrupted)
		if mutationErr != nil {
			return mutationErr
		}
		mutations = append(mutations, attempt)
	}
	events := make([]EncodedDurableEvent, 0, 3)
	if rt.state.CyclePhase == cycleModelStarted {
		cycleInterrupted, _ := lifecycleEvent("model_cycle.interrupted", rt.state.TurnID, rt.state.AttemptID, nil)
		events = append(events, cycleInterrupted)
		rt.state.CyclePhase = cycleReady
	}
	rt.state.Status = ExecutionInterrupted
	rt.state.AttemptOpen = false
	if attemptWasOpen {
		attemptSettled, _ := lifecycleEvent("attempt.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"status": ExecutionInterrupted})
		events = append(events, attemptSettled)
	}
	interrupted, _ := lifecycleEvent("execution.interrupted", rt.state.TurnID, rt.state.AttemptID, map[string]any{"error": rt.state.Error})
	events = append(events, interrupted)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	rt.releaseRunLocked()
	if rt.handle != nil {
		rt.handle.complete(outcomeFromState(rt.conversation, rt.state))
	}
	return nil
}

func (rt *sdkRuntime) settleLocked(ctx context.Context, status ExecutionStatus, failure *DroidError) error {
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	attemptWasOpen := rt.state.AttemptOpen
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	abortMutations, abortEvents, abortErr := rt.finalizeAbortedModelLocked(status)
	if abortErr != nil {
		rt.state = before
		return abortErr
	}
	mutations = append(mutations, abortMutations...)
	events = append(events, abortEvents...)
	toolMutations, toolEvents, finalizeErr := rt.finalizeUnresolvedToolsLocked(status)
	if finalizeErr != nil {
		rt.state = before
		return finalizeErr
	}
	mutations = append(mutations, toolMutations...)
	events = append(events, toolEvents...)
	rt.state.Error = durableError(failure)
	rt.state.Reason = errorMessage(failure)
	if attemptWasOpen {
		attempt, mutationErr := attemptHistoryMutation(rt.state, status)
		if mutationErr != nil {
			rt.state = before
			return mutationErr
		}
		mutations = append(mutations, attempt)
	}
	rt.state.Status = status
	rt.state.AttemptOpen = false
	rt.state.CyclePhase = cycleReady
	rt.state.TerminatePending = false
	rt.state.AbortRequested = status == ExecutionAborted
	turn, err := turnHistoryMutation(rt.state, status)
	if err != nil {
		rt.state = before
		return err
	}
	mutations = append(mutations, turn)
	if attemptWasOpen {
		attemptEvent, _ := lifecycleEvent("attempt.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"status": status})
		events = append(events, attemptEvent)
	}
	executionEvent, _ := lifecycleEvent("execution.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"status": status, "error": rt.state.Error})
	turnEvent, _ := lifecycleEvent("turn.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"status": status, "error": rt.state.Error})
	events = append(events, executionEvent, turnEvent)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	rt.releaseRunLocked()
	if rt.handle != nil {
		rt.handle.complete(outcomeFromState(rt.conversation, rt.state))
	}
	return nil
}

func (rt *sdkRuntime) finalizeAbortedModelLocked(status ExecutionStatus) ([]EncodedMutation, []EncodedDurableEvent, error) {
	if status != ExecutionAborted || rt.state.CyclePhase != cycleModelStarted || len(rt.state.Tools) > 0 {
		return nil, nil, nil
	}
	if len(rt.state.Context) > 0 {
		last, err := messageEnvelopeFromWire(rt.state.Context[len(rt.state.Context)-1])
		if err != nil {
			return nil, nil, err
		}
		if assistant, ok := last.Message.(AssistantMessage); ok && len(assistant.ToolCalls()) > 0 {
			return nil, nil, nil
		}
	}
	messageID, err := newMessageID()
	if err != nil {
		return nil, nil, err
	}
	assistant := AssistantMessage{
		ID: string(messageID), Provider: rt.droid.model.Provider, Model: rt.droid.model.ID,
		StopReason: StopReasonAborted, ErrorMessage: "Execution aborted",
		Timestamp: time.Now().UnixMilli(),
	}
	envelope := MessageEnvelope{
		ID: messageID, ConversationID: rt.conversation, TurnID: rt.state.TurnID,
		CreatedAt: time.Now().UTC(), Message: assistant,
	}
	wire, err := messageEnvelopeToWire(envelope)
	if err != nil {
		return nil, nil, err
	}
	rt.state.Final = &wire
	mutation, err := messageHistoryMutation(envelope)
	if err != nil {
		return nil, nil, err
	}
	event, _ := lifecycleEvent("message.persisted", rt.state.TurnID, rt.state.AttemptID, map[string]any{
		"message_id": messageID, "role": "assistant", "stop_reason": StopReasonAborted,
	})
	return []EncodedMutation{mutation}, []EncodedDurableEvent{event}, nil
}

func (rt *sdkRuntime) finalizeUnresolvedToolsLocked(status ExecutionStatus) ([]EncodedMutation, []EncodedDurableEvent, error) {
	if len(rt.state.Context) == 0 {
		if len(rt.state.Tools) == 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("droids: unresolved tools have no assistant message")
	}
	last, err := messageEnvelopeFromWire(rt.state.Context[len(rt.state.Context)-1])
	if err != nil {
		return nil, nil, err
	}
	assistant, ok := last.Message.(AssistantMessage)
	if !ok {
		if len(rt.state.Tools) == 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("droids: unresolved tools do not follow an assistant message")
	}
	calls := assistant.ToolCalls()
	if len(calls) == 0 {
		if len(rt.state.Tools) == 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("droids: unresolved tool records have no assistant calls")
	}

	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	for _, call := range calls {
		tool, admitted := rt.state.Tools[call.ID]
		var result ToolResultMessage
		if admitted && tool.Final != nil {
			message, decodeErr := messageFromWire(*tool.Final)
			if decodeErr != nil {
				return nil, nil, decodeErr
			}
			var ok bool
			result, ok = message.(ToolResultMessage)
			if !ok {
				return nil, nil, fmt.Errorf("droids: final tool record %q is not a tool result", tool.ID)
			}
		} else {
			result = toolResultMessage(call, toolErrorText("Tool call ended without a final result because the turn was "+string(status)))
		}
		messageID, err := newMessageID()
		if err != nil {
			return nil, nil, err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: rt.state.TurnID,
			CreatedAt: time.Now().UTC(), Message: result,
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			return nil, nil, err
		}
		messageMutation, err := messageHistoryMutation(envelope)
		if err != nil {
			return nil, nil, err
		}
		mutations = append(mutations, messageMutation)
		if admitted {
			archived := tool
			archived.InterruptedPhase = tool.Phase
			archived.Phase = toolPhaseCompleted
			finalWire, err := messageToWire(result)
			if err != nil {
				return nil, nil, err
			}
			archived.Final = &finalWire
			payload, err := json.Marshal(archived)
			if err != nil {
				return nil, nil, err
			}
			mutations = append(mutations, EncodedMutation{
				Operation: MutationPut, RecordKind: toolRecordKind, RecordID: string(tool.ID),
				Scope: RecordHistory, Version: recordVersion, Payload: payload,
			})
			delete(rt.state.Tools, call.ID)
		}
		toolID := call.ID
		attemptID := rt.state.AttemptID
		if admitted {
			toolID = tool.ID
			attemptID = tool.AdmissionAttemptID
		}
		event, _ := lifecycleEvent("tool.completed", rt.state.TurnID, attemptID, map[string]any{
			"tool_call_id": toolID, "provider_call_id": call.ProviderCallID,
			"message_id": messageID, "is_error": result.IsError, "terminal_status": status,
		})
		events = append(events, event)
	}
	if len(rt.state.Tools) > 0 {
		return nil, nil, fmt.Errorf("droids: unresolved tool records do not match the active assistant message")
	}
	return mutations, events, nil
}

func executionSnapshot(state durableRuntime) ExecutionSnapshot {
	return ExecutionSnapshot{
		TurnID: state.TurnID, Status: state.Status, Reason: state.Reason,
		BoundaryReaction: state.BoundaryReaction,
		Error:            expandDurableError(state.Error),
	}
}

func outcomeFromState(conversation ConversationID, state durableRuntime) Outcome {
	outcome := Outcome{
		ConversationID: conversation, TurnID: state.TurnID, Status: state.Status,
		Error: expandDurableError(state.Error), CheckpointID: state.CheckpointID, Usage: state.Usage,
	}
	if state.Final != nil {
		message, err := messageEnvelopeFromWire(*state.Final)
		if err == nil {
			outcome.FinalMessage = &message
		}
	}
	return outcome
}

func durableError(err *DroidError) *durableDroidError {
	if err == nil {
		return nil
	}
	return &durableDroidError{Kind: err.Kind, Message: boundedDiagnostic(err.Message), Retryable: err.Retryable}
}

func expandDurableError(err *durableDroidError) *DroidError {
	if err == nil {
		return nil
	}
	return &DroidError{Kind: err.Kind, Message: err.Message, Retryable: err.Retryable}
}

func cloneDroidError(err *DroidError) *DroidError {
	if err == nil {
		return nil
	}
	clone := *err
	return &clone
}

func errorMessage(err *DroidError) string {
	if err == nil {
		return ""
	}
	return boundedDiagnostic(err.Message)
}

func cloneDurableRuntime(state durableRuntime) (durableRuntime, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return durableRuntime{}, err
	}
	var clone durableRuntime
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return durableRuntime{}, err
	}
	if clone.Tools == nil {
		clone.Tools = make(map[ToolCallID]durableTool)
	}
	return clone, nil
}
