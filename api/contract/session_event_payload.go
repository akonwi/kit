package contract

import (
	"encoding/json"
	"fmt"
)

// SessionEventPayload is the kind-specific data of a SessionEvent.
type SessionEventPayload interface{ sessionEventKind() SessionEventKind }

// TurnStartedEvent records that a turn began.
type TurnStartedEvent struct {
	Status TurnStatus `json:"status"`
}

// UserMessageAddedEvent records submitted user text.
type UserMessageAddedEvent struct {
	Text string `json:"text"`
}

// PluginMessageAddedEvent records a plugin-submitted message that started the
// turn. PluginID identifies the submitting plugin.
type PluginMessageAddedEvent struct {
	PluginID string `json:"pluginId"`
	Text     string `json:"text"`
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

// ToolOutputDeltaEvent appends tool result content.
type ToolOutputDeltaEvent struct {
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

// CompactionCompletedEvent records a completed compaction. ErrorMessage is set
// when compaction completed unsuccessfully.
type CompactionCompletedEvent struct {
	CompactionID string `json:"compactionId"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ProviderRetryScheduledEvent records a retry deadline.
type ProviderRetryScheduledEvent struct {
	ProviderRetry *ProviderRetry `json:"providerRetry"`
}

// ProviderRetryStartedEvent records a retry attempt.
type ProviderRetryStartedEvent struct {
	ProviderRetry *ProviderRetry `json:"providerRetry"`
}

// ContextChangedEvent records context consumption.
type ContextChangedEvent struct {
	ContextTokens int `json:"contextTokens"`
	ContextWindow int `json:"contextWindow"`
}

// UsageChangedEvent records cumulative session usage.
type UsageChangedEvent struct {
	Usage *SessionUsage `json:"usage"`
}

// TurnCompletedEvent records terminal turn status.
type TurnCompletedEvent struct {
	Status       TurnStatus        `json:"status"`
	ErrorKind    ProviderErrorKind `json:"errorKind,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
}

// SessionNameChangedEvent records the new session name.
type SessionNameChangedEvent struct {
	SessionName string `json:"sessionName"`
}

// SessionCWDChangedEvent records the current workspace.
type SessionCWDChangedEvent struct {
	Workspace *WorkspaceRef `json:"workspace"`
}

// BoundaryCreatedEvent carries a context record the session created. The
// record is the same one later snapshots list under BoundaryID. Content and
// Details are bounded for the live stream; ContentTruncated and DetailsOmitted
// mark a shortened record whose full form the next snapshot carries.
type BoundaryCreatedEvent struct {
	BoundaryID       string          `json:"boundaryId"`
	BoundaryKind     string          `json:"boundaryKind"`
	BoundarySource   string          `json:"boundarySource,omitempty"`
	Content          ContextContent  `json:"content"`
	ContentTruncated bool            `json:"contentTruncated,omitempty"`
	Details          json.RawMessage `json:"details,omitempty"`
	DetailsOmitted   bool            `json:"detailsOmitted,omitempty"`
	CreatedAt        string          `json:"createdAt"`
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

// URLOpenRequestedEvent asks the session's attached clients to open a web
// page. A plugin requested it; it belongs to the session and no turn.
//
// The request is ephemeral and live-only. It is never part of a snapshot or
// the transcript, and does not survive the runtime that accepted it. URL is
// the canonical address (an absolute http or https URL with a host and no
// credentials) only on an event stream that was already open when the session
// accepted the request. Event pages, replays, and the catch-up a stream
// delivers when it opens omit URL, so a client opens each request at most once
// per stream and never for work that happened before it attached.
type URLOpenRequestedEvent struct {
	URL string `json:"url,omitempty"`
}

// ClientPluginCallRequestedEvent asks one client instance to run a tool it
// hosts (ADR 0051). It belongs to the session and no turn.
//
// The request is ephemeral and live-only, like url.open.requested. The call
// is present only on the event stream bound to the target instance, and only
// while the call is pending. Every other stream, event page, and replay
// carries the event without the call. Binding a stream publishes the
// instance's pending calls again, so a client may see a call more than once
// and runs each call ID at most once.
type ClientPluginCallRequestedEvent struct {
	ClientPluginCallID     string            `json:"clientPluginCallId"`
	ClientPluginInstanceID string            `json:"clientPluginInstanceId"`
	ClientPluginCall       *ClientPluginCall `json:"clientPluginCall,omitempty"`
}

// ClientPluginCallResolvedEvent reports how a client tool call ended. It is
// ephemeral like ClientPluginCallRequestedEvent.
type ClientPluginCallResolvedEvent struct {
	ClientPluginCallID         string                     `json:"clientPluginCallId"`
	ClientPluginInstanceID     string                     `json:"clientPluginInstanceId"`
	ClientPluginCallResolution ClientPluginCallResolution `json:"clientPluginCallResolution"`
}

func (TurnStartedEvent) sessionEventKind() SessionEventKind      { return SessionEventTurnStarted }
func (UserMessageAddedEvent) sessionEventKind() SessionEventKind { return SessionEventUserMessageAdded }
func (PluginMessageAddedEvent) sessionEventKind() SessionEventKind {
	return SessionEventPluginMessageAdded
}
func (AssistantStartedEvent) sessionEventKind() SessionEventKind { return SessionEventAssistantStarted }
func (AssistantTextDeltaEvent) sessionEventKind() SessionEventKind {
	return SessionEventAssistantTextDelta
}
func (ThinkingDeltaEvent) sessionEventKind() SessionEventKind { return SessionEventThinkingDelta }
func (AssistantCompletedEvent) sessionEventKind() SessionEventKind {
	return SessionEventAssistantCompleted
}
func (ToolPlannedEvent) sessionEventKind() SessionEventKind     { return SessionEventToolPlanned }
func (ToolStartedEvent) sessionEventKind() SessionEventKind     { return SessionEventToolStarted }
func (ToolOutputDeltaEvent) sessionEventKind() SessionEventKind { return SessionEventToolOutputDelta }
func (ToolCompletedEvent) sessionEventKind() SessionEventKind   { return SessionEventToolCompleted }
func (CompactionStartedEvent) sessionEventKind() SessionEventKind {
	return SessionEventCompactionStarted
}
func (CompactionCompletedEvent) sessionEventKind() SessionEventKind {
	return SessionEventCompactionCompleted
}
func (ProviderRetryScheduledEvent) sessionEventKind() SessionEventKind {
	return SessionEventProviderRetryScheduled
}
func (ProviderRetryStartedEvent) sessionEventKind() SessionEventKind {
	return SessionEventProviderRetryStarted
}
func (ContextChangedEvent) sessionEventKind() SessionEventKind { return SessionEventContextChanged }
func (UsageChangedEvent) sessionEventKind() SessionEventKind   { return SessionEventUsageChanged }
func (TurnCompletedEvent) sessionEventKind() SessionEventKind  { return SessionEventTurnCompleted }
func (SessionNameChangedEvent) sessionEventKind() SessionEventKind {
	return SessionEventSessionNameChanged
}
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
func (BoundaryCreatedEvent) sessionEventKind() SessionEventKind {
	return SessionEventBoundaryCreated
}
func (URLOpenRequestedEvent) sessionEventKind() SessionEventKind {
	return SessionEventURLOpenRequested
}
func (ClientPluginCallRequestedEvent) sessionEventKind() SessionEventKind {
	return SessionEventClientPluginCallRequested
}
func (ClientPluginCallResolvedEvent) sessionEventKind() SessionEventKind {
	return SessionEventClientPluginCallResolved
}

func sessionEventVariants() []UnionVariant {
	return []UnionVariant{
		{Kind: string(SessionEventTurnStarted), Payload: TurnStartedEvent{}}, {Kind: string(SessionEventUserMessageAdded), Payload: UserMessageAddedEvent{}}, {Kind: string(SessionEventPluginMessageAdded), Payload: PluginMessageAddedEvent{}}, {Kind: string(SessionEventAssistantStarted), Payload: AssistantStartedEvent{}}, {Kind: string(SessionEventAssistantTextDelta), Payload: AssistantTextDeltaEvent{}}, {Kind: string(SessionEventThinkingDelta), Payload: ThinkingDeltaEvent{}}, {Kind: string(SessionEventAssistantCompleted), Payload: AssistantCompletedEvent{}}, {Kind: string(SessionEventToolPlanned), Payload: ToolPlannedEvent{}}, {Kind: string(SessionEventToolStarted), Payload: ToolStartedEvent{}}, {Kind: string(SessionEventToolOutputDelta), Payload: ToolOutputDeltaEvent{}}, {Kind: string(SessionEventToolCompleted), Payload: ToolCompletedEvent{}}, {Kind: string(SessionEventCompactionStarted), Payload: CompactionStartedEvent{}}, {Kind: string(SessionEventCompactionCompleted), Payload: CompactionCompletedEvent{}}, {Kind: string(SessionEventProviderRetryScheduled), Payload: ProviderRetryScheduledEvent{}}, {Kind: string(SessionEventProviderRetryStarted), Payload: ProviderRetryStartedEvent{}}, {Kind: string(SessionEventContextChanged), Payload: ContextChangedEvent{}}, {Kind: string(SessionEventUsageChanged), Payload: UsageChangedEvent{}}, {Kind: string(SessionEventTurnCompleted), Payload: TurnCompletedEvent{}}, {Kind: string(SessionEventSessionNameChanged), Payload: SessionNameChangedEvent{}}, {Kind: string(SessionEventSessionCWDChanged), Payload: SessionCWDChangedEvent{}}, {Kind: string(SessionEventSubagentChanged), Payload: SubagentChangedEvent{}}, {Kind: string(SessionEventPeerQueryChanged), Payload: PeerQueryChangedEvent{}}, {Kind: string(SessionEventInteractionRequested), Payload: InteractionRequestedEvent{}}, {Kind: string(SessionEventInteractionResolved), Payload: InteractionResolvedEvent{}}, {Kind: string(SessionEventAnnotationCreated), Payload: AnnotationCreatedEvent{}}, {Kind: string(SessionEventAnnotationUpdated), Payload: AnnotationUpdatedEvent{}}, {Kind: string(SessionEventAnnotationDeleted), Payload: AnnotationDeletedEvent{}}, {Kind: string(SessionEventAnnotationSubmitted), Payload: AnnotationSubmittedEvent{}}, {Kind: string(SessionEventScratchpadChanged), Payload: ScratchpadChangedEvent{}}, {Kind: string(SessionEventBoundaryCreated), Payload: BoundaryCreatedEvent{}}, {Kind: string(SessionEventURLOpenRequested), Payload: URLOpenRequestedEvent{}}, {Kind: string(SessionEventClientPluginCallRequested), Payload: ClientPluginCallRequestedEvent{}}, {Kind: string(SessionEventClientPluginCallResolved), Payload: ClientPluginCallResolvedEvent{}},
	}
}

// PayloadVariant returns the event's canonical payload.
func (event SessionEvent) PayloadVariant() (SessionEventPayload, error) {
	if event.Payload == nil {
		return nil, fmt.Errorf("session event has no payload")
	}
	return event.Payload, nil
}
