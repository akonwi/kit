package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// ViewRevealRecord is the only record of the view reveal stream.
const ViewRevealRecord = "view.reveal"

// MaxViewRevealRecordBytes bounds one encoded view reveal record.
const MaxViewRevealRecordBytes = 1 << 10

// ViewPath names one view identifier of a session (ADR 0068).
type ViewPath struct {
	SessionID string `path:"sessionID"`
	ViewID    string `path:"viewID"`
}

var (
	// RevealView asks the view clients watching a view identifier to show a
	// line or an annotation, and reports whether one was watching. Servers
	// that predate it answer with an unrouted 404, which clients treat as
	// unsupported.
	RevealView = Operation[ViewPath, protocol.ViewRevealInput, protocol.ViewRevealResult]{
		ID: "revealView", Tag: "workspace", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/views/{viewID}/reveal", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
	// StreamViewReveals pushes the reveals sent to a view identifier after
	// the stream opens. It sends nothing on connect and does not resume or
	// replay. Servers that predate it answer with an unrouted 404.
	StreamViewReveals = StreamOperation[ViewPath, protocol.ViewReveal]{
		ID: "streamViewReveals", Tag: "workspace", Path: "/v1/sessions/{sessionID}/views/{viewID}/events",
		Records: []string{ViewRevealRecord}, MaxRecordBytes: MaxViewRevealRecordBytes,
		Validate: protocol.ViewReveal.Validate,
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
