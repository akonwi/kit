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
		// conflict reports a session that is being deleted.
		{Status: http.StatusConflict, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorStaleWorkspace), ErrorCode(protocol.WorkspaceErrorStaleFile), ErrorCode(protocol.WorkspaceErrorStaleCursor), ErrorConflict}, Details: map[ErrorCode]reflect.Type{ErrorCode(protocol.WorkspaceErrorStaleWorkspace): details, ErrorCode(protocol.WorkspaceErrorStaleFile): details}},
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

// FileContentParams name one file to download (ADR 0074). With WorkspaceID,
// Path is a canonical workspace path; without it, Path is an absolute, "~",
// or "~/…" host path. The headers are the HTTP conditional and range
// requests the download honors.
type FileContentParams struct {
	SessionID   string `path:"sessionID"`
	WorkspaceID string `query:"workspaceId,omitempty"`
	Path        string `query:"path"`
	Range       string `header:"Range,omitempty"`
	IfRange     string `header:"If-Range,omitempty"`
	IfMatch     string `header:"If-Match,omitempty"`
	IfNoneMatch string `header:"If-None-Match,omitempty"`
}

// DownloadedFile is the raw body of a file download.
type DownloadedFile []byte

// fileContentErrors are the failures of a download before its body: those
// of a workspace read and of a host read, without binary_file.
func fileContentErrors() []ErrorResponse {
	details := reflect.TypeOf(WorkspaceErrorDetails{})
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest, ErrorCode(protocol.WorkspaceErrorInvalidPath), ErrorCode(protocol.WorkspaceErrorNotFile), ErrorCode(protocol.WorkspaceErrorSymlink)}},
		{Status: http.StatusForbidden, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorPermissionDenied), ErrorCode(protocol.WorkspaceErrorOutside)}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
		// conflict also reports a session that is being deleted.
		{Status: http.StatusConflict, Codes: []ErrorCode{ErrorCode(protocol.WorkspaceErrorStaleWorkspace), ErrorConflict}, Details: map[ErrorCode]reflect.Type{ErrorCode(protocol.WorkspaceErrorStaleWorkspace): details}},
		{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}, Details: map[ErrorCode]reflect.Type{ErrorLimitExceeded: details}},
		{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}, Details: map[ErrorCode]reflect.Type{ErrorCapacityExceeded: details}},
		{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

// Download response headers (ADR 0074).
const (
	FileWorkspaceHeader  = "X-Kit-Workspace-ID"
	FilePathHeader       = "X-Kit-File-Path"
	FileModifiedAtHeader = "X-Kit-File-Modified-At"
	SessionIDHeader      = "X-Kit-Session-ID"
)

var fileContentHeaders = []ResponseHeader{
	{Name: "ETag", Description: "The file's revision as a strong validator; absent for a file that reports a size of zero."},
	{Name: SessionIDHeader, Description: "The session.", Required: true},
	{Name: FileWorkspaceHeader, Description: "Workspace files only: the workspace the file was resolved in."},
	{Name: FilePathHeader, Description: "The canonical workspace path, or the cleaned absolute host path, percent-encoded as UTF-8.", Required: true},
	{Name: FileModifiedAtHeader, Description: "The file's modification time, RFC 3339 with nanoseconds.", Required: true},
	{Name: "Cache-Control", Description: "private, no-cache; or no-store for a file that reports a size of zero.", Required: true},
	{Name: "Accept-Ranges", Description: "bytes, or none for a file that reports a size of zero."},
	{Name: "Content-Range", Description: "The byte range a 206 or 416 response describes."},
	{Name: "X-Content-Type-Options", Description: "nosniff."},
	{Name: "Content-Security-Policy", Description: "sandbox; default-src 'none'."},
	{Name: "Content-Disposition", Description: "attachment, with the file's base name."},
}

var fileContentEmptyResponses = []EmptyResponse{
	{Status: http.StatusNotModified, Description: "The client's copy, named by If-None-Match, is current."},
	{Status: http.StatusPreconditionFailed, Description: "The file no longer has the revision If-Match names."},
	{Status: http.StatusRequestedRangeNotSatisfiable, Description: "The Range starts past the end of the file."},
}

var (
	// DownloadFile serves the complete bytes of a workspace or host file as an
	// ordinary HTTP resource (ADR 0074).
	DownloadFile = Operation[FileContentParams, NoBody, DownloadedFile]{
		ID: "downloadFile", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/files/content",
		Success: http.StatusOK, AdditionalSuccess: []int{http.StatusPartialContent}, Errors: fileContentErrors(),
		ResponseMediaType: "application/octet-stream", ResponseHeaders: fileContentHeaders, EmptyResponses: fileContentEmptyResponses,
	}
	// CheckFile returns the headers DownloadFile would, without the body: the
	// cheap way to learn a file's current revision.
	CheckFile = Operation[FileContentParams, NoBody, NoBody]{
		ID: "checkFile", Tag: "workspace", Method: http.MethodHead, Path: "/v1/sessions/{sessionID}/files/content",
		Success: http.StatusOK, Errors: fileContentErrors(), ResponseHeaders: fileContentHeaders, EmptyResponses: fileContentEmptyResponses,
	}
)

var (
	GetWorkspace           = Operation[SessionPath, NoBody, protocol.WorkspaceRef]{ID: "getWorkspace", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/workspace", Success: http.StatusOK, Errors: workspaceErrors()}
	ListWorkspaceDirectory = Operation[SessionPath, protocol.ListDirectoryInput, protocol.DirectoryPage]{ID: "listWorkspaceDirectory", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/workspace/directories", Success: http.StatusOK, Errors: workspaceErrors()}
	ReadWorkspaceFile      = Operation[SessionPath, protocol.ReadWorkspaceFileInput, protocol.WorkspaceFileRead]{ID: "readWorkspaceFile", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/workspace/files/read", Success: http.StatusOK, Errors: workspaceErrors()}
	// ReadHostFile reads one file anywhere on the server's machine, view-only.
	ReadHostFile        = Operation[SessionPath, protocol.ReadHostFileInput, protocol.HostFileRead]{ID: "readHostFile", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/host-files/read", Success: http.StatusOK, Errors: hostFileErrors()}
	GetSessionFileIndex = Operation[FileIndexParams, NoBody, protocol.SessionFileIndex]{ID: "getSessionFileIndex", Tag: "workspace", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/files", Success: http.StatusOK, Errors: workspaceErrors()}
)
