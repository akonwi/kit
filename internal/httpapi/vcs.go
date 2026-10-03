package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// VCSStatusRecord is the only record of the repository-status stream.
const VCSStatusRecord = "vcs.status"

// MaxVCSRecordBytes bounds one encoded repository-status record.
const MaxVCSRecordBytes = 64 << 10

var (
	// GetSessionVCS reads volatile repository status for a session workspace.
	GetSessionVCS = Operation[SessionPath, NoBody, protocol.SessionVCSStatus]{
		ID: "getSessionVCS", Tag: "vcs", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/vcs", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
	// StreamSessionVCS pushes the latest repository status on connect, then
	// deduplicated latest-only updates. It does not resume or replay.
	StreamSessionVCS = StreamOperation[SessionPath, protocol.SessionVCSStatus]{
		ID: "streamSessionVCS", Tag: "vcs", Path: "/v1/sessions/{sessionID}/vcs/events",
		Records: []string{VCSStatusRecord}, MaxRecordBytes: MaxVCSRecordBytes,
		Validate: protocol.SessionVCSStatus.Validate,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
)
