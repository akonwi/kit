package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxContextOperationIDBytes = 256
	maxContextModelBytes       = 512
	maxContextReasoningBytes   = 32
)

type resolvedContextTarget struct {
	public    ContextTarget
	provider  Provider
	model     Model
	maxTokens int
}

type unsuitableCompactionCandidate struct {
	cause error
}

func (err *unsuitableCompactionCandidate) Error() string { return err.cause.Error() }
func (err *unsuitableCompactionCandidate) Unwrap() error { return err.cause }

func unsuitableCandidate(err error) error {
	return &unsuitableCompactionCandidate{cause: err}
}

type contextMaintenanceFlight struct {
	kind        string
	operationID string
	target      ContextTarget
	force       bool
	cancel      context.CancelFunc
	done        chan struct{}
	result      CompactContextResult
	err         error
}

type durableContextUsage struct {
	Model          Model `json:"model"`
	EstimatedInput int   `json:"estimated_input"`
	ReservedOutput int   `json:"reserved_output"`
	ContextWindow  int   `json:"context_window"`
	MaxInputTokens int   `json:"max_input_tokens"`
	Remaining      int   `json:"remaining"`
	Exact          bool  `json:"exact"`
}

type durableCompactionIntent struct {
	OperationID string `json:"operation_id"`
	TargetModel string `json:"target_model"`
	Reasoning   string `json:"reasoning,omitempty"`
	Force       *bool  `json:"force,omitempty"`
}

type durableCompactionReceipt struct {
	OperationID  string              `json:"operation_id"`
	TargetModel  string              `json:"target_model"`
	Reasoning    string              `json:"reasoning,omitempty"`
	Force        *bool               `json:"force,omitempty"`
	Compacted    bool                `json:"compacted"`
	CheckpointID CheckpointID        `json:"checkpoint_id,omitempty"`
	Before       durableContextUsage `json:"before"`
	After        durableContextUsage `json:"after"`
}

