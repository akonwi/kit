package contract

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// sessionEventWire preserves the pre-union flat field order and JSON tags.
type sessionEventWire struct {
	StreamID                   string                     `json:"streamId"`
	Sequence                   int64                      `json:"sequence"`
	SessionID                  string                     `json:"sessionId"`
	TurnID                     string                     `json:"turnId,omitempty"`
	MessageID                  string                     `json:"messageId,omitempty"`
	Kind                       SessionEventKind           `json:"kind"`
	ContentIndex               int                        `json:"contentIndex,omitempty"`
	Delta                      string                     `json:"delta,omitempty"`
	Text                       string                     `json:"text,omitempty"`
	PluginID                   string                     `json:"pluginId,omitempty"`
	Thinking                   string                     `json:"thinking,omitempty"`
	ToolCallID                 string                     `json:"toolCallId,omitempty"`
	ToolName                   string                     `json:"toolName,omitempty"`
	Arguments                  string                     `json:"arguments,omitempty"`
	ArgumentsTruncated         bool                       `json:"argumentsTruncated,omitempty"`
	Content                    []TranscriptContent        `json:"content,omitempty"`
	ContentTruncated           bool                       `json:"contentTruncated,omitempty"`
	Details                    json.RawMessage            `json:"details,omitempty"`
	DetailsOmitted             bool                       `json:"detailsOmitted,omitempty"`
	IsError                    bool                       `json:"isError,omitempty"`
	Status                     TurnStatus                 `json:"status,omitempty"`
	ErrorKind                  ProviderErrorKind          `json:"errorKind,omitempty"`
	ErrorMessage               string                     `json:"errorMessage,omitempty"`
	CompactionID               string                     `json:"compactionId,omitempty"`
	CompactionFailureReason    CompactionFailureReason    `json:"compactionFailureReason,omitempty"`
	ProviderRetry              *ProviderRetry             `json:"providerRetry,omitempty"`
	ContextTokens              int                        `json:"contextTokens,omitempty"`
	ContextWindow              int                        `json:"contextWindow,omitempty"`
	Usage                      *SessionUsage              `json:"usage,omitempty"`
	SessionName                string                     `json:"sessionName,omitempty"`
	Workspace                  *WorkspaceRef              `json:"workspace,omitempty"`
	SubagentConversationID     string                     `json:"subagentConversationId,omitempty"`
	SubagentTaskID             string                     `json:"subagentTaskId,omitempty"`
	PeerRequestID              string                     `json:"peerRequestId,omitempty"`
	Interaction                *InteractionRequest        `json:"interaction,omitempty"`
	InteractionID              string                     `json:"interactionId,omitempty"`
	InteractionResolution      string                     `json:"interactionResolution,omitempty"`
	AnnotationID               uint64                     `json:"annotationId,omitempty"`
	Annotation                 *Annotation                `json:"annotation,omitempty"`
	AnnotationIDs              []uint64                   `json:"annotationIds,omitempty"`
	AcceptedMessageID          string                     `json:"acceptedMessageId,omitempty"`
	Scratchpad                 *Scratchpad                `json:"scratchpad,omitempty"`
	BoundaryID                 string                     `json:"boundaryId,omitempty"`
	BoundaryKind               string                     `json:"boundaryKind,omitempty"`
	BoundarySource             string                     `json:"boundarySource,omitempty"`
	CreatedAt                  string                     `json:"createdAt,omitempty"`
	URL                        string                     `json:"url,omitempty"`
	ClientPluginCallID         string                     `json:"clientPluginCallId,omitempty"`
	ClientPluginInstanceID     string                     `json:"clientPluginInstanceId,omitempty"`
	ClientPluginCall           *ClientPluginCall          `json:"clientPluginCall,omitempty"`
	ClientPluginCallResolution ClientPluginCallResolution `json:"clientPluginCallResolution,omitempty"`
	Configuration              *SessionConfiguration      `json:"configuration,omitempty"`
	FollowUps                  *FollowUpQueue             `json:"followUps,omitempty"`
	BashExecution              *BashExecution             `json:"bashExecution,omitempty"`
}

var sessionEventCodec = newUnionCodec[SessionEvent, sessionEventWire](sessionEventVariants())

func (event SessionEvent) marshalJSON() ([]byte, error) {
	if unrecognized, ok := event.Payload.(UnrecognizedSessionEvent); ok {
		// Only the members every kind shares survive: a client keeps an
		// unrecognized event only to advance past it.
		return json.Marshal(unrecognizedSessionEventWire{StreamID: event.StreamID, Sequence: event.Sequence, SessionID: event.SessionID, TurnID: event.TurnID, Kind: unrecognized.Kind})
	}
	if err := validateSessionEventPayloadPresence(event.Payload); err != nil {
		return nil, err
	}
	return sessionEventCodec.marshal(event, event.Payload)
}

