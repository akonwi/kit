package httpapi

import (
	"github.com/akonwi/kit/internal/protocol"
	"net/http"
)

type BashHistoryPath struct {
	SessionID string `path:"sessionID"`
	Before    uint64 `query:"before,omitempty"`
	Limit     int    `query:"limit,omitempty"`
}
type BashExecutionPath struct {
	SessionID   string `path:"sessionID"`
	ExecutionID string `path:"executionID"`
}
type BashAbortResult struct {
	Aborting bool `json:"aborting"`
}

func bashErrors() []ErrorResponse {
	return []ErrorResponse{{Status: 400, Codes: []ErrorCode{ErrorInvalidRequest}}, {Status: 404, Codes: []ErrorCode{ErrorNotFound}}, {Status: 409, Codes: []ErrorCode{ErrorConflict}}, {Status: 503, Codes: []ErrorCode{ErrorUnavailable}}, {Status: 500, Codes: []ErrorCode{ErrorInternal}}}
}

var (
	GetBashHistory = Operation[BashHistoryPath, NoBody, protocol.BashHistoryPage]{ID: "getBashHistory", Tag: "bash", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/bash-history", Success: 200, Errors: bashErrors()}
	StartBash      = Operation[SessionPath, protocol.BashExecutionInput, protocol.BashExecution]{ID: "startBash", Tag: "bash", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/bash-executions", Success: 202, Errors: bashErrors()}
	GetBash        = Operation[BashExecutionPath, NoBody, protocol.BashExecution]{ID: "getBash", Tag: "bash", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/bash-executions/{executionID}", Success: 200, Errors: bashErrors()}
	AbortBash      = Operation[BashExecutionPath, NoBody, BashAbortResult]{ID: "abortBash", Tag: "bash", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/bash-executions/{executionID}/abort", Success: 202, Errors: bashErrors()}
)
