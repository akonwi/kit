package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

func (rt *sdkRuntime) run(ctx context.Context, turnID TurnID, generation uint64) {
	defer func() {
		rt.mu.Lock()
		if rt.runGeneration == generation {
			rt.runCancel = nil
			rt.signalChangedLocked()
		}
		rt.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			rt.finishCanceledRun(turnID, err)
			return
		}
		continuedRetry, err := rt.continueRetry(ctx, turnID)
		if err != nil {
			rt.finishRunFailure(turnID, DroidErrorPersistence, err)
			return
		}
		if continuedRetry {
			continue
		}
		rt.mu.Lock()
		if rt.state.TurnID != turnID || rt.state.Status == ExecutionPaused || isTerminalStatus(rt.state.Status) {
			rt.mu.Unlock()
			return
		}
		pendingCalls, err := unresolvedToolCalls(rt.state)
		rt.mu.Unlock()
		if err != nil {
			rt.finishRunFailure(turnID, DroidErrorUnsafe, err)
			return
		}
		if len(pendingCalls) > 0 {
			results, terminate, err := rt.executeToolBatch(ctx, turnID, pendingCalls)
			if err != nil {
				if ctx.Err() != nil {
					rt.finishCanceledRun(turnID, ctx.Err())
				} else if rt.toolContinuationIsUnsafe() || errors.Is(err, ErrUnsafeContinuation) {
					rt.finishRunInterruption(turnID, err)
				} else {
					rt.finishRunFailure(turnID, DroidErrorTool, err)
				}
				return
			}
			if terminate && rt.tryFinishRunSuccess(turnID, nil, results) {
				return
			}
			continue
		}
		recovered, err := rt.recoverPersistedCycle(ctx, turnID)
		if err != nil {
			rt.finishRunFailure(turnID, DroidErrorPersistence, err)
			return
		}
		if recovered {
			continue
		}

		if err := rt.prepareModelBoundary(ctx, turnID); err != nil {
			kind := DroidErrorPersistence
			if errors.Is(err, ErrReactionLimit) {
				kind = DroidErrorLimit
			}
			rt.finishRunFailure(turnID, kind, err)
			return
		}

		if paused, err := rt.checkPauseOrBudget(ctx, turnID); err != nil {
			rt.finishRunFailure(turnID, DroidErrorPersistence, err)
			return
		} else if paused {
			return
		}

		if err := rt.compactIfNeeded(ctx, turnID, false); err != nil {
			rt.finishRunFailure(turnID, DroidErrorCompaction, err)
			return
		}
		started, err := rt.markModelCycleStarted(ctx, turnID)
		if err != nil {
			rt.finishRunFailure(turnID, DroidErrorPersistence, err)
			return
		}
		if !started {
			continue
		}

		envelope, err := rt.requestAssistant(ctx, turnID)
		if err != nil {
			if ctx.Err() != nil {
				rt.finishCanceledRun(turnID, ctx.Err())
			} else {
				rt.finishRunFailure(turnID, DroidErrorProvider, err)
			}
			return
		}
		message, ok := envelope.Message.(AssistantMessage)
		if !ok {
			rt.finishRunFailure(turnID, DroidErrorInternal, fmt.Errorf("droids: provider returned %T", envelope.Message))
			return
		}

		switch message.StopReason {
		case StopReasonContextWindow:
			if err := rt.persistObservedAssistant(ctx, envelope, false); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			if err := rt.compactIfNeeded(ctx, turnID, true); err != nil {
				rt.finishRunFailure(turnID, DroidErrorCompaction, err)
				return
			}
			if err := rt.startOverflowAttempt(ctx, turnID); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			continue
		case StopReasonError:
			if err := rt.persistObservedAssistant(ctx, envelope, false); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			retried, retryErr := rt.scheduleRetry(ctx, turnID, message)
			if retryErr != nil {
				if ctx.Err() != nil {
					rt.finishCanceledRun(turnID, ctx.Err())
				} else {
					rt.finishRunFailure(turnID, DroidErrorPersistence, retryErr)
				}
				return
			}
			if retried {
				continue
			}
			if ctx.Err() != nil {
				rt.finishCanceledRun(turnID, ctx.Err())
				return
			}
			rt.mu.Lock()
			status := rt.state.Status
			rt.mu.Unlock()
			if status == ExecutionPausing {
				continue
			}
			if status == ExecutionAborting {
				rt.finishCanceledRun(turnID, context.Canceled)
				return
			}
			rt.finishRunFailure(turnID, DroidErrorProvider, errors.New(errText(message)))
			return
		case StopReasonAborted:
			if err := rt.persistObservedAssistant(ctx, envelope, false); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			rt.finishCanceledRun(turnID, errors.New(errText(message)))
			return
		}

		if err := rt.persistObservedAssistant(ctx, envelope, true); err != nil {
			rt.finishRunFailure(turnID, DroidErrorPersistence, err)
			return
		}
		calls := message.ToolCalls()
		if message.StopReason != StopReasonToolUse && len(calls) > 0 {
			if err := rt.persistSyntheticToolResults(ctx, turnID, calls); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			continue
		}
		if message.StopReason == StopReasonToolUse && len(calls) > 0 {
			if err := rt.admitToolBatch(ctx, turnID, calls); err != nil {
				rt.finishRunFailure(turnID, DroidErrorPersistence, err)
				return
			}
			continue
		}

		if rt.tryFinishRunSuccess(turnID, &envelope, nil) {
			return
		}
	}
}

