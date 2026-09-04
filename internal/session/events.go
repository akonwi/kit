package session

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
)

// EventKind identifies one renderer-neutral live session update.
type EventKind string

const (
	maxEventPayloadBytes  = 128 << 10
	maxLiveEventTextBytes = 64 << 10
)

const (
	EventRunStarted         EventKind = "run.started"
	EventUserMessage        EventKind = "message.user"
	EventAssistantStarted   EventKind = "assistant.started"
	EventAssistantTextDelta EventKind = "assistant.text.delta"
	EventThinkingDelta      EventKind = "assistant.thinking.delta"
	EventAssistantCompleted EventKind = "assistant.completed"
	EventToolPlanned        EventKind = "tool.planned"
	EventToolStarted        EventKind = "tool.started"
	EventToolUpdated        EventKind = "tool.updated"
	EventToolCompleted      EventKind = "tool.completed"
	EventRunFinished        EventKind = "run.finished"
)

// NewEvent is a live session update awaiting a durable stream sequence.
type NewEvent struct {
	SessionID          string
	TurnID             string
	RunID              string
	MessageID          string
	Kind               EventKind
	ContentIndex       int
	Delta              string
	Text               string
	Thinking           string
	ToolCallID         string
	ToolName           string
	Arguments          string
	ArgumentsTruncated bool
	IsError            bool
	Status             RunStatus
	ErrorKind          ProviderErrorKind
	ErrorMessage       string
}

// Event is one durable, ordered live session update.
type Event struct {
	NewEvent
	StreamID string
	Sequence int64
}

// EventPage contains ordered updates after a caller's last seen sequence.
type EventPage struct {
	StreamID       string
	FirstSequence  int64
	LastSequence   int64
	ResyncRequired bool
	Events         []Event
}

// Validate checks that an event is safe to persist and project to clients.
func (event NewEvent) Validate() error {
	if event.SessionID == "" || event.TurnID == "" || event.RunID == "" {
		return fmt.Errorf("session, turn, and run ids are required")
	}
	if len(event.Delta)+len(event.Text)+len(event.Thinking)+len(event.Arguments)+len(event.ErrorMessage) > maxEventPayloadBytes {
		return fmt.Errorf("event payload exceeds 128 KiB")
	}
	switch event.Kind {
	case EventRunStarted:
		if event.Status != RunStatusRunning {
			return fmt.Errorf("started run status must be running")
		}
	case EventUserMessage:
		if strings.TrimSpace(event.Text) == "" {
			return fmt.Errorf("user message text is required")
		}
	case EventAssistantStarted, EventAssistantCompleted:
		if event.MessageID == "" {
			return fmt.Errorf("assistant event requires a message id")
		}
	case EventAssistantTextDelta, EventThinkingDelta:
		if event.MessageID == "" || event.ContentIndex < 0 || event.Delta == "" {
			return fmt.Errorf("assistant delta requires a message id, content index, and text")
		}
	case EventToolPlanned, EventToolStarted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("tool start requires call id and name")
		}
		if event.Kind == EventToolPlanned && event.MessageID == "" {
			return fmt.Errorf("planned tool call requires an assistant message id")
		}
		if event.Kind == EventToolPlanned && (event.Arguments != "" || event.ArgumentsTruncated) {
			return fmt.Errorf("planned tool call cannot carry complete arguments")
		}
		if event.Kind == EventToolStarted && (event.Arguments == "") == !event.ArgumentsTruncated {
			return fmt.Errorf("started tool call requires either complete or explicitly truncated arguments")
		}
	case EventToolUpdated, EventToolCompleted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("tool event requires call id and name")
		}
	case EventRunFinished:
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
	if event.Kind != EventRunStarted && event.Kind != EventRunFinished && event.Status != "" {
		return fmt.Errorf("event kind %q cannot carry run status", event.Kind)
	}
	if event.Kind != EventRunFinished && (event.ErrorKind != "" || event.ErrorMessage != "") {
		return fmt.Errorf("event kind %q cannot carry run error metadata", event.Kind)
	}
	isTool := event.Kind == EventToolPlanned || event.Kind == EventToolStarted || event.Kind == EventToolUpdated || event.Kind == EventToolCompleted
	if !isTool && (event.ToolCallID != "" || event.ToolName != "" || event.Arguments != "" || event.ArgumentsTruncated || event.IsError) {
		return fmt.Errorf("event kind %q cannot carry tool data", event.Kind)
	}
	if event.Kind != EventToolPlanned && event.Kind != EventToolStarted && (event.Arguments != "" || event.ArgumentsTruncated) {
		return fmt.Errorf("event kind %q cannot carry tool arguments", event.Kind)
	}
	carriesMessageID := event.Kind == EventAssistantStarted || event.Kind == EventAssistantTextDelta ||
		event.Kind == EventThinkingDelta || event.Kind == EventAssistantCompleted || event.Kind == EventToolPlanned
	if !carriesMessageID && event.MessageID != "" {
		return fmt.Errorf("event kind %q cannot carry a message id", event.Kind)
	}
	return nil
}

