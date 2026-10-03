package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// ListSessionsPath binds the optional working-directory session filter.
type ListSessionsPath struct {
	CWD string `query:"cwd,omitempty"`
}

func lifecycleErrors(statuses ...int) []ErrorResponse {
	responses := make([]ErrorResponse, 0, len(statuses))
	for _, status := range statuses {
		code := ErrorCode("")
		switch status {
		case http.StatusBadRequest:
			code = ErrorInvalidRequest
		case http.StatusNotFound:
			code = ErrorNotFound
		case http.StatusConflict:
			code = ErrorConflict
		case http.StatusRequestEntityTooLarge:
			code = ErrorLimitExceeded
		case http.StatusUnprocessableEntity:
			code = ErrorUnprocessable
		case http.StatusServiceUnavailable:
			code = ErrorUnavailable
		case http.StatusInternalServerError:
			code = ErrorInternal
		}
		responses = append(responses, ErrorResponse{Status: status, Codes: []ErrorCode{code}})
	}
	return responses
}

var (
	// ListSessions lists sessions, optionally restricted to a working directory.
	ListSessions = Operation[ListSessionsPath, NoBody, protocol.SessionList]{ID: "listSessions", Tag: "sessions", Method: http.MethodGet, Path: "/v1/sessions", Success: http.StatusOK, Errors: lifecycleErrors(400, 503, 500)}
	// CreateSession creates a persisted or temporary session.
	CreateSession = Operation[NoBody, protocol.CreateSessionInput, protocol.SessionInfo]{ID: "createSession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions", Success: http.StatusCreated, Errors: lifecycleErrors(400, 413, 409, 503, 500)}
	// GetSession reads an authoritative session snapshot.
	GetSession = Operation[SessionPath, NoBody, protocol.SessionSnapshot]{ID: "getSession", Tag: "sessions", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}", Success: http.StatusOK, Errors: lifecycleErrors(400, 404, 503, 500)}
	// RenameSession changes a session display name.
	RenameSession = Operation[SessionPath, protocol.RenameSessionInput, protocol.SessionInfo]{ID: "renameSession", Tag: "sessions", Method: http.MethodPatch, Path: "/v1/sessions/{sessionID}", Success: http.StatusOK, Errors: lifecycleErrors(400, 413, 404, 409, 503, 500)}
	// DeleteSession permanently removes a persisted session.
	DeleteSession = Operation[SessionPath, NoBody, NoBody]{ID: "deleteSession", Tag: "sessions", Method: http.MethodDelete, Path: "/v1/sessions/{sessionID}", Success: http.StatusNoContent, Errors: lifecycleErrors(400, 404, 409, 503, 500)}
	// DisposeTemporarySession removes a temporary session.
	DisposeTemporarySession = Operation[SessionPath, NoBody, NoBody]{ID: "disposeTemporarySession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/dispose", Success: http.StatusNoContent, Errors: lifecycleErrors(400, 404, 503, 500)}
	// ForkSession creates a linked child from a persistent session.
	ForkSession = Operation[SessionPath, protocol.ForkSessionInput, protocol.SessionInfo]{ID: "forkSession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/forks", Success: http.StatusCreated, Errors: lifecycleErrors(400, 413, 404, 409, 503, 500)}
	// ChangeSessionCWD changes a session working directory.
	ChangeSessionCWD = Operation[SessionPath, protocol.ChangeCWDInput, protocol.ChangeWorkspaceCWDResult]{ID: "changeSessionCWD", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/cwd", Success: http.StatusOK, Errors: lifecycleErrors(400, 413, 404, 409, 503, 500)}
	// ConfigureSession applies a revision-guarded model configuration.
	ConfigureSession = Operation[SessionPath, protocol.ConfigureSessionInput, protocol.ConfigureSessionResult]{ID: "configureSession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/configure", Success: http.StatusOK, Errors: lifecycleErrors(400, 413, 404, 409, 422, 503, 500)}
	// CompactSession performs an explicit context compaction.
	CompactSession = Operation[SessionPath, protocol.CompactSessionInput, protocol.CompactSessionResult]{ID: "compactSession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/compact", Success: http.StatusOK, Errors: lifecycleErrors(400, 413, 404, 409, 422, 503, 500)}
	// ReloadSession refreshes an idle session runtime.
	ReloadSession = Operation[SessionPath, NoBody, protocol.ReloadSessionResult]{ID: "reloadSession", Tag: "sessions", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/reload", Success: http.StatusOK, Errors: lifecycleErrors(400, 404, 409, 503, 500)}
)
