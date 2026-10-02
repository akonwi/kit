package httpapi

import (
	"net/http"
	"reflect"

	"github.com/akonwi/kit/internal/protocol"
)

type WorkspaceErrorDetails map[string]string

type FileIndexParams struct {
	SessionID string `path:"sessionID"`
	Refresh   bool   `query:"refresh,omitempty"`
}

func workspaceErrors() []ErrorResponse {
	details := reflect.TypeOf(WorkspaceErrorDetails{})
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest, ErrorCode(protocol.WorkspaceErrorInvalidPath), ErrorCode(protocol.WorkspaceErrorNotDirectory), ErrorCode(protocol.WorkspaceErrorNotFile), ErrorCode(protocol.WorkspaceErrorSymlink)}},
		{Status: http.StatusForbidden, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorPermissionDenied), ErrorCode(protocol.WorkspaceErrorOutside)}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
		{Status: http.StatusConflict, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorStaleWorkspace), ErrorCode(protocol.WorkspaceErrorStaleFile), ErrorCode(protocol.WorkspaceErrorStaleCursor)}, Details: map[ErrorCode]reflect.Type{ErrorCode(protocol.WorkspaceErrorStaleWorkspace): details, ErrorCode(protocol.WorkspaceErrorStaleFile): details}},
		{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}, Details: map[ErrorCode]reflect.Type{ErrorLimitExceeded: details}},
		{Status: http.StatusUnsupportedMediaType, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorBinary)}},
		{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}, Details: map[ErrorCode]reflect.Type{ErrorCapacityExceeded: details}},
		{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

var (
	GetWorkspace           = Operation[SessionPath, NoBody, protocol.WorkspaceRef]{ID: "getWorkspace", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/workspace", Success: http.StatusOK, Errors: workspaceErrors()}
	ListWorkspaceDirectory = Operation[SessionPath, protocol.ListDirectoryInput, protocol.DirectoryPage]{ID: "listWorkspaceDirectory", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/workspace/directories", Success: http.StatusOK, Errors: workspaceErrors()}
	ReadWorkspaceFile      = Operation[SessionPath, protocol.ReadWorkspaceFileInput, protocol.WorkspaceFileRead]{ID: "readWorkspaceFile", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/workspace/files/read", Success: http.StatusOK, Errors: workspaceErrors()}
	GetSessionFileIndex    = Operation[FileIndexParams, NoBody, protocol.SessionFileIndex]{ID: "getSessionFileIndex", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/files", Success: http.StatusOK, Errors: workspaceErrors()}
)
