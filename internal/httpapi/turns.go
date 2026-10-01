package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

const (
	// SessionEventsRecord is one ordinary session-event stream page.
	SessionEventsRecord = "session.events"
	// SessionResyncRecord tells a resumable session-event stream client to fetch
	// a snapshot or finite page before reconnecting.
	SessionResyncRecord = "session.resync"
	// MaxSessionEventRecordBytes bounds a framed session-event page.
	MaxSessionEventRecordBytes = 528 << 10
)

// TurnPath binds one turn beneath its parent session.
type TurnPath struct {
	SessionID string `path:"sessionID"`
	TurnID    string `path:"turnID"`
}

// InteractionPath binds one interaction beneath its parent session.
type InteractionPath struct {
	SessionID     string `path:"sessionID"`
	InteractionID string `path:"interactionID"`
}

// MessagePagePath describes the query accepted by the finite message page.
type MessagePagePath struct {
	SessionID string   `path:"sessionID"`
	Before    uint64   `query:"before,omitempty"`
	Limit     int      `query:"limit,omitempty"`
	Roles     []string `query:"role,omitempty"`
}

// TranscriptPagePath describes the required transcript cursor.
type TranscriptPagePath struct {
	SessionID string `path:"sessionID"`
	Before    string `query:"before"`
}

// EventPagePath describes a finite event-page cursor.
type EventPagePath struct {
	SessionID string `path:"sessionID"`
	StreamID  string `query:"stream,omitempty"`
	After     int64  `query:"after,omitempty"`
}

// EventStreamPath describes the resumable stream cursor. LastEventID uses the
// canonical streamID:sequence cursor when supplied by an SSE client.
type EventStreamPath struct {
	SessionID   string `path:"sessionID"`
	StreamID    string `query:"stream,omitempty"`
	After       int64  `query:"after,omitempty"`
	LastEventID string `header:"Last-Event-ID,omitempty"`
}

var turnErrors = []ErrorResponse{
	{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
	{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
	{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
	{Status: http.StatusUnprocessableEntity, Codes: []ErrorCode{ErrorUnprocessable}},
	{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}},
	{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
	{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
}

var (
	// SubmitPrompt admits a prompt or records it as a deferred follow-up.
	SubmitPrompt = Operation[SessionPath, protocol.PromptInput, protocol.PromptSubmission]{
		ID: "submitPrompt", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/submissions", Success: http.StatusAccepted, Errors: turnErrors,
	}
	// StartPrompt admits one prompt immediately when the session is ready.
	StartPrompt = Operation[SessionPath, protocol.PromptInput, protocol.TurnReservation]{
		ID: "startPrompt", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/prompts", Success: http.StatusAccepted, Errors: turnErrors,
	}
	// StartPromptCommand expands and admits one discovered prompt command.
	StartPromptCommand = Operation[SessionPath, protocol.PromptCommandInput, protocol.TurnReservation]{
		ID: "startPromptCommand", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/prompt-commands", Success: http.StatusAccepted, Errors: turnErrors,
	}
	// Prompt admits and waits for one turn.
	Prompt = Operation[SessionPath, protocol.PromptInput, protocol.PromptOutcome]{
		ID: "prompt", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/prompt", Success: http.StatusOK, Errors: turnErrors,
	}
	// RestoreTurnFollowUps drains deferred prompts for a session.
	RestoreTurnFollowUps = Operation[SessionPath, NoBody, protocol.RestoreFollowUpsResult]{
		ID: "restoreTurnFollowUps", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/follow-ups/restore", Success: http.StatusOK, Errors: turnErrors,
	}
	// PromoteTurnFollowUps steers every deferred prompt into the active turn.
	PromoteTurnFollowUps = Operation[SessionPath, NoBody, protocol.PromoteFollowUpsResult]{
		ID: "promoteTurnFollowUps", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/follow-ups/promote", Success: http.StatusOK, Errors: turnErrors,
	}
	// GetTurn reads one admitted turn's current projection.
	GetTurn = Operation[TurnPath, NoBody, protocol.TurnInfo]{
		ID: "getTurn", Tag: "turns", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/turns/{turnID}", Success: http.StatusOK, Errors: turnErrors,
	}
	// AbortTurn requests cancellation of one admitted turn.
	AbortTurn = Operation[TurnPath, NoBody, protocol.TurnAbortResult]{
		ID: "abortTurn", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/turns/{turnID}/abort", Success: http.StatusAccepted, Errors: turnErrors,
	}
	// RespondInteraction settles a pending interaction.
	RespondInteraction = Operation[InteractionPath, protocol.InteractionResponse, protocol.InteractionResponseResult]{
		ID: "respondInteraction", Tag: "turns", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/interactions/{interactionID}/response", Success: http.StatusOK, Errors: turnErrors,
	}
	// GetMessagePage returns newest-first durable messages.
	GetMessagePage = Operation[MessagePagePath, NoBody, protocol.MessagePage]{
		ID: "getMessagePage", Tag: "transcript", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/messages", Success: http.StatusOK, Errors: turnErrors,
	}
	// GetTranscriptPage returns a complete-turn-bounded transcript page.
	GetTranscriptPage = Operation[TranscriptPagePath, NoBody, protocol.TranscriptPage]{
		ID: "getTranscriptPage", Tag: "transcript", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/transcript", Success: http.StatusOK, Errors: turnErrors,
	}
	// GetSessionEventPage returns one bounded event page.
	GetSessionEventPage = Operation[EventPagePath, NoBody, protocol.SessionEventBatch]{
		ID: "getSessionEventPage", Tag: "events", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/events", Success: http.StatusOK, Errors: turnErrors,
	}
	// StreamSessionEvents is the resumable SSE event stream. A resync record is
	// terminal and carries the same bounded page payload as ordinary records.
	StreamSessionEvents = StreamOperation[EventStreamPath, protocol.SessionEventBatch]{
		ID: "streamSessionEvents", Tag: "events", Path: "/v1/sessions/{sessionID}/events/stream",
		Records: []string{SessionEventsRecord, SessionResyncRecord}, Resumable: true, MaxRecordBytes: MaxSessionEventRecordBytes,
		Validate: protocol.SessionEventBatch.Validate, Errors: turnErrors,
	}
)
