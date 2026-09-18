package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

func (rt *sdkRuntime) compactIfNeeded(ctx context.Context, turnID TurnID, force bool) error {
	rt.mu.Lock()
	if rt.state.TurnID != turnID {
		rt.mu.Unlock()
		return nil
	}
	envelopes, err := runtimeMessageEnvelopes(rt.state)
	contextWire := append([]wireMessageEnvelope(nil), rt.state.Context...)
	attemptID := rt.state.AttemptID
	sourceCheckpoint := rt.state.CheckpointID
	rt.mu.Unlock()
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	messages := make([]Message, 0, len(envelopes))
	for _, envelope := range envelopes {
		messages = append(messages, envelope.Message)
	}
	configuration := rt.currentRequestConfiguration()
	usage, err := rt.measureContextWithConfiguration(ctx, rt.provider, rt.droid.model, configuration.reasoning, configuration.maxTokens, messages, configuration)
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	rt.mu.Lock()
	recovering := rt.state.Compaction != nil && rt.state.Compaction.TurnID == turnID && rt.state.Compaction.ID != ""
	if recovering {
		force = rt.state.Compaction.Forced
	}
	rt.mu.Unlock()
	if !recovering && !force && !sdkShouldCompact(usage) {
		return nil
	}

	prefixEnds, err := compactionPrefixEnds(messages, unconsumedCompactionTail(messages))
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	if len(prefixEnds) == 0 {
		return rt.recordCompactionFailure(turnID, ErrContextNotAdaptable)
	}

	rt.mu.Lock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		rt.mu.Unlock()
		return nil
	}
	if rt.state.Compaction == nil {
		compactionID, err := newID("compact_")
		if err != nil {
			rt.mu.Unlock()
			return err
		}
		rt.state.Compaction = &durableCompaction{ID: compactionID, TurnID: turnID, Forced: force}
		started, _ := lifecycleEvent("compaction.started", turnID, attemptID, map[string]any{
			"compaction_id": compactionID, "estimated_input": usage.EstimatedInput, "prefix_messages": prefixEnds[0],
		})
		if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{started}); err != nil {
			rt.state.Compaction = nil
			rt.mu.Unlock()
			return err
		}
	} else if rt.state.Compaction.ID == "" || rt.state.Compaction.TurnID != turnID {
		rt.mu.Unlock()
		return fmt.Errorf("droids: active compaction does not match turn")
	}
	rt.mu.Unlock()

	target := resolvedContextTarget{
		public:   ContextTarget{Model: rt.droid.model, Reasoning: configuration.reasoning},
		provider: rt.provider, model: rt.droid.model, maxTokens: configuration.maxTokens,
	}
	compacted, err := rt.compactContext(ctx, target, contextWire, usage, usage, configuration, turnID)
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	checkpointID, err := newCheckpointID()
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || rt.state.CheckpointID != sourceCheckpoint || !sameContext(rt.state.Context, contextWire) {
		return fmt.Errorf("droids: active context changed during compaction")
	}
	if rt.currentRequestConfiguration() != configuration {
		return rt.recordCompactionFailureLocked(turnID, fmt.Errorf("droids: request configuration changed during compaction: %w", ErrConflict))
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	rt.state.Context = compacted.context
	rt.state.CheckpointID = checkpointID
	checkpointPayload, err := json.Marshal(map[string]any{
		"checkpoint_id": checkpointID, "source_checkpoint_id": before.CheckpointID,
		"turn_id": turnID, "summary_message": compacted.summary,
		"retained_from": compacted.retainedFrom(), "before": usage, "after": compacted.after,
	})
	if err != nil {
		rt.state = before
		return err
	}
	checkpoint := EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: checkpointKind,
		RecordID: string(checkpointID), Scope: RecordHistory,
		Version: recordVersion, Payload: checkpointPayload,
	}
	if rt.state.Compaction == nil || rt.state.Compaction.ID == "" || rt.state.Compaction.TurnID != turnID {
		rt.state = before
		return fmt.Errorf("droids: active compaction identity was lost")
	}
	compactionID := rt.state.Compaction.ID
	rt.state.Compaction = nil
	completed, _ := lifecycleEvent("compaction.completed", turnID, rt.state.AttemptID, map[string]any{
		"compaction_id": compactionID, "checkpoint_id": checkpointID, "before": usage.EstimatedInput, "after": compacted.after.EstimatedInput,
	})
	contextUpdated, _ := lifecycleEvent("context.updated", turnID, rt.state.AttemptID, map[string]any{
		"compaction_id": compactionID, "checkpoint_id": checkpointID, "estimated_input": compacted.after.EstimatedInput, "context_window": compacted.after.ContextWindow,
	})
	if err := rt.commitLocked(ctx, []EncodedMutation{checkpoint}, []EncodedDurableEvent{completed, contextUpdated}); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func (rt *sdkRuntime) measureContext(ctx context.Context, provider Provider, model Model, messages []Message) (ContextUsage, error) {
	configuration := rt.currentRequestConfiguration()
	return rt.measureContextWithConfiguration(ctx, provider, model, configuration.reasoning, configuration.maxTokens, messages, configuration)
}

