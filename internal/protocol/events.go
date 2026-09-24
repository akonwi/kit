package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	maxSessionEventPayloadBytes  = 128 << 10
	maxSessionEventContentBlocks = 128
	maxSessionEventPageBytes     = 512 << 10
)

// SessionEventKind identifies one live renderer-neutral session update.
type SessionEventKind string

const (
	SessionEventRunStarted             SessionEventKind = "run.started"
	SessionEventUserMessage            SessionEventKind = "message.user"
	SessionEventAssistantStarted       SessionEventKind = "assistant.started"
	SessionEventAssistantTextDelta     SessionEventKind = "assistant.text.delta"
	SessionEventThinkingDelta          SessionEventKind = "assistant.thinking.delta"
	SessionEventAssistantCompleted     SessionEventKind = "assistant.completed"
	SessionEventToolPlanned            SessionEventKind = "tool.planned"
	SessionEventToolStarted            SessionEventKind = "tool.started"
	SessionEventToolUpdated            SessionEventKind = "tool.updated"
	SessionEventToolCompleted          SessionEventKind = "tool.completed"
	SessionEventCompactionStarted      SessionEventKind = "compaction.started"
	SessionEventCompactionCompleted    SessionEventKind = "compaction.completed"
	SessionEventCompactionFailed       SessionEventKind = "compaction.failed"
	SessionEventProviderRetryScheduled SessionEventKind = "provider.retry.scheduled"
	SessionEventProviderRetryStarted   SessionEventKind = "provider.retry.started"
	SessionEventContextUpdated         SessionEventKind = "context.updated"
	SessionEventUsageUpdated           SessionEventKind = "usage.updated"
	SessionEventRunFinished            SessionEventKind = "run.finished"
	SessionEventSessionRenamed         SessionEventKind = "session.renamed"
	SessionEventSessionCWDChanged      SessionEventKind = "session.cwd.changed"
	SessionEventSubagentChanged        SessionEventKind = "subagent.changed"
	SessionEventPeerQueryChanged       SessionEventKind = "peer_query.changed"
	SessionEventInteractionRequested   SessionEventKind = "interaction.requested"
	SessionEventInteractionResolved    SessionEventKind = "interaction.resolved"
	SessionEventAnnotationCreated      SessionEventKind = "annotation.created"
	SessionEventAnnotationUpdated      SessionEventKind = "annotation.updated"
	SessionEventAnnotationDeleted      SessionEventKind = "annotation.deleted"
	SessionEventAnnotationSubmitted    SessionEventKind = "annotation.submitted"
	SessionEventScratchpadChanged      SessionEventKind = "scratchpad.changed"
)

// SessionEvent is one ordered update in a loaded runtime stream.
type SessionEvent struct {
	StreamID               string              `json:"streamId"`
	Sequence               int64               `json:"sequence"`
	SessionID              string              `json:"sessionId"`
	TurnID                 string              `json:"turnId"`
	RunID                  string              `json:"runId"`
	MessageID              string              `json:"messageId,omitempty"`
	Kind                   SessionEventKind    `json:"kind"`
	ContentIndex           int                 `json:"contentIndex,omitempty"`
	Delta                  string              `json:"delta,omitempty"`
	Text                   string              `json:"text,omitempty"`
	Thinking               string              `json:"thinking,omitempty"`
	ToolCallID             string              `json:"toolCallId,omitempty"`
	ToolName               string              `json:"toolName,omitempty"`
	Arguments              string              `json:"arguments,omitempty"`
	ArgumentsTruncated     bool                `json:"argumentsTruncated,omitempty"`
	Content                []TranscriptContent `json:"content,omitempty"`
	ContentTruncated       bool                `json:"contentTruncated,omitempty"`
	Details                json.RawMessage     `json:"details,omitempty"`
	DetailsOmitted         bool                `json:"detailsOmitted,omitempty"`
	IsError                bool                `json:"isError,omitempty"`
	Status                 RunStatus           `json:"status,omitempty"`
	ErrorKind              ProviderErrorKind   `json:"errorKind,omitempty"`
	ErrorMessage           string              `json:"errorMessage,omitempty"`
	CompactionID           string              `json:"compactionId,omitempty"`
	ProviderRetry          *ProviderRetry      `json:"providerRetry,omitempty"`
	ContextTokens          int                 `json:"contextTokens,omitempty"`
	ContextWindow          int                 `json:"contextWindow,omitempty"`
	Usage                  *SessionUsage       `json:"usage,omitempty"`
	SessionName            string              `json:"sessionName,omitempty"`
	Workspace              *WorkspaceRef       `json:"workspace,omitempty"`
	SubagentConversationID string              `json:"subagentConversationId,omitempty"`
	SubagentTaskID         string              `json:"subagentTaskId,omitempty"`
	PeerRequestID          string              `json:"peerRequestId,omitempty"`
	Interaction            *InteractionRequest `json:"interaction,omitempty"`
	InteractionID          string              `json:"interactionId,omitempty"`
	InteractionResolution  string              `json:"interactionResolution,omitempty"`
	AnnotationID           uint64              `json:"annotationId,omitempty"`
	Annotation             *Annotation         `json:"annotation,omitempty"`
	AnnotationIDs          []uint64            `json:"annotationIds,omitempty"`
	AcceptedMessageID      string              `json:"acceptedMessageId,omitempty"`
	Scratchpad             *Scratchpad         `json:"scratchpad,omitempty"`
}

