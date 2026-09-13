package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"strings"
	"sync"
	"unicode"
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
	EventRunStarted           EventKind = "run.started"
	EventUserMessage          EventKind = "message.user"
	EventAssistantStarted     EventKind = "assistant.started"
	EventAssistantTextDelta   EventKind = "assistant.text.delta"
	EventThinkingDelta        EventKind = "assistant.thinking.delta"
	EventAssistantCompleted   EventKind = "assistant.completed"
	EventToolPlanned          EventKind = "tool.planned"
	EventToolStarted          EventKind = "tool.started"
	EventToolUpdated          EventKind = "tool.updated"
	EventToolCompleted        EventKind = "tool.completed"
	EventCompactionStarted    EventKind = "compaction.started"
	EventCompactionCompleted  EventKind = "compaction.completed"
	EventCompactionFailed     EventKind = "compaction.failed"
	EventContextUpdated       EventKind = "context.updated"
	EventUsageUpdated         EventKind = "usage.updated"
	EventRunFinished          EventKind = "run.finished"
	EventSessionRenamed       EventKind = "session.renamed"
	EventSubagentChanged      EventKind = "subagent.changed"
	EventInteractionRequested EventKind = "interaction.requested"
	EventInteractionResolved  EventKind = "interaction.resolved"
)

// NewEvent is a live session update awaiting a runtime-local stream sequence.
type NewEvent struct {
	SessionID              string
	TurnID                 string
	RunID                  string
	MessageID              string
	Kind                   EventKind
	ContentIndex           int
	Delta                  string
	Text                   string
	Thinking               string
	ToolCallID             string
	ToolName               string
	Arguments              string
	ArgumentsTruncated     bool
	Content                []TranscriptContent
	ContentTruncated       bool
	Details                json.RawMessage
	DetailsOmitted         bool
	IsError                bool
	Status                 RunStatus
	ErrorKind              ProviderErrorKind
	ErrorMessage           string
	ContextTokens          int
	ContextWindow          int
	Usage                  *SessionUsage
	SessionName            string
	SubagentConversationID string
	SubagentTaskID         string
	Interaction            *InteractionRequest
	InteractionID          string
	InteractionResolution  string
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
	UsageBaseline  *SessionUsage
	Events         []Event
}

func validateSessionUsage(usage SessionUsage) error {
	for name, value := range map[string]int{
		"input": usage.Input, "output": usage.Output,
		"cache read": usage.CacheRead, "cache write": usage.CacheWrite,
		"reasoning": usage.Reasoning, "total": usage.TotalTokens,
	} {
		if value < 0 {
			return fmt.Errorf("session usage %s cannot be negative", name)
		}
	}
	for name, value := range map[string]float64{
		"input cost": usage.Cost.Input, "output cost": usage.Cost.Output,
		"cache read cost": usage.Cost.CacheRead, "cache write cost": usage.Cost.CacheWrite,
		"total cost": usage.Cost.Total,
	} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("session usage %s is invalid", name)
		}
	}
	return nil
}

