package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ContextUsage describes the estimated context budget for a provider request.
// Exact is false when Droids used its approximate provider-neutral estimator.
type ContextUsage struct {
	Model          Model
	EstimatedInput int
	ReservedOutput int
	ContextWindow  int
	MaxInputTokens int
	Remaining      int
	Exact          bool
}

// CompactionReason explains why Droids requested a smaller context.
type CompactionReason string

const (
	// CompactionThreshold means the estimated request crossed the proactive
	// context-pressure threshold.
	CompactionThreshold CompactionReason = "threshold"
	// CompactionOverflow means the provider rejected the request because its
	// input exceeded the model context window.
	CompactionOverflow CompactionReason = "context_overflow"
)

// CompactionRequest is passed to an application's optional compaction hook.
// Messages is a snapshot; replacing it does not mutate the Droid transcript.
type CompactionRequest struct {
	Reason   CompactionReason
	Model    Model
	Usage    ContextUsage
	Messages []Message
}

// CompactionResult is the replacement active context returned by a hook.
// Applied=false declines compaction and leaves the transcript unchanged.
type CompactionResult struct {
	Applied  bool
	Messages []Message
}

// CompactionHook produces a smaller, provider-neutral active context. Durable
// summaries and storage changes remain the application's responsibility.
type CompactionHook func(context.Context, CompactionRequest) (CompactionResult, error)

const defaultCompactionTriggerFraction = 0.8

func (d *Droid) contextUsage(messages []Message) ContextUsage {
	req := Request{
		SystemPrompt: d.opts.SystemPrompt,
		Messages:     messages,
		Tools:        d.providerToolSchemas(),
		Reasoning:    d.opts.Reasoning,
		MaxTokens:    d.maxTokens,
	}
	input := estimateRequestTokens(req)
	remaining := 0
	hasRemaining := false
	if d.model.ContextWindow > 0 {
		remaining = d.model.ContextWindow - input - d.compactionReserve
		hasRemaining = true
	}
	if d.model.MaxInputTokens > 0 {
		inputRemaining := d.model.MaxInputTokens - input
		if !hasRemaining || inputRemaining < remaining {
			remaining = inputRemaining
		}
		hasRemaining = true
	}
	return ContextUsage{
		Model:          cloneModel(d.model),
		EstimatedInput: input,
		ReservedOutput: d.compactionReserve,
		ContextWindow:  d.model.ContextWindow,
		MaxInputTokens: d.model.MaxInputTokens,
		Remaining:      remaining,
		Exact:          false,
	}
}

func cloneContextUsage(usage ContextUsage) ContextUsage {
	usage.Model = cloneModel(usage.Model)
	return usage
}

func (d *Droid) shouldCompact(usage ContextUsage) bool {
	if d.opts.Compact == nil {
		return false
	}
	if usage.ContextWindow > 0 {
		trigger := int(float64(usage.ContextWindow) * defaultCompactionTriggerFraction)
		if usage.EstimatedInput+usage.ReservedOutput >= trigger {
			return true
		}
	}
	if usage.MaxInputTokens > 0 {
		trigger := int(float64(usage.MaxInputTokens) * defaultCompactionTriggerFraction)
		if usage.EstimatedInput >= trigger {
			return true
		}
	}
	return false
}

// compact invokes the application hook and atomically installs a valid,
// smaller active transcript. It returns whether replacement was applied.
func (d *Droid) compact(ctx context.Context, reason CompactionReason, force bool) (bool, error) {
	if d.opts.Compact == nil {
		return false, nil
	}

	beforeMessages := d.snapshot()
	before := d.contextUsage(beforeMessages)
	if !force && !d.shouldCompact(before) {
		return false, nil
	}

	d.emit(CompactionStart{Reason: reason, Usage: cloneContextUsage(before)})
	result, err := d.opts.Compact(ctx, CompactionRequest{
		Reason:   reason,
		Model:    cloneModel(d.model),
		Usage:    cloneContextUsage(before),
		Messages: cloneMessages(beforeMessages),
	})
	if err != nil {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: before})
		return false, fmt.Errorf("droids: compact context: %w", err)
	}
	if err := ctx.Err(); err != nil {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: before})
		return false, err
	}
	if !result.Applied {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: before})
		return false, nil
	}
	if len(result.Messages) == 0 {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: before})
		return false, fmt.Errorf("droids: compact context: hook returned an empty replacement")
	}
	if err := validateMessageSequence(result.Messages); err != nil {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: before})
		return false, fmt.Errorf("droids: compact context: invalid replacement: %w", err)
	}

	replacement := cloneMessages(result.Messages)
	after := d.contextUsage(replacement)
	if after.EstimatedInput >= before.EstimatedInput {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: after})
		return false, fmt.Errorf(
			"droids: compact context: replacement did not reduce estimated input (%d >= %d tokens)",
			after.EstimatedInput, before.EstimatedInput,
		)
	}
	// There is no consumer-facing target policy. A replacement only needs to
	// resolve the same fixed pressure trigger that caused compaction.
	if d.shouldCompact(after) {
		d.emit(CompactionEnd{Reason: reason, Before: before, After: after})
		return false, fmt.Errorf("droids: compact context: replacement remains above the compaction trigger")
	}
	d.mu.Lock()
	d.transcript = replacement
	d.mu.Unlock()
	d.emit(CompactionEnd{Reason: reason, Before: before, After: after, Applied: true})
	return true, nil
}

