package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const defaultCompactionPrompt = `Summarize the supplied conversation for another model that must continue the work. Preserve goals, decisions, constraints, file paths, code changes, errors, unresolved tasks, and any facts required to continue. Do not add commentary.`

func (rt *sdkRuntime) compactIfNeeded(ctx context.Context, turnID TurnID, force bool) error {
	rt.mu.Lock()
	if rt.state.TurnID != turnID {
		rt.mu.Unlock()
		return nil
	}
	envelopes, err := runtimeMessageEnvelopes(rt.state)
	contextWire := append([]wireMessageEnvelope(nil), rt.state.Context...)
	attemptID := rt.state.AttemptID
	rt.mu.Unlock()
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	messages := make([]Message, 0, len(envelopes))
	for _, envelope := range envelopes {
		messages = append(messages, envelope.Message)
	}
	usage, err := rt.measureContext(ctx, rt.provider, rt.droid.model, messages)
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
	prefixEnd := compactionPrefixEnd(messages)
	if prefixEnd == 0 {
		if force {
			return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: context cannot be compacted without splitting the active tail"))
		}
		return nil
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
			"compaction_id": compactionID, "estimated_input": usage.EstimatedInput, "prefix_messages": prefixEnd,
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

	provider := rt.provider
	model := rt.droid.model
	if rt.config.Compaction.Model != "" {
		resolvedProvider, resolvedModel, err := rt.config.Providers.Resolve(rt.config.Compaction.Model)
		if err != nil {
			return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: resolve compaction model %q: %w", rt.config.Compaction.Model, err))
		}
		provider, model = resolvedProvider, resolvedModel
	}
	if err := provider.ValidateReplay(ctx, model, messages[:prefixEnd]); err != nil {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compaction prefix is not replayable: %w", err))
	}
	prompt := rt.config.Compaction.Prompt
	if prompt == "" {
		prompt = defaultCompactionPrompt
	}
	requestMaxTokens, err := resolveRequestMaxTokens(model, 0, "")
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	if model.OutputLimitMode == OutputLimitProviderControlled {
		requestMaxTokens = 0
	}
	stream, err := provider.Stream(ctx, model, Request{
		SessionID: string(rt.conversation), SystemPrompt: prompt,
		Messages:  messages[:prefixEnd],
		MaxTokens: requestMaxTokens,
	})
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	if assistantStreamIsNil(stream) {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compaction provider returned a nil stream"))
	}
	defer stream.Close()
	if err := consumeAssistantStream(ctx, stream, func(StreamEvent) {}); err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	summaryResponse, resultErr := stream.Result()
	if err := rt.accountCompactionResponse(ctx, model, &summaryResponse, turnID); err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	if resultErr != nil {
		return rt.recordCompactionFailure(turnID, resultErr)
	}
	if summaryResponse.StopReason != StopReasonStop && summaryResponse.StopReason != StopReasonLength {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compaction model stopped with %s: %s", summaryResponse.StopReason, errText(summaryResponse)))
	}
	if summaryResponse.Text() == "" {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compaction model returned an empty summary"))
	}
	messageID, err := newMessageID()
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	checkpointID, err := newCheckpointID()
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
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
		return rt.recordCompactionFailure(turnID, err)
	}

	replacementWire := append([]wireMessageEnvelope{summaryWire}, contextWire[prefixEnd:]...)
	replacementMessages, err := runtimeMessageEnvelopes(durableRuntime{Context: replacementWire})
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	plainReplacement := make([]Message, 0, len(replacementMessages))
	for _, envelope := range replacementMessages {
		plainReplacement = append(plainReplacement, envelope.Message)
	}
	if err := validateMessageSequence(plainReplacement); err != nil {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: invalid compacted context: %w", err))
	}
	after, err := rt.measureContext(ctx, rt.provider, rt.droid.model, plainReplacement)
	if err != nil {
		return rt.recordCompactionFailure(turnID, err)
	}
	if after.EstimatedInput >= usage.EstimatedInput {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compaction did not reduce context"))
	}
	if sdkShouldCompact(after) {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compacted context remains above the target budget"))
	}
	if err := rt.provider.ValidateReplay(ctx, rt.droid.model, plainReplacement); err != nil {
		return rt.recordCompactionFailure(turnID, fmt.Errorf("droids: compacted context is not replayable: %w", err))
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || !sameContext(rt.state.Context, contextWire) {
		return fmt.Errorf("droids: active context changed during compaction")
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	rt.state.Context = replacementWire
	rt.state.CheckpointID = checkpointID
	checkpointPayload, err := json.Marshal(map[string]any{
		"checkpoint_id": checkpointID, "source_checkpoint_id": before.CheckpointID,
		"turn_id": turnID, "summary_message": summaryWire,
		"retained_from": envelopes[prefixEnd].ID, "before": usage, "after": after,
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
		"compaction_id": compactionID, "checkpoint_id": checkpointID, "before": usage.EstimatedInput, "after": after.EstimatedInput,
	})
	contextUpdated, _ := lifecycleEvent("context.updated", turnID, rt.state.AttemptID, map[string]any{
		"compaction_id": compactionID, "checkpoint_id": checkpointID, "estimated_input": after.EstimatedInput, "context_window": after.ContextWindow,
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

func (rt *sdkRuntime) measureContextWithConfiguration(
	ctx context.Context,
	provider Provider,
	model Model,
	reasoning string,
	reservedOutput int,
	messages []Message,
	configuration *runtimeRequestConfiguration,
) (ContextUsage, error) {
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
		usage.Model = cloneModel(model)
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

func compactionPrefixEnd(messages []Message) int {
	if len(messages) < 6 {
		return 0
	}
	target := len(messages) / 2
	for target < len(messages)-2 {
		if _, isResult := messages[target].(ToolResultMessage); isResult {
			target++
			continue
		}
		break
	}
	if target <= 0 || target >= len(messages)-1 {
		return 0
	}
	prefix := messages[:target]
	if validateMessageSequence(prefix) != nil {
		return 0
	}
	if validateMessageSequence(messages[target:]) != nil {
		return 0
	}
	return target
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