func (rt *sdkRuntime) prepareModelBoundary(ctx context.Context, turnID TurnID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		return nil
	}
	if len(rt.state.PendingSteering) == 0 && len(rt.state.PendingBoundaries) == 0 {
		return nil
	}
	subagentReaction := false
	for _, pending := range rt.state.PendingBoundaries {
		subagentReaction = subagentReaction || pending.Message.Kind == "subagent_result"
	}
	reactionLimited := subagentReaction && rt.state.AutonomousReactions >= maxAutonomousReactions
	if reactionLimited && len(rt.state.PendingSteering) == 0 {
		if rt.state.ReactionLimitDeferred {
			return nil
		}
		return ErrReactionLimit
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	if subagentReaction && !reactionLimited {
		rt.state.AutonomousReactions++
	}
	if reactionLimited && len(rt.state.PendingSteering) > 0 {
		rt.state.ReactionLimitDeferred = true
	}
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	var consumedBoundaryIDs []string
	for _, wire := range rt.state.PendingSteering {
		envelope, err := messageEnvelopeFromWire(wire)
		if err != nil {
			rt.state = before
			return err
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			rt.state = before
			return err
		}
		mutation, err := messageHistoryMutation(envelope)
		if err != nil {
			rt.state = before
			return err
		}
		mutations = append(mutations, mutation)
		event, _ := lifecycleEvent("steering.consumed", turnID, rt.state.AttemptID, map[string]any{"message_id": envelope.ID})
		events = append(events, event)
	}
	boundaries := rt.state.PendingBoundaries
	if reactionLimited {
		boundaries = nil
	}
	for _, pending := range boundaries {
		content, err := inputFromWire(pending.Message.Content)
		if err != nil {
			rt.state = before
			return err
		}
		prefix := fmt.Sprintf("[%s", pending.Message.Kind)
		if pending.Message.Source != "" {
			prefix += " from " + pending.Message.Source
		}
		prefix += "]"
		content = append([]InputContent{TextInput{Text: prefix}}, content...)
		message := ContextMessage{
			BoundaryID: pending.Message.ID,
			Kind:       pending.Message.Kind, Source: pending.Message.Source, Content: content,
			Details: append(json.RawMessage(nil), pending.Message.Details...),
		}
		messageID, err := newMessageID()
		if err != nil {
			rt.state = before
			return err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
			CreatedAt: time.Now().UTC(), Message: message,
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			rt.state = before
			return err
		}
		mutation, err := messageHistoryMutation(envelope)
		if err != nil {
			rt.state = before
			return err
		}
		mutations = append(mutations, mutation)
		for _, receiptID := range boundaryReceiptIDs(pending.Message) {
			consumption, err := boundaryConsumptionMutation(receiptID, turnID)
			if err != nil {
				rt.state = before
				return err
			}
			mutations = append(mutations, consumption)
			consumedBoundaryIDs = append(consumedBoundaryIDs, receiptID)
		}
		event, _ := lifecycleEvent("boundary.consumed", turnID, rt.state.AttemptID, map[string]any{"message_id": messageID, "kind": pending.Message.Kind})
		events = append(events, event)
	}
	rt.state.PendingSteering = nil
	if !reactionLimited {
		rt.state.PendingBoundaries = nil
	}
	rt.state.CyclePhase = cycleReady
	rt.state.TerminatePending = false
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	for _, id := range consumedBoundaryIDs {
		rt.boundaryConsumptions[id] = turnID
	}
	return nil
}

func (rt *sdkRuntime) recoverPersistedCycle(ctx context.Context, turnID TurnID) (bool, error) {
	rt.mu.Lock()
	phase := rt.state.CyclePhase
	var last MessageEnvelope
	if len(rt.state.Context) > 0 {
		decoded, err := messageEnvelopeFromWire(rt.state.Context[len(rt.state.Context)-1])
		if err != nil {
			rt.mu.Unlock()
			return false, err
		}
		last = decoded
	}
	rt.mu.Unlock()

	switch phase {
	case cycleReady:
		rt.mu.Lock()
		terminate := rt.state.TerminatePending
		rt.mu.Unlock()
		if !terminate {
			return false, nil
		}
		if rt.tryFinishRunSuccess(turnID, nil, nil) {
			return true, nil
		}
		return true, rt.prepareModelBoundary(ctx, turnID)
	case cycleToolsAdmitted:
		return false, nil
	case cycleModelStarted:
		rt.mu.Lock()
		before := rt.state
		rt.state.CyclePhase = cycleReady
		event, _ := lifecycleEvent("model_cycle.interrupted", turnID, rt.state.AttemptID, nil)
		err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event})
		if err != nil {
			rt.state = before
		}
		rt.mu.Unlock()
		return true, err
	case cycleSyntheticPending:
		assistant, ok := last.Message.(AssistantMessage)
		if !ok || len(assistant.ToolCalls()) == 0 {
			return false, fmt.Errorf("droids: synthetic-result phase has no tool calls")
		}
		return true, rt.persistSyntheticToolResults(ctx, turnID, assistant.ToolCalls())
	case cycleAssistantPersisted:
		assistant, ok := last.Message.(AssistantMessage)
		if !ok {
			return false, fmt.Errorf("droids: persisted assistant cycle has no assistant message")
		}
		calls := assistant.ToolCalls()
		if assistant.StopReason == StopReasonToolUse && len(calls) > 0 {
			return true, rt.admitToolBatch(ctx, turnID, calls)
		}
		if len(calls) > 0 {
			return true, rt.persistSyntheticToolResults(ctx, turnID, calls)
		}
		if rt.tryFinishRunSuccess(turnID, &last, nil) {
			return true, nil
		}
		return true, rt.prepareModelBoundary(ctx, turnID)
	default:
		return false, fmt.Errorf("droids: unsupported cycle phase %q", phase)
	}
}

func (rt *sdkRuntime) checkPauseOrBudget(ctx context.Context, turnID TurnID) (bool, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID {
		return true, nil
	}
	budget := rt.config.Execution.Budget.MaxModelCycles
	pause := rt.state.Status == ExecutionPausing || (budget > 0 && rt.state.ModelCycles >= budget)
	if !pause {
		return false, nil
	}
	if err := rt.pauseAtBoundaryLocked(ctx, turnID); err != nil {
		return false, err
	}
	return true, nil
}

func (rt *sdkRuntime) pauseAtBoundaryLocked(ctx context.Context, turnID TurnID) error {
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	checkpointID, err := newCheckpointID()
	if err != nil {
		return err
	}
	var mutations []EncodedMutation
	attemptWasOpen := rt.state.AttemptOpen
	if attemptWasOpen {
		attempt, err := attemptHistoryMutation(rt.state, ExecutionPaused)
		if err != nil {
			return err
		}
		mutations = append(mutations, attempt)
	}
	rt.state.Status = ExecutionPaused
	rt.state.AttemptOpen = false
	rt.state.CheckpointID = checkpointID
	if rt.state.Reason == "" {
		rt.state.Reason = "model cycle budget exhausted"
	}
	checkpoint, err := checkpointMutation(rt.state)
	if err != nil {
		rt.state = before
		return err
	}
	mutations = append(mutations, checkpoint)
	var events []EncodedDurableEvent
	if attemptWasOpen {
		attemptSettled, _ := lifecycleEvent("attempt.settled", turnID, rt.state.AttemptID, map[string]any{"status": ExecutionPaused})
		events = append(events, attemptSettled)
	}
	event, _ := lifecycleEvent("execution.paused", turnID, rt.state.AttemptID, map[string]any{"checkpoint_id": checkpointID, "reason": rt.state.Reason})
	events = append(events, event)
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

func checkpointMutation(state durableRuntime) (EncodedMutation, error) {
	payload, err := json.Marshal(map[string]any{
		"checkpoint_id": state.CheckpointID, "turn_id": state.TurnID,
		"attempt_id": state.AttemptID, "messages": state.Context,
		"model_cycles": state.ModelCycles, "reason": state.Reason,
	})
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: checkpointKind,
		RecordID: string(state.CheckpointID), Scope: RecordHistory,
		Version: recordVersion, Payload: payload,
	}, nil
}

func (rt *sdkRuntime) markModelCycleStarted(ctx context.Context, turnID TurnID) (bool, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || rt.state.Status != ExecutionRunning || rt.state.CyclePhase != cycleReady {
		return false, nil
	}
	before := rt.state
	rt.state.ModelCycles++
	rt.state.CyclePhase = cycleModelStarted
	event, _ := lifecycleEvent("model_cycle.started", turnID, rt.state.AttemptID, map[string]any{"cycle": rt.state.ModelCycles})
	if err := rt.commitLocked(ctx, nil, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return false, err
	}
	return true, nil
}