// unrecognizedSessionEventWire is the common part of an event of a kind this
// protocol doesn't declare.
type unrecognizedSessionEventWire struct {
	StreamID  string           `json:"streamId"`
	Sequence  int64            `json:"sequence"`
	SessionID string           `json:"sessionId"`
	TurnID    string           `json:"turnId,omitempty"`
	Kind      SessionEventKind `json:"kind"`
}

// known reports whether this protocol declares the kind.
func (kind SessionEventKind) known() bool {
	_, ok := sessionEventCodec.byKind[string(kind)]
	return ok
}

// sessionEventKindGrammar is the ADR 0033 kind grammar: lowercase dotted
// segments, multi-word segments in snake_case.
var sessionEventKindGrammar = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// decodeUnrecognizedSessionEvent decodes an event whose kind this protocol
// doesn't declare (ADR 0072): the kind must follow the event grammar, and the
// members every kind shares must be well formed; the rest is skipped.
func decodeUnrecognizedSessionEvent(data []byte, kind string) (SessionEvent, error) {
	if len(kind) > 128 || !sessionEventKindGrammar.MatchString(kind) {
		return SessionEvent{}, fmt.Errorf("decode SessionEvent: kind %q is invalid", kind)
	}
	var common unrecognizedSessionEventWire
	if err := json.Unmarshal(data, &common); err != nil {
		return SessionEvent{}, fmt.Errorf("decode SessionEvent: %w", err)
	}
	return SessionEvent{StreamID: common.StreamID, Sequence: common.Sequence, SessionID: common.SessionID, TurnID: common.TurnID, Payload: UnrecognizedSessionEvent{Kind: SessionEventKind(kind)}}, nil
}

func (event *SessionEvent) unmarshalJSON(data []byte) error {
	var probe struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.Kind != nil && !SessionEventKind(*probe.Kind).known() {
		decoded, err := decodeUnrecognizedSessionEvent(data, *probe.Kind)
		if err != nil {
			return err
		}
		*event = decoded
		return nil
	}
	var decoded SessionEvent
	payload, err := sessionEventCodec.unmarshal(data, &decoded)
	if err != nil {
		return err
	}
	decoded.Payload = payload.(SessionEventPayload)
	if err := validateSessionEventPayloadPresence(decoded.Payload); err != nil {
		return err
	}
	*event = decoded
	return nil
}

func decodeSessionEventUnion(data []byte) (SessionEvent, error) {
	var event SessionEvent
	if err := event.unmarshalJSON(data); err != nil {
		return SessionEvent{}, err
	}
	return event, nil
}

func encodeSessionEventUnion(event SessionEvent) ([]byte, error) { return event.marshalJSON() }

var _ = json.RawMessage{}

func validateSessionEventPayloadPresence(payload SessionEventPayload) error {
	switch payload := payload.(type) {
	case ProviderRetryScheduledEvent:
		if payload.ProviderRetry == nil {
			return fmt.Errorf("provider retry is required")
		}
	case ProviderRetryStartedEvent:
		if payload.ProviderRetry == nil {
			return fmt.Errorf("provider retry is required")
		}
	case UsageChangedEvent:
		if payload.Usage == nil {
			return fmt.Errorf("usage is required")
		}
	case SessionCWDChangedEvent:
		if payload.Workspace == nil {
			return fmt.Errorf("workspace is required")
		}
	case InteractionRequestedEvent:
		if payload.Interaction == nil {
			return fmt.Errorf("interaction is required")
		}
	case AnnotationCreatedEvent:
		if payload.Annotation == nil {
			return fmt.Errorf("annotation is required")
		}
	case AnnotationUpdatedEvent:
		if payload.Annotation == nil {
			return fmt.Errorf("annotation is required")
		}
	case ScratchpadChangedEvent:
		if payload.Scratchpad == nil {
			return fmt.Errorf("scratchpad is required")
		}
	case SessionConfigurationChangedEvent:
		if payload.Configuration == nil {
			return fmt.Errorf("configuration is required")
		}
	case FollowUpsChangedEvent:
		if payload.FollowUps == nil {
			return fmt.Errorf("follow-up queue is required")
		}
	case BashStartedEvent:
		if payload.BashExecution == nil {
			return fmt.Errorf("bash execution is required")
		}
	case BashCompletedEvent:
		if payload.BashExecution == nil {
			return fmt.Errorf("bash execution is required")
		}
	}
	return nil
}
