package protocol

import "fmt"

// WorkspaceErrorCode is a stable client-safe workspace failure class.
type WorkspaceErrorCode string

const (
	WorkspaceErrorInvalidPath      WorkspaceErrorCode = "invalid_path"
	WorkspaceErrorNotFound         WorkspaceErrorCode = "not_found"
	WorkspaceErrorNotDirectory     WorkspaceErrorCode = "not_directory"
	WorkspaceErrorNotFile          WorkspaceErrorCode = "not_file"
	WorkspaceErrorPermissionDenied WorkspaceErrorCode = "permission_denied"
	WorkspaceErrorOutside          WorkspaceErrorCode = "outside_workspace"
	WorkspaceErrorSymlink          WorkspaceErrorCode = "symlink_traversal"
	WorkspaceErrorBinary           WorkspaceErrorCode = "binary_file"
	WorkspaceErrorStaleWorkspace   WorkspaceErrorCode = "stale_workspace"
	WorkspaceErrorStaleFile        WorkspaceErrorCode = "stale_file"
	WorkspaceErrorStaleCursor      WorkspaceErrorCode = "stale_cursor"
	WorkspaceErrorLimit            WorkspaceErrorCode = "limit_exceeded"
	WorkspaceErrorCapacity         WorkspaceErrorCode = "capacity_exceeded"
	WorkspaceErrorUnavailable      WorkspaceErrorCode = "unavailable"
)

// WorkspaceError is the canonical wire and client error envelope.
type WorkspaceError struct {
	Code    WorkspaceErrorCode `json:"code"`
	Message string             `json:"message"`
	Details map[string]string  `json:"details,omitempty"`
}

func (e *WorkspaceError) Error() string { return e.Message }

func (e WorkspaceError) Validate() error {
	switch e.Code {
	case WorkspaceErrorInvalidPath, WorkspaceErrorNotFound, WorkspaceErrorNotDirectory, WorkspaceErrorNotFile,
		WorkspaceErrorPermissionDenied, WorkspaceErrorOutside, WorkspaceErrorSymlink, WorkspaceErrorBinary,
		WorkspaceErrorStaleWorkspace, WorkspaceErrorStaleFile, WorkspaceErrorStaleCursor, WorkspaceErrorLimit,
		WorkspaceErrorCapacity, WorkspaceErrorUnavailable:
	default:
		return fmt.Errorf("workspace error code is invalid")
	}
	if !validRendererText(e.Message, 512) || len(e.Details) > 4 {
		return fmt.Errorf("workspace error message or details are invalid")
	}
	allowed := map[string]bool{}
	switch e.Code {
	case WorkspaceErrorStaleWorkspace:
		allowed["currentWorkspaceId"], allowed["currentWorkspaceState"] = true, true
	case WorkspaceErrorStaleFile:
		allowed["currentFileRevision"] = true
	case WorkspaceErrorLimit:
		allowed["limit"] = true
	case WorkspaceErrorCapacity:
		allowed["scope"] = true
	}
	for key, value := range e.Details {
		if !allowed[key] || !validRendererText(key, 64) || !validRendererText(value, 128) {
			return fmt.Errorf("workspace error detail is invalid")
		}
		switch key {
		case "currentWorkspaceId":
			if !validWorkspaceToken(value, "workspace_") {
				return fmt.Errorf("workspace error id detail is invalid")
			}
		case "currentWorkspaceState":
			if value != string(WorkspaceReady) && value != string(WorkspaceUnavailable) {
				return fmt.Errorf("workspace error state detail is invalid")
			}
		case "currentFileRevision":
			if !validWorkspaceToken(value, "file_") {
				return fmt.Errorf("workspace error revision detail is invalid")
			}
		case "limit":
			if !allowedWorkspaceLimit(value) {
				return fmt.Errorf("workspace error limit detail is invalid")
			}
		}
	}
	if e.Code == WorkspaceErrorLimit && e.Details["limit"] == "" || e.Code == WorkspaceErrorCapacity && e.Details["scope"] != "session" && e.Details["scope"] != "daemon" {
		return fmt.Errorf("workspace error required detail is invalid")
	}
	return nil
}

func allowedWorkspaceLimit(value string) bool {
	switch value {
	case "path_bytes", "path_components", "path_name_checks", "page_size", "directory_entries", "directory_response_bytes", "directory_observation_bytes":
		return true
	default:
		return false
	}
}
