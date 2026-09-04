package droids

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// loop.go — the bounded multi-step tool loop (Layer 2). Consumes the queue
// filled by Send/Run, streams assistant turns from the provider, executes tool
// calls, and repeats until the model stops, MaxSteps is hit, or the run is
// aborted. Steering messages are drained between turns of normal runs (and
// retained during continuations); each queued prompt is its own run.

func (d *Droid) worker() {
	for {
		select {
		case <-d.closed:
			d.shutdownWorker()
			return
		case qp := <-d.queue:
			// Reject work dequeued after Close won the earlier race, so a
			// post-close prompt never mutates the transcript.
			select {
			case <-d.closed:
				qp.complete(runResult{err: fmt.Errorf("droids: droid closed")})
				d.shutdownWorker()
				return
			default:
			}

			if qp.stream != nil {
				d.setActiveRun(qp.stream)
			}
			result := d.runPrompt(qp)
			if qp.stream != nil {
				d.setActiveRun(nil)
			}
			qp.complete(result)
		}
	}
}

// shutdownWorker completes queued per-run streams/results, then closes the
// long-lived session event stream. Work racing concurrently with Close may
// enqueue successfully, but never executes.
func (d *Droid) shutdownWorker() {
	// Holding queueMu while marking admission closed and draining guarantees
	// no successful enqueue can land after the final empty check.
	d.queueMu.Lock()
	d.accepting = false
	closed := runResult{err: fmt.Errorf("droids: droid closed")}
	for {
		select {
		case qp := <-d.queue:
			qp.complete(closed)
		default:
			d.queueMu.Unlock()
			d.finishEvents()
			return
		}
	}
}

// finishEvents closes the event channel exactly once on shutdown. The channel
// is kept (not niled) so Events() keeps returning the same, now-closed channel.
func (d *Droid) finishEvents() {
	d.eventsMu.Lock()
	if !d.eventsClosed {
		d.eventsClosed = true
		if d.events != nil {
			close(d.events)
		}
	}
	d.eventsMu.Unlock()
}

