package protocol

import (
	"encoding/json"
	"fmt"
)

const (
	maxSessionEventPayloadBytes  = 128 << 10
	maxSessionEventContentBlocks = 128
)

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
	StreamID           string              `json:"streamId"`
	Sequence           int64               `json:"sequence"`
	SessionID          string              `json:"sessionId"`
	TurnID             string              `json:"turnId"`
	RunID              string              `json:"runId"`
	MessageID          string              `json:"messageId,omitempty"`
	Kind               SessionEventKind    `json:"kind"`
	ContentIndex       int                 `json:"contentIndex,omitempty"`
	Delta              string              `json:"delta,omitempty"`
	Text               string              `json:"text,omitempty"`
	Thinking           string              `json:"thinking,omitempty"`
	ToolCallID         string              `json:"toolCallId,omitempty"`
	ToolName           string              `json:"toolName,omitempty"`
	Arguments          string              `json:"arguments,omitempty"`
	ArgumentsTruncated bool                `json:"argumentsTruncated,omitempty"`
	Content            []TranscriptContent `json:"content,omitempty"`
	ContentTruncated   bool                `json:"contentTruncated,omitempty"`
	Details            json.RawMessage     `json:"details,omitempty"`
	DetailsOmitted     bool                `json:"detailsOmitted,omitempty"`
	IsError            bool                `json:"isError,omitempty"`
	Status             RunStatus           `json:"status,omitempty"`
	ErrorKind          ProviderErrorKind   `json:"errorKind,omitempty"`
	ErrorMessage       string              `json:"errorMessage,omitempty"`
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
	if len(event.Content) > maxSessionEventContentBlocks {
		return fmt.Errorf("event tool content exceeds %d blocks", maxSessionEventContentBlocks)
	}
	payloadBytes := len(event.Delta) + len(event.Text) + len(event.Thinking) + len(event.Arguments) + len(event.Details) + len(event.ErrorMessage)
	for _, block := range event.Content {
		payloadBytes += len(block.Text) + len(block.ToolCallID) + len(block.ToolName) + len(block.Arguments) + len(block.Filename) + len(block.MediaType)
	}
	if payloadBytes > maxSessionEventPayloadBytes {
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
		if event.MessageID == "" {
			return fmt.Errorf("assistant event requires a message id")
		}
	case SessionEventAssistantTextDelta, SessionEventThinkingDelta:
		if event.MessageID == "" || event.ContentIndex < 0 || event.Delta == "" {
			return fmt.Errorf("assistant delta requires a message id, content index, and text")
		}
	case SessionEventToolPlanned, SessionEventToolStarted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("tool event requires call id and name")
		}
		if event.Kind == SessionEventToolPlanned && event.MessageID == "" {
			return fmt.Errorf("planned tool call requires an assistant message id")
		}
		if (event.Arguments == "") == !event.ArgumentsTruncated {
			return fmt.Errorf("tool event requires either complete or explicitly truncated arguments")
		}
	case SessionEventToolUpdated:
		if event.ToolCallID == "" || event.ToolName == "" || len(event.Content) == 0 {
			return fmt.Errorf("tool update requires call id, name, and append-only content")
		}
		if event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted {
			return fmt.Errorf("tool update cannot carry final result metadata")
		}
	case SessionEventToolCompleted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("completed tool requires call id and name")
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
	if !isTool && (event.ToolCallID != "" || event.ToolName != "" || event.Arguments != "" || event.ArgumentsTruncated || len(event.Content) > 0 || event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted || event.IsError) {
		return fmt.Errorf("event kind %q cannot carry tool data", event.Kind)
	}
	if isTool && (event.Text != "" || event.Thinking != "" || event.Delta != "") {
		return fmt.Errorf("tool event cannot carry flattened text or assistant deltas")
	}
	if event.Kind != SessionEventToolPlanned && event.Kind != SessionEventToolStarted && (event.Arguments != "" || event.ArgumentsTruncated) {
		return fmt.Errorf("event kind %q cannot carry tool arguments", event.Kind)
	}
	if event.Kind != SessionEventToolUpdated && event.Kind != SessionEventToolCompleted && (len(event.Content) > 0 || event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted) {
		return fmt.Errorf("event kind %q cannot carry tool result data", event.Kind)
	}
	if event.DetailsOmitted && len(event.Details) > 0 {
		return fmt.Errorf("event cannot carry details and mark them omitted")
	}
	if len(event.Details) > 0 && !json.Valid(event.Details) {
		return fmt.Errorf("tool details are not valid JSON")
	}
	for index, block := range event.Content {
		if err := block.validate(); err != nil {
			return fmt.Errorf("tool content block %d: %w", index, err)
		}
		if !contentAllowedForRole("tool", block.Kind) {
			return fmt.Errorf("tool content block %d kind %q is invalid", index, block.Kind)
		}
	}
	carriesMessageID := event.Kind == SessionEventAssistantStarted || event.Kind == SessionEventAssistantTextDelta ||
		event.Kind == SessionEventThinkingDelta || event.Kind == SessionEventAssistantCompleted || event.Kind == SessionEventToolPlanned
	if !carriesMessageID && event.MessageID != "" {
		return fmt.Errorf("event kind %q cannot carry a message id", event.Kind)
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
	activeAssistantRunID := ""
	activeAssistantMessageID := ""
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
		if index > 0 && event.Sequence != previous+1 {
			return fmt.Errorf("event %d sequence is not contiguous", index)
		}
		if batch.FirstSequence > 0 && event.Sequence < batch.FirstSequence || batch.LastSequence > 0 && event.Sequence > batch.LastSequence {
			return fmt.Errorf("event %d sequence is outside the retention range", index)
		}
		if event.RunID != activeAssistantRunID || event.Kind == SessionEventRunStarted {
			activeAssistantRunID = event.RunID
			activeAssistantMessageID = ""
		}
		if event.MessageID != "" {
			if activeAssistantMessageID == "" {
				activeAssistantMessageID = event.MessageID
			} else if event.MessageID != activeAssistantMessageID {
				return fmt.Errorf("event %d assistant message id %q does not match active message %q", index, event.MessageID, activeAssistantMessageID)
			}
			if event.Kind == SessionEventAssistantCompleted {
				activeAssistantMessageID = ""
			}
		}
		if event.Kind == SessionEventRunFinished {
			activeAssistantMessageID = ""
		}
		previous = event.Sequence
	}
	return nil
}