// AssessContext measures settled active context against a target model without
// changing the conversation.
func (d *Droid) AssessContext(ctx context.Context, target ContextTarget) (ContextAssessment, error) {
	if d == nil || d.sdk == nil {
		return ContextAssessment{}, fmt.Errorf("droids: AssessContext requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := canonicalRequestedContextTarget(target)
	if err != nil {
		return ContextAssessment{}, err
	}
	resolved, err := d.resolveContextTarget(target)
	if err != nil {
		return ContextAssessment{}, err
	}
	rt := d.sdk
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return ContextAssessment{}, ErrClosed
	}
	if rt.contextFlight != nil || !settledContextStatus(rt.state) {
		rt.mu.Unlock()
		return ContextAssessment{}, ErrBusy
	}
	contextWire := cloneWireContext(rt.state.Context)
	checkpointID := rt.state.CheckpointID
	operationContext, cancel := context.WithCancel(ctx)
	flight := &contextMaintenanceFlight{kind: "assessment", cancel: cancel, done: make(chan struct{})}
	rt.contextFlight = flight
	rt.signalChangedLocked()
	rt.mu.Unlock()

	assessment, operationErr := rt.assessCapturedContext(operationContext, resolved, contextWire)

	rt.mu.Lock()
	if operationErr == nil {
		switch {
		case rt.closed:
			operationErr = ErrClosed
		case !sameContext(rt.state.Context, contextWire) || rt.state.CheckpointID != checkpointID:
			operationErr = ErrConflict
		case !settledContextStatus(rt.state):
			operationErr = ErrBusy
		}
	}
	if rt.contextFlight == flight {
		rt.contextFlight = nil
	}
	flight.err = operationErr
	close(flight.done)
	rt.signalChangedLocked()
	rt.mu.Unlock()
	cancel()
	return assessment, operationErr
}

// CompactContext idempotently adapts settled active context to a target model.
// It leaves the droid configured with its current model; the replacement context
// is validated against both current and target configurations.
func (d *Droid) CompactContext(ctx context.Context, options CompactContextOptions) (CompactContextResult, error) {
	if d == nil || d.sdk == nil {
		return CompactContextResult{}, fmt.Errorf("droids: CompactContext requires a droid opened with droids.Open")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateContextOperationID(options.OperationID); err != nil {
		return CompactContextResult{}, err
	}
	target, err := canonicalRequestedContextTarget(options.Target)
	if err != nil {
		return CompactContextResult{}, err
	}
	rt := d.sdk
	rt.mu.Lock()
	closed := rt.closed
	rt.mu.Unlock()
	if closed {
		return CompactContextResult{}, ErrClosed
	}

	// Receipts are immutable and conversation-scoped. Resolve them before busy
	// state or current catalog lookup so delayed retries reproduce their original
	// result even after execution resumed or the target model disappeared.
	receipt, found, err := findCompactionReceipt(ctx, rt.store, options.OperationID)
	if err != nil {
		return CompactContextResult{}, err
	}
	if found {
		rt.mu.Lock()
		closed = rt.closed
		rt.mu.Unlock()
		if closed {
			return CompactContextResult{}, ErrClosed
		}
		return receipt.result(target, options.Force)
	}
	intent, intentFound, err := findCompactionIntent(ctx, rt.store, options.OperationID)
	if err != nil {
		return CompactContextResult{}, err
	}
	force := options.Force
	if intentFound {
		force, err = intent.effectiveForce(target, options.Force)
		if err != nil {
			return CompactContextResult{}, err
		}
	}

	resolved, err := d.resolveContextTarget(target)
	if err != nil {
		return CompactContextResult{}, err
	}
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return CompactContextResult{}, ErrClosed
	}
	if flight := rt.contextFlight; flight != nil {
		if flight.kind != "compaction" || flight.operationID != options.OperationID {
			rt.mu.Unlock()
			return CompactContextResult{}, ErrBusy
		}
		if flight.target != target || flight.force != force {
			rt.mu.Unlock()
			return CompactContextResult{}, fmt.Errorf("droids: context operation id %q was reused with a different target: %w", options.OperationID, ErrConflict)
		}
		done := flight.done
		rt.mu.Unlock()
		select {
		case <-done:
			return flight.result, flight.err
		case <-ctx.Done():
			return CompactContextResult{}, ctx.Err()
		}
	}
	if !settledContextStatus(rt.state) {
		rt.mu.Unlock()
		return CompactContextResult{}, ErrBusy
	}
	contextWire := cloneWireContext(rt.state.Context)
	checkpointID := rt.state.CheckpointID
	operationContext, cancel := context.WithCancel(ctx)
	flight := &contextMaintenanceFlight{
		kind: "compaction", operationID: options.OperationID,
		target: target, force: force, cancel: cancel, done: make(chan struct{}),
	}
	rt.contextFlight = flight
	rt.signalChangedLocked()
	rt.mu.Unlock()

	// Close the race in which another identical operation committed after the
	// initial receipt lookup but before this flight was installed.
	receipt, found, operationErr := findCompactionReceipt(operationContext, rt.store, options.OperationID)
	var result CompactContextResult
	if operationErr == nil && found {
		result, operationErr = receipt.result(target, options.Force)
	} else if operationErr == nil {
		intent, intentFound, operationErr = findCompactionIntent(operationContext, rt.store, options.OperationID)
		if operationErr == nil && intentFound {
			force, operationErr = intent.effectiveForce(target, options.Force)
		}
		if operationErr == nil {
			result, operationErr = rt.compactCapturedContext(
				operationContext, options.OperationID, resolved, contextWire, checkpointID, intentFound, force,
			)
		}
	}
	rt.finishContextCompaction(flight, result, operationErr)
	return result, operationErr
}

func (rt *sdkRuntime) finishContextCompaction(flight *contextMaintenanceFlight, result CompactContextResult, err error) {
	rt.mu.Lock()
	if rt.contextFlight == flight {
		rt.contextFlight = nil
	}
	flight.result = result
	flight.err = err
	close(flight.done)
	rt.signalChangedLocked()
	rt.mu.Unlock()
	flight.cancel()
}

func (d *Droid) resolveContextTarget(target ContextTarget) (resolvedContextTarget, error) {
	provider, model, err := d.sdk.config.Providers.Resolve(target.Model)
	if err != nil {
		return resolvedContextTarget{}, fmt.Errorf("droids: resolve target model %q: %w", target.Model, err)
	}
	canonicalModel := model.Provider + "/" + model.ID
	if canonicalModel != target.Model {
		return resolvedContextTarget{}, fmt.Errorf("droids: target model %q did not resolve exactly to %q", target.Model, canonicalModel)
	}
	maxTokens, err := resolveRequestMaxTokens(model, 0, target.Reasoning)
	if err != nil {
		return resolvedContextTarget{}, err
	}
	return resolvedContextTarget{
		public: target, provider: provider, model: model, maxTokens: maxTokens,
	}, nil
}

func (rt *sdkRuntime) assessCapturedContext(ctx context.Context, target resolvedContextTarget, contextWire []wireMessageEnvelope) (ContextAssessment, error) {
	messages, err := messagesFromWireContext(contextWire)
	if err != nil {
		return ContextAssessment{}, err
	}
	usage, err := rt.measureContextFor(ctx, target.provider, target.model, target.public.Reasoning, target.maxTokens, messages)
	if err != nil {
		return ContextAssessment{}, err
	}
	replayErr := validateContextReplay(ctx, target.provider, target.model, messages)
	if errors.Is(replayErr, context.Canceled) || errors.Is(replayErr, context.DeadlineExceeded) {
		return ContextAssessment{}, replayErr
	}
	compatible := replayErr == nil
	return ContextAssessment{
		Target: target.public, Usage: usage, ReplayCompatible: compatible,
		RequiresCompaction: !compatible || sdkShouldCompact(usage),
	}, nil
}

func (rt *sdkRuntime) compactCapturedContext(
	ctx context.Context,
	operationID string,
	target resolvedContextTarget,
	contextWire []wireMessageEnvelope,
	sourceCheckpoint CheckpointID,
	intentExists bool,
	force bool,
) (CompactContextResult, error) {
	assessment, err := rt.assessCapturedContext(ctx, target, contextWire)
	if err != nil {
		return CompactContextResult{}, err
	}
	result := CompactContextResult{
		OperationID: operationID, Target: target.public, Forced: force,
		CheckpointID: sourceCheckpoint, Before: assessment.Usage, After: assessment.Usage,
	}
	if !assessment.RequiresCompaction && !force {
		if err := rt.commitCompactionResult(ctx, contextWire, sourceCheckpoint, result, nil, nil, "", intentExists); err != nil {
			return CompactContextResult{}, err
		}
		return result, nil
	}

	messages, err := messagesFromWireContext(contextWire)
	if err != nil {
		return CompactContextResult{}, err
	}
	if len(messages) == 0 {
		if err := rt.commitCompactionResult(ctx, contextWire, sourceCheckpoint, result, nil, nil, "", intentExists); err != nil {
			return CompactContextResult{}, err
		}
		return result, nil
	}
	if err := validateContextReplay(ctx, rt.provider, rt.droid.model, messages); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CompactContextResult{}, err
		}
		return CompactContextResult{}, errors.Join(ErrUnsafeContinuation, err)
	}
	currentConfiguration := rt.currentRequestConfiguration()
	currentBefore, err := rt.measureContextWithConfiguration(
		ctx, rt.provider, rt.droid.model, currentConfiguration.reasoning, currentConfiguration.maxTokens, messages, currentConfiguration,
	)
	if err != nil {
		return CompactContextResult{}, err
	}
	if compactionSummaryTurnID(contextWire) == "" {
		return CompactContextResult{}, fmt.Errorf("droids: context has no message provenance")
	}
	started, _ := lifecycleEvent("compaction.started", "", "", map[string]any{
		"operation_id": operationID, "target_model": target.public.Model,
		"estimated_input": assessment.Usage.EstimatedInput,
	})
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return CompactContextResult{}, ErrClosed
	}
	if rt.contextFlight == nil || rt.contextFlight.operationID != operationID ||
		!sameContext(rt.state.Context, contextWire) || rt.state.CheckpointID != sourceCheckpoint ||
		!settledContextStatus(rt.state) {
		rt.mu.Unlock()
		return CompactContextResult{}, ErrConflict
	}
	var startMutations []EncodedMutation
	if !intentExists {
		intentMutation, err := compactionIntentMutation(operationID, target.public, force)
		if err != nil {
			rt.mu.Unlock()
			return CompactContextResult{}, err
		}
		startMutations = append(startMutations, intentMutation)
	}
	if err := rt.commitLocked(ctx, startMutations, []EncodedDurableEvent{started}); err != nil {
		rt.mu.Unlock()
		return CompactContextResult{}, err
	}
	rt.mu.Unlock()

	var lastCandidateErr error
	for _, prefixEnd := range quiescentCompactionPrefixEnds(messages) {
		if err := contextError(ctx); err != nil {
			return CompactContextResult{}, err
		}
		replacement, summaryWire, after, candidateErr := rt.compactCandidate(
			ctx, target, contextWire, messages, prefixEnd, assessment.Usage, currentBefore, currentConfiguration,
		)
		if candidateErr != nil {
			var unsuitable *unsuitableCompactionCandidate
			if errors.As(candidateErr, &unsuitable) {
				lastCandidateErr = candidateErr
				continue
			}
			return CompactContextResult{}, rt.recordExplicitCompactionFailure(operationID, candidateErr)
		}
		checkpointID, err := newCheckpointID()
		if err != nil {
			return CompactContextResult{}, rt.recordExplicitCompactionFailure(operationID, err)
		}
		result.Compacted = true
		result.CheckpointID = checkpointID
		result.After = after
		retainedFrom := ""
		if prefixEnd < len(contextWire) {
			retainedFrom = string(contextWire[prefixEnd].ID)
		}
		if err := rt.commitCompactionResult(ctx, contextWire, sourceCheckpoint, result, replacement, &summaryWire, retainedFrom, true); err != nil {
			return CompactContextResult{}, err
		}
		return result, nil
	}
	if lastCandidateErr == nil {
		lastCandidateErr = fmt.Errorf("droids: context cannot be compacted without splitting canonical messages")
	}
	return CompactContextResult{}, rt.recordExplicitCompactionFailure(
		operationID, errors.Join(ErrContextNotAdaptable, lastCandidateErr),
	)
}