// runPrompt executes one full agent run for the given prompt. The run context
// derives from the caller's ctx (Send/Run), so cancelling it aborts provider
// calls and tool execution; Abort and Close cancel it too.
func (d *Droid) runPrompt(qp queuedPrompt) runResult {
	base := qp.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	d.mu.Lock()
	d.cancelRun = cancel
	d.mu.Unlock()

	// Cancel the run when the droid is closed. The watcher exits when the run
	// finishes (stop) or the droid closes.
	stop := make(chan struct{})
	go func() {
		select {
		case <-d.closed:
			cancel()
		case <-stop:
		}
	}()

	defer func() {
		d.mu.Lock()
		d.cancelRun = nil
		d.mu.Unlock()
		cancel()
		close(stop)
	}()

	seed := qp.messages

	// If the prompt was cancelled while queued (caller ctx ended, or the droid
	// closed), drop it without emitting events or mutating the transcript.
	if err := ctx.Err(); err != nil {
		return runResult{
			message: AssistantMessage{
				Provider:     d.model.Provider,
				Model:        d.model.ID,
				StopReason:   StopReasonAborted,
				ErrorMessage: err.Error(),
				Timestamp:    time.Now().UnixMilli(),
			},
			err: err,
		}
	}

	// Validate at execution time rather than admission time: a continuation may
	// wait behind another run that changes the transcript before it starts.
	if qp.continuation {
		if err := d.validateContinuation(); err != nil {
			return runResult{err: err}
		}
	}

	d.emit(AgentStart{})

	// Append and announce the seed prompt(s).
	for _, m := range seed {
		d.appendMessage(ctx, m)
		d.emit(MessageStart{Message: m})
		d.emit(MessageEnd{Message: m})
	}

	var last AssistantMessage
	for step := 0; step < d.opts.MaxSteps; step++ {
		// Stop promptly if the run was cancelled (caller ctx, Abort, or Close)
		// rather than starting another turn.
		if err := ctx.Err(); err != nil {
			aborted := AssistantMessage{
				Provider:     d.model.Provider,
				Model:        d.model.ID,
				StopReason:   StopReasonAborted,
				ErrorMessage: err.Error(),
				Timestamp:    time.Now().UnixMilli(),
			}
			d.emit(ErrorEvent{Message: aborted, Aborted: true})
			d.emit(AgentEnd{Messages: d.snapshot()})
			return runResult{message: aborted, err: err}
		}

		// At run start, pending steering is part of the prospective first request
		// and must be included in compaction accounting. Like seed input, announce
		// it before the first assistant turn. Continuations retain steering so
		// their exact tool-result tail is unchanged.
		if step == 0 && !qp.continuation {
			for _, m := range d.drainSteering() {
				d.appendMessage(ctx, m)
				d.emit(MessageStart{Message: m})
				d.emit(MessageEnd{Message: m})
			}
			if _, err := d.compact(ctx, CompactionThreshold, false); err != nil {
				failed := compactionErrorMessage(d.model, err)
				d.emit(ErrorEvent{Message: failed})
				d.emit(AgentEnd{Messages: d.snapshot()})
				return runResult{message: failed, err: err}
			}
		}

		d.emit(TurnStart{})

		// Steering that arrives during a multi-step run is picked up between
		// later turns. The first turn drained it above for compaction accounting.
		if step > 0 && !qp.continuation {
			for _, m := range d.drainSteering() {
				d.appendMessage(ctx, m)
				d.emit(MessageStart{Message: m})
				d.emit(MessageEnd{Message: m})
			}
		}

		msg := d.streamTurn(ctx)
		last = msg
		overflowTurnEnded := false

		// A provider-confirmed overflow gets one compaction-and-retry attempt on
		// the first provider step of a normal run. Close the rejected attempt's
		// turn before signaling compaction; a successful retry starts a new turn.
		if step == 0 && !qp.continuation && msg.StopReason == StopReasonContextWindow && d.opts.Compact != nil {
			d.emit(TurnEnd{Message: msg})
			overflowTurnEnded = true
			applied, err := d.compact(ctx, CompactionOverflow, true)
			if err != nil {
				failed := compactionErrorMessage(d.model, err)
				d.emit(ErrorEvent{Message: failed})
				d.emit(AgentEnd{Messages: d.snapshot()})
				return runResult{message: failed, err: err}
			}
			if applied {
				d.emit(TurnStart{})
				msg = d.streamTurn(ctx)
				last = msg
				overflowTurnEnded = false
			}
		}

		if msg.StopReason == StopReasonError || msg.StopReason == StopReasonContextWindow || msg.StopReason == StopReasonAborted {
			if !overflowTurnEnded {
				d.emit(TurnEnd{Message: msg})
			}
			d.emit(ErrorEvent{Message: msg, Aborted: msg.StopReason == StopReasonAborted})
			d.emit(AgentEnd{Messages: d.snapshot()})
			// Preserve cancellation error identity (errors.Is) when the abort
			// was caused by the run context ending.
			if msg.StopReason == StopReasonAborted && ctx.Err() != nil {
				return runResult{message: msg, err: ctx.Err()}
			}
			return runResult{message: msg, err: fmt.Errorf("%s", errText(msg))}
		}

		calls := msg.ToolCalls()
		// Only an explicit tool-use stop authorizes execution. A provider may
		// include a partial tool call in a length-limited response; executing it
		// would run truncated or unintended arguments.
		if msg.StopReason != StopReasonToolUse || len(calls) == 0 {
			d.emit(TurnEnd{Message: msg})
			d.emit(AgentEnd{Messages: d.snapshot()})
			return runResult{message: msg}
		}

		results := d.executeTools(ctx, calls)
		d.emit(TurnEnd{Message: msg, ToolResults: results})
	}

	d.emit(AgentEnd{Messages: d.snapshot()})
	return runResult{message: last, err: fmt.Errorf("droids: reached MaxSteps (%d)", d.opts.MaxSteps)}
}