// estimateRequestTokens is deliberately provider-neutral and approximate.
// It favors early compaction, but model tokenization can still differ. Provider
// token counters can replace it later without changing the hook API.
func estimateRequestTokens(req Request) int {
	bytes := len(req.SystemPrompt) + 32 + estimateMessagesBytes(req.Messages)
	for _, tool := range req.Tools {
		bytes += 32 + len(tool.Name) + len(tool.Description)
		if encoded, err := json.Marshal(tool.Parameters); err == nil {
			bytes += len(encoded)
		}
	}
	// Two UTF-8 bytes per token intentionally biases the generic estimate high
	// for prose and code. Round upward and retain framing overhead so tiny
	// requests never estimate to zero. Exact provider counters may replace it.
	return (bytes + 1) / 2
}

// EstimateMessagesTokens returns Droids' conservative, provider-neutral token
// estimate for a message slice. Compaction hooks can use it to bound chunks
// with the same approximation Droids uses for context-pressure decisions.
func EstimateMessagesTokens(messages []Message) int {
	return (estimateMessagesBytes(messages) + 1) / 2
}

func estimateMessagesBytes(messages []Message) int {
	bytes := 0
	for _, message := range messages {
		bytes += 24
		switch msg := message.(type) {
		case UserMessage:
			bytes += estimateContentBytes(msg.Content)
		case AssistantMessage:
			bytes += len(msg.Provider) + len(msg.Model) + len(msg.ResponseModel) + len(msg.ResponseID) + len(msg.ProviderScope)
			bytes += estimateContentBytes(msg.Content)
		case ToolResultMessage:
			bytes += len(msg.ToolCallID) + len(msg.ToolName)
			bytes += estimateContentBytes(msg.Content)
		}
	}
	return bytes
}

func estimateContentBytes(content []Content) int {
	bytes := 0
	for _, block := range content {
		bytes += 12
		switch value := block.(type) {
		case TextContent:
			bytes += len(value.Text) + len(value.Signature)
		case ThinkingContent:
			bytes += len(value.Thinking) + len(value.Signature)
		case ImageContent:
			bytes += len(value.MediaType) + len(value.URL)
		case FileContent:
			bytes += len(value.Filename) + len(value.MediaType) + len(value.URL)
		case ToolCall:
			bytes += len(value.ID) + len(value.Name) + len(value.Arguments) + len(value.Signature)
		}
	}
	return bytes
}

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, message := range messages {
		switch msg := message.(type) {
		case UserMessage:
			msg.Content = cloneContent(msg.Content)
			out[i] = msg
		case AssistantMessage:
			msg.Content = cloneContent(msg.Content)
			out[i] = msg
		case ToolResultMessage:
			msg.Content = cloneContent(msg.Content)
			out[i] = msg
		}
	}
	return out
}

func cloneContent(content []Content) []Content {
	out := make([]Content, len(content))
	for i, block := range content {
		if call, ok := block.(ToolCall); ok {
			call.Arguments = append([]byte(nil), call.Arguments...)
			out[i] = call
			continue
		}
		out[i] = block
	}
	return out
}

func validateMessageSequence(messages []Message) error {
	for i := 0; i < len(messages); i++ {
		switch msg := messages[i].(type) {
		case UserMessage:
		case ToolResultMessage:
			return fmt.Errorf("tool result at index %d has no preceding tool call", i)
		case AssistantMessage:
			calls := msg.ToolCalls()
			if msg.StopReason == StopReasonToolUse && len(calls) == 0 {
				return fmt.Errorf("assistant message at index %d stopped for tool use without tool calls", i)
			}
			if len(calls) == 0 {
				continue
			}
			if msg.StopReason != StopReasonToolUse {
				return fmt.Errorf("assistant message at index %d contains incomplete tool calls", i)
			}
			if len(messages)-i-1 < len(calls) {
				return fmt.Errorf("assistant message at index %d has incomplete tool results", i)
			}
			seen := make(map[string]struct{}, len(calls))
			for j, call := range calls {
				if call.ID == "" || call.Name == "" {
					return fmt.Errorf("tool call %d at index %d requires an id and name", j, i)
				}
				if _, exists := seen[call.ID]; exists {
					return fmt.Errorf("tool call %d at index %d has duplicate id %q", j, i, call.ID)
				}
				seen[call.ID] = struct{}{}
				result, ok := messages[i+1+j].(ToolResultMessage)
				if !ok {
					return fmt.Errorf("tool call %q at index %d is not followed by its result", call.ID, i)
				}
				if result.ToolCallID != call.ID {
					return fmt.Errorf("tool result for call %q has id %q", call.ID, result.ToolCallID)
				}
				if result.ToolName != "" && result.ToolName != call.Name {
					return fmt.Errorf("tool result for call %q has name %q, want %q", call.ID, result.ToolName, call.Name)
				}
			}
			i += len(calls)
		default:
			return fmt.Errorf("unsupported message type %T at index %d", messages[i], i)
		}
	}
	return nil
}

func isContextWindowError(code, message string) bool {
	text := strings.ToLower(code + " " + message)
	for _, marker := range []string{
		"context_length_exceeded",
		"context_window_exceeded",
		"model_context_window_exceeded",
		"maximum context length",
		"prompt is too long",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
