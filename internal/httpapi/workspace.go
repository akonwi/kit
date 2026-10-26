package httpapi

import (
	"net/http"
	"reflect"

	protocol "github.com/akonwi/kit/api/contract"
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

// hostFileErrors are the failures of a host file read. A host read is not
// confined to a workspace, so it has no workspace staleness or traversal
// errors.
func hostFileErrors() []ErrorResponse {
	details := reflect.TypeOf(WorkspaceErrorDetails{})
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest, ErrorCode(protocol.WorkspaceErrorInvalidPath), ErrorCode(protocol.WorkspaceErrorNotFile)}},
		{Status: http.StatusForbidden, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorPermissionDenied)}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
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
	// ReadHostFile reads one file anywhere on the server's machine, view-only.
	ReadHostFile        = Operation[SessionPath, protocol.ReadHostFileInput, protocol.HostFileRead]{ID: "readHostFile", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/host-files/read", Success: http.StatusOK, Errors: hostFileErrors()}
	GetSessionFileIndex = Operation[FileIndexParams, NoBody, protocol.SessionFileIndex]{ID: "getSessionFileIndex", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/files", Success: http.StatusOK, Errors: workspaceErrors()}
)
