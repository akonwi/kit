package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// PluginToastRecord is the only record of the live plugin-notification stream.
const PluginToastRecord = "plugin.toast"

// MaxPluginToastRecordBytes bounds one encoded plugin-notification record.
const MaxPluginToastRecordBytes = 32 << 10

// ClientPluginPath names one client plugin instance of a session.
type ClientPluginPath struct {
	SessionID  string `path:"sessionID"`
	InstanceID string `path:"instanceID"`
}

// ClientPluginCallPath names one call to a client plugin instance.
type ClientPluginCallPath struct {
	SessionID  string `path:"sessionID"`
	InstanceID string `path:"instanceID"`
	CallID     string `path:"callID"`
}

var (
	// AttachClientPlugin declares or replaces a client-hosted plugin instance
	// (ADR 0051).
	AttachClientPlugin = Operation[ClientPluginPath, protocol.AttachClientPluginInput, NoBody]{
		ID: "attachClientPlugin", Tag: "plugin", Method: http.MethodPut, Path: "/v1/sessions/{sessionID}/client-plugins/{instanceID}", Success: http.StatusNoContent,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
	// DetachClientPlugin stops a client-hosted plugin instance.
	DetachClientPlugin = Operation[ClientPluginPath, NoBody, NoBody]{
		ID: "detachClientPlugin", Tag: "plugin", Method: http.MethodDelete, Path: "/v1/sessions/{sessionID}/client-plugins/{instanceID}", Success: http.StatusNoContent,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
	// CompleteClientPluginCall answers one pending call to a client-hosted
	// plugin instance.
	CompleteClientPluginCall = Operation[ClientPluginCallPath, protocol.CompleteClientPluginCallInput, NoBody]{
		ID: "completeClientPluginCall", Tag: "plugin", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/client-plugins/{instanceID}/calls/{callID}/result", Success: http.StatusNoContent,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{ErrorConflict}},
			{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{ErrorUnavailable}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
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
