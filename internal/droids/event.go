package droids

// event.go — the agent-level event union emitted by Droid.Events() and
// Run.Events(). This is the public consumption surface (Layer 2 output).
// Sealed interface: consumers
// type-switch on concrete variants. A run produces:
//
//	AgentStart
//	  CompactionStart / CompactionEnd  (optional, before a turn)
//	  TurnStart
//	    MessageStart / MessageDelta* / MessageEnd   (assistant + injected msgs)
//	    ToolExecutionStart / ToolExecutionUpdate* / ToolExecutionEnd  (per call)
//	  TurnEnd
//	  ... (more turns)
//	AgentEnd | ErrorEvent

// Event is a sealed interface over agent lifecycle events.
type Event interface{ isEvent() }

// AgentStart is emitted once when a run begins.
type AgentStart struct{}

func (AgentStart) isEvent() {}

// AgentEnd is emitted once when a run finishes. Messages is the active
// transcript snapshot, including any context replacement applied by compaction.
type AgentEnd struct{ Messages []Message }

func (AgentEnd) isEvent() {}

// CompactionStart is emitted immediately before the application compaction
// hook runs. Summary content is deliberately excluded.
type CompactionStart struct {
	Reason CompactionReason
	Usage  ContextUsage
}

func (CompactionStart) isEvent() {}

// CompactionEnd reports whether a hook replaced the active context and how its
// estimated usage changed. It is also emitted when the hook fails or declines.
type CompactionEnd struct {
	Reason  CompactionReason
	Before  ContextUsage
	After   ContextUsage
	Applied bool
}

func (CompactionEnd) isEvent() {}

// TurnStart marks the beginning of one assistant turn.
type TurnStart struct{}

func (TurnStart) isEvent() {}

// TurnEnd marks the end of one assistant turn and its tool results.
type TurnEnd struct {
	Message     AssistantMessage
	ToolResults []ToolResultMessage
}

func (TurnEnd) isEvent() {}

// MessageStart is emitted for each user, assistant, or tool-result message.
type MessageStart struct{ Message Message }

func (MessageStart) isEvent() {}

// MessageDelta carries a streaming update to the in-progress assistant message.
// StreamEvent is the underlying provider delta; Partial is the message so far.
type MessageDelta struct {
	Partial AssistantMessage
	Stream  StreamEvent
}

func (MessageDelta) isEvent() {}

// MessageEnd is emitted when a message is complete.
type MessageEnd struct{ Message Message }

func (MessageEnd) isEvent() {}

// ToolExecutionStart is emitted before a tool runs.
type ToolExecutionStart struct {
	ToolCallID string
	ToolName   string
	Arguments  []byte
}

func (ToolExecutionStart) isEvent() {}

// ToolExecutionUpdate carries a partial result streamed by a running tool.
type ToolExecutionUpdate struct {
	ToolCallID    string
	ToolName      string
	PartialResult ToolResult
}

func (ToolExecutionUpdate) isEvent() {}

// ToolExecutionEnd is emitted when a tool finishes.
type ToolExecutionEnd struct {
	ToolCallID string
	ToolName   string
	Result     ToolResult
	IsError    bool
}

func (ToolExecutionEnd) isEvent() {}

// ErrorEvent is emitted when a turn fails or is aborted. The run then ends.
type ErrorEvent struct {
	Message AssistantMessage
	Aborted bool
}

func (ErrorEvent) isEvent() {}
