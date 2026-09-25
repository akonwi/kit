package protocol

import "fmt"

type DiffErrorCode string

const (
	DiffErrorInvalidPath           DiffErrorCode = "invalid_path"
	DiffErrorNotRepository         DiffErrorCode = "not_repository"
	DiffErrorUnsupportedRepository DiffErrorCode = "unsupported_repository"
	DiffErrorStaleWorkspace        DiffErrorCode = "stale_workspace"
	DiffErrorStaleTarget           DiffErrorCode = "stale_target"
	DiffErrorStaleFile             DiffErrorCode = "stale_file"
	DiffErrorStaleCursor           DiffErrorCode = "stale_cursor"
	DiffErrorNotFound              DiffErrorCode = "not_found"
	DiffErrorPermissionDenied      DiffErrorCode = "permission_denied"
	DiffErrorLimit                 DiffErrorCode = "limit_exceeded"
	DiffErrorCapacity              DiffErrorCode = "capacity_exceeded"
	DiffErrorRepositoryUnavailable DiffErrorCode = "repository_unavailable"
	DiffErrorUnavailable           DiffErrorCode = "unavailable"
)

type DiffError struct {
	Code    DiffErrorCode     `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func (e *DiffError) Error() string { return e.Message }
func (e DiffError) Validate() error {
	valid := map[DiffErrorCode]bool{DiffErrorInvalidPath: true, DiffErrorNotRepository: true, DiffErrorUnsupportedRepository: true, DiffErrorStaleWorkspace: true, DiffErrorStaleTarget: true, DiffErrorStaleFile: true, DiffErrorStaleCursor: true, DiffErrorNotFound: true, DiffErrorPermissionDenied: true, DiffErrorLimit: true, DiffErrorCapacity: true, DiffErrorRepositoryUnavailable: true, DiffErrorUnavailable: true}
	if !valid[e.Code] || !validRendererText(e.Message, 512) || len(e.Details) > 4 {
		return fmt.Errorf("diff error is invalid")
	}
	allowed := map[string]bool{}
	switch e.Code {
	case DiffErrorUnsupportedRepository:
		allowed["reason"] = true
	case DiffErrorLimit:
		allowed["limit"] = true
	case DiffErrorCapacity:
		allowed["scope"] = true
	case DiffErrorStaleWorkspace:
		allowed["currentWorkspaceId"] = true
	case DiffErrorStaleTarget:
		allowed["currentTargetRevision"] = true
	case DiffErrorStaleFile:
		allowed["currentFileRevision"] = true
	}
	for k, v := range e.Details {
		if !allowed[k] || !validRendererText(v, 128) {
			return fmt.Errorf("diff error detail is invalid")
		}
	}
	if e.Code == DiffErrorUnsupportedRepository && !map[string]bool{"workspace_repository_mismatch": true, "bare_repository": true, "gitfile": true, "object_alternates": true, "partial_clone": true, "sparse_checkout": true, "sparse_index": true, "config_authority": true, "repository_format": true, "metadata_authority": true}[e.Details["reason"]] {
		return fmt.Errorf("diff repository reason is invalid")
	}
	if e.Code == DiffErrorLimit && !map[string]bool{"path_bytes": true, "path_components": true, "path_name_checks": true, "page_size": true, "response_bytes": true, "candidate_limit": true, "byte_limit": true, "observation_limit": true, "deadline": true, "diff_work": true, "diff_memory": true, "edit_limit": true, "hunk_limit": true}[e.Details["limit"]] {
		return fmt.Errorf("diff limit detail is invalid")
	}
	if e.Code == DiffErrorCapacity && e.Details["scope"] != "session" && e.Details["scope"] != "daemon" {
		return fmt.Errorf("diff capacity scope is invalid")
	}
	return nil
}