// streamTurn runs one provider stream and returns the final assistant message,
// emitting deltas and persisting the result.
func (d *Droid) streamTurn(ctx context.Context) AssistantMessage {
	requestMaxTokens := d.maxTokens
	if d.model.OutputLimitMode == OutputLimitProviderControlled {
		requestMaxTokens = 0
	}
	req := Request{
		SystemPrompt: d.opts.SystemPrompt,
		Messages:     d.snapshot(),
		Tools:        d.providerToolSchemas(),
		Reasoning:    d.opts.Reasoning,
		MaxTokens:    requestMaxTokens,
	}

	stream := d.providers.Stream(ctx, d.model, req)

	started := false
	for ev := range stream.Events() {
		switch e := ev.(type) {
		case StreamStart:
			started = true
			d.emit(MessageStart{Message: e.Partial})
		case StreamDone, StreamError:
			// handled after loop via Result()
		default:
			if started {
				d.emit(MessageDelta{Stream: ev})
			}
		}
	}

	final := stream.Result()
	calculateCost(d.model, &final.Usage)
	if final.Provider == "" {
		final.Provider = d.model.Provider
	}
	if final.Model == "" {
		final.Model = d.model.ID
	}
	if err := validateAssistantToolCalls(final); err != nil {
		final.Content = nil
		final.StopReason = StopReasonError
		final.ErrorKind = ErrorProtocol
		final.ErrorMessage = err.Error()
	}
	// A context-window rejection is a failed request rather than a completed
	// assistant message. Keep it out of active and durable history so a compacted
	// retry can proceed from the original transcript.
	if final.StopReason != StopReasonContextWindow {
		d.appendMessage(ctx, final)
	}
	if !started {
		d.emit(MessageStart{Message: final})
	}
	d.emit(MessageEnd{Message: final})
	return final
}

func validateAssistantToolCalls(message AssistantMessage) error {
	calls := message.ToolCalls()
	if message.StopReason == StopReasonToolUse && len(calls) == 0 {
		return fmt.Errorf("droids: provider ended with tool use but returned no tool calls")
	}
	return validateToolCallIdentities(calls, "provider")
}

func validateToolCallIdentities(calls []ToolCall, source string) error {
	seen := make(map[string]struct{}, len(calls))
	for index, call := range calls {
		if call.ID == "" {
			return fmt.Errorf("droids: %s tool call %d has an empty id", source, index)
		}
		if call.Name == "" {
			return fmt.Errorf("droids: %s tool call %d has an empty name", source, index)
		}
		if _, duplicate := seen[call.ID]; duplicate {
			return fmt.Errorf("droids: %s tool call %d has duplicate id %q", source, index, call.ID)
		}
		seen[call.ID] = struct{}{}
	}
	return nil
}

// toolOutcome is the finalized result of a single tool call.
type toolOutcome struct {
	result  ToolResult
	isError bool
}

// executeTools runs a batch of tool calls and returns their results in
// assistant source order. The batch runs sequentially when Options.ToolExecution
// is sequential or any tool in the batch is ModeSequential; otherwise tools
// execute concurrently. Regardless of execution order, ToolResultMessages are
// appended to the transcript in source order (deterministic for the model and
// resume); live ToolExecutionEnd events fire in completion order.
func (d *Droid) executeTools(ctx context.Context, calls []ToolCall) []ToolResultMessage {
	if d.batchIsSequential(calls) {
		return d.executeToolsSequential(ctx, calls)
	}
	return d.executeToolsParallel(ctx, calls)
}

func (d *Droid) batchIsSequential(calls []ToolCall) bool {
	if d.opts.ToolExecution == ModeSequential {
		return true
	}
	for _, call := range calls {
		if tool, ok := d.toolsByName[call.Name]; ok && tool.mode() == ModeSequential {
			return true
		}
	}
	return false
}

