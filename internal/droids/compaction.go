package droids

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultCompactionTriggerFraction = 0.8
	// A bounded 12K-token allowance avoids treating encoded transport bytes as
	// text while conservatively accounting for provider-specific image tokens.
	estimatedImageContentBytes = 24 << 10
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

// contextUsage estimates usage for messages plus the dispatched configuration
// update items projected onto them.
func (d *Droid) contextUsage(messages []Message, history *ReasoningHistory) ContextUsage {
	configuration := d.sdk.currentRequestConfiguration()
	model := configuredContextModel(d.model, configuration)
	return estimateContextUsage(
		configuration.systemPrompt, append([]ToolSchema(nil), configuration.toolSchemas...), model,
		configuration.reasoning, configuration.maxTokens, messages, history,
	)
}

func estimateContextUsage(systemPrompt string, tools []ToolSchema, model Model, reasoning string, reservedOutput int, messages []Message, history *ReasoningHistory) ContextUsage {
	request := Request{
		SystemPrompt: systemPrompt, Messages: messages, Tools: tools,
		Reasoning: reasoning, MaxTokens: reservedOutput, ReasoningHistory: history,
	}
	input := estimateRequestTokens(request)
	remaining := 0
	hasRemaining := false
	if model.ContextWindow > 0 {
		remaining = model.ContextWindow - input - reservedOutput
		hasRemaining = true
	}
	if model.MaxInputTokens > 0 {
		inputRemaining := model.MaxInputTokens - input
		if !hasRemaining || inputRemaining < remaining {
			remaining = inputRemaining
		}
	}
	return ContextUsage{
		Model: model.metadata(), EstimatedInput: input,
		ReservedOutput: reservedOutput, ContextWindow: model.ContextWindow,
		MaxInputTokens: model.MaxInputTokens, Remaining: remaining,
	}
}

// estimateRequestTokens is deliberately provider-neutral and approximate.
func estimateRequestTokens(req Request) int {
	bytes := len(req.SystemPrompt) + 32 + estimateMessagesBytes(req.Messages) + estimateReasoningHistoryBytes(req.ReasoningHistory)
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

// estimateReasoningHistoryBytes accounts for the configuration update items Kit
// inserts into the provider input array. They are separate input items, not
// canonical messages, so message estimates alone understate a replayed request.
func estimateReasoningHistoryBytes(history *ReasoningHistory) int {
	if history == nil {
		return 0
	}
	bytes := 0
	for _, update := range history.Updates {
		bytes += len(`{"type":"configuration_update","reasoning":{"effort":""}}`) + len(update.Effort)
	}
	return bytes
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
		case ContextMessage:
			bytes += len(msg.Kind) + len(msg.Source)
			bytes += estimateContentBytes(msg.Content)
		}
	}
	return bytes
}

func estimateContentBytes[T any](content []T) int {
	bytes := 0
	for _, block := range content {
		bytes += 12
		switch value := any(block).(type) {
		case TextInput:
			bytes += len(value.Text)
		case AnnotationInput:
			bytes += len(value.Text)
			for _, annotation := range value.Annotations {
				bytes += len(annotation.Kind) + len(annotation.WorkspaceID) + len(annotation.Path) + len(annotation.FileRevision) + len(annotation.Preview) + len(annotation.Body) + 64
			}
		case FileInput:
			bytes += estimateFileBytes(value.Filename, value.MediaType, value.URL)
		case TextContent:
			bytes += len(value.Text) + len(value.Signature)
		case ThinkingContent:
			bytes += len(value.Thinking) + len(value.Signature)
		case FileContent:
			bytes += estimateFileBytes(value.Filename, value.MediaType, value.URL)
		case ToolCall:
			bytes += len(value.ID) + len(value.Name) + len(value.Arguments) + len(value.Signature)
		}
	}
	return bytes
}

func estimateFileBytes(filename, mediaType, source string) int {
	bytes := len(filename) + len(mediaType)
	if strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return bytes + estimatedImageContentBytes
	}
	return bytes + len(source)
}

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, message := range messages {
		switch msg := message.(type) {
		case UserMessage:
			msg.Content = cloneInputContent(msg.Content)
			out[i] = msg
		case AssistantMessage:
			msg.Content = cloneAssistantContent(msg.Content)
			out[i] = msg
		case ToolResultMessage:
			msg.Content = append([]ResultContent(nil), msg.Content...)
			msg.Details = append(json.RawMessage(nil), msg.Details...)
			out[i] = msg
		case ContextMessage:
			msg.Content = cloneInputContent(msg.Content)
			out[i] = msg
		}
	}
	return out
}

func cloneInputContent(content []InputContent) []InputContent {
	out := append([]InputContent(nil), content...)
	for index, block := range out {
		if annotation, ok := block.(AnnotationInput); ok {
			annotation.Annotations = append([]SubmittedAnnotation(nil), annotation.Annotations...)
			out[index] = annotation
		}
	}
	return out
}

func cloneAssistantContent(content []AssistantContent) []AssistantContent {
	out := make([]AssistantContent, len(content))
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
		case UserMessage, ContextMessage:
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
			if len(messages)-i-1 < len(calls) {
				return fmt.Errorf("assistant message at index %d has incomplete tool results", i)
			}
			seen := make(map[ToolCallID]struct{}, len(calls))
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
