package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

// SubagentTranscriptParams describes one child transcript page. Before is an
// exclusive durable sequence; omitted, it selects the newest page.
type SubagentTranscriptParams struct {
	SessionID      string `path:"sessionID"`
	ConversationID string `path:"conversationID"`
	Before         string `query:"before,omitempty"`
}
type SubagentEventsParams struct {
	SessionID      string `path:"sessionID"`
	ConversationID string `path:"conversationID"`
	StreamID       string `query:"stream,omitempty"`
	After          int64  `query:"after,omitempty"`
}

func subagentErrors() []ErrorResponse {
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
		{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
		{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}},
		{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

func subagentTranscriptErrors() []ErrorResponse {
	responses := subagentErrors()
	for index := range responses {
		if responses[index].Status == http.StatusConflict {
			responses[index].Codes = append([]ErrorCode{ErrorTranscriptCursorUnavailable}, responses[index].Codes...)
		}
	}
	return responses
}

var (
	GetSubagentEvents     = Operation[SubagentEventsParams, NoBody, protocol.SubagentLiveEventPage]{ID: "getSubagentEvents", Tag: "subagents", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/subagents/{conversationID}/events", Success: http.StatusOK, Errors: subagentErrors()}
	GetSubagentTranscript = Operation[SubagentTranscriptParams, NoBody, protocol.SubagentTranscript]{ID: "getSubagentTranscript", Tag: "subagents", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/subagents/{conversationID}/transcript", Success: http.StatusOK, Errors: subagentTranscriptErrors()}
	OperateSubagent       = Operation[SessionPath, protocol.SubagentOperationInput, protocol.SubagentOperationResult]{ID: "operateSubagent", Tag: "subagents", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/subagents", Success: http.StatusOK, AdditionalSuccess: []int{http.StatusAccepted}, Errors: subagentErrors()}
)