// SessionEventBatch is one bounded page after a client's cursor.
type SessionEventBatch struct {
	StreamID       string         `json:"streamId,omitempty"`
	FirstSequence  int64          `json:"firstSequence,omitempty"`
	LastSequence   int64          `json:"lastSequence,omitempty"`
	ResyncRequired bool           `json:"resyncRequired,omitempty"`
	UsageBaseline  *SessionUsage  `json:"usageBaseline,omitempty"`
	Events         []SessionEvent `json:"events"`
}

// Validate checks an event received across a transport boundary.
func (event SessionEvent) Validate() error {
	if event.StreamID == "" || event.Sequence < 1 {
		return fmt.Errorf("event stream id and positive sequence are required")
	}
	if event.SessionID == "" {
		return fmt.Errorf("event session id is required")
	}
	if event.Kind == SessionEventScratchpadChanged {
		if event.TurnID != "" || event.RunID != "" || event.Scratchpad == nil {
			return fmt.Errorf("scratchpad event requires a record without parent turn identity")
		}
	} else if event.Kind == SessionEventSessionRenamed || event.Kind == SessionEventSessionCWDChanged {
		if event.TurnID != "" || event.RunID != "" {
			return fmt.Errorf("session rename event cannot carry parent turn identity")
		}
	} else if event.Kind == SessionEventSubagentChanged {
		if event.TurnID != "" || event.RunID != "" || event.SubagentConversationID == "" {
			return fmt.Errorf("subagent event requires conversation identity without parent turn identity")
		}
	} else if event.Kind == SessionEventPeerQueryChanged {
		if event.TurnID != "" || event.RunID != "" || !identifier.Valid(event.PeerRequestID, "peer_") {
			return fmt.Errorf("peer query event requires request identity without turn identity")
		}
	} else if event.Kind == SessionEventAnnotationCreated || event.Kind == SessionEventAnnotationUpdated {
		if event.TurnID != "" || event.RunID != "" || event.AnnotationID == 0 || event.Annotation == nil || event.Annotation.ID != event.AnnotationID || event.Annotation.SessionID != event.SessionID {
			return fmt.Errorf("annotation change event is invalid")
		}
	} else if event.Kind == SessionEventAnnotationDeleted {
		if event.TurnID != "" || event.RunID != "" || event.AnnotationID == 0 || event.Annotation != nil {
			return fmt.Errorf("annotation deletion event is invalid")
		}
	} else if event.Kind == SessionEventAnnotationSubmitted {
		if event.TurnID != "" || event.RunID != "" || event.AnnotationID != 0 || event.Annotation != nil || !identifier.Valid(event.AcceptedMessageID, "message_") || len(event.AnnotationIDs) == 0 || len(event.AnnotationIDs) > MaxAnnotationsPerPrompt {
			return fmt.Errorf("annotation submission event is invalid")
		}
		seen := make(map[uint64]struct{}, len(event.AnnotationIDs))
		for _, id := range event.AnnotationIDs {
			if id == 0 {
				return fmt.Errorf("annotation submission id is invalid")
			}
			if _, duplicate := seen[id]; duplicate {
				return fmt.Errorf("annotation submission ids are duplicated")
			}
			seen[id] = struct{}{}
		}
	} else if (event.Kind == SessionEventInteractionRequested || event.Kind == SessionEventInteractionResolved) && event.RunID == "" {
		if event.TurnID != "" || event.SubagentConversationID != "" || event.SubagentTaskID != "" || event.PeerRequestID != "" || event.AnnotationID != 0 {
			return fmt.Errorf("invalid session interaction identity")
		}
		if event.Kind == SessionEventInteractionRequested && (event.Interaction == nil || event.Interaction.Plugin == nil) {
			return fmt.Errorf("session interaction requires plugin ownership")
		}
	} else {
		if event.TurnID == "" || event.RunID == "" {
			return fmt.Errorf("event turn and run ids are required")
		}
		if event.RunID != event.TurnID {
			return fmt.Errorf("event run identity must equal its droid turn identity")
		}
		if event.SubagentConversationID != "" || event.SubagentTaskID != "" || event.PeerRequestID != "" || event.AnnotationID != 0 {
			return fmt.Errorf("parent run event cannot carry external identity")
		}
	}
	if len(event.Content) > maxSessionEventContentBlocks {
		return fmt.Errorf("event tool content exceeds %d blocks", maxSessionEventContentBlocks)
	}
	payloadBytes := len(event.Delta) + len(event.Text) + len(event.Thinking) + len(event.Arguments) + len(event.Details) + len(event.ErrorMessage) + len(event.CompactionID) + len(event.SessionName) + len(event.InteractionID) + len(event.InteractionResolution) + len(event.AcceptedMessageID) + len(event.AnnotationIDs)*8
	if event.Scratchpad != nil {
		payloadBytes += len(event.Scratchpad.OwnerSessionID) + len(event.Scratchpad.Content) + 64
	}
	if event.Annotation != nil {
		raw, err := json.Marshal(event.Annotation)
		if err != nil {
			return fmt.Errorf("encode annotation event: %w", err)
		}
		payloadBytes += len(raw)
	}
	if event.Workspace != nil {
		raw, err := json.Marshal(event.Workspace)
		if err != nil {
			return err
		}
		payloadBytes += len(raw)
	}
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
	case SessionEventCompactionStarted, SessionEventCompactionCompleted:
		if !validRendererText(event.CompactionID, 256) {
			return fmt.Errorf("compaction event requires a valid identity")
		}
	case SessionEventCompactionFailed:
		if !validRendererText(event.CompactionID, 256) || event.ErrorKind != "" || !validRendererText(event.ErrorMessage, maxSessionEventPayloadBytes) {
			return fmt.Errorf("failed compaction requires an error message")
		}
	case SessionEventProviderRetryScheduled:
		if event.ProviderRetry == nil || event.ProviderRetry.Count <= 0 || event.ProviderRetry.RetryAt == "" {
			return fmt.Errorf("scheduled provider retry requires a positive count and deadline")
		}
		if _, err := time.Parse(time.RFC3339Nano, event.ProviderRetry.RetryAt); err != nil {
			return fmt.Errorf("scheduled provider retry deadline is invalid: %w", err)
		}
	case SessionEventProviderRetryStarted:
		if event.ProviderRetry == nil || event.ProviderRetry.Count <= 0 || event.ProviderRetry.RetryAt != "" {
			return fmt.Errorf("started provider retry requires a positive count without a deadline")
		}
	case SessionEventContextUpdated:
		if event.ContextTokens < 0 || event.ContextWindow <= 0 {
			return fmt.Errorf("context update requires non-negative tokens and a positive window")
		}
	case SessionEventUsageUpdated:
		if event.Usage == nil {
			return fmt.Errorf("usage update requires an absolute session total")
		}
		if err := event.Usage.Validate(); err != nil {
			return fmt.Errorf("usage update: %w", err)
		}
	case SessionEventSessionRenamed:
		if strings.TrimSpace(event.SessionName) != event.SessionName || !validRendererText(event.SessionName, 256) {
			return fmt.Errorf("session rename event requires a renderer-safe name")
		}
	case SessionEventSessionCWDChanged:
		if event.Workspace == nil || event.Workspace.Validate() != nil || event.Workspace.SessionID != event.SessionID {
			return fmt.Errorf("session cwd event requires a valid workspace")
		}
	case SessionEventScratchpadChanged:
		if err := event.Scratchpad.Validate(); err != nil {
			return fmt.Errorf("scratchpad event record: %w", err)
		}
	case SessionEventSubagentChanged:
		if !validRendererText(event.SubagentConversationID, 128) ||
			(event.SubagentTaskID != "" && !validRendererText(event.SubagentTaskID, 128)) || event.PeerRequestID != "" || payloadBytes != 0 {
			return fmt.Errorf("subagent event identity or payload is invalid")
		}
	case SessionEventPeerQueryChanged:
		if payloadBytes != 0 {
			return fmt.Errorf("peer query event payload is invalid")
		}
	case SessionEventAnnotationCreated, SessionEventAnnotationUpdated:
		if err := event.Annotation.Validate(); err != nil {
			return fmt.Errorf("annotation event record: %w", err)
		}
	case SessionEventAnnotationDeleted:
		if payloadBytes != 0 {
			return fmt.Errorf("annotation deletion event payload is invalid")
		}
	case SessionEventAnnotationSubmitted:
		if payloadBytes != len(event.AcceptedMessageID)+len(event.AnnotationIDs)*8 {
			return fmt.Errorf("annotation submission event payload is invalid")
		}
	case SessionEventInteractionRequested:
		if event.Interaction == nil || event.InteractionID != "" || event.Interaction.SessionID != event.SessionID || event.Interaction.RunID != event.RunID {
			return fmt.Errorf("interaction request event is invalid")
		}
		if err := event.Interaction.Validate(); err != nil {
			return err
		}
	case SessionEventInteractionResolved:
		if event.Interaction != nil || !identifier.Valid(event.InteractionID, "interaction_") || !validProtocolInteractionResolution(event.InteractionResolution) {
			return fmt.Errorf("interaction resolution event is invalid")
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
	if event.Kind == SessionEventSessionRenamed {
		if payloadBytes != len(event.SessionName) || event.MessageID != "" || event.Status != "" || event.ErrorKind != "" || event.Usage != nil ||
			event.ContextTokens != 0 || event.ContextWindow != 0 || event.SubagentConversationID != "" || event.SubagentTaskID != "" || event.PeerRequestID != "" ||
			event.IsError || event.ArgumentsTruncated || event.ContentTruncated || event.DetailsOmitted {
			return fmt.Errorf("session rename event carries invalid payload")
		}
	}
	if event.Kind == SessionEventSessionCWDChanged && (event.Workspace == nil || event.MessageID != "" || event.Status != "" || event.ErrorKind != "" || event.Usage != nil || event.ContextTokens != 0 || event.ContextWindow != 0 || event.IsError || event.ArgumentsTruncated || event.ContentTruncated || event.DetailsOmitted) {
		return fmt.Errorf("session cwd event carries invalid payload")
	}
	if event.Kind == SessionEventScratchpadChanged {
		scratchpadBytes := len(event.Scratchpad.OwnerSessionID) + len(event.Scratchpad.Content) + 64
		if payloadBytes != scratchpadBytes || event.MessageID != "" || event.Status != "" || event.ErrorKind != "" || event.Usage != nil ||
			event.ContextTokens != 0 || event.ContextWindow != 0 || event.SubagentConversationID != "" || event.SubagentTaskID != "" || event.PeerRequestID != "" ||
			event.IsError || event.ArgumentsTruncated || event.ContentTruncated || event.DetailsOmitted {
			return fmt.Errorf("scratchpad event carries invalid payload")
		}
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
	if event.Kind != SessionEventRunFinished && event.Kind != SessionEventCompactionFailed && (event.ErrorKind != "" || event.ErrorMessage != "") {
		return fmt.Errorf("event kind %q cannot carry run error metadata", event.Kind)
	}
	if event.Kind != SessionEventUsageUpdated && event.Usage != nil {
		return fmt.Errorf("event kind %q cannot carry session usage", event.Kind)
	}
	if event.Kind != SessionEventContextUpdated && (event.ContextTokens != 0 || event.ContextWindow != 0) {
		return fmt.Errorf("event kind %q cannot carry context usage", event.Kind)
	}
	isCompaction := event.Kind == SessionEventCompactionStarted || event.Kind == SessionEventCompactionCompleted || event.Kind == SessionEventCompactionFailed
	if !isCompaction && event.CompactionID != "" {
		return fmt.Errorf("event kind %q cannot carry compaction identity", event.Kind)
	}
	if isCompaction && (event.ContentIndex != 0 || event.Delta != "" || event.Text != "" || event.Thinking != "") {
		return fmt.Errorf("compaction event cannot carry assistant content")
	}
	isRetry := event.Kind == SessionEventProviderRetryScheduled || event.Kind == SessionEventProviderRetryStarted
	if !isRetry && event.ProviderRetry != nil {
		return fmt.Errorf("event kind %q cannot carry provider retry state", event.Kind)
	}
	if isRetry && (event.ContentIndex != 0 || event.Delta != "" || event.Text != "" || event.Thinking != "") {
		return fmt.Errorf("provider retry event cannot carry assistant content")
	}
	if event.Kind != SessionEventSessionRenamed && event.SessionName != "" {
		return fmt.Errorf("event kind %q cannot carry a session name", event.Kind)
	}
	if event.Kind != SessionEventSessionCWDChanged && event.Workspace != nil {
		return fmt.Errorf("event kind %q cannot carry a workspace", event.Kind)
	}
	if event.Kind != SessionEventInteractionRequested && event.Interaction != nil {
		return fmt.Errorf("event kind %q cannot carry an interaction", event.Kind)
	}
	if event.Kind != SessionEventInteractionResolved && (event.InteractionID != "" || event.InteractionResolution != "") {
		return fmt.Errorf("event kind %q cannot carry interaction resolution data", event.Kind)
	}
	if event.Kind != SessionEventScratchpadChanged && event.Scratchpad != nil {
		return fmt.Errorf("event kind %q cannot carry a scratchpad", event.Kind)
	}
	isAnnotation := event.Kind == SessionEventAnnotationCreated || event.Kind == SessionEventAnnotationUpdated || event.Kind == SessionEventAnnotationDeleted || event.Kind == SessionEventAnnotationSubmitted
	if !isAnnotation && (event.AnnotationID != 0 || event.Annotation != nil || len(event.AnnotationIDs) > 0 || event.AcceptedMessageID != "") {
		return fmt.Errorf("event kind %q cannot carry annotation data", event.Kind)
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

func validProtocolInteractionResolution(reason string) bool {
	switch reason {
	case "answered", "negative_confirmation", "user_cancelled", "run_abort", "deleted", "shutdown", "unavailable":
		return true
	default:
		return false
	}
}

// Validate checks event ordering and runtime-stream identity for a transport page.
func (batch SessionEventBatch) Validate() error {
	encoded, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode event batch: %w", err)
	}
	if len(encoded) > maxSessionEventPageBytes {
		return fmt.Errorf("event batch exceeds 512 KiB")
	}
	if batch.FirstSequence < 0 || batch.LastSequence < 0 ||
		(batch.FirstSequence == 0) != (batch.LastSequence == 0) ||
		batch.FirstSequence > batch.LastSequence {
		return fmt.Errorf("event retention range is invalid")
	}
	if batch.FirstSequence > 0 && batch.StreamID == "" {
		return fmt.Errorf("event retention range requires a stream id")
	}
	if batch.UsageBaseline != nil {
		if err := batch.UsageBaseline.Validate(); err != nil {
			return fmt.Errorf("event usage baseline: %w", err)
		}
	}
	if batch.ResyncRequired && (len(batch.Events) != 0 || batch.UsageBaseline != nil) {
		return fmt.Errorf("resync-required event batch must not contain updates or a usage baseline")
	}
	if len(batch.Events) > 0 && batch.FirstSequence == 0 {
		return fmt.Errorf("non-empty event batch requires a retention range")
	}
	previous := int64(0)
	activeAssistantRunID := ""
	activeAssistantMessageID := ""
	previousUsage := batch.UsageBaseline
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
		if event.Kind == SessionEventSessionRenamed || event.Kind == SessionEventSessionCWDChanged || event.Kind == SessionEventScratchpadChanged || event.Kind == SessionEventSubagentChanged || event.Kind == SessionEventPeerQueryChanged || event.Kind == SessionEventAnnotationCreated || event.Kind == SessionEventAnnotationUpdated || event.Kind == SessionEventAnnotationDeleted || event.Kind == SessionEventAnnotationSubmitted {
			previous = event.Sequence
			continue
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
		if event.Usage != nil {
			if previousUsage != nil && protocolUsageDecreased(*previousUsage, *event.Usage) {
				return fmt.Errorf("event %d session usage decreased", index)
			}
			copy := *event.Usage
			previousUsage = &copy
		}
		previous = event.Sequence
	}
	return nil
}

func protocolUsageDecreased(before, after SessionUsage) bool {
	return after.Input < before.Input || after.Output < before.Output ||
		after.CacheRead < before.CacheRead || after.CacheWrite < before.CacheWrite ||
		after.Reasoning < before.Reasoning || after.TotalTokens < before.TotalTokens ||
		after.Cost.Input < before.Cost.Input || after.Cost.Output < before.Cost.Output ||
		after.Cost.CacheRead < before.Cost.CacheRead || after.Cost.CacheWrite < before.Cost.CacheWrite ||
		after.Cost.Total < before.Cost.Total
}