func (rt *sdkRuntime) compactCandidate(
	ctx context.Context,
	target resolvedContextTarget,
	contextWire []wireMessageEnvelope,
	messages []Message,
	prefixEnd int,
	before ContextUsage,
	currentBefore ContextUsage,
	currentConfiguration *runtimeRequestConfiguration,
) ([]wireMessageEnvelope, wireMessageEnvelope, ContextUsage, error) {
	if prefixEnd <= 0 || prefixEnd > len(messages) {
		return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: invalid compaction prefix")
	}
	prefix, suffix := messages[:prefixEnd], messages[prefixEnd:]
	if err := validateMessageSequence(prefix); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(err)
	}
	if err := validateMessageSequence(suffix); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(err)
	}
	placeholder := ContextMessage{
		Kind: "summary", Source: "compaction",
		Content: []InputContent{TextInput{Text: "[context summary]\nplaceholder"}},
	}
	candidateShape := append([]Message{placeholder}, suffix...)
	if err := validateMessageSequence(candidateShape); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(err)
	}
	if err := validateContextReplay(ctx, target.provider, target.model, candidateShape); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wireMessageEnvelope{}, ContextUsage{}, err
		}
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: retained context is not replayable by target model: %w", err))
	}
	if err := validateContextReplay(ctx, rt.provider, rt.droid.model, candidateShape); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wireMessageEnvelope{}, ContextUsage{}, err
		}
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: retained context is not replayable by current model: %w", err))
	}

	provider := rt.provider
	model := rt.droid.model
	if rt.config.Compaction.Model != "" {
		resolvedProvider, resolvedModel, err := rt.config.Providers.Resolve(rt.config.Compaction.Model)
		if err != nil {
			return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: resolve compaction model %q: %w", rt.config.Compaction.Model, err)
		}
		provider, model = resolvedProvider, resolvedModel
	}
	if err := validateContextReplay(ctx, provider, model, prefix); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wireMessageEnvelope{}, ContextUsage{}, err
		}
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compaction prefix is not replayable: %w", err))
	}
	prompt := rt.config.Compaction.Prompt
	if prompt == "" {
		prompt = defaultCompactionPrompt
	}
	requestMaxTokens, err := resolveRequestMaxTokens(model, 0, "")
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if model.OutputLimitMode == OutputLimitProviderControlled {
		requestMaxTokens = 0
	}
	stream, err := provider.Stream(ctx, model, Request{
		SystemPrompt: prompt, Messages: prefix, MaxTokens: requestMaxTokens,
	})
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if assistantStreamIsNil(stream) {
		return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: compaction provider returned a nil stream")
	}
	defer stream.Close()
	if err := consumeAssistantStream(ctx, stream, func(StreamEvent) {}); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	summaryResponse, resultErr := stream.Result()
	if err := rt.accountCompactionResponse(ctx, model, &summaryResponse, ""); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if resultErr != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, resultErr
	}
	if summaryResponse.StopReason != StopReasonStop && summaryResponse.StopReason != StopReasonLength {
		return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: compaction model stopped with %s: %s", summaryResponse.StopReason, errText(summaryResponse))
	}
	if summaryResponse.Text() == "" {
		return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: compaction model returned an empty summary")
	}
	messageID, err := newMessageID()
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	turnID := compactionSummaryTurnID(contextWire[:prefixEnd])
	if turnID == "" {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compaction prefix has no message provenance"))
	}
	summaryEnvelope := MessageEnvelope{
		ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
		CreatedAt: time.Now().UTC(),
		Message: ContextMessage{
			Kind: "summary", Source: "compaction",
			Content: []InputContent{TextInput{Text: "[context summary]\n" + summaryResponse.Text()}},
		},
	}
	summaryWire, err := messageEnvelopeToWire(summaryEnvelope)
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	replacementWire := append([]wireMessageEnvelope{summaryWire}, contextWire[prefixEnd:]...)
	replacementMessages, err := messagesFromWireContext(replacementWire)
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if err := validateMessageSequence(replacementMessages); err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, fmt.Errorf("droids: invalid compacted context: %w", err)
	}
	after, err := rt.measureContextFor(ctx, target.provider, target.model, target.public.Reasoning, target.maxTokens, replacementMessages)
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if after.EstimatedInput >= before.EstimatedInput {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compaction did not reduce target context"))
	}
	if sdkShouldCompact(after) {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context remains above the target budget"))
	}
	if err := validateContextReplay(ctx, target.provider, target.model, replacementMessages); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wireMessageEnvelope{}, ContextUsage{}, err
		}
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context is not replayable by target model: %w", err))
	}
	if err := validateContextReplay(ctx, rt.provider, rt.droid.model, replacementMessages); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wireMessageEnvelope{}, ContextUsage{}, err
		}
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context is not replayable by current model: %w", err))
	}
	currentAfter, err := rt.measureContextWithConfiguration(
		ctx, rt.provider, rt.droid.model, currentConfiguration.reasoning, currentConfiguration.maxTokens, replacementMessages, currentConfiguration,
	)
	if err != nil {
		return nil, wireMessageEnvelope{}, ContextUsage{}, err
	}
	if !contextCanRun(currentAfter) || currentAfter.EstimatedInput > currentBefore.EstimatedInput {
		return nil, wireMessageEnvelope{}, ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context is not safe for the current model"))
	}
	return replacementWire, summaryWire, after, nil
}