func (rt *sdkRuntime) requestAssistant(ctx context.Context, turnID TurnID) (MessageEnvelope, error) {
	rt.mu.Lock()
	messages, err := runtimeMessageEnvelopes(rt.state)
	attemptID := rt.state.AttemptID
	configuration := rt.currentRequestConfiguration()
	rt.mu.Unlock()
	if err != nil {
		return MessageEnvelope{}, err
	}
	plain := make([]Message, 0, len(messages))
	for _, envelope := range messages {
		plain = append(plain, envelope.Message)
	}
	requestMaxTokens := configuration.maxTokens
	if rt.droid.model.OutputLimitMode == OutputLimitProviderControlled {
		requestMaxTokens = 0
	}
	request := Request{
		SessionID: string(rt.conversation), SystemPrompt: configuration.systemPrompt, Messages: plain,
		Tools: append([]ToolSchema(nil), configuration.toolSchemas...), Reasoning: configuration.reasoning,
		MaxTokens: requestMaxTokens,
	}
	if err := rt.provider.ValidateReplay(ctx, rt.droid.model, plain); err != nil {
		return MessageEnvelope{}, err
	}
	messageID, err := newMessageID()
	if err != nil {
		return MessageEnvelope{}, err
	}
	stream, err := rt.provider.Stream(ctx, rt.droid.model, request)
	if err != nil {
		return MessageEnvelope{}, err
	}
	if assistantStreamIsNil(stream) {
		return MessageEnvelope{}, fmt.Errorf("droids: provider returned a nil stream")
	}
	defer stream.Close()
	started := false
	if err := consumeAssistantStream(ctx, stream, func(event StreamEvent) {
		switch value := event.(type) {
		case StreamStart:
			started = true
			value.Partial.ID = string(messageID)
			rt.publishTransient(turnID, attemptID, MessageStart{Message: value.Partial})
		case StreamDone, StreamError, StreamToolCallStart, StreamToolCallDelta, StreamToolCallEnd:
		default:
			if started {
				rt.publishTransient(turnID, attemptID, MessageDelta{MessageID: string(messageID), Stream: event})
			}
		}
	}); err != nil {
		return MessageEnvelope{}, err
	}
	final, resultErr := stream.Result()
	if resultErr != nil && final.StopReason != StopReasonError && final.StopReason != StopReasonContextWindow && final.StopReason != StopReasonAborted {
		final.Provider = rt.droid.model.Provider
		final.Model = rt.droid.model.ID
		final.StopReason = StopReasonError
		final.ErrorKind = ProviderProtocol
		final.ErrorMessage = boundedErrorText(resultErr)
		final.Error = &ProviderError{Kind: ProviderProtocol, Message: boundedErrorText(resultErr)}
	}
	final.ID = string(messageID)
	if final.Provider == "" {
		final.Provider = rt.droid.model.Provider
	}
	if final.Model == "" {
		final.Model = rt.droid.model.ID
	}
	if final.Timestamp == 0 {
		final.Timestamp = time.Now().UnixMilli()
	}
	usageErr := validateUsageTokens(final.Usage)
	if usageErr == nil {
		calculateCost(rt.droid.model, &final.Usage)
		usageErr = validateUsage(final.Usage)
	}
	normalizeProviderError(&final)
	if usageErr != nil {
		final.Usage = Usage{}
		setProtocolFailure(&final, rt.droid.model, usageErr)
	} else if validationErr := validateProviderTerminal(rt.droid.model, final); validationErr != nil {
		setProtocolFailure(&final, rt.droid.model, validationErr)
	} else if validationErr := validateAssistantToolCalls(final); validationErr != nil {
		setProtocolFailure(&final, rt.droid.model, validationErr)
	} else if canonicalErr := canonicalizeToolCalls(&final); canonicalErr != nil {
		setProtocolFailure(&final, rt.droid.model, canonicalErr)
	}
	if !started {
		rt.publishTransient(turnID, attemptID, MessageStart{Message: final})
	}
	if final.StopReason == StopReasonToolUse {
		for contentIndex, content := range final.Content {
			if call, ok := content.(ToolCall); ok {
				rt.publishTransient(turnID, attemptID, MessageDelta{
					MessageID: string(messageID),
					Stream:    StreamToolCallEnd{ContentIndex: contentIndex, ToolCall: call},
				})
			}
		}
	}
	rt.publishTransient(turnID, attemptID, MessageEnd{Message: final})
	return MessageEnvelope{
		ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
		CreatedAt: time.Now().UTC(), Message: final,
	}, nil
}

func assistantStreamIsNil(stream AssistantStream) bool {
	if stream == nil {
		return true
	}
	value := reflect.ValueOf(stream)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func consumeAssistantStream(ctx context.Context, stream AssistantStream, consume func(StreamEvent)) error {
	events := stream.Events()
	if events == nil {
		return fmt.Errorf("droids: provider returned a nil event channel")
	}
	for {
		select {
		case <-ctx.Done():
			_ = stream.Close()
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}
			consume(event)
		}
	}
}

func validateProviderTerminal(expected Model, message AssistantMessage) error {
	if message.Provider != expected.Provider || message.Model != expected.ID {
		return fmt.Errorf("provider/model identity does not match the request")
	}
	switch message.StopReason {
	case StopReasonStop, StopReasonLength, StopReasonToolUse, StopReasonContextWindow, StopReasonError, StopReasonAborted:
	default:
		return fmt.Errorf("unknown stop reason %q", message.StopReason)
	}
	if err := validateUsage(message.Usage); err != nil {
		return err
	}
	isFailure := message.StopReason == StopReasonError || message.StopReason == StopReasonContextWindow
	if isFailure && message.Error == nil {
		return fmt.Errorf("provider failure has no typed error")
	}
	if !isFailure && message.StopReason != StopReasonAborted && message.Error != nil {
		return fmt.Errorf("successful provider response contains an error")
	}
	if message.Error != nil {
		switch message.Error.Kind {
		case ProviderAuthentication, ProviderEntitlement, ProviderUsageLimit,
			ProviderRateLimit, ProviderTransport, ProviderContextWindow,
			ProviderInvalidRequest, ProviderProtocol, ProviderInternal:
		default:
			return fmt.Errorf("unknown provider error kind %q", message.Error.Kind)
		}
		if message.Error.RetryAfter < 0 {
			return fmt.Errorf("provider retry delay is negative")
		}
	}
	return nil
}

func setProtocolFailure(message *AssistantMessage, expected Model, _ error) {
	message.Provider = expected.Provider
	message.Model = expected.ID
	message.Content = nil
	message.StopReason = StopReasonError
	message.ErrorKind = ProviderProtocol
	message.ErrorMessage = safeProviderMessage(ProviderProtocol, "")
	message.Error = &ProviderError{Kind: ProviderProtocol, Message: message.ErrorMessage}
}