func (d *Droid) executeToolsSequential(ctx context.Context, calls []ToolCall) []ToolResultMessage {
	var out []ToolResultMessage
	for _, call := range calls {
		d.emit(ToolExecutionStart{ToolCallID: call.ID, ToolName: call.Name, Arguments: call.Arguments})
		result, isError := d.runToolCall(ctx, call)
		d.emit(ToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: result, IsError: isError})
		out = append(out, d.finalizeToolResult(ctx, call, result, isError))
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

func (d *Droid) executeToolsParallel(ctx context.Context, calls []ToolCall) []ToolResultMessage {
	outcomes := make([]toolOutcome, len(calls))
	var execIdx []int

	// Preflight, sequentially in source order: emit starts and resolve before
	// hooks (reject / short-circuit) deterministically. Survivors execute.
	for i, call := range calls {
		d.emit(ToolExecutionStart{ToolCallID: call.ID, ToolName: call.Name, Arguments: call.Arguments})
		if oc, proceed := d.beforeTool(ctx, call); !proceed {
			outcomes[i] = oc
			d.emit(ToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: oc.result, IsError: oc.isError})
			continue
		}
		execIdx = append(execIdx, i)
	}

	// Execute survivors with a fixed worker count; emit ToolExecutionEnd in
	// completion order without creating one waiting goroutine per model call.
	workerCount := len(execIdx)
	if d.opts.MaxParallelTools > 0 {
		workerCount = min(workerCount, d.opts.MaxParallelTools)
	}
	jobs := make(chan int, len(execIdx))
	for _, index := range execIdx {
		jobs <- index
	}
	close(jobs)
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				call := calls[index]
				result, isError := d.executeAndAfter(ctx, call)
				outcomes[index] = toolOutcome{result: result, isError: isError}
				d.emit(ToolExecutionEnd{ToolCallID: call.ID, ToolName: call.Name, Result: result, IsError: isError})
			}
		}()
	}
	wg.Wait()

	// Finalize in source order: append + persist + emit message events.
	out := make([]ToolResultMessage, 0, len(calls))
	for i, call := range calls {
		oc := outcomes[i]
		out = append(out, d.finalizeToolResult(ctx, call, oc.result, oc.isError))
	}
	return out
}

// finalizeToolResult appends a tool result to the transcript, persists it, and
// emits its message lifecycle events.
func (d *Droid) finalizeToolResult(ctx context.Context, call ToolCall, result ToolResult, isError bool) ToolResultMessage {
	trm := ToolResultMessage{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    result.Content,
		Details:    result.Details,
		IsError:    isError,
		Timestamp:  time.Now().UnixMilli(),
	}
	d.appendMessage(ctx, trm)
	d.emit(MessageStart{Message: trm})
	d.emit(MessageEnd{Message: trm})
	return trm
}

// runToolCall runs the full before/execute/after path for one call. Used by
// the sequential batch path.
func (d *Droid) runToolCall(ctx context.Context, call ToolCall) (ToolResult, bool) {
	if oc, proceed := d.beforeTool(ctx, call); !proceed {
		return oc.result, oc.isError
	}
	return d.executeAndAfter(ctx, call)
}

// beforeTool applies the before hook. proceed is false when the hook produced a
// per-call outcome (error, rejection, or short-circuit) and the tool must not run.
// A short-circuit result also skips the after hook. Hook errors degrade to an
// error result rather than crashing the loop.
func (d *Droid) beforeTool(ctx context.Context, call ToolCall) (oc toolOutcome, proceed bool) {
	if d.opts.BeforeToolCall == nil {
		return toolOutcome{}, true
	}
	br, err := d.opts.BeforeToolCall(ctx, BeforeToolContext{ToolCall: call, Args: call.Arguments})
	switch {
	case err != nil:
		return toolOutcome{result: toolErrorText(err.Error()), isError: true}, false
	case br.Reject:
		reason := br.Reason
		if reason == "" {
			reason = "Tool execution was rejected"
		}
		return toolOutcome{result: toolErrorText(reason), isError: true}, false
	case br.Result != nil:
		// Short-circuit: skip execution and the after hook.
		return toolOutcome{result: *br.Result, isError: br.Result.IsError}, false
	}
	return toolOutcome{}, true
}

// executeAndAfter runs the tool and applies the after hook.
func (d *Droid) executeAndAfter(ctx context.Context, call ToolCall) (ToolResult, bool) {
	result, isError := d.executeTool(ctx, call)
	if d.opts.AfterToolCall != nil {
		replacement, err := d.opts.AfterToolCall(ctx, AfterToolContext{ToolCall: call, Result: result, IsError: isError})
		if err != nil {
			return toolErrorText(err.Error()), true
		}
		if replacement != nil {
			return *replacement, replacement.IsError
		}
	}
	return result, isError
}

