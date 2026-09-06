package droids

// Event is the sealed transient/durable event payload surface. Durable
// lifecycle transitions use LifecycleEvent; the concrete values below carry
// provider and tool updates that are useful to live clients.
type Event interface{ isEvent() }

// MessageStart announces an assistant message identity before deltas.
type MessageStart struct{ Message Message }

func (MessageStart) isEvent() {}

// MessageDelta carries a streaming update to an in-progress assistant message.
type MessageDelta struct {
	MessageID string
	Partial   AssistantMessage
	Stream    StreamEvent
}

func (MessageDelta) isEvent() {}

// MessageEnd announces the authoritative completed assistant message.
type MessageEnd struct{ Message Message }

func (MessageEnd) isEvent() {}

// ToolExecutionStart is emitted before a tool runs.
type ToolExecutionStart struct {
	ToolCallID ToolCallID
	ToolName   string
	Arguments  []byte
}

func (ToolExecutionStart) isEvent() {}

// ToolExecutionUpdate carries append-only content emitted by a running tool.
type ToolExecutionUpdate struct {
	ToolCallID ToolCallID
	ToolName   string
	Delta      ToolResultDelta
}

func (ToolExecutionUpdate) isEvent() {}

// ToolExecutionEnd is emitted when a tool finishes.
type ToolExecutionEnd struct {
	ToolCallID ToolCallID
	ToolName   string
	Result     ToolResult
	IsError    bool
}

func (ToolExecutionEnd) isEvent() {}
