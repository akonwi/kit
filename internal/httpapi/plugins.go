package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

// PluginToastRecord is the only record of the live plugin-notification stream.
const PluginToastRecord = "plugin.toast"

// MaxPluginToastRecordBytes bounds one encoded plugin-notification record.
const MaxPluginToastRecordBytes = 32 << 10

var (
	// ExecutePluginCommand invokes one selected plugin command.
	ExecutePluginCommand = Operation[SessionPath, protocol.PluginCommandInput, NoBody]{
		ID: "executePluginCommand", Tag: "plugin", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/plugin-commands", Success: http.StatusNoContent,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict, ErrorCode(protocol.PluginCommandUnavailable)}},
			{Status: http.StatusUnprocessableEntity, Codes: []ErrorCode{ErrorCode(protocol.PluginCommandFailed)}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
		},
	}
	// StreamPluginToasts pushes fresh live-only plugin notifications.
	StreamPluginToasts = StreamOperation[SessionPath, protocol.PluginToast]{
		ID: "streamPluginToasts", Tag: "plugin", Path: "/v1/sessions/{sessionID}/plugin-toasts",
		Records: []string{PluginToastRecord}, MaxRecordBytes: MaxPluginToastRecordBytes,
		Validate: protocol.PluginToast.Validate,
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