func normalizeProviderError(message *AssistantMessage) {
	message.ErrorMessage = boundedDiagnostic(message.ErrorMessage)
	if message.Error != nil {
		if message.ErrorKind == "" {
			message.ErrorKind = message.Error.Kind
		}
		if message.ErrorMessage == "" {
			message.ErrorMessage = boundedDiagnostic(message.Error.Message)
		}
		message.Error.Message = safeProviderMessage(message.Error.Kind, message.Error.Message)
		message.ErrorMessage = message.Error.Message
		return
	}
	kind := message.ErrorKind
	if message.StopReason == StopReasonContextWindow && kind == "" {
		kind = ProviderContextWindow
	}
	if kind == "" && message.StopReason == StopReasonError {
		kind = ProviderInternal
	}
	if kind == "" {
		return
	}
	message.ErrorMessage = safeProviderMessage(kind, message.ErrorMessage)
	message.Error = &ProviderError{
		Kind: kind, Message: message.ErrorMessage,
		Retryable: kind == ProviderRateLimit || kind == ProviderTransport || kind == ProviderInternal,
	}
}

func safeProviderMessage(kind ProviderErrorKind, fallback string) string {
	switch kind {
	case ProviderAuthentication:
		return "Provider authentication failed"
	case ProviderEntitlement:
		return "Provider access is not entitled for this model"
	case ProviderUsageLimit:
		return "Provider usage limit reached"
	case ProviderRateLimit:
		return "Provider request was rate limited"
	case ProviderContextWindow:
		return "Provider context window exceeded"
	case ProviderInvalidRequest:
		return "Provider rejected the request"
	case ProviderTransport:
		return "Provider transport failed"
	case ProviderInternal:
		return "Provider request failed"
	case ProviderProtocol:
		return "Provider response was invalid"
	default:
		return boundedDiagnostic(fallback)
	}
}

func canonicalizeToolCalls(message *AssistantMessage) error {
	for index, block := range message.Content {
		call, ok := block.(ToolCall)
		if !ok {
			continue
		}
		call.ProviderCallID = string(call.ID)
		if call.Name == "" {
			call.Name = "unknown"
		}
		id, err := newID("tool_")
		if err != nil {
			return err
		}
		call.ID = ToolCallID(id)
		message.Content[index] = call
	}
	return nil
}

func (rt *sdkRuntime) persistObservedAssistant(ctx context.Context, envelope MessageEnvelope, active bool) error {
	persistenceContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return rt.persistAssistant(persistenceContext, envelope, active)
}

