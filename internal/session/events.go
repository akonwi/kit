package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
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

// NewEvent is a live session update awaiting a runtime-local stream sequence.
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
	Content            []TranscriptContent
	ContentTruncated   bool
	Details            json.RawMessage
	DetailsOmitted     bool
	IsError            bool
	Status             RunStatus
	ErrorKind          ProviderErrorKind
	ErrorMessage       string
}

// Event is one ordered live update retained by a loaded runtime.
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
	if event.RunID != event.TurnID {
		return fmt.Errorf("run identity must equal droid turn identity")
	}
	if len(event.Content) > maxLiveEventContentBlocks {
		return fmt.Errorf("event tool content exceeds %d blocks", maxLiveEventContentBlocks)
	}
	payloadBytes := len(event.Delta) + len(event.Text) + len(event.Thinking) + len(event.Arguments) + len(event.Details) + len(event.ErrorMessage)
	for _, block := range event.Content {
		payloadBytes += len(block.Text) + len(block.ToolCallID) + len(block.ToolName) + len(block.Arguments) + len(block.Filename) + len(block.MediaType)
	}
	if payloadBytes > maxEventPayloadBytes {
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
			return fmt.Errorf("tool event requires call id and name")
		}
		if event.Kind == EventToolPlanned && event.MessageID == "" {
			return fmt.Errorf("planned tool call requires an assistant message id")
		}
		if (event.Arguments == "") == !event.ArgumentsTruncated {
			return fmt.Errorf("tool event requires either complete or explicitly truncated arguments")
		}
	case EventToolUpdated:
		if event.ToolCallID == "" || event.ToolName == "" || len(event.Content) == 0 {
			return fmt.Errorf("tool update requires call id, name, and append-only content")
		}
		if event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted {
			return fmt.Errorf("tool update cannot carry final result metadata")
		}
	case EventToolCompleted:
		if event.ToolCallID == "" || event.ToolName == "" {
			return fmt.Errorf("completed tool requires call id and name")
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
	if !isTool && (event.ToolCallID != "" || event.ToolName != "" || event.Arguments != "" || event.ArgumentsTruncated || len(event.Content) > 0 || event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted || event.IsError) {
		return fmt.Errorf("event kind %q cannot carry tool data", event.Kind)
	}
	if isTool && (event.Text != "" || event.Thinking != "" || event.Delta != "") {
		return fmt.Errorf("tool event cannot carry flattened text or assistant deltas")
	}
	if event.Kind != EventToolPlanned && event.Kind != EventToolStarted && (event.Arguments != "" || event.ArgumentsTruncated) {
		return fmt.Errorf("event kind %q cannot carry tool arguments", event.Kind)
	}
	if event.Kind != EventToolUpdated && event.Kind != EventToolCompleted && (len(event.Content) > 0 || event.ContentTruncated || len(event.Details) > 0 || event.DetailsOmitted) {
		return fmt.Errorf("event kind %q cannot carry tool result data", event.Kind)
	}
	if event.DetailsOmitted && len(event.Details) > 0 {
		return fmt.Errorf("event cannot carry details and mark them omitted")
	}
	if len(event.Details) > 0 && !json.Valid(event.Details) {
		return fmt.Errorf("tool details are not valid JSON")
	}
	for index, block := range event.Content {
		if err := validateLiveToolContent(block); err != nil {
			return fmt.Errorf("tool content block %d: %w", index, err)
		}
	}
	carriesMessageID := event.Kind == EventAssistantStarted || event.Kind == EventAssistantTextDelta ||
		event.Kind == EventThinkingDelta || event.Kind == EventAssistantCompleted || event.Kind == EventToolPlanned
	if !carriesMessageID && event.MessageID != "" {
		return fmt.Errorf("event kind %q cannot carry a message id", event.Kind)
	}
	return nil
}

type eventLog struct {
	mu              sync.Mutex
	streamID        string
	next            int64
	events          []Event
	replayAvailable bool
}

func newEventLog() (*eventLog, error) {
	id, err := identifier.New("stream_")
	if err != nil {
		return nil, err
	}
	return &eventLog{streamID: id, next: 1, replayAvailable: true}, nil
}

func (log *eventLog) reset() error {
	id, err := identifier.New("stream_")
	if err != nil {
		return err
	}
	log.mu.Lock()
	log.streamID = id
	log.next = 1
	log.events = nil
	log.replayAvailable = true
	log.mu.Unlock()
	return nil
}

func (log *eventLog) append(events []NewEvent) error {
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, event := range events {
		log.events = append(log.events, Event{NewEvent: event, StreamID: log.streamID, Sequence: log.next})
		log.next++
	}
	const retained = 4096
	if len(log.events) > retained {
		log.events = append([]Event(nil), log.events[len(log.events)-retained:]...)
		log.replayAvailable = false
	}
	return nil
}

func (log *eventLog) invalidate() {
	log.mu.Lock()
	log.replayAvailable = false
	log.mu.Unlock()
}

func (log *eventLog) page(expectedStream string, after int64) EventPage {
	log.mu.Lock()
	defer log.mu.Unlock()
	page := EventPage{StreamID: log.streamID, LastSequence: log.next - 1}
	if !log.replayAvailable || expectedStream != "" && expectedStream != log.streamID {
		page.ResyncRequired = true
		return page
	}
	if len(log.events) > 0 {
		page.FirstSequence = log.events[0].Sequence
	}
	if after > page.LastSequence || page.FirstSequence > after+1 {
		page.ResyncRequired = true
		return page
	}
	for _, event := range log.events {
		if event.Sequence > after {
			page.Events = append(page.Events, event)
			if len(page.Events) == 32 {
				break
			}
		}
	}
	return page
}