func (rt *sdkRuntime) commitCompactionResult(
	ctx context.Context,
	sourceContext []wireMessageEnvelope,
	sourceCheckpoint CheckpointID,
	result CompactContextResult,
	replacementContext []wireMessageEnvelope,
	summaryWire *wireMessageEnvelope,
	retainedFrom string,
	intentExists bool,
) error {
	receipt, err := newDurableCompactionReceipt(result)
	if err != nil {
		return err
	}
	receiptPayload, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	receiptMutation := EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: compactionReceiptKind,
		RecordID: result.OperationID, Scope: RecordHistory,
		Version: recordVersion, Payload: receiptPayload,
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	if rt.contextFlight == nil || rt.contextFlight.operationID != result.OperationID ||
		!sameContext(rt.state.Context, sourceContext) || rt.state.CheckpointID != sourceCheckpoint ||
		!settledContextStatus(rt.state) {
		return ErrConflict
	}
	beforeState, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	var mutations []EncodedMutation
	if !intentExists {
		intentMutation, err := compactionIntentMutation(result.OperationID, result.Target, result.Forced)
		if err != nil {
			return err
		}
		mutations = append(mutations, intentMutation)
	}
	mutations = append(mutations, receiptMutation)
	if result.Compacted {
		if summaryWire == nil || len(replacementContext) == 0 || replacementContext[0].ID != summaryWire.ID {
			return fmt.Errorf("droids: compacted result is missing its validated replacement")
		}
		rt.state.Context = cloneWireContext(replacementContext)
		rt.state.CheckpointID = result.CheckpointID
		checkpointPayload, err := json.Marshal(map[string]any{
			"checkpoint_id": result.CheckpointID, "source_checkpoint_id": sourceCheckpoint,
			"operation_id": result.OperationID, "target_model": result.Target.Model,
			"target_reasoning": result.Target.Reasoning, "summary_message": *summaryWire,
			"retained_from": retainedFrom, "before": result.Before, "after": result.After,
		})
		if err != nil {
			rt.state = beforeState
			return err
		}
		mutations = append(mutations, EncodedMutation{
			Operation: MutationAssertAbsent, RecordKind: checkpointKind,
			RecordID: string(result.CheckpointID), Scope: RecordHistory,
			Version: recordVersion, Payload: checkpointPayload,
		})
	}
	var events []EncodedDurableEvent
	if !result.Compacted {
		started, _ := lifecycleEvent("compaction.started", "", "", map[string]any{
			"operation_id": result.OperationID, "target_model": result.Target.Model,
			"estimated_input": result.Before.EstimatedInput,
		})
		events = append(events, started)
	}
	completed, _ := lifecycleEvent("compaction.completed", "", "", map[string]any{
		"operation_id": result.OperationID, "checkpoint_id": result.CheckpointID,
		"compacted": result.Compacted, "before": result.Before.EstimatedInput,
		"after": result.After.EstimatedInput,
	})
	events = append(events, completed)
	if result.Compacted {
		updated, _ := lifecycleEvent("context.updated", "", "", map[string]any{
			"operation_id": result.OperationID, "checkpoint_id": result.CheckpointID,
			"estimated_input": result.After.EstimatedInput, "context_window": result.After.ContextWindow,
		})
		events = append(events, updated)
	}
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = beforeState
		return err
	}
	return nil
}