func (rt *sdkRuntime) persistAssistant(ctx context.Context, envelope MessageEnvelope, active bool) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	message := envelope.Message.(AssistantMessage)
	if active {
		activeEnvelope := envelope
		if len(message.ToolCalls()) > 0 && message.StopReason != StopReasonToolUse {
			activeMessage := message
			activeMessage.StopReason = StopReasonToolUse
			activeEnvelope.Message = activeMessage
		}
		if err := appendRuntimeEnvelope(&rt.state, activeEnvelope); err != nil {
			return err
		}
		if len(message.ToolCalls()) > 0 && message.StopReason != StopReasonToolUse {
			rt.state.CyclePhase = cycleSyntheticPending
		} else {
			rt.state.CyclePhase = cycleAssistantPersisted
		}
	} else {
		rt.state.CyclePhase = cycleReady
	}
	usageMutation, usageEvent, usageChanged, err := applyUsageContribution(
		&rt.state, "assistant:"+string(envelope.ID), "assistant", message.Usage, true,
	)
	if err != nil {
		rt.state = before
		return err
	}
	mutation, err := messageHistoryMutation(envelope)
	if err != nil {
		rt.state = before
		return err
	}
	messageEvent, _ := lifecycleEvent("message.completed", rt.state.TurnID, rt.state.AttemptID, map[string]any{"message_id": envelope.ID, "role": RoleAssistant})
	cycleEvent, _ := lifecycleEvent("model_cycle.settled", rt.state.TurnID, rt.state.AttemptID, map[string]any{"stop_reason": message.StopReason})
	mutations := []EncodedMutation{mutation}
	events := []EncodedDurableEvent{messageEvent, cycleEvent}
	if usageChanged {
		mutations = append(mutations, usageMutation)
		events = append(events, usageEvent)
	}
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func (rt *sdkRuntime) startOverflowAttempt(ctx context.Context, turnID TurnID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || rt.state.Status != ExecutionRunning {
		return nil
	}
	before := rt.state
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	if rt.state.AttemptOpen {
		attempt, err := attemptHistoryMutation(rt.state, ExecutionFailed)
		if err != nil {
			return err
		}
		mutations = append(mutations, attempt)
		settled, _ := lifecycleEvent("attempt.settled", turnID, rt.state.AttemptID, map[string]any{"status": ExecutionFailed, "reason": "context_overflow"})
		events = append(events, settled)
	}
	attemptID, err := newAttemptID()
	if err != nil {
		return err
	}
	rt.state.AttemptID = attemptID
	rt.state.AttemptOpen = true
	rt.state.ModelCycles = 0
	rt.state.CyclePhase = cycleReady
	started, _ := lifecycleEvent("attempt.started", turnID, attemptID, map[string]any{"reason": "context_overflow"})
	events = append(events, started)
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func (rt *sdkRuntime) scheduleRetry(ctx context.Context, turnID TurnID, message AssistantMessage) (bool, error) {
	policy := *rt.config.Retry
	if !policy.Enabled || message.Error == nil || !message.Error.Retryable {
		return false, nil
	}
	rt.mu.Lock()
	if rt.state.TurnID != turnID || rt.state.RetryCount >= policy.MaxRetries {
		rt.mu.Unlock()
		return false, nil
	}
	before := rt.state
	attemptState := rt.state
	attemptState.Error = &durableDroidError{Kind: DroidErrorProvider, Message: safeProviderMessage(message.Error.Kind, message.Error.Message), Retryable: true}
	attempt, err := attemptHistoryMutation(attemptState, ExecutionFailed)
	if err != nil {
		rt.mu.Unlock()
		return false, err
	}
	rt.state.RetryCount++
	rt.state.Status = ExecutionRetrying
	rt.state.AttemptOpen = false
	delay := retryDelay(policy, rt.state.RetryCount)
	if policy.UseProviderDelay && message.Error.RetryAfter > 0 {
		delay = message.Error.RetryAfter
		if policy.MaxDelay > 0 && delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
	}
	retryAt := time.Now().UTC().Add(delay)
	rt.state.RetryAt = retryAt
	attemptSettled, _ := lifecycleEvent("attempt.settled", turnID, rt.state.AttemptID, map[string]any{"status": ExecutionFailed})
	event, _ := lifecycleEvent("attempt.retry_scheduled", turnID, rt.state.AttemptID, map[string]any{
		"retry": rt.state.RetryCount, "delay_ms": delay.Milliseconds(), "retry_at": retryAt,
	})
	if err := rt.commitLocked(context.WithoutCancel(ctx), []EncodedMutation{attempt}, []EncodedDurableEvent{attemptSettled, event}); err != nil {
		rt.state = before
		rt.mu.Unlock()
		return false, err
	}
	rt.mu.Unlock()

	return rt.continueRetry(ctx, turnID)
}

func (rt *sdkRuntime) continueRetry(ctx context.Context, turnID TurnID) (bool, error) {
	rt.mu.Lock()
	if rt.state.TurnID != turnID || rt.state.Status != ExecutionRetrying {
		rt.mu.Unlock()
		return false, nil
	}
	retryAt := rt.state.RetryAt
	rt.mu.Unlock()

	timer := time.NewTimer(max(time.Duration(0), time.Until(retryAt)))
	defer timer.Stop()
	for {
		rt.mu.Lock()
		status := rt.state.Status
		changed := rt.changed
		rt.mu.Unlock()
		if status != ExecutionRetrying {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-changed:
			continue
		case <-timer.C:
		}
		break
	}
	attemptID, err := newAttemptID()
	if err != nil {
		return false, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || rt.state.Status != ExecutionRetrying {
		return false, nil
	}
	before := rt.state
	rt.state.Status = ExecutionRunning
	rt.state.AttemptID = attemptID
	rt.state.AttemptOpen = true
	rt.state.ModelCycles = 0
	rt.state.RetryAt = time.Time{}
	event, _ := lifecycleEvent("attempt.started", turnID, attemptID, map[string]any{"retry": rt.state.RetryCount})
	if err := rt.commitLocked(context.WithoutCancel(ctx), nil, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return false, err
	}
	return true, nil
}

func retryDelay(policy RetryPolicy, retry int) time.Duration {
	delay := policy.BaseDelay
	for range max(0, retry-1) {
		if policy.MaxDelay > 0 && delay >= policy.MaxDelay/2 {
			return policy.MaxDelay
		}
		delay *= 2
	}
	if policy.MaxDelay > 0 && delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

func (rt *sdkRuntime) persistSyntheticToolResults(ctx context.Context, turnID TurnID, calls []ToolCall) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	mutations := make([]EncodedMutation, 0, len(calls))
	events := make([]EncodedDurableEvent, 0, len(calls))
	for _, call := range calls {
		result := toolResultMessage(call, toolErrorText("Tool call was not executed because the provider response ended before tool use completed"))
		messageID, err := newMessageID()
		if err != nil {
			rt.state = before
			return err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
			CreatedAt: time.Now().UTC(), Message: result,
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			rt.state = before
			return err
		}
		mutation, err := messageHistoryMutation(envelope)
		if err != nil {
			rt.state = before
			return err
		}
		mutations = append(mutations, mutation)
		event, _ := lifecycleEvent("tool.completed", turnID, rt.state.AttemptID, map[string]any{
			"tool_call_id": call.ID, "provider_call_id": call.ProviderCallID,
			"message_id": messageID, "is_error": true,
		})
		events = append(events, event)
	}
	rt.state.CyclePhase = cycleReady
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func (rt *sdkRuntime) admitToolBatch(ctx context.Context, turnID TurnID, calls []ToolCall) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	for _, call := range calls {
		if _, exists := rt.state.Tools[call.ID]; exists {
			continue
		}
		wire := wireContent{
			Type: "tool_call", ID: call.ID, ProviderCallID: call.ProviderCallID,
			Name: call.Name, Arguments: append([]byte(nil), call.Arguments...), Signature: call.Signature,
		}
		tool := durableTool{
			ID: call.ID, AdmissionAttemptID: rt.state.AttemptID,
			Call: wire, Phase: toolPhaseBeforeHook,
			RequiresBeforeHook: rt.config.BeforeToolCall != nil,
			RequiresAfterHook:  rt.config.AfterToolCall != nil,
		}
		definition, exists := rt.currentRequestConfiguration().toolsByName[call.Name]
		if !exists {
			tool.ValidationError = fmt.Sprintf("Tool %q not found", call.Name)
		} else if validationErr := definition.validate(call.Arguments); validationErr != nil {
			tool.ValidationError = boundedErrorText(validationErr)
		}
		rt.state.Tools[call.ID] = tool
		payload, err := json.Marshal(tool)
		if err != nil {
			rt.state = before
			return err
		}
		mutations = append(mutations, EncodedMutation{
			Operation: MutationAssertAbsent, RecordKind: toolRecordKind, RecordID: string(tool.ID),
			Scope: RecordRuntime, Version: recordVersion, Payload: payload,
		})
		event, _ := lifecycleEvent("tool.admitted", turnID, rt.state.AttemptID, map[string]any{"tool_call_id": tool.ID, "provider_call_id": call.ProviderCallID, "name": call.Name})
		hookPending, _ := lifecycleEvent("tool.hook_pending", turnID, rt.state.AttemptID, map[string]any{"tool_call_id": tool.ID, "phase": toolPhaseBeforeHook})
		events = append(events, event, hookPending)
	}
	if len(mutations) == 0 {
		return nil
	}
	rt.state.CyclePhase = cycleToolsAdmitted
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func unresolvedToolCalls(state durableRuntime) ([]ToolCall, error) {
	if len(state.Tools) == 0 || len(state.Context) == 0 {
		return nil, nil
	}
	last, err := messageEnvelopeFromWire(state.Context[len(state.Context)-1])
	if err != nil {
		return nil, err
	}
	assistant, ok := last.Message.(AssistantMessage)
	if !ok {
		return nil, nil
	}
	var calls []ToolCall
	for _, call := range assistant.ToolCalls() {
		if tool, exists := state.Tools[call.ID]; exists && tool.Phase != toolPhaseCompleted {
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		for _, call := range assistant.ToolCalls() {
			if _, exists := state.Tools[call.ID]; exists {
				calls = append(calls, call)
			}
		}
	}
	return calls, nil
}

func (rt *sdkRuntime) executeToolBatch(ctx context.Context, turnID TurnID, calls []ToolCall) ([]ToolResultMessage, bool, error) {
	sequential := rt.config.Execution.ToolExecution == ModeSequential
	for _, call := range calls {
		if tool, ok := rt.currentRequestConfiguration().toolsByName[call.Name]; ok && tool.mode() == ModeSequential {
			sequential = true
		}
	}
	results := make([]ToolResultMessage, len(calls))
	var firstErr error
	if sequential || len(calls) < 2 {
		for index, call := range calls {
			result, err := rt.processTool(ctx, turnID, call)
			results[index] = result
			if err != nil {
				firstErr = err
				break
			}
		}
	} else {
		execute := make([]bool, len(calls))
		prepared := make([]int, 0, len(calls))
		for index, call := range calls {
			shouldExecute, completed, err := rt.prepareTool(ctx, turnID, call)
			if err != nil {
				firstErr = err
				break
			}
			if completed != nil {
				results[index] = *completed
				continue
			}
			execute[index] = shouldExecute
			prepared = append(prepared, index)
		}
		if firstErr == nil {
			workers := min(len(prepared), rt.config.Execution.MaxParallelTools)
			slots := make(chan struct{}, workers)
			var wg sync.WaitGroup
			var errMu sync.Mutex
		feed:
			for _, index := range prepared {
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					break feed
				}
				errMu.Lock()
				stop := firstErr != nil
				errMu.Unlock()
				if stop {
					<-slots
					break feed
				}
				started := false
				if execute[index] {
					if err := rt.startToolExecution(ctx, turnID, calls[index]); err != nil {
						<-slots
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
						break feed
					}
					started = true
				}
				wg.Add(1)
				go func(index int, started bool) {
					defer wg.Done()
					defer func() { <-slots }()
					result, err := rt.finishPreparedTool(ctx, turnID, calls[index], execute[index], started)
					results[index] = result
					if err != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
					}
				}(index, started)
			}
			wg.Wait()
		}
	}
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	if firstErr != nil {
		return nil, false, firstErr
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return nil, false, err
	}
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	terminate := false
	for index, call := range calls {
		result := results[index]
		messageID, err := newMessageID()
		if err != nil {
			rt.state = before
			return nil, false, err
		}
		envelope := MessageEnvelope{
			ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
			CreatedAt: time.Now().UTC(), Message: result,
		}
		if err := appendRuntimeEnvelope(&rt.state, envelope); err != nil {
			rt.state = before
			return nil, false, err
		}
		messageMutation, err := messageHistoryMutation(envelope)
		if err != nil {
			rt.state = before
			return nil, false, err
		}
		mutations = append(mutations, messageMutation)
		tool := rt.state.Tools[call.ID]
		tool.Phase = toolPhaseCompleted
		final, err := messageToWire(result)
		if err != nil {
			rt.state = before
			return nil, false, err
		}
		tool.Final = &final
		payload, _ := json.Marshal(tool)
		mutations = append(mutations, EncodedMutation{
			Operation: MutationPut, RecordKind: toolRecordKind, RecordID: string(tool.ID),
			Scope: RecordHistory, Version: recordVersion, Payload: payload,
		})
		delete(rt.state.Tools, call.ID)
		event, _ := lifecycleEvent("tool.completed", turnID, rt.state.AttemptID, map[string]any{"tool_call_id": tool.ID, "provider_call_id": call.ProviderCallID, "message_id": messageID, "is_error": result.IsError})
		events = append(events, event)
		terminate = terminate || result.Terminate
	}
	rt.state.CyclePhase = cycleReady
	rt.state.TerminatePending = terminate
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return nil, false, err
	}
	return results, terminate, nil
}

func (rt *sdkRuntime) processTool(ctx context.Context, turnID TurnID, call ToolCall) (ToolResultMessage, error) {
	execute, completed, err := rt.prepareTool(ctx, turnID, call)
	if err != nil {
		return ToolResultMessage{}, err
	}
	if completed != nil {
		return *completed, nil
	}
	return rt.finishPreparedTool(ctx, turnID, call, execute, false)
}

func (rt *sdkRuntime) prepareTool(ctx context.Context, turnID TurnID, call ToolCall) (bool, *ToolResultMessage, error) {
	for {
		rt.mu.Lock()
		tool, exists := rt.state.Tools[call.ID]
		rt.mu.Unlock()
		if !exists {
			return false, nil, fmt.Errorf("droids: tool call %q is not admitted", call.ID)
		}
		toolContext := ToolContext{
			ConversationID: rt.conversation, TurnID: turnID,
			AttemptID: tool.AdmissionAttemptID, ToolCallID: tool.ID,
		}
		switch tool.Phase {
		case toolPhaseBeforeHook:
			if tool.ValidationError != "" {
				result, err := rt.completeToolWithoutExecution(ctx, call, toolErrorText(tool.ValidationError))
				return false, &result, err
			}
			if !tool.RequiresBeforeHook {
				if err := rt.updateToolPhase(ctx, call.ID, toolPhaseReady, nil, nil, "tool.hook_completed"); err != nil {
					return false, nil, err
				}
				continue
			}
			if rt.config.BeforeToolCall == nil {
				return false, nil, errors.Join(ErrUnsafeContinuation, fmt.Errorf("droids: required before-tool hook is unavailable"))
			}
			decision, err := rt.config.BeforeToolCall(ctx, toolContext, call)
			if err != nil {
				if ctx.Err() != nil {
					return false, nil, ctx.Err()
				}
				result, completeErr := rt.completeToolWithoutExecution(ctx, call, toolErrorText(err.Error()))
				return false, &result, completeErr
			}
			switch {
			case decision.Reject:
				reason := decision.Reason
				if reason == "" {
					reason = "Tool execution was rejected"
				}
				result, completeErr := rt.completeToolWithoutExecution(ctx, call, toolErrorText(reason))
				return false, &result, completeErr
			case decision.Result != nil:
				result, completeErr := rt.completeToolWithoutExecution(ctx, call, *decision.Result)
				return false, &result, completeErr
			default:
				if err := rt.updateToolPhase(ctx, call.ID, toolPhaseReady, nil, nil, "tool.hook_completed"); err != nil {
					return false, nil, err
				}
			}
		case toolPhaseReady:
			return true, nil, nil
		case toolPhaseExecuting:
			if tool.RawResult == nil {
				return false, nil, ErrUnsafeContinuation
			}
			return false, nil, nil
		case toolPhaseAfterHook:
			return false, nil, nil
		case toolPhaseCompleted:
			if tool.Final == nil {
				return false, nil, fmt.Errorf("droids: completed tool %q has no result", call.ID)
			}
			message, err := messageFromWire(*tool.Final)
			if err != nil {
				return false, nil, err
			}
			result, ok := message.(ToolResultMessage)
			if !ok {
				return false, nil, fmt.Errorf("droids: final tool record %q is not a tool result", tool.ID)
			}
			return false, &result, nil
		default:
			return false, nil, fmt.Errorf("droids: unsupported tool phase %q", tool.Phase)
		}
	}
}

func (rt *sdkRuntime) finishPreparedTool(ctx context.Context, turnID TurnID, call ToolCall, execute, started bool) (ToolResultMessage, error) {
	if execute {
		if !started {
			if err := rt.startToolExecution(ctx, turnID, call); err != nil {
				return ToolResultMessage{}, err
			}
		}
		rt.mu.Lock()
		tool := rt.state.Tools[call.ID]
		rt.mu.Unlock()
		toolContext := ToolContext{
			ConversationID: rt.conversation, TurnID: turnID,
			AttemptID: tool.AdmissionAttemptID, ToolCallID: tool.ID,
		}
		result := rt.invokeTool(ctx, toolContext, call)
		_, wire, omitted, err := validatedToolResultMessage(call, result)
		if err != nil {
			return ToolResultMessage{}, err
		}
		if err := rt.updateToolPhase(ctx, call.ID, toolPhaseAfterHook, &wire, nil, "tool.raw_result"); err != nil {
			return ToolResultMessage{}, err
		}
		if omitted {
			_ = rt.recordToolDetailsOmitted(context.WithoutCancel(ctx), call.ID)
		}
	}

	for {
		rt.mu.Lock()
		tool, exists := rt.state.Tools[call.ID]
		rt.mu.Unlock()
		if !exists {
			return ToolResultMessage{}, fmt.Errorf("droids: tool call %q is not admitted", call.ID)
		}
		toolContext := ToolContext{
			ConversationID: rt.conversation, TurnID: turnID,
			AttemptID: tool.AdmissionAttemptID, ToolCallID: tool.ID,
		}
		switch tool.Phase {
		case toolPhaseExecuting:
			if tool.RawResult == nil {
				return ToolResultMessage{}, ErrUnsafeContinuation
			}
			if err := rt.updateToolPhase(ctx, call.ID, toolPhaseAfterHook, tool.RawResult, nil, "tool.raw_result"); err != nil {
				return ToolResultMessage{}, err
			}
		case toolPhaseAfterHook:
			if tool.RawResult == nil {
				return ToolResultMessage{}, ErrUnsafeContinuation
			}
			rawMessage, err := messageFromWire(*tool.RawResult)
			if err != nil {
				return ToolResultMessage{}, err
			}
			result, ok := rawMessage.(ToolResultMessage)
			if !ok {
				return ToolResultMessage{}, fmt.Errorf("droids: raw tool record %q is not a tool result", tool.ID)
			}
			if tool.RequiresAfterHook && rt.config.AfterToolCall == nil {
				return ToolResultMessage{}, errors.Join(ErrUnsafeContinuation, fmt.Errorf("droids: required after-tool hook is unavailable"))
			}
			if tool.RequiresAfterHook {
				replacement, hookErr := rt.config.AfterToolCall(ctx, toolContext, toolResultFromMessage(result))
				if hookErr != nil {
					if ctx.Err() != nil {
						return ToolResultMessage{}, ctx.Err()
					}
					result = toolResultMessage(call, toolErrorText(hookErr.Error()))
				} else if replacement != nil {
					result = toolResultMessage(call, *replacement)
				}
			}
			result, wire, omitted, err := validatedToolResultMessage(call, toolResultFromMessage(result))
			if err != nil {
				return ToolResultMessage{}, err
			}
			if err := rt.updateToolPhase(ctx, call.ID, toolPhaseCompleted, tool.RawResult, &wire, "tool.hook_completed"); err != nil {
				return ToolResultMessage{}, err
			}
			if omitted {
				_ = rt.recordToolDetailsOmitted(context.WithoutCancel(ctx), call.ID)
			}
			rt.publishToolExecutionEnd(call, result)
			return result, nil
		case toolPhaseCompleted:
			if tool.Final == nil {
				return ToolResultMessage{}, fmt.Errorf("droids: completed tool %q has no result", call.ID)
			}
			message, err := messageFromWire(*tool.Final)
			if err != nil {
				return ToolResultMessage{}, err
			}
			result, ok := message.(ToolResultMessage)
			if !ok {
				return ToolResultMessage{}, fmt.Errorf("droids: final tool record %q is not a tool result", tool.ID)
			}
			return result, nil
		default:
			return ToolResultMessage{}, fmt.Errorf("droids: tool %q is not ready to finish from phase %q", call.ID, tool.Phase)
		}
	}
}

func (rt *sdkRuntime) completeToolWithoutExecution(ctx context.Context, call ToolCall, result ToolResult) (ToolResultMessage, error) {
	message, wire, omitted, err := validatedToolResultMessage(call, result)
	if err != nil {
		return ToolResultMessage{}, err
	}
	if err := rt.updateToolPhase(ctx, call.ID, toolPhaseCompleted, &wire, &wire, "tool.hook_completed"); err != nil {
		return ToolResultMessage{}, err
	}
	if omitted {
		_ = rt.recordToolDetailsOmitted(context.WithoutCancel(ctx), call.ID)
	}
	rt.publishToolExecutionEnd(call, message)
	return message, nil
}

func (rt *sdkRuntime) startToolExecution(ctx context.Context, turnID TurnID, call ToolCall) error {
	if err := rt.updateToolPhase(ctx, call.ID, toolPhaseExecuting, nil, nil, "tool.started"); err != nil {
		return err
	}
	rt.mu.Lock()
	tool, exists := rt.state.Tools[call.ID]
	rt.mu.Unlock()
	if exists {
		rt.publishTransient(turnID, tool.AdmissionAttemptID, ToolExecutionStart{
			ToolCallID: call.ID, ToolName: call.Name, Arguments: append([]byte(nil), call.Arguments...),
		})
	}
	return nil
}

func (rt *sdkRuntime) publishToolExecutionEnd(call ToolCall, result ToolResultMessage) {
	rt.mu.Lock()
	tool, exists := rt.state.Tools[call.ID]
	turnID := rt.state.TurnID
	rt.mu.Unlock()
	if !exists {
		return
	}
	rt.publishTransient(turnID, tool.AdmissionAttemptID, ToolExecutionEnd{
		ToolCallID: call.ID, ToolName: call.Name, Result: toolResultFromMessage(result), IsError: result.IsError,
	})
}

func (rt *sdkRuntime) recordToolDetailsOmitted(ctx context.Context, callID ToolCallID) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	tool, exists := rt.state.Tools[callID]
	if !exists {
		return fmt.Errorf("droids: tool call %q is not admitted", callID)
	}
	event, _ := lifecycleEvent("tool.details_omitted", rt.state.TurnID, tool.AdmissionAttemptID, map[string]any{"tool_call_id": tool.ID})
	return rt.commitLocked(ctx, nil, []EncodedDurableEvent{event})
}