// executeTool looks up and runs a single tool.
func (d *Droid) executeTool(ctx context.Context, call ToolCall) (ToolResult, bool) {
	tool, ok := d.toolsByName[call.Name]
	if !ok {
		return toolErrorText(fmt.Sprintf("Tool %q not found", call.Name)), true
	}
	r, err := tool.execute(ctx, call.Arguments)
	if err != nil {
		return toolErrorText(err.Error()), true
	}
	return r, r.IsError
}

func toolErrorText(text string) ToolResult {
	result := ToolText(text)
	result.IsError = true
	return result
}

// --- transcript + steering helpers (mutex-guarded) ---

// validateContinuation verifies that the transcript ends with exactly one
// source-ordered result for every tool call in the preceding assistant message.
// Providers require this shape before another assistant turn can be requested.
func (d *Droid) validateContinuation() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.transcript) == 0 {
		return fmt.Errorf("droids: cannot continue an empty transcript")
	}

	firstResult := len(d.transcript)
	for firstResult > 0 {
		if _, ok := d.transcript[firstResult-1].(ToolResultMessage); !ok {
			break
		}
		firstResult--
	}
	if firstResult == len(d.transcript) {
		return fmt.Errorf("droids: continuation transcript must end with tool results")
	}
	if firstResult == 0 {
		return fmt.Errorf("droids: continuation tool results have no preceding assistant message")
	}

	assistant, ok := d.transcript[firstResult-1].(AssistantMessage)
	if !ok {
		return fmt.Errorf("droids: continuation tool results must follow an assistant message")
	}
	calls := assistant.ToolCalls()
	results := d.transcript[firstResult:]
	if len(calls) == 0 {
		return fmt.Errorf("droids: continuation assistant message has no tool calls")
	}
	if len(results) != len(calls) {
		return fmt.Errorf(
			"droids: continuation has %d tool results for %d tool calls",
			len(results), len(calls),
		)
	}
	if err := validateToolCallIdentities(calls, "continuation"); err != nil {
		return err
	}
	for i, call := range calls {
		result := results[i].(ToolResultMessage)
		if result.ToolCallID != call.ID {
			return fmt.Errorf(
				"droids: continuation tool result %d has id %q, want %q",
				i, result.ToolCallID, call.ID,
			)
		}
		if result.ToolName != "" && result.ToolName != call.Name {
			return fmt.Errorf(
				"droids: continuation tool result %d has name %q, want %q",
				i, result.ToolName, call.Name,
			)
		}
	}
	return nil
}

// persistTimeout bounds a best-effort Storage.Append that is detached from run
// cancellation, so a stuck store cannot wedge the worker forever.
const persistTimeout = 30 * time.Second

func (d *Droid) appendMessage(ctx context.Context, m Message) {
	d.mu.Lock()
	d.transcript = append(d.transcript, m)
	d.mu.Unlock()
	// Persistence is independent of run cancellation: a message that completed
	// must be durably recorded even if the caller cancelled or the run was
	// aborted, so the transcript and storage stay consistent. Detach from the
	// run context but keep a bound so a hung store cannot block the worker.
	// Best-effort — a failing store must not crash the loop.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()
	_ = d.opts.Storage.Append(pctx, d.opts.Session, m)
}

func (d *Droid) snapshot() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Message, len(d.transcript))
	copy(out, d.transcript)
	return out
}

func (d *Droid) drainSteering() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.steering) == 0 {
		return nil
	}
	out := d.steering
	d.steering = nil
	return out
}

func (d *Droid) providerToolSchemas() []ToolSchema {
	return append([]ToolSchema(nil), d.orderedToolSchemas...)
}

func errText(m AssistantMessage) string {
	if m.ErrorMessage != "" {
		return m.ErrorMessage
	}
	return string(m.StopReason)
}

func compactionErrorMessage(model Model, err error) AssistantMessage {
	return AssistantMessage{
		Provider:     model.Provider,
		Model:        model.ID,
		StopReason:   StopReasonError,
		ErrorMessage: err.Error(),
		Timestamp:    time.Now().UnixMilli(),
	}
}