func (rt *sdkRuntime) recordExplicitCompactionFailure(operationID string, failure error) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	event, _ := lifecycleEvent("compaction.failed", "", "", map[string]any{
		"operation_id": operationID, "error": safeRuntimeError(DroidErrorCompaction, failure),
	})
	if err := rt.commitLocked(context.Background(), nil, []EncodedDurableEvent{event}); err != nil {
		return errorsJoin(failure, err)
	}
	return failure
}

func boolPointer(value bool) *bool { return &value }

func compactionIntentMutation(operationID string, target ContextTarget, force bool) (EncodedMutation, error) {
	payload, err := json.Marshal(durableCompactionIntent{
		OperationID: operationID, TargetModel: target.Model, Reasoning: target.Reasoning, Force: boolPointer(force),
	})
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: compactionIntentKind,
		RecordID: operationID, Scope: RecordHistory, Version: recordVersion,
		Payload: payload,
	}, nil
}

func (intent durableCompactionIntent) effectiveForce(target ContextTarget, requested bool) (bool, error) {
	if intent.OperationID == "" || intent.TargetModel == "" {
		return false, fmt.Errorf("droids: persisted compaction intent is incomplete")
	}
	if intent.TargetModel != target.Model || canonicalContextReasoning(intent.Reasoning) != target.Reasoning ||
		intent.Force != nil && *intent.Force != requested {
		return false, fmt.Errorf("droids: context operation id %q was reused with a different target: %w", intent.OperationID, ErrConflict)
	}
	if intent.Force == nil {
		return false, nil
	}
	return *intent.Force, nil
}