func (rt *sdkRuntime) updateToolPhase(
	ctx context.Context,
	callID ToolCallID,
	phase toolPhase,
	rawResult, final *wireMessage,
	eventKind string,
) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	tool, exists := rt.state.Tools[callID]
	if !exists {
		return fmt.Errorf("droids: tool call %q is not admitted", callID)
	}
	tool.Phase = phase
	if rawResult != nil {
		copy := *rawResult
		tool.RawResult = &copy
	}
	if final != nil {
		copy := *final
		tool.Final = &copy
	}
	rt.state.Tools[callID] = tool
	payload, err := json.Marshal(tool)
	if err != nil {
		rt.state = before
		return err
	}
	mutation := EncodedMutation{
		Operation: MutationPut, RecordKind: toolRecordKind, RecordID: string(tool.ID),
		Scope: RecordRuntime, Version: recordVersion, Payload: payload,
	}
	event, _ := lifecycleEvent(eventKind, rt.state.TurnID, tool.AdmissionAttemptID, map[string]any{"tool_call_id": tool.ID, "provider_call_id": tool.Call.ProviderCallID, "phase": phase})
	if err := rt.commitLocked(ctx, []EncodedMutation{mutation}, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func (rt *sdkRuntime) invokeTool(ctx context.Context, toolContext ToolContext, call ToolCall) ToolResult {
	tool, ok := rt.currentRequestConfiguration().toolsByName[call.Name]
	if !ok {
		return toolErrorText(fmt.Sprintf("Tool %q not found", call.Name))
	}
	var updateMu sync.Mutex
	updatesOpen := true
	result, err := tool.execute(ctx, toolContext, call.Arguments, func(delta ToolResultDelta) {
		updateMu.Lock()
		defer updateMu.Unlock()
		if !updatesOpen {
			return
		}
		delta.Content = append([]ResultContent(nil), delta.Content...)
		rt.publishTransient(toolContext.TurnID, toolContext.AttemptID, ToolExecutionUpdate{ToolCallID: toolContext.ToolCallID, ToolName: call.Name, Delta: delta})
	})
	updateMu.Lock()
	updatesOpen = false
	updateMu.Unlock()
	if err != nil {
		return toolErrorText(err.Error())
	}
	return result
}

func validatedToolResultMessage(call ToolCall, result ToolResult) (ToolResultMessage, wireMessage, bool, error) {
	message := toolResultMessage(call, result)
	wire, err := messageToWire(message)
	if err == nil {
		return message, wire, false, nil
	}
	if len(message.Details) > 0 {
		message.Details = nil
		wire, detailsErr := messageToWire(message)
		if detailsErr == nil {
			return message, wire, true, nil
		}
	}
	message = toolResultMessage(call, toolErrorText("Tool returned an invalid result: "+boundedErrorText(err)))
	wire, encodeErr := messageToWire(message)
	return message, wire, false, encodeErr
}

func toolResultMessage(call ToolCall, result ToolResult) ToolResultMessage {
	return ToolResultMessage{
		ToolCallID: call.ID, ProviderCallID: call.ProviderCallID,
		ToolName: call.Name, Content: result.Content,
		Details: result.Details, IsError: result.IsError, Terminate: result.Terminate,
		Timestamp: time.Now().UnixMilli(),
	}
}

func toolResultFromMessage(message ToolResultMessage) ToolResult {
	return ToolResult{
		Content: message.Content, Details: message.Details,
		IsError: message.IsError, Terminate: message.Terminate,
	}
}

func (rt *sdkRuntime) publishTransient(turnID TurnID, attemptID AttemptID, event Event) {
	rt.publish(EventEnvelope{
		OccurredAt: time.Now().UTC(), ConversationID: rt.conversation,
		TurnID: turnID, AttemptID: attemptID, Event: event,
	})
}

func (rt *sdkRuntime) tryFinishRunSuccess(turnID TurnID, final *MessageEnvelope, _ []ToolResultMessage) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		return true
	}
	if rt.closed && !rt.state.AbortRequested {
		failure := &DroidError{Kind: DroidErrorInternal, Message: "droid shut down during execution"}
		if err := rt.interruptLocked(context.Background(), failure); err != nil {
			rt.workerSettlementFailedLocked(err)
		}
		return true
	}
	if rt.state.Status == ExecutionAborting || rt.state.AbortRequested {
		if err := rt.settleLocked(context.Background(), ExecutionAborted, nil); err != nil {
			rt.workerSettlementFailedLocked(err)
		}
		return true
	}
	if rt.state.Status == ExecutionPausing {
		if err := rt.pauseAtBoundaryLocked(context.Background(), turnID); err != nil {
			rt.workerSettlementFailedLocked(err)
		}
		return true
	}
	if len(rt.state.PendingSteering) > 0 || (len(rt.state.PendingBoundaries) > 0 && !rt.state.ReactionLimitDeferred) {
		return false
	}
	if final != nil {
		wire, err := messageEnvelopeToWire(*final)
		if err != nil {
			rt.workerSettlementFailedLocked(err)
			return true
		}
		rt.state.Final = &wire
	}
	if err := rt.settleLocked(context.Background(), ExecutionCompleted, nil); err != nil {
		rt.workerSettlementFailedLocked(err)
	}
	return true
}

