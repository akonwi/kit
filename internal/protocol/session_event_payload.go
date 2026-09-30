package protocol

import (
	"encoding/json"
	"fmt"
)

// SessionEventPayload is the kind-specific data of a SessionEvent.
type SessionEventPayload interface{ sessionEventKind() SessionEventKind }

// RunStartedEvent records that a run began.
type RunStartedEvent struct {
	Status RunStatus `json:"status"`
}

// UserMessageEvent records submitted user text.
type UserMessageEvent struct {
	Text string `json:"text"`
}

// AssistantStartedEvent identifies a new assistant message.
type AssistantStartedEvent struct {
	MessageID string `json:"messageId"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
}

// AssistantTextDeltaEvent appends visible assistant text.
type AssistantTextDeltaEvent struct {
	MessageID    string `json:"messageId"`
	ContentIndex int    `json:"contentIndex,omitempty"`
	Delta        string `json:"delta"`
}

// ThinkingDeltaEvent appends assistant thinking text.
type ThinkingDeltaEvent struct {
	MessageID    string `json:"messageId"`
	ContentIndex int    `json:"contentIndex,omitempty"`
	Delta        string `json:"delta"`
}

// AssistantCompletedEvent identifies a completed assistant message.
type AssistantCompletedEvent struct {
	MessageID string `json:"messageId"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
}

// ToolPlannedEvent describes a planned assistant tool call.
type ToolPlannedEvent struct {
	MessageID          string `json:"messageId"`
	ContentIndex       int    `json:"contentIndex,omitempty"`
	ToolCallID         string `json:"toolCallId"`
	ToolName           string `json:"toolName"`
	Arguments          string `json:"arguments,omitempty"`
	ArgumentsTruncated bool   `json:"argumentsTruncated,omitempty"`
}

// ToolStartedEvent describes a started tool call.
type ToolStartedEvent struct {
	ToolCallID         string `json:"toolCallId"`
	ToolName           string `json:"toolName"`
	Arguments          string `json:"arguments,omitempty"`
	ArgumentsTruncated bool   `json:"argumentsTruncated,omitempty"`
}

// ToolUpdatedEvent appends tool result content.
type ToolUpdatedEvent struct {
	ToolCallID string            `json:"toolCallId"`
	ToolName   string            `json:"toolName"`
	Content    ToolResultContent `json:"content"`
	IsError    bool              `json:"isError,omitempty"`
}

// ToolCompletedEvent records a final tool result.
type ToolCompletedEvent struct {
	ToolCallID       string            `json:"toolCallId"`
	ToolName         string            `json:"toolName"`
	Content          ToolResultContent `json:"content,omitempty"`
	ContentTruncated bool              `json:"contentTruncated,omitempty"`
	Details          json.RawMessage   `json:"details,omitempty"`
	DetailsOmitted   bool              `json:"detailsOmitted,omitempty"`
	IsError          bool              `json:"isError,omitempty"`
}

// CompactionStartedEvent identifies a started compaction.
type CompactionStartedEvent struct {
	CompactionID string `json:"compactionId"`
}

// CompactionCompletedEvent identifies a completed compaction.
type CompactionCompletedEvent struct {
	CompactionID string `json:"compactionId"`
}

// CompactionFailedEvent records a failed compaction pending its dedicated error model.
type CompactionFailedEvent struct {
	CompactionID string `json:"compactionId"`
	ErrorMessage string `json:"errorMessage"`
}

// ProviderRetryScheduledEvent records a retry deadline.
type ProviderRetryScheduledEvent struct {
	ProviderRetry *ProviderRetry `json:"providerRetry"`
}

// ProviderRetryStartedEvent records a retry attempt.
type ProviderRetryStartedEvent struct {
	ProviderRetry *ProviderRetry `json:"providerRetry"`
}

// ContextUpdatedEvent records context consumption.
type ContextUpdatedEvent struct {
	ContextTokens int `json:"contextTokens"`
	ContextWindow int `json:"contextWindow"`
}

// UsageUpdatedEvent records cumulative session usage.
type UsageUpdatedEvent struct {
	Usage *SessionUsage `json:"usage"`
}