func findCompactionIntent(ctx context.Context, store Store, operationID string) (durableCompactionIntent, bool, error) {
	record, err := store.Record(ctx, compactionIntentKind, operationID)
	if errors.Is(err, ErrRecordNotFound) {
		return durableCompactionIntent{}, false, nil
	}
	if err != nil {
		return durableCompactionIntent{}, false, fmt.Errorf("droids: load compaction intent: %w", err)
	}
	if record.Scope != RecordHistory || record.Version != recordVersion || record.ID != operationID {
		return durableCompactionIntent{}, false, fmt.Errorf("droids: unsupported compaction intent record")
	}
	var intent durableCompactionIntent
	if err := json.Unmarshal(record.Payload, &intent); err != nil {
		return durableCompactionIntent{}, false, fmt.Errorf("droids: decode compaction intent %q: %w", operationID, err)
	}
	if intent.OperationID != operationID {
		return durableCompactionIntent{}, false, fmt.Errorf("droids: compaction intent identity mismatch")
	}
	return intent, true, nil
}

func newDurableCompactionReceipt(result CompactContextResult) (durableCompactionReceipt, error) {
	if result.OperationID == "" || result.Target.Model == "" {
		return durableCompactionReceipt{}, fmt.Errorf("droids: compaction receipt is incomplete")
	}
	beforeModel := result.Before.Model.Provider + "/" + result.Before.Model.ID
	afterModel := result.After.Model.Provider + "/" + result.After.Model.ID
	if beforeModel != result.Target.Model || afterModel != result.Target.Model ||
		result.Before.EstimatedInput < 0 || result.After.EstimatedInput < 0 {
		return durableCompactionReceipt{}, fmt.Errorf("droids: compaction receipt usage is invalid")
	}
	return durableCompactionReceipt{
		OperationID: result.OperationID, TargetModel: result.Target.Model,
		Reasoning: result.Target.Reasoning, Force: boolPointer(result.Forced), Compacted: result.Compacted,
		CheckpointID: result.CheckpointID,
		Before:       durableUsage(result.Before), After: durableUsage(result.After),
	}, nil
}