func (rt *sdkRuntime) finishRunFailure(turnID TurnID, kind DroidErrorKind, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		return
	}
	if rt.closed && !rt.state.AbortRequested {
		failure := &DroidError{Kind: DroidErrorInternal, Message: "droid shut down during execution"}
		if interruptErr := rt.interruptLocked(context.Background(), failure); interruptErr != nil {
			rt.workerSettlementFailedLocked(interruptErr)
		}
		return
	}
	if rt.state.Status == ExecutionAborting || rt.state.AbortRequested {
		if settleErr := rt.settleLocked(context.Background(), ExecutionAborted, nil); settleErr != nil {
			rt.workerSettlementFailedLocked(settleErr)
		}
		return
	}
	if rt.state.Status == ExecutionPausing {
		if pauseErr := rt.pauseAtBoundaryLocked(context.Background(), turnID); pauseErr != nil {
			rt.workerSettlementFailedLocked(pauseErr)
		}
		return
	}
	failure := &DroidError{Kind: kind, Message: safeRuntimeError(kind, err), Cause: err}
	if settleErr := rt.settleLocked(context.Background(), ExecutionFailed, failure); settleErr != nil {
		rt.workerSettlementFailedLocked(errors.Join(err, settleErr))
	}
}

func (rt *sdkRuntime) toolContinuationIsUnsafe() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, tool := range rt.state.Tools {
		if tool.Phase == toolPhaseExecuting && tool.RawResult == nil {
			return true
		}
	}
	return false
}