// Validate checks that an event is safe to persist and project to clients.
func (event NewEvent) Validate() error {
	if event.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if event.Kind == EventSessionRenamed {
		if event.TurnID != "" || event.RunID != "" {
			return fmt.Errorf("session rename event cannot carry parent turn identity")
		}
	} else if event.Kind == EventSubagentChanged {
		if event.TurnID != "" || event.RunID != "" || event.SubagentConversationID == "" {
			return fmt.Errorf("subagent event requires conversation identity without parent turn identity")
		}
	} else {
		if event.TurnID == "" || event.RunID == "" {
			return fmt.Errorf("turn and run ids are required")
		}
		if event.RunID != event.TurnID {
			return fmt.Errorf("run identity must equal droid turn identity")
		}
		if event.SubagentConversationID != "" || event.SubagentTaskID != "" {
			return fmt.Errorf("parent run event cannot carry subagent identity")
		}
	}
	if len(event.Content) > maxLiveEventContentBlocks {
		return fmt.Errorf("event tool content exceeds %d blocks", maxLiveEventContentBlocks)
	}
	payloadBytes := len(event.Delta) + len(event.Text) + len(event.Thinking) + len(event.Arguments) + len(event.Details) + len(event.ErrorMessage) + len(event.SessionName) + len(event.InteractionID) + len(event.InteractionResolution)
	if event.Interaction != nil {
		raw, err := json.Marshal(event.Interaction)
		if err != nil {
			return fmt.Errorf("encode interaction event: %w", err)
		}
		payloadBytes += len(raw)
	}
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
	case EventCompactionStarted, EventCompactionCompleted:
	case EventCompactionFailed:
		if event.ErrorKind != "" || event.ErrorMessage == "" || rendererSafeLiveError(event.ErrorMessage) != event.ErrorMessage {
			return fmt.Errorf("failed compaction requires a renderer-safe error message")
		}
	case EventContextUpdated:
		if event.ContextTokens < 0 || event.ContextWindow <= 0 {
			return fmt.Errorf("context update requires non-negative tokens and a positive window")
		}
	case EventUsageUpdated:
		if event.Usage == nil {
			return fmt.Errorf("usage update requires an absolute session total")
		}
		if err := validateSessionUsage(*event.Usage); err != nil {
			return err
		}
	case EventSessionRenamed:
		if strings.TrimSpace(event.SessionName) != event.SessionName || !validSessionName(event.SessionName) || event.SessionName == "" {
			return fmt.Errorf("session rename event requires a renderer-safe name")
		}
	case EventSubagentChanged:
	case EventInteractionRequested:
		if event.Interaction == nil || event.InteractionID != "" || event.Interaction.ID == "" || event.Interaction.SessionID != event.SessionID || event.Interaction.RunID != event.RunID {
			return fmt.Errorf("interaction request event is invalid")
		}
		if err := validateInteractionRequest(*event.Interaction); err != nil {
			return err
		}
	case EventInteractionResolved:
		if event.Interaction != nil || !identifier.Valid(event.InteractionID, "interaction_") || !validInteractionResolution(event.InteractionResolution) {
			return fmt.Errorf("interaction resolution event is invalid")
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
	if event.Kind == EventSessionRenamed {
		if payloadBytes != len(event.SessionName) || event.MessageID != "" || event.Status != "" || event.ErrorKind != "" || event.Usage != nil ||
			event.ContextTokens != 0 || event.ContextWindow != 0 || event.SubagentConversationID != "" || event.SubagentTaskID != "" ||
			event.IsError || event.ArgumentsTruncated || event.ContentTruncated || event.DetailsOmitted {
			return fmt.Errorf("session rename event carries invalid payload")
		}
	}
	if event.Kind == EventSubagentChanged {
		if !identifier.Valid(event.SubagentConversationID, "subagent_") ||
			(event.SubagentTaskID != "" && !identifier.Valid(event.SubagentTaskID, "task_")) ||
			payloadBytes != 0 || event.MessageID != "" || event.Status != "" || event.ErrorKind != "" || event.Usage != nil ||
			event.ContextTokens != 0 || event.ContextWindow != 0 || event.IsError || event.ArgumentsTruncated || event.ContentTruncated || event.DetailsOmitted {
			return fmt.Errorf("subagent event carries invalid identity or payload")
		}
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
	if event.Kind != EventRunFinished && event.Kind != EventCompactionFailed && (event.ErrorKind != "" || event.ErrorMessage != "") {
		return fmt.Errorf("event kind %q cannot carry run error metadata", event.Kind)
	}
	if event.Kind != EventUsageUpdated && event.Usage != nil {
		return fmt.Errorf("event kind %q cannot carry session usage", event.Kind)
	}
	if event.Kind != EventContextUpdated && (event.ContextTokens != 0 || event.ContextWindow != 0) {
		return fmt.Errorf("event kind %q cannot carry context usage", event.Kind)
	}
	if event.Kind != EventSessionRenamed && event.SessionName != "" {
		return fmt.Errorf("event kind %q cannot carry a session name", event.Kind)
	}
	if event.Kind != EventInteractionRequested && event.Interaction != nil {
		return fmt.Errorf("event kind %q cannot carry an interaction request", event.Kind)
	}
	if event.Kind != EventInteractionResolved && (event.InteractionID != "" || event.InteractionResolution != "") {
		return fmt.Errorf("event kind %q cannot carry interaction resolution data", event.Kind)
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

func validInteractionResolution(reason string) bool {
	switch reason {
	case "answered", "negative_confirmation", "user_cancelled", "run_abort", "deleted", "shutdown", "unavailable":
		return true
	default:
		return false
	}
}

type eventLog struct {
	mu              sync.Mutex
	streamID        string
	next            int64
	events          []Event
	replayAvailable bool
	tailFrom        int64
	changed         chan struct{}
}

func newEventLog() (*eventLog, error) {
	id, err := identifier.New("stream_")
	if err != nil {
		return nil, err
	}
	return &eventLog{streamID: id, next: 1, replayAvailable: true, changed: make(chan struct{})}, nil
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
	log.tailFrom = 0
	log.signalChangedLocked()
	log.mu.Unlock()
	return nil
}

func (log *eventLog) replace(replacement *eventLog) {
	replacement.mu.Lock()
	streamID := replacement.streamID
	next := replacement.next
	replayAvailable := replacement.replayAvailable
	tailFrom := replacement.tailFrom
	replacement.mu.Unlock()

	log.mu.Lock()
	log.streamID = streamID
	log.next = next
	log.events = nil
	log.replayAvailable = replayAvailable
	log.tailFrom = tailFrom
	log.signalChangedLocked()
	log.mu.Unlock()
}

func (log *eventLog) append(events []NewEvent) error {
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	var previousUsage *SessionUsage
	for index := len(log.events) - 1; index >= 0; index-- {
		if log.events[index].Usage != nil {
			copy := *log.events[index].Usage
			previousUsage = &copy
			break
		}
	}
	for _, event := range events {
		if event.Usage == nil {
			continue
		}
		if previousUsage != nil && sessionUsageDecreased(*previousUsage, *event.Usage) {
			return fmt.Errorf("session usage decreased within event stream")
		}
		copy := *event.Usage
		previousUsage = &copy
	}
	for _, event := range events {
		log.events = append(log.events, Event{NewEvent: event, StreamID: log.streamID, Sequence: log.next})
		log.next++
	}
	const retained = 4096
	if len(log.events) > retained {
		log.events = append([]Event(nil), log.events[len(log.events)-retained:]...)
		log.replayAvailable = false
		log.tailFrom = log.events[0].Sequence - 1
	}
	if len(events) > 0 {
		log.signalChangedLocked()
	}
	return nil
}

func (log *eventLog) invalidate() {
	streamID, err := identifier.New("stream_")
	log.mu.Lock()
	if err != nil {
		// Preserve a canonical, guaranteed-distinct identity even if the random
		// identifier source is unavailable during recovery.
		suffix := []byte(strings.TrimPrefix(log.streamID, "stream_"))
		if suffix[0] == '0' {
			suffix[0] = '1'
		} else {
			suffix[0] = '0'
		}
		streamID = "stream_" + string(suffix)
	}
	log.streamID = streamID
	log.next = 1
	log.events = nil
	log.tailFrom = 0
	log.replayAvailable = false
	log.signalChangedLocked()
	log.mu.Unlock()
}

func (log *eventLog) signalChangedLocked() {
	close(log.changed)
	log.changed = make(chan struct{})
}

func sessionUsageDecreased(before, after SessionUsage) bool {
	return after.Input < before.Input || after.Output < before.Output ||
		after.CacheRead < before.CacheRead || after.CacheWrite < before.CacheWrite ||
		after.Reasoning < before.Reasoning || after.TotalTokens < before.TotalTokens ||
		after.Cost.Input < before.Cost.Input || after.Cost.Output < before.Cost.Output ||
		after.Cost.CacheRead < before.Cost.CacheRead || after.Cost.CacheWrite < before.Cost.CacheWrite ||
		after.Cost.Total < before.Cost.Total
}

func (log *eventLog) page(expectedStream string, after int64) EventPage {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.pageLocked(expectedStream, after)
}

func (log *eventLog) pageLocked(expectedStream string, after int64) EventPage {
	page := EventPage{StreamID: log.streamID, LastSequence: log.next - 1}
	if expectedStream != "" && expectedStream != log.streamID {
		page.ResyncRequired = true
		return page
	}
	if !log.replayAvailable && expectedStream == "" {
		page.ResyncRequired = true
		return page
	}
	if len(log.events) > 0 {
		page.FirstSequence = log.events[0].Sequence
	}
	if !log.replayAvailable && after < log.tailFrom {
		page.ResyncRequired = true
		return page
	}
	if after > page.LastSequence || page.FirstSequence > after+1 {
		page.ResyncRequired = true
		return page
	}
	for _, event := range log.events {
		if event.Sequence <= after && event.Usage != nil {
			copy := *event.Usage
			page.UsageBaseline = &copy
		}
		if event.Sequence > after {
			page.Events = append(page.Events, event)
			if len(page.Events) == 32 {
				break
			}
		}
	}
	return page
}

func (log *eventLog) wait(ctx context.Context, expectedStream string, after int64) (EventPage, error) {
	for {
		log.mu.Lock()
		page := log.pageLocked(expectedStream, after)
		if page.ResyncRequired || len(page.Events) > 0 {
			log.mu.Unlock()
			return page, nil
		}
		changed := log.changed
		log.mu.Unlock()
		select {
		case <-ctx.Done():
			return EventPage{}, ctx.Err()
		case <-changed:
		}
	}
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

// WaitEvents waits until a bounded event page or resynchronization signal is available.
func (m *Manager) WaitEvents(ctx context.Context, sessionID, streamID string, after int64) (EventPage, error) {
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
	return loaded.events.wait(ctx, streamID, after)
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
		projectToolImagePresentation(base.Content, typed.ToolName, typed.IsError || base.DetailsOmitted, base.Details)
		return []NewEvent{base}
	case droids.LifecycleEvent:
		switch typed.Kind {
		case "compaction.started":
			base.Kind = EventCompactionStarted
		case "compaction.completed":
			base.Kind = EventCompactionCompleted
		case "compaction.failed":
			var data struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(typed.Data, &data) != nil || strings.TrimSpace(data.Error) == "" {
				data.Error = "Context compaction failed"
			}
			base.Kind = EventCompactionFailed
			base.ErrorMessage = rendererSafeLiveError(data.Error)
		case "context.updated":
			var data struct {
				EstimatedInput int `json:"estimated_input"`
				ContextWindow  int `json:"context_window"`
			}
			if json.Unmarshal(typed.Data, &data) != nil || data.EstimatedInput < 0 || data.ContextWindow <= 0 {
				return nil
			}
			base.Kind = EventContextUpdated
			base.ContextTokens = data.EstimatedInput
			base.ContextWindow = data.ContextWindow
		default:
			return nil
		}
		return []NewEvent{base}
	case droids.UsageUpdated:
		usage := projectSessionUsage(typed.Usage)
		base.Kind = EventUsageUpdated
		base.Usage = &usage
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
		if block.Text != "" || !validLiveMediaType(block.MediaType, true) {
			return fmt.Errorf("image content requires an image media type without text")
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

func rendererSafeLiveError(text string) string {
	text = strings.ToValidUTF8(text, "�")
	text = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return ' '
		}
		return character
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "Context compaction failed"
	}
	if len(text) <= maxLiveEventTextBytes {
		return text
	}
	end := maxLiveEventTextBytes - len("…")
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "…"
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