func (receipt durableCompactionReceipt) result(target ContextTarget, force bool) (CompactContextResult, error) {
	if receipt.OperationID == "" || receipt.TargetModel == "" {
		return CompactContextResult{}, fmt.Errorf("droids: persisted compaction receipt is incomplete")
	}
	if receipt.TargetModel != target.Model || canonicalContextReasoning(receipt.Reasoning) != target.Reasoning ||
		receipt.Force != nil && *receipt.Force != force {
		return CompactContextResult{}, fmt.Errorf("droids: context operation id %q was reused with a different target: %w", receipt.OperationID, ErrConflict)
	}
	before, err := receipt.Before.usage()
	if err != nil {
		return CompactContextResult{}, err
	}
	after, err := receipt.After.usage()
	if err != nil {
		return CompactContextResult{}, err
	}
	if before.Model.Provider+"/"+before.Model.ID != receipt.TargetModel ||
		after.Model.Provider+"/"+after.Model.ID != receipt.TargetModel {
		return CompactContextResult{}, fmt.Errorf("droids: persisted compaction receipt model mismatch")
	}
	if receipt.Compacted && receipt.CheckpointID == "" {
		return CompactContextResult{}, fmt.Errorf("droids: persisted compaction receipt has no checkpoint")
	}
	return CompactContextResult{
		OperationID: receipt.OperationID, Target: target, Forced: receipt.Force != nil && *receipt.Force,
		Compacted: receipt.Compacted, CheckpointID: receipt.CheckpointID,
		Before: before, After: after,
	}, nil
}

func durableUsage(usage ContextUsage) durableContextUsage {
	return durableContextUsage{
		Model: cloneModel(usage.Model), EstimatedInput: usage.EstimatedInput, ReservedOutput: usage.ReservedOutput,
		ContextWindow: usage.ContextWindow, MaxInputTokens: usage.MaxInputTokens,
		Remaining: usage.Remaining, Exact: usage.Exact,
	}
}

func (usage durableContextUsage) usage() (ContextUsage, error) {
	if usage.Model.Provider == "" || usage.Model.ID == "" || usage.EstimatedInput < 0 || usage.ReservedOutput < 0 || usage.ContextWindow < 0 || usage.MaxInputTokens < 0 {
		return ContextUsage{}, fmt.Errorf("droids: persisted compaction usage is invalid")
	}
	return ContextUsage{
		Model: cloneModel(usage.Model), EstimatedInput: usage.EstimatedInput,
		ReservedOutput: usage.ReservedOutput, ContextWindow: usage.ContextWindow,
		MaxInputTokens: usage.MaxInputTokens, Remaining: usage.Remaining,
		Exact: usage.Exact,
	}, nil
}