func (rt *sdkRuntime) finishRunInterruption(turnID TurnID, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		return
	}
	if rt.state.Status == ExecutionAborting || rt.state.AbortRequested {
		if settleErr := rt.settleLocked(context.Background(), ExecutionAborted, nil); settleErr != nil {
			rt.workerSettlementFailedLocked(settleErr)
		}
		return
	}
	failure := &DroidError{Kind: DroidErrorUnsafe, Message: safeRuntimeError(DroidErrorUnsafe, err), Cause: err}
	if interruptErr := rt.interruptLocked(context.Background(), failure); interruptErr != nil {
		rt.workerSettlementFailedLocked(errors.Join(err, interruptErr))
	}
}

func (rt *sdkRuntime) finishCanceledRun(turnID TurnID, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state.TurnID != turnID || isTerminalStatus(rt.state.Status) {
		return
	}
	if rt.state.AbortRequested || rt.state.Status == ExecutionAborting {
		if settleErr := rt.settleLocked(context.Background(), ExecutionAborted, nil); settleErr != nil {
			rt.workerSettlementFailedLocked(settleErr)
		}
		return
	}
	failure := &DroidError{Kind: DroidErrorInternal, Message: safeRuntimeError(DroidErrorInternal, err), Cause: err}
	if interruptErr := rt.interruptLocked(context.Background(), failure); interruptErr != nil {
		rt.workerSettlementFailedLocked(errors.Join(err, interruptErr))
	}
}

func (rt *sdkRuntime) workerSettlementFailedLocked(err error) {
	failure := &DroidError{Kind: DroidErrorPersistence, Message: safeRuntimeError(DroidErrorPersistence, err), Cause: err}
	rt.state.Status = ExecutionInterrupted
	rt.state.Error = durableError(failure)
	rt.state.Reason = failure.Message
	rt.persistenceErr = errors.Join(rt.persistenceErr, err)
	rt.releaseRunLocked()
	if rt.handle != nil {
		rt.handle.completeWithError(outcomeFromState(rt.conversation, rt.state), err)
	}
}

func isTerminalStatus(status ExecutionStatus) bool {
	switch status {
	case ExecutionCompleted, ExecutionFailed, ExecutionAborted:
		return true
	default:
		return false
	}
}