// RunFinishedEvent records terminal run status.
type RunFinishedEvent struct {
	Status       RunStatus         `json:"status"`
	ErrorKind    ProviderErrorKind `json:"errorKind,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
}

// SessionRenamedEvent records the new session name.
type SessionRenamedEvent struct {
	SessionName string `json:"sessionName"`
}

// SessionCWDChangedEvent records the current workspace.
type SessionCWDChangedEvent struct {
	Workspace *WorkspaceRef `json:"workspace"`
}

// SubagentChangedEvent identifies a changed child conversation.
type SubagentChangedEvent struct {
	SubagentConversationID string `json:"subagentConversationId"`
	SubagentTaskID         string `json:"subagentTaskId,omitempty"`
}

// PeerQueryChangedEvent identifies a changed peer query.
type PeerQueryChangedEvent struct {
	PeerRequestID string `json:"peerRequestId"`
}

// InteractionRequestedEvent carries an interaction request.
type InteractionRequestedEvent struct {
	Interaction *InteractionRequest `json:"interaction"`
}

// InteractionResolvedEvent records an interaction outcome.
type InteractionResolvedEvent struct {
	InteractionID         string `json:"interactionId"`
	InteractionResolution string `json:"interactionResolution"`
}

// AnnotationCreatedEvent carries a newly created annotation.
type AnnotationCreatedEvent struct {
	AnnotationID uint64      `json:"annotationId"`
	Annotation   *Annotation `json:"annotation"`
}

// AnnotationUpdatedEvent carries an updated annotation.
type AnnotationUpdatedEvent struct {
	AnnotationID uint64      `json:"annotationId"`
	Annotation   *Annotation `json:"annotation"`
}

// AnnotationDeletedEvent identifies a deleted annotation.
type AnnotationDeletedEvent struct {
	AnnotationID uint64 `json:"annotationId"`
}

// AnnotationSubmittedEvent records submitted annotations.
type AnnotationSubmittedEvent struct {
	AnnotationIDs     []uint64 `json:"annotationIds"`
	AcceptedMessageID string   `json:"acceptedMessageId"`
}

// ScratchpadChangedEvent carries a changed scratchpad.
type ScratchpadChangedEvent struct {
	Scratchpad *Scratchpad `json:"scratchpad"`
}

func (RunStartedEvent) sessionEventKind() SessionEventKind       { return SessionEventRunStarted }
func (UserMessageEvent) sessionEventKind() SessionEventKind      { return SessionEventUserMessage }
func (AssistantStartedEvent) sessionEventKind() SessionEventKind { return SessionEventAssistantStarted }
func (AssistantTextDeltaEvent) sessionEventKind() SessionEventKind {
	return SessionEventAssistantTextDelta
}
func (ThinkingDeltaEvent) sessionEventKind() SessionEventKind { return SessionEventThinkingDelta }
func (AssistantCompletedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAssistantCompleted
}
func (ToolPlannedEvent) sessionEventKind() SessionEventKind   { return SessionEventToolPlanned }
func (ToolStartedEvent) sessionEventKind() SessionEventKind   { return SessionEventToolStarted }
func (ToolUpdatedEvent) sessionEventKind() SessionEventKind   { return SessionEventToolUpdated }
func (ToolCompletedEvent) sessionEventKind() SessionEventKind { return SessionEventToolCompleted }
func (CompactionStartedEvent) sessionEventKind() SessionEventKind {
	return SessionEventCompactionStarted
}
func (CompactionCompletedEvent) sessionEventKind() SessionEventKind {
	return SessionEventCompactionCompleted
}
func (CompactionFailedEvent) sessionEventKind() SessionEventKind { return SessionEventCompactionFailed }
func (ProviderRetryScheduledEvent) sessionEventKind() SessionEventKind {
	return SessionEventProviderRetryScheduled
}
func (ProviderRetryStartedEvent) sessionEventKind() SessionEventKind {
	return SessionEventProviderRetryStarted
}
func (ContextUpdatedEvent) sessionEventKind() SessionEventKind { return SessionEventContextUpdated }
func (UsageUpdatedEvent) sessionEventKind() SessionEventKind   { return SessionEventUsageUpdated }
func (RunFinishedEvent) sessionEventKind() SessionEventKind    { return SessionEventRunFinished }
func (SessionRenamedEvent) sessionEventKind() SessionEventKind { return SessionEventSessionRenamed }
func (SessionCWDChangedEvent) sessionEventKind() SessionEventKind {
	return SessionEventSessionCWDChanged
}
func (SubagentChangedEvent) sessionEventKind() SessionEventKind  { return SessionEventSubagentChanged }
func (PeerQueryChangedEvent) sessionEventKind() SessionEventKind { return SessionEventPeerQueryChanged }
func (InteractionRequestedEvent) sessionEventKind() SessionEventKind {
	return SessionEventInteractionRequested
}
func (InteractionResolvedEvent) sessionEventKind() SessionEventKind {
	return SessionEventInteractionResolved
}
func (AnnotationCreatedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAnnotationCreated
}
func (AnnotationUpdatedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAnnotationUpdated
}
func (AnnotationDeletedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAnnotationDeleted
}
func (AnnotationSubmittedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAnnotationSubmitted
}
func (ScratchpadChangedEvent) sessionEventKind() SessionEventKind {
	return SessionEventScratchpadChanged
}

func sessionEventVariants() []UnionVariant {
	return []UnionVariant{
		{Kind: string(SessionEventRunStarted), Payload: RunStartedEvent{}}, {Kind: string(SessionEventUserMessage), Payload: UserMessageEvent{}}, {Kind: string(SessionEventAssistantStarted), Payload: AssistantStartedEvent{}}, {Kind: string(SessionEventAssistantTextDelta), Payload: AssistantTextDeltaEvent{}}, {Kind: string(SessionEventThinkingDelta), Payload: ThinkingDeltaEvent{}}, {Kind: string(SessionEventAssistantCompleted), Payload: AssistantCompletedEvent{}}, {Kind: string(SessionEventToolPlanned), Payload: ToolPlannedEvent{}}, {Kind: string(SessionEventToolStarted), Payload: ToolStartedEvent{}}, {Kind: string(SessionEventToolUpdated), Payload: ToolUpdatedEvent{}}, {Kind: string(SessionEventToolCompleted), Payload: ToolCompletedEvent{}}, {Kind: string(SessionEventCompactionStarted), Payload: CompactionStartedEvent{}}, {Kind: string(SessionEventCompactionCompleted), Payload: CompactionCompletedEvent{}}, {Kind: string(SessionEventCompactionFailed), Payload: CompactionFailedEvent{}}, {Kind: string(SessionEventProviderRetryScheduled), Payload: ProviderRetryScheduledEvent{}}, {Kind: string(SessionEventProviderRetryStarted), Payload: ProviderRetryStartedEvent{}}, {Kind: string(SessionEventContextUpdated), Payload: ContextUpdatedEvent{}}, {Kind: string(SessionEventUsageUpdated), Payload: UsageUpdatedEvent{}}, {Kind: string(SessionEventRunFinished), Payload: RunFinishedEvent{}}, {Kind: string(SessionEventSessionRenamed), Payload: SessionRenamedEvent{}}, {Kind: string(SessionEventSessionCWDChanged), Payload: SessionCWDChangedEvent{}}, {Kind: string(SessionEventSubagentChanged), Payload: SubagentChangedEvent{}}, {Kind: string(SessionEventPeerQueryChanged), Payload: PeerQueryChangedEvent{}}, {Kind: string(SessionEventInteractionRequested), Payload: InteractionRequestedEvent{}}, {Kind: string(SessionEventInteractionResolved), Payload: InteractionResolvedEvent{}}, {Kind: string(SessionEventAnnotationCreated), Payload: AnnotationCreatedEvent{}}, {Kind: string(SessionEventAnnotationUpdated), Payload: AnnotationUpdatedEvent{}}, {Kind: string(SessionEventAnnotationDeleted), Payload: AnnotationDeletedEvent{}}, {Kind: string(SessionEventAnnotationSubmitted), Payload: AnnotationSubmittedEvent{}}, {Kind: string(SessionEventScratchpadChanged), Payload: ScratchpadChangedEvent{}},
	}
}

// PayloadVariant returns the event's canonical payload.
func (event SessionEvent) PayloadVariant() (SessionEventPayload, error) {
	if event.Payload == nil {
		return nil, fmt.Errorf("session event has no payload")
	}
	return event.Payload, nil
}
