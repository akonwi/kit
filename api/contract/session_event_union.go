package contract

import (
	"encoding/json"
	"fmt"
)

// sessionEventWire preserves the pre-union flat field order and JSON tags.
type sessionEventWire struct {
	StreamID               string              `json:"streamId"`
	Sequence               int64               `json:"sequence"`
	SessionID              string              `json:"sessionId"`
	TurnID                 string              `json:"turnId,omitempty"`
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
	Status                 TurnStatus          `json:"status,omitempty"`
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

var sessionEventCodec = newUnionCodec[SessionEvent, sessionEventWire](sessionEventVariants())

func (event SessionEvent) marshalJSON() ([]byte, error) {
	if err := validateSessionEventPayloadPresence(event.Payload); err != nil {
		return nil, err
	}
	return sessionEventCodec.marshal(event, event.Payload)
}
func (event *SessionEvent) unmarshalJSON(data []byte) error {
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
	}
	return nil
}