func (rt *sdkRuntime) measureContextFor(
	ctx context.Context,
	provider Provider,
	model Model,
	reasoning string,
	reservedOutput int,
	messages []Message,
) (ContextUsage, error) {
	return rt.measureContextWithConfiguration(
		ctx, provider, model, reasoning, reservedOutput, messages, rt.currentRequestConfiguration(),
	)
}

func configuredContextModel(model Model, configuration *runtimeRequestConfiguration) Model {
	if configuration != nil && configuration.contextWindow > 0 {
		model.ContextWindow = configuration.contextWindow
		model.MaxInputTokens = configuration.contextWindow
	}
	return model
}

func (rt *sdkRuntime) measureContextWithConfiguration(
	ctx context.Context,
	provider Provider,
	model Model,
	reasoning string,
	reservedOutput int,
	messages []Message,
	configuration *runtimeRequestConfiguration,
) (ContextUsage, error) {
	if model.Provider == rt.droid.model.Provider && model.ID == rt.droid.model.ID {
		model = configuredContextModel(model, configuration)
	}
	tools := append([]ToolSchema(nil), configuration.toolSchemas...)
	request := Request{
		SystemPrompt: configuration.systemPrompt, Messages: messages,
		Tools: tools, Reasoning: reasoning,
		MaxTokens: reservedOutput,
	}
	if measurer, ok := provider.(ContextMeasurer); ok {
		usage, err := measurer.MeasureContext(ctx, model, request)
		if err != nil {
			return ContextUsage{}, err
		}
		usage.Model = model.metadata()
		return usage, nil
	}
	return estimateContextUsage(
		configuration.systemPrompt, tools, model, reasoning, reservedOutput, messages,
	), nil
}

func sameContext(current, expected []wireMessageEnvelope) bool {
	if len(current) != len(expected) {
		return false
	}
	for index := range current {
		if current[index].ID != expected[index].ID {
			return false
		}
	}
	return true
}

func sdkShouldCompact(usage ContextUsage) bool {
	if usage.ContextWindow > 0 {
		trigger := int(float64(usage.ContextWindow) * defaultCompactionTriggerFraction)
		if usage.EstimatedInput+usage.ReservedOutput >= trigger {
			return true
		}
	}
	if usage.MaxInputTokens > 0 {
		trigger := int(float64(usage.MaxInputTokens) * defaultCompactionTriggerFraction)
		return usage.EstimatedInput >= trigger
	}
	return false
}

func (rt *sdkRuntime) recordCompactionFailure(turnID TurnID, failure error) error {
	if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
		return failure
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.recordCompactionFailureLocked(turnID, failure)
}

func (rt *sdkRuntime) recordCompactionFailureLocked(turnID TurnID, failure error) error {
	if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
		return failure
	}
	compactionID := ""
	if rt.state.Compaction != nil && rt.state.Compaction.TurnID == turnID {
		compactionID = rt.state.Compaction.ID
	}
	if compactionID == "" {
		var err error
		compactionID, err = newID("compact_")
		if err != nil {
			return errorsJoin(failure, err)
		}
	}
	before := rt.state.Compaction
	rt.state.Compaction = nil
	event, _ := lifecycleEvent("compaction.failed", turnID, rt.state.AttemptID, map[string]any{
		"compaction_id": compactionID, "error": safeRuntimeError(DroidErrorCompaction, failure),
	})
	if err := rt.commitLocked(context.Background(), nil, []EncodedDurableEvent{event}); err != nil {
		rt.state.Compaction = before
		return errorsJoin(failure, err)
	}
	return failure
}

func errorsJoin(first, second error) error {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return fmt.Errorf("%v: %w", first, second)
}
