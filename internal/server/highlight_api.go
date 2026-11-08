package server

import (
	"errors"
	"fmt"
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/codehighlight"
	"github.com/akonwi/kit/internal/httpapi"
)

// registerHighlightRoutes serves highlightCode (ADR 0062). The route is not
// bound to a session, but like every API route it is reached only after the
// handler's Host, Origin, credential, instance, and protocol checks.
func registerHighlightRoutes(mux *http.ServeMux, highlights *codehighlight.Service) {
	httpapi.HandleRaw(mux, httpapi.HighlightCode, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.HighlightCodeInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
				writeSessionError(writer, highlightLimitAPIError(protocol.HighlightCodeLimitTotalBytes))
				return
			}
			writeSessionError(writer, invalidAPIError())
			return
		}
		if err := input.Validate(); err != nil {
			if limit, ok := errors.AsType[*protocol.HighlightCodeLimitError](err); ok {
				writeSessionError(writer, highlightLimitAPIError(limit.Limit))
				return
			}
			writeSessionError(writer, invalidAPIError())
			return
		}
		result := highlights.Code(request.Context(), input)
		if err := result.Validate(input); err != nil {
			writeSessionError(writer, internalAPIErrorCausedBy(fmt.Errorf("invalid highlight result: %w", err)))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
}

// highlightLimitAPIError rejects a highlightCode request over limit.
func highlightLimitAPIError(limit protocol.HighlightCodeLimit) error {
	return httpapi.NewAPIError(http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, "highlight request is too large", protocol.HighlightCodeLimitDetails{Limit: limit})
}
