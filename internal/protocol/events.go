package protocol

import (
	"encoding/json"
	"fmt"
)

const maxSessionEventPayloadBytes = 128 << 10

// SessionEventKind identifies one live renderer-neutral session update.
type SessionEventKind string

const (
	SessionEventRunStarted         SessionEventKind = "run.started"
	SessionEventUserMessage        SessionEventKind = "message.user"
	SessionEventAssistantStarted   SessionEventKind = "assistant.started"
	SessionEventAssistantTextDelta SessionEventKind = "assistant.text.delta"
	SessionEventThinkingDelta      SessionEventKind = "assistant.thinking.delta"
	SessionEventAssistantCompleted SessionEventKind = "assistant.completed"
	SessionEventToolPlanned        SessionEventKind = "tool.planned"
	SessionEventToolStarted        SessionEventKind = "tool.started"
	SessionEventToolUpdated        SessionEventKind = "tool.updated"
	SessionEventToolCompleted      SessionEventKind = "tool.completed"
	SessionEventRunFinished        SessionEventKind = "run.finished"
)

// SessionEvent is one durable ordered update in a session stream.
type SessionEvent struct {
	StreamID     string            `json:"streamId"`
	Sequence     int64             `json:"sequence"`
	SessionID    string            `json:"sessionId"`
	TurnID       string            `json:"turnId"`
	RunID        string            `json:"runId"`
	Kind         SessionEventKind  `json:"kind"`
	ContentIndex int               `json:"contentIndex,omitempty"`
	Delta        string            `json:"delta,omitempty"`
	Text         string            `json:"text,omitempty"`
	Thinking     string            `json:"thinking,omitempty"`
	ToolCallID   string            `json:"toolCallId,omitempty"`
	ToolName     string            `json:"toolName,omitempty"`
	Arguments    string            `json:"arguments,omitempty"`
	IsError      bool              `json:"isError,omitempty"`
	Status       RunStatus         `json:"status,omitempty"`
	ErrorKind    ProviderErrorKind `json:"errorKind,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
}

// SessionEventBatch is one bounded page after a client's cursor.
type SessionEventBatch struct {
	StreamID       string         `json:"streamId,omitempty"`
	FirstSequence  int64          `json:"firstSequence,omitempty"`
	LastSequence   int64          `json:"lastSequence,omitempty"`
	ResyncRequired bool           `json:"resyncRequired,omitempty"`
	Events         []SessionEvent `json:"events"`
}

// Validate checks an event received across a transport boundary.
func (event SessionEvent) Validate() error {
	if event.StreamID == "" || event.Sequence < 1 {
		return fmt.Errorf("event stream id and positive sequence are required")
	}
	if event.SessionID == "" || event.TurnID == "" || event.RunID == "" {
		return fmt.Errorf("event session, turn, and run ids are required")
	}
	if len(event.Delta)+len(event.Text)+len(event.Thinking)+len(event.Arguments)+len(event.ErrorMessage) > maxSessionEventPayloadBytes {
		return fmt.Errorf("event payload exceeds 128 KiB")
	}
	switch event.Kind {
	case SessionEventRunStarted:
		if event.Status != RunStatusRunning {
			return fmt.Errorf("started run status must be running")
		}
	case SessionEventUserMessage:
		if event.Text == "" {
			return fmt.Errorf("user message text is required")
		}
	case SessionEventAssistantStarted, SessionEventAssistantCompleted:
	case SessionEventAssistantTextDelta, SessionEventThinkingDelta:
		if event.ContentIndex < 0 || event.Delta == "" {
			return fmt.Errorf("assistant delta requires a content index and text")
		}
	case SessionEventToolPlanned, SessionEventToolStarted, SessionEventToolUpdated, SessionEventToolCompleted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("tool event requires call id and name")
		}
		if (event.Kind == SessionEventToolPlanned || event.Kind == SessionEventToolStarted) && event.Arguments != "" && !json.Valid([]byte(event.Arguments)) {
			return fmt.Errorf("tool arguments are not valid JSON")
		}
	case SessionEventRunFinished:
		switch event.Status {
		case RunStatusCompleted:
			if event.ErrorKind != "" || event.ErrorMessage != "" {
				return fmt.Errorf("completed run cannot carry error metadata")
			}
		case RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
			if event.ErrorMessage == "" {
				return fmt.Errorf("terminal run status %q requires an error message", event.Status)
			}
		default:
			return fmt.Errorf("run finish status %q is invalid", event.Status)
		}
	default:
		return fmt.Errorf("event kind %q is invalid", event.Kind)
	}
	switch event.ErrorKind {
	case "", ProviderErrorAuthentication, ProviderErrorEntitlement,
		ProviderErrorUsageLimit, ProviderErrorRateLimit, ProviderErrorTransport,
		ProviderErrorProtocol:
	default:
		return fmt.Errorf("error kind %q is invalid", event.ErrorKind)
	}
	if event.Kind != SessionEventRunStarted && event.Kind != SessionEventRunFinished && event.Status != "" {
		return fmt.Errorf("event kind %q cannot carry run status", event.Kind)
	}
	if event.Kind != SessionEventRunFinished && (event.ErrorKind != "" || event.ErrorMessage != "") {
		return fmt.Errorf("event kind %q cannot carry run error metadata", event.Kind)
	}
	isTool := event.Kind == SessionEventToolPlanned || event.Kind == SessionEventToolStarted || event.Kind == SessionEventToolUpdated || event.Kind == SessionEventToolCompleted
	if !isTool && (event.ToolCallID != "" || event.ToolName != "" || event.Arguments != "" || event.IsError) {
		return fmt.Errorf("event kind %q cannot carry tool data", event.Kind)
	}
	if (event.Kind == SessionEventToolUpdated || event.Kind == SessionEventToolCompleted) && event.Arguments != "" {
		return fmt.Errorf("tool result event cannot carry arguments")
	}
	return nil
}

// Validate checks event ordering and stream identity for a transport page.
func (batch SessionEventBatch) Validate() error {
	if batch.FirstSequence < 0 || batch.LastSequence < 0 ||
		(batch.FirstSequence == 0) != (batch.LastSequence == 0) ||
		batch.FirstSequence > batch.LastSequence {
		return fmt.Errorf("event retention range is invalid")
	}
	if batch.FirstSequence > 0 && batch.StreamID == "" {
		return fmt.Errorf("event retention range requires a stream id")
	}
	if batch.ResyncRequired && len(batch.Events) != 0 {
		return fmt.Errorf("resync-required event batch must not contain updates")
	}
	if len(batch.Events) > 0 && batch.FirstSequence == 0 {
		return fmt.Errorf("non-empty event batch requires a retention range")
	}
	previous := int64(0)
	for index, event := range batch.Events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("event %d: %w", index, err)
		}
		if batch.StreamID == "" || event.StreamID != batch.StreamID {
			return fmt.Errorf("event %d stream identity mismatch", index)
		}
		if event.Sequence <= previous {
			return fmt.Errorf("event %d sequence is not increasing", index)
		}
		if batch.FirstSequence > 0 && event.Sequence < batch.FirstSequence || batch.LastSequence > 0 && event.Sequence > batch.LastSequence {
			return fmt.Errorf("event %d sequence is outside the retention range", index)
		}
		previous = event.Sequence
	}
	return nil
}