func findCompactionReceipt(ctx context.Context, store Store, operationID string) (durableCompactionReceipt, bool, error) {
	record, err := store.Record(ctx, compactionReceiptKind, operationID)
	if errors.Is(err, ErrRecordNotFound) {
		return durableCompactionReceipt{}, false, nil
	}
	if err != nil {
		return durableCompactionReceipt{}, false, fmt.Errorf("droids: load compaction receipt: %w", err)
	}
	if record.Scope != RecordHistory || record.Version != recordVersion {
		return durableCompactionReceipt{}, false, fmt.Errorf("droids: unsupported compaction receipt record")
	}
	var receipt durableCompactionReceipt
	if err := json.Unmarshal(record.Payload, &receipt); err != nil {
		return durableCompactionReceipt{}, false, fmt.Errorf("droids: decode compaction receipt %q: %w", operationID, err)
	}
	if receipt.OperationID != operationID || record.ID != operationID {
		return durableCompactionReceipt{}, false, fmt.Errorf("droids: compaction receipt identity mismatch")
	}
	return receipt, true, nil
}

func messagesFromWireContext(contextWire []wireMessageEnvelope) ([]Message, error) {
	envelopes, err := runtimeMessageEnvelopes(durableRuntime{Context: contextWire})
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(envelopes))
	for _, envelope := range envelopes {
		messages = append(messages, envelope.Message)
	}
	return messages, nil
}

func cloneWireContext(contextWire []wireMessageEnvelope) []wireMessageEnvelope {
	return append([]wireMessageEnvelope(nil), contextWire...)
}

func compactionSummaryTurnID(contextWire []wireMessageEnvelope) TurnID {
	for index := len(contextWire) - 1; index >= 0; index-- {
		if contextWire[index].TurnID != "" {
			return contextWire[index].TurnID
		}
	}
	return ""
}

func quiescentCompactionPrefixEnds(messages []Message) []int {
	if len(messages) == 0 {
		return nil
	}
	start := len(messages) / 2
	if start < 1 {
		start = 1
	}
	var candidates []int
	for prefixEnd := start; prefixEnd <= len(messages); prefixEnd++ {
		if validateMessageSequence(messages[:prefixEnd]) != nil || validateMessageSequence(messages[prefixEnd:]) != nil {
			continue
		}
		candidates = append(candidates, prefixEnd)
	}
	return candidates
}

func validateContextReplay(ctx context.Context, provider Provider, model Model, messages []Message) error {
	err := provider.ValidateReplay(ctx, model, messages)
	if contextErr := contextError(ctx); contextErr != nil {
		return contextErr
	}
	return err
}

func contextCanRun(usage ContextUsage) bool {
	if usage.ContextWindow > 0 && usage.EstimatedInput+usage.ReservedOutput > usage.ContextWindow {
		return false
	}
	return usage.MaxInputTokens <= 0 || usage.EstimatedInput <= usage.MaxInputTokens
}

func settledContextStatus(state durableRuntime) bool {
	if state.AttemptOpen || len(state.PendingSteering) != 0 || len(state.Tools) != 0 {
		return false
	}
	return forkableStatus(state.Status)
}

func canonicalRequestedContextTarget(target ContextTarget) (ContextTarget, error) {
	if target.Model != strings.TrimSpace(target.Model) || !validBoundedContextValue(target.Model, maxContextModelBytes) {
		return ContextTarget{}, fmt.Errorf("droids: target model is invalid")
	}
	separator := strings.IndexByte(target.Model, '/')
	if separator <= 0 || separator == len(target.Model)-1 {
		return ContextTarget{}, fmt.Errorf("droids: target model must be an exact provider/model id")
	}
	if target.Reasoning != strings.TrimSpace(target.Reasoning) {
		return ContextTarget{}, fmt.Errorf("droids: target reasoning level is invalid")
	}
	target.Reasoning = canonicalContextReasoning(target.Reasoning)
	if target.Reasoning != "" && !validBoundedContextValue(target.Reasoning, maxContextReasoningBytes) {
		return ContextTarget{}, fmt.Errorf("droids: target reasoning level is invalid")
	}
	return target, nil
}

func canonicalContextReasoning(reasoning string) string {
	if reasoning == "none" {
		return "off"
	}
	return reasoning
}

func validateContextOperationID(id string) error {
	if !validBoundedContextValue(id, maxContextOperationIDBytes) {
		return fmt.Errorf("droids: context operation id is invalid")
	}
	return nil
}

func validBoundedContextValue(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}
