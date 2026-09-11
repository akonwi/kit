package protocol

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

// SubagentAction identifies one asynchronous child lifecycle operation.
type SubagentAction string

const (
	SubagentListAgents SubagentAction = "list_agents"
	SubagentStart      SubagentAction = "start"
	SubagentMessage    SubagentAction = "message"
	SubagentInspect    SubagentAction = "inspect"
	SubagentWait       SubagentAction = "wait"
	SubagentCancel     SubagentAction = "cancel"
	SubagentDismiss    SubagentAction = "dismiss"
)

// SubagentOperationInput is one validated bound-session operation.
type SubagentOperationInput struct {
	Action         SubagentAction `json:"action"`
	Agent          string         `json:"agent,omitempty"`
	Message        string         `json:"message,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	TaskID         string         `json:"taskId,omitempty"`
	TimeoutMS      int64          `json:"timeoutMs,omitempty"`
	Generation     uint64         `json:"generation,omitempty"`
}

// Validate checks a subagent operation received across a process boundary.
func (input SubagentOperationInput) Validate() error {
	if input.Agent != "" && (!validRendererText(input.Agent, 128) || input.Agent != strings.TrimSpace(input.Agent) || strings.ContainsAny(input.Agent, " /\\\t\r\n")) {
		return fmt.Errorf("subagent agent name is invalid")
	}
	if input.Message != "" && !validSubagentText(input.Message, 128<<10) {
		return fmt.Errorf("subagent message is invalid or oversized")
	}
	if input.ConversationID != "" && !identifier.Valid(input.ConversationID, "subagent_") {
		return fmt.Errorf("subagent conversationId is not canonical")
	}
	if input.TaskID != "" && !identifier.Valid(input.TaskID, "task_") {
		return fmt.Errorf("subagent taskId is not canonical")
	}
	hasConversation, hasAgent := input.ConversationID != "", input.Agent != ""
	unused := func(agent, message, conversation, task, timeout, generation bool) bool {
		return agent && hasAgent || message && input.Message != "" || conversation && hasConversation || task && input.TaskID != "" || timeout && input.TimeoutMS != 0 || generation && input.Generation != 0
	}
	switch input.Action {
	case SubagentListAgents:
		if unused(true, true, true, true, true, true) {
			return fmt.Errorf("list_agents accepts no other fields")
		}
	case SubagentStart:
		if !hasAgent || strings.TrimSpace(input.Message) == "" || unused(false, false, true, true, true, true) {
			return fmt.Errorf("start requires agent and message only")
		}
	case SubagentMessage:
		if hasAgent == hasConversation || strings.TrimSpace(input.Message) == "" || unused(false, false, false, true, true, true) {
			return fmt.Errorf("message requires exactly one conversation selector plus message")
		}
	case SubagentInspect:
		selectors := 0
		for _, present := range []bool{hasAgent, hasConversation, input.TaskID != ""} {
			if present {
				selectors++
			}
		}
		if selectors != 1 || unused(false, true, false, false, true, true) {
			return fmt.Errorf("inspect requires exactly one selector")
		}
	case SubagentWait:
		if input.TaskID == "" || input.TimeoutMS < 1 || input.TimeoutMS > 30_000 || unused(true, true, true, false, false, true) {
			return fmt.Errorf("wait requires taskId and timeoutMs from 1 to 30000 only")
		}
	case SubagentCancel:
		if input.TaskID == "" || input.Generation == 0 || unused(true, true, true, false, true, false) {
			return fmt.Errorf("cancel requires taskId and generation only")
		}
	case SubagentDismiss:
		if hasAgent == hasConversation || input.Generation == 0 || unused(false, true, false, true, true, false) {
			return fmt.Errorf("dismiss requires one conversation selector and generation only")
		}
	default:
		return fmt.Errorf("unknown subagent action %q", input.Action)
	}
	return nil
}

func validSubagentText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// SubagentOperationResult is one bounded lifecycle response.
type SubagentOperationResult struct {
	Definitions   []SubagentDefinition   `json:"definitions,omitempty"`
	Diagnostics   []SubagentDiagnostic   `json:"diagnostics,omitempty"`
	Conversation  *SubagentConversation  `json:"conversation,omitempty"`
	Conversations []SubagentConversation `json:"conversations,omitempty"`
	Task          *SubagentTask          `json:"task,omitempty"`
	Tasks         []SubagentTask         `json:"tasks,omitempty"`
	TimedOut      bool                   `json:"timedOut,omitempty"`
	Dismissed     bool                   `json:"dismissed,omitempty"`
	Warning       string                 `json:"warning,omitempty"`
}

// Validate checks a subagent result received across a transport boundary.
func (result SubagentOperationResult) Validate() error {
	if len(result.Definitions) > 128 || len(result.Diagnostics) > 128 || len(result.Conversations) > 128 || len(result.Tasks) > 20 ||
		(result.Warning != "" && !validRendererText(result.Warning, 4096)) {
		return fmt.Errorf("subagent result exceeds collection bounds")
	}
	conversations := append([]SubagentConversation(nil), result.Conversations...)
	if result.Conversation != nil {
		conversations = append(conversations, *result.Conversation)
	}
	if result.Task != nil || len(result.Tasks) > 0 {
		tasks := append([]SubagentTask(nil), result.Tasks...)
		if result.Task != nil {
			tasks = append(tasks, *result.Task)
		}
		conversations = append(conversations, SubagentConversation{
			ID: "subagent_00000000000000000000000000000000", AgentName: "validation", Model: "validation/model",
			State: "idle", Generation: 1,
			UpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339Nano), Tasks: tasks,
		})
	}
	return validateSubagentSnapshot(SessionSnapshot{
		SubagentDefinitions: result.Definitions, SubagentDiagnostics: result.Diagnostics,
		SubagentConversations: conversations,
	})
}

// SubagentLiveEvent is one bounded child delta or tool update.
type SubagentLiveEvent struct {
	Sequence     int64  `json:"sequence"`
	Kind         string `json:"kind"`
	TurnID       string `json:"turnId,omitempty"`
	MessageID    string `json:"messageId,omitempty"`
	ContentIndex int    `json:"contentIndex,omitempty"`
	Delta        string `json:"delta,omitempty"`
	Text         string `json:"text,omitempty"`
	ToolCallID   string `json:"toolCallId,omitempty"`
	ToolName     string `json:"toolName,omitempty"`
	IsError      bool   `json:"isError,omitempty"`
}

// SubagentLiveEventPage is one runtime-local child event page.
type SubagentLiveEventPage struct {
	StreamID       string              `json:"streamId,omitempty"`
	FirstSequence  int64               `json:"firstSequence,omitempty"`
	LastSequence   int64               `json:"lastSequence,omitempty"`
	ResyncRequired bool                `json:"resyncRequired,omitempty"`
	Events         []SubagentLiveEvent `json:"events"`
}

// Validate checks bounded child event ordering and payloads.
func (page SubagentLiveEventPage) Validate() error {
	if page.FirstSequence < 0 || page.LastSequence < 0 || (page.FirstSequence == 0) != (page.LastSequence == 0) ||
		page.FirstSequence > page.LastSequence || len(page.Events) > 128 {
		return fmt.Errorf("subagent event retention range is invalid")
	}
	if page.StreamID != "" && !identifier.Valid(page.StreamID, "substream_") || page.FirstSequence > 0 && page.StreamID == "" {
		return fmt.Errorf("subagent event range requires canonical stream identity")
	}
	if page.ResyncRequired && len(page.Events) != 0 {
		return fmt.Errorf("resync-required subagent event page cannot contain events")
	}
	previous, aggregate := int64(0), 0
	for index, event := range page.Events {
		aggregate += len(event.Kind) + len(event.TurnID) + len(event.MessageID) + len(event.Delta) + len(event.Text) + len(event.ToolCallID) + len(event.ToolName)
		if event.Sequence < 1 || index > 0 && event.Sequence != previous+1 || event.Sequence < page.FirstSequence || event.Sequence > page.LastSequence ||
			len(event.Delta)+len(event.Text) > 16<<10 || !validSubagentText(event.Kind, 128) || event.ContentIndex < 0 {
			return fmt.Errorf("subagent event %d is invalid", index)
		}
		switch event.Kind {
		case "message.text.delta", "message.thinking.delta":
			if event.MessageID == "" || event.Delta == "" || event.ToolCallID != "" || event.ToolName != "" || event.Text != "" || event.IsError {
				return fmt.Errorf("subagent delta event %d has invalid fields", index)
			}
		case "tool.planned", "tool.started", "tool.updated", "tool.completed":
			if event.ToolCallID == "" || event.ToolName == "" || event.Delta != "" || event.MessageID != "" || event.IsError && event.Kind != "tool.updated" && event.Kind != "tool.completed" {
				return fmt.Errorf("subagent tool event %d has invalid fields", index)
			}
		case "message.completed":
			if event.Delta != "" || event.Text != "" || event.ToolCallID != "" || event.ToolName != "" || event.IsError {
				return fmt.Errorf("subagent message event %d has invalid fields", index)
			}
		default:
			if !strings.Contains(event.Kind, ".") || event.Delta != "" || event.Text != "" || event.ToolCallID != "" || event.ToolName != "" || event.IsError {
				return fmt.Errorf("subagent lifecycle event %d has invalid fields", index)
			}
		}
		previous = event.Sequence
	}
	if aggregate > 1<<20 {
		return fmt.Errorf("subagent event page exceeds one MiB")
	}
	return nil
}

// SubagentTranscript is durable child history selected by conversation identity.
type SubagentTranscript struct {
	ConversationID string              `json:"conversationId"`
	Messages       []TranscriptMessage `json:"messages"`
}

// Validate checks a child transcript received across a transport boundary.
func (transcript SubagentTranscript) Validate() error {
	if !identifier.Valid(transcript.ConversationID, "subagent_") || len(transcript.Messages) > 1000 {
		return fmt.Errorf("subagent transcript identity or size is invalid")
	}
	previous, aggregate := int64(-1), 0
	for index, message := range transcript.Messages {
		aggregate += len(message.ID) + len(message.TurnID) + len(message.ErrorMessage) + len(message.Details)
		for _, block := range message.Content {
			aggregate += len(block.Text) + len(block.Arguments) + len(block.Filename) + len(block.MediaType)
		}
		if message.ID == "" || message.TurnID == "" || message.Sequence <= previous {
			return fmt.Errorf("subagent transcript message %d identity or sequence is invalid", index)
		}
		previous = message.Sequence
		if err := message.validate(); err != nil {
			return fmt.Errorf("subagent transcript message %d: %w", index, err)
		}
		if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
			return fmt.Errorf("subagent transcript message %d createdAt is invalid", index)
		}
	}
	if aggregate > 8<<20 {
		return fmt.Errorf("subagent transcript exceeds eight MiB")
	}
	return nil
}