// Events returns a bounded page from the loaded runtime's transient stream.
func (m *Manager) Events(ctx context.Context, sessionID, streamID string, after int64) (EventPage, error) {
	if err := m.beginOperation(); err != nil {
		return EventPage{}, err
	}
	defer m.ops.Done()
	if strings.TrimSpace(sessionID) == "" || after < 0 {
		return EventPage{}, fmt.Errorf("%w: session id and non-negative sequence are required", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return EventPage{}, err
	}
	return loaded.events.page(streamID, after), nil
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
		case droids.StreamToolCallEnd:
			if delta.ToolCall.ID == "" || delta.ToolCall.Name == "" {
				return nil
			}
			base.Kind = EventToolPlanned
			base.ContentIndex = delta.ContentIndex
			base.ToolCallID = string(delta.ToolCall.ID)
			base.ToolName = delta.ToolCall.Name
			base.Arguments, base.ArgumentsTruncated = presentationToolArguments(delta.ToolCall.Arguments)
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
		base.ToolCallID = string(typed.ToolCallID)
		base.ToolName = typed.ToolName
		base.Arguments, base.ArgumentsTruncated = presentationToolArguments(typed.Arguments)
		return []NewEvent{base}
	case droids.ToolExecutionUpdate:
		base.Kind = EventToolUpdated
		base.ToolCallID = string(typed.ToolCallID)
		base.ToolName = typed.ToolName
		base.IsError = typed.Delta.IsError
		return projectToolContentDelta(base, typed.Delta.Content)
	case droids.ToolExecutionEnd:
		base.Kind = EventToolCompleted
		base.ToolCallID = string(typed.ToolCallID)
		base.ToolName = typed.ToolName
		base.Content, base.ContentTruncated = boundedLiveToolContent(typed.Result.Content)
		base.Details, base.DetailsOmitted = boundedLiveToolDetails(typed.Result.Details)
		base.IsError = typed.IsError
		return []NewEvent{base}
	default:
		return nil
	}
	return nil
}

func projectToolContentDelta(base NewEvent, content []droids.ResultContent) []NewEvent {
	projected, _ := boundedLiveToolContent(content)
	if len(projected) == 0 {
		return nil
	}
	base.Content = projected
	return []NewEvent{base}
}

const (
	maxLiveEventContentBytes  = 60 << 10
	maxLiveEventContentBlocks = 128
)

func boundedLiveToolContent(content []droids.ResultContent) ([]TranscriptContent, bool) {
	projected, err := projectDroidContent(content)
	if err != nil {
		return nil, true
	}
	result := make([]TranscriptContent, 0, min(len(projected), maxLiveEventContentBlocks))
	remaining := maxLiveEventContentBytes
	truncated := false
	for _, block := range projected {
		if len(result) == maxLiveEventContentBlocks {
			truncated = true
			break
		}
		if err := validateLiveToolContent(block); err != nil {
			truncated = true
			continue
		}
		size := liveToolContentSize(block)
		if size <= remaining {
			result = append(result, block)
			remaining -= size
			continue
		}
		if block.Kind == TranscriptContentText && remaining > 0 {
			end := min(len(block.Text), remaining)
			for end > 0 && end < len(block.Text) && !utf8.RuneStart(block.Text[end]) {
				end--
			}
			if end > 0 {
				block.Text = block.Text[:end]
				result = append(result, block)
			}
		}
		truncated = true
		break
	}
	return result, truncated
}

func liveToolContentSize(block TranscriptContent) int {
	return len(block.Kind) + len(block.Text) + len(block.Filename) + len(block.MediaType)
}

const maxLiveEventDetailsBytes = 48 << 10

func boundedLiveToolDetails(details json.RawMessage) (json.RawMessage, bool) {
	if len(details) == 0 || bytes.Equal(details, []byte("null")) {
		return nil, false
	}
	if !json.Valid(details) || len(details) > maxLiveEventDetailsBytes {
		return nil, true
	}
	return append(json.RawMessage(nil), details...), false
}

func validateLiveToolContent(block TranscriptContent) error {
	switch block.Kind {
	case TranscriptContentText:
		if block.Text == "" {
			return fmt.Errorf("text content is empty")
		}
	case TranscriptContentImage:
		if block.Text != "" || block.Filename != "" || !validLiveMediaType(block.MediaType, true) {
			return fmt.Errorf("image content requires an image media type only")
		}
	case TranscriptContentFile:
		if strings.TrimSpace(block.Filename) == "" || block.Text != "" || !validLiveMediaType(block.MediaType, false) {
			return fmt.Errorf("file content requires filename and media type")
		}
	default:
		return fmt.Errorf("kind %q is invalid for a tool result", block.Kind)
	}
	if block.ToolCallID != "" || block.ToolName != "" || block.Arguments != "" || block.ArgumentsTruncated {
		return fmt.Errorf("tool result content carries tool-call metadata")
	}
	return nil
}

func validLiveMediaType(raw string, imageOnly bool) bool {
	parsed, _, err := mime.ParseMediaType(raw)
	parts := strings.Split(parsed, "/")
	if err != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "*" || parts[1] == "*" {
		return false
	}
	return !imageOnly || strings.EqualFold(parts[0], "image")
}

func splitLiveDelta(base NewEvent, delta string) []NewEvent {
	result := make([]NewEvent, 0, len(delta)/maxLiveEventTextBytes+1)
	for len(delta) > 0 {
		end := liveTextChunkEnd(delta)
		event := base
		event.Delta = delta[:end]
		result = append(result, event)
		delta = delta[end:]
	}
	return result
}

func liveTextChunkEnd(text string) int {
	end := min(len(text), maxLiveEventTextBytes)
	for end > 0 && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	if end == 0 {
		_, end = utf8.DecodeRuneInString(text)
	}
	return end
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
