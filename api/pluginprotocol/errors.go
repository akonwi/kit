package pluginprotocol

// ErrorCode is a JSON-RPC error code used by the protocol.
type ErrorCode int

// Standard JSON-RPC and protocol error codes.
const (
	CodeParseError     ErrorCode = -32700 // The line is not JSON.
	CodeInvalidRequest ErrorCode = -32600 // Not a valid request, including a batch.
	CodeMethodNotFound ErrorCode = -32601 // Unknown method.
	CodeInvalidParams  ErrorCode = -32602 // Invalid params; the message names the rule broken.
	CodeInternalError  ErrorCode = -32603 // Internal error.
	CodeFailed         ErrorCode = -32000 // A command or tool failed, or Kit cannot show dialogs.
	CodeCancelled      ErrorCode = -32001 // The request was cancelled.
	CodeUnavailable    ErrorCode = -32002 // The instance's generation is stopping, stopped, or replaced.
	CodeConflict       ErrorCode = -32003 // Duplicate contribution or reused idempotency key.
	CodeLimitExceeded  ErrorCode = -32005 // A size or count limit was exceeded.
	CodeTryLater       ErrorCode = -32006 // Try again later; data.reason says why.
)

// ErrorCodeInfo describes one error code.
type ErrorCodeInfo struct {
	Code ErrorCode
	// Name is the code's name without the Code prefix, as generated code
	// spells it.
	Name    string
	Meaning string
}

// ErrorCodes lists every code with its name and meaning, in specification
// order.
func ErrorCodes() []ErrorCodeInfo {
	return []ErrorCodeInfo{
		{CodeParseError, "ParseError", "The line is not valid JSON or UTF-8."},
		{CodeInvalidRequest, "InvalidRequest", "The message is not a valid request, including a batch."},
		{CodeMethodNotFound, "MethodNotFound", "Unknown method."},
		{CodeInvalidParams, "InvalidParams", "Invalid params; the message names the rule broken."},
		{CodeInternalError, "InternalError", "Internal error."},
		{CodeFailed, "Failed", "A command or tool failed, or Kit cannot show dialogs (data.reason interactivity_unavailable)."},
		{CodeCancelled, "Cancelled", "The request was cancelled."},
		{CodeUnavailable, "Unavailable", "The instance's generation is stopping, stopped, or replaced."},
		{CodeConflict, "Conflict", "A duplicate contribution, or an idempotency key reused with different text."},
		{CodeLimitExceeded, "LimitExceeded", "A size or count limit was exceeded."},
		{CodeTryLater, "TryLater", "Try again later; data.reason says why."},
	}
}

// ErrorReason is the data.reason of an error that carries one.
type ErrorReason string

// Error reasons.
const (
	// ReasonBusy: the plugin has too many requests to Kit in flight (-32006).
	ReasonBusy ErrorReason = "busy"
	// ReasonSessionBusy: the session cannot start a turn now (-32006).
	ReasonSessionBusy ErrorReason = "session_busy"
	// ReasonRateLimited: too many URL requests in the window (-32006).
	ReasonRateLimited ErrorReason = "rate_limited"
	// ReasonInteractivityUnavailable: Kit cannot show dialogs (-32000).
	ReasonInteractivityUnavailable ErrorReason = "interactivity_unavailable"
)

// EnumValues lists the error reasons.
func (ErrorReason) EnumValues() []string {
	return []string{string(ReasonBusy), string(ReasonSessionBusy), string(ReasonRateLimited), string(ReasonInteractivityUnavailable)}
}

// ErrorData is the structured data of an error Kit sends. Plugins ignore
// members they do not know.
type ErrorData struct {
	Reason *ErrorReason `json:"reason,omitempty"`
}
