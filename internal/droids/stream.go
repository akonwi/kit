package droids

import "context"

// stream.go — the provider streaming contract. A provider translates a
// Request into a channel of StreamEvents that assemble into one
// AssistantMessage. This is the Layer-1 boundary; the agent loop (Layer 2)
// consumes it and re-emits higher-level agent Events.

// Request is a single provider call: the neutral context plus per-call knobs.
type Request struct {
	SystemPrompt string
	Messages     []Message
	Tools        []ToolSchema

	// Reasoning selects a thinking level. "" leaves the field unset (provider
	// default); "none"/"off" explicitly disables reasoning; "minimal", "low",
	// "medium", "high", "xhigh", and "max" request effort. Ignored by
	// non-reasoning models.
	Reasoning   string
	Temperature *float64
	// MaxTokens is omitted for models with provider-controlled output limits.
	MaxTokens int
}

// ToolSchema is the wire-level tool definition handed to a provider: a name,
// description, and a JSON Schema for the arguments. Erased from the generic
// Tool[Args] by the loop before the provider call.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema
}

// StreamEvent is a sealed interface over the provider streaming protocol.
// A well-behaved stream emits StreamStart, then any number of delta events,
// and terminates with exactly one StreamDone or StreamError. Request/runtime
// failures are encoded as StreamError, never returned as a Go error from the
// channel.
type StreamEvent interface{ isStreamEvent() }

type StreamStart struct{ Partial AssistantMessage }

func (StreamStart) isStreamEvent() {}

type StreamTextStart struct{ ContentIndex int }

func (StreamTextStart) isStreamEvent() {}

type StreamTextDelta struct {
	ContentIndex int
	Delta        string
}

func (StreamTextDelta) isStreamEvent() {}

type StreamTextEnd struct {
	ContentIndex int
	Text         string
}

func (StreamTextEnd) isStreamEvent() {}

type StreamThinkingDelta struct {
	ContentIndex int
	Delta        string
}

func (StreamThinkingDelta) isStreamEvent() {}

type StreamToolCallStart struct {
	ContentIndex int
	ID           string
	Name         string
}

func (StreamToolCallStart) isStreamEvent() {}

type StreamToolCallDelta struct {
	ContentIndex int
	Delta        string // partial JSON arguments
}

func (StreamToolCallDelta) isStreamEvent() {}

type StreamToolCallEnd struct {
	ContentIndex int
	ToolCall     ToolCall
}

func (StreamToolCallEnd) isStreamEvent() {}

// StreamDone terminates a successful stream with the assembled message.
type StreamDone struct{ Message AssistantMessage }

func (StreamDone) isStreamEvent() {}

// StreamError terminates a failed, context-overflowed, or aborted stream.
// Message carries the corresponding StopReason and an ErrorMessage.
type StreamError struct{ Message AssistantMessage }

func (StreamError) isStreamEvent() {}

// Stream is what a provider returns: an event channel plus a Result future.
// Consumers may range over Events for every delta, await Result without reading
// Events, or range Events to closure and then call Result. Calling Result before
// Events closes abandons any pending events so the provider cannot deadlock.
type Stream interface {
	Events() <-chan StreamEvent
	// Result blocks until the stream terminates and returns the final message.
	// It never returns a Go error — failures live in Message.StopReason /
	// Message.ErrorMessage.
	Result() AssistantMessage
}

// streamFn is the shape a provider's stream implementation satisfies. Kept as
// a type so the loop and registry can pass it around as a value.
type streamFn func(ctx context.Context, model Model, req Request, opts callOptions) Stream

// callOptions are resolved per-call values (auth, headers) injected by the
// registry after auth resolution.
type callOptions struct {
	APIKey  string
	Headers map[string]string
}
