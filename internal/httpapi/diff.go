package httpapi

import (
	"net/http"
	"reflect"

	protocol "github.com/akonwi/kit/api/contract"
)

type diffUnsupportedRepositoryDetails struct {
	Reason string `json:"reason"`
}

type diffLimitDetails struct {
	Limit string `json:"limit"`
}

type diffCapacityDetails struct {
	Scope string `json:"scope"`
}

func diffCode(code protocol.DiffErrorCode) ErrorCode { return ErrorCode(code) }

func diffErrors(includeFileErrors bool) []ErrorResponse {
	conflicts := []ErrorCode{
		diffCode(protocol.DiffErrorStaleWorkspace),
		diffCode(protocol.DiffErrorStaleTarget),
	}
	if includeFileErrors {
		conflicts = append(conflicts, diffCode(protocol.DiffErrorStaleFile), diffCode(protocol.DiffErrorStaleCursor))
	}
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest, diffCode(protocol.DiffErrorInvalidPath)}},
		{Status: http.StatusForbidden, Codes: []ErrorCode{diffCode(protocol.DiffErrorPermissionDenied)}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound, diffCode(protocol.DiffErrorNotRepository)}},
		{Status: http.StatusConflict, Codes: conflicts},
		{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{diffCode(protocol.DiffErrorLimit)}, Details: map[ErrorCode]reflect.Type{diffCode(protocol.DiffErrorLimit): reflect.TypeOf(diffLimitDetails{})}},
		{Status: http.StatusUnprocessableEntity, Codes: []ErrorCode{diffCode(protocol.DiffErrorUnsupportedRepository)}, Details: map[ErrorCode]reflect.Type{diffCode(protocol.DiffErrorUnsupportedRepository): reflect.TypeOf(diffUnsupportedRepositoryDetails{})}},
		{Status: http.StatusTooManyRequests, Codes: []ErrorCode{diffCode(protocol.DiffErrorCapacity)}, Details: map[ErrorCode]reflect.Type{diffCode(protocol.DiffErrorCapacity): reflect.TypeOf(diffCapacityDetails{})}},
		{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{diffCode(protocol.DiffErrorRepositoryUnavailable), ErrorUnavailable}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

var (
	// ListDiffTargets returns the bounded catalog of selectable diff targets.
	ListDiffTargets = Operation[SessionPath, protocol.ListDiffTargetsInput, protocol.DiffTargetCatalog]{ID: "listDiffTargets", Tag: "diff", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/diff/targets", Success: http.StatusOK, Errors: diffErrors(false)}
	// ObserveDiff returns a retained observation for a server-issued target reference.
	ObserveDiff = Operation[SessionPath, protocol.ObserveDiffInput, protocol.DiffPage]{ID: "observeDiff", Tag: "diff", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/diff/observations", Success: http.StatusOK, Errors: diffErrors(true)}
	// ObserveWorkingTree returns a retained observation of the current working tree.
	ObserveWorkingTree = Operation[SessionPath, protocol.ObserveWorkingTreeInput, protocol.WorkingTreePage]{ID: "observeWorkingTree", Tag: "diff", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/diff/working-tree", Success: http.StatusOK, Errors: diffErrors(true)}
	// ReadFileDiff returns guarded semantic hunk fragments for one observed file.
	ReadFileDiff = Operation[SessionPath, protocol.ReadFileDiffInput, protocol.FileDiffPage]{ID: "readFileDiff", Tag: "diff", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/diff/files/read", Success: http.StatusOK, Errors: diffErrors(true)}
)
