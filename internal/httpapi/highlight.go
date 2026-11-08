package httpapi

import (
	"net/http"
	"reflect"

	protocol "github.com/akonwi/kit/api/contract"
)

func highlightErrors() []ErrorResponse {
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
		{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}, Details: map[ErrorCode]reflect.Type{ErrorLimitExceeded: reflect.TypeOf(protocol.HighlightCodeLimitDetails{})}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

// HighlightCode colors a bounded batch of code blocks with the server's
// highlighter (ADR 0062). A request over the bounds of HighlightCodeInput,
// including a body over the session request limit, is limit_exceeded.
var HighlightCode = Operation[ServerPath, protocol.HighlightCodeInput, protocol.HighlightCodeResult]{ID: "highlightCode", Tag: "highlight", Method: http.MethodPost, Path: "/v1/highlight", Success: http.StatusOK, Errors: highlightErrors()}