// Events returns a bounded page of session updates after sequence.
func (m *Manager) Events(ctx context.Context, sessionID string, after int64) (EventPage, error) {
	if err := m.beginOperation(); err != nil {
		return EventPage{}, err
	}
	defer m.ops.Done()
	if strings.TrimSpace(sessionID) == "" || after < 0 {
		return EventPage{}, fmt.Errorf("%w: session id and non-negative sequence are required", ErrInvalidInput)
	}
	return m.store.ListSessionEvents(ctx, sessionID, after, 32)
}

func projectDroidEvent(sessionID, turnID, runID string, event droids.Event) []NewEvent {
	base := NewEvent{SessionID: sessionID, TurnID: turnID, RunID: runID}
	switch typed := event.(type) {
	case droids.MessageStart:
		switch message := typed.Message.(type) {
		case droids.UserMessage:
			base.Kind = EventUserMessage
			base.Text = boundedLiveText(contentText(message.Content))
			return []NewEvent{base}
		case droids.AssistantMessage:
			if message.ID == "" {
				return nil
			}
			base.Kind = EventAssistantStarted
			base.MessageID = message.ID
			return []NewEvent{base}
		default:
			return nil
		}
	case droids.MessageDelta:
		if typed.MessageID == "" {
			return nil
		}
		base.MessageID = typed.MessageID
		switch delta := typed.Stream.(type) {
		case droids.StreamTextDelta:
			if delta.Delta == "" {
				return nil
			}
			base.Kind = EventAssistantTextDelta
			base.ContentIndex = delta.ContentIndex
			return splitLiveDelta(base, delta.Delta)
		case droids.StreamThinkingDelta:
			if delta.Delta == "" {
				return nil
			}
			base.Kind = EventThinkingDelta
			base.ContentIndex = delta.ContentIndex
			return splitLiveDelta(base, delta.Delta)
		case droids.StreamToolCallStart:
			if delta.ID == "" || delta.Name == "" {
				return nil
			}
			base.Kind = EventToolPlanned
			base.ContentIndex = delta.ContentIndex
			base.ToolCallID = delta.ID
			base.ToolName = delta.Name
			return []NewEvent{base}
		}
	case droids.MessageEnd:
		message, ok := typed.Message.(droids.AssistantMessage)
		if !ok || message.ID == "" {
			return nil
		}
		base.Kind = EventAssistantCompleted
		base.MessageID = message.ID
		return []NewEvent{base}
	case droids.ToolExecutionStart:
		base.Kind = EventToolStarted
		base.ToolCallID = typed.ToolCallID
		base.ToolName = typed.ToolName
		base.Arguments, base.ArgumentsTruncated = presentationToolArguments(typed.Arguments)
		return []NewEvent{base}
	case droids.ToolExecutionUpdate:
		base.Kind = EventToolUpdated
		base.ToolCallID = typed.ToolCallID
		base.ToolName = typed.ToolName
		base.Text = boundedLiveText(contentText(typed.PartialResult.Content))
		base.IsError = typed.PartialResult.IsError
		return []NewEvent{base}
	case droids.ToolExecutionEnd:
		base.Kind = EventToolCompleted
		base.ToolCallID = typed.ToolCallID
		base.ToolName = typed.ToolName
		base.Text = boundedLiveText(contentText(typed.Result.Content))
		base.IsError = typed.IsError
		return []NewEvent{base}
	default:
		return nil
	}
	return nil
}

func splitLiveDelta(base NewEvent, delta string) []NewEvent {
	result := make([]NewEvent, 0, len(delta)/maxLiveEventTextBytes+1)
	for len(delta) > 0 {
		end := min(len(delta), maxLiveEventTextBytes)
		for end > 0 && end < len(delta) && !utf8.RuneStart(delta[end]) {
			end--
		}
		if end == 0 {
			_, end = utf8.DecodeRuneInString(delta)
		}
		event := base
		event.Delta = delta[:end]
		result = append(result, event)
		delta = delta[end:]
	}
	return result
}

func boundedLiveText(text string) string {
	if len(text) <= maxLiveEventTextBytes {
		return text
	}
	end := maxLiveEventTextBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "\n… live output truncated"
}

func assistantPresentation(message droids.AssistantMessage) (string, string) {
	var textParts, thinkingParts []string
	for _, block := range message.Content {
		switch typed := block.(type) {
		case droids.TextContent:
			if typed.Text != "" {
				textParts = append(textParts, typed.Text)
			}
		case droids.ThinkingContent:
			if !typed.Redacted && typed.Thinking != "" {
				thinkingParts = append(thinkingParts, typed.Thinking)
			}
		}
	}
	return strings.Join(textParts, "\n"), strings.Join(thinkingParts, "\n")
}
