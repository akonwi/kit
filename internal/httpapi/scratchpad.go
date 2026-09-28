package httpapi

import (
	"net/http"
	"reflect"

	"github.com/akonwi/kit/internal/protocol"
)

var (
	stringErrorType          = reflect.TypeOf(StringErrorEnvelope{})
	typedScratchpadErrorType = reflect.TypeOf(ScratchpadTypedErrorEnvelope{})
	scratchpadErrorBodies    = []reflect.Type{typedScratchpadErrorType, stringErrorType}

	// GetScratchpad reads the authoritative shared scratchpad.
	GetScratchpad = Operation[SessionPath, NoBody, protocol.Scratchpad]{
		ID: "getScratchpad", Tag: "scratchpad", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/scratchpad", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Bodies: []reflect.Type{stringErrorType}},
			{Status: http.StatusNotFound, Bodies: []reflect.Type{stringErrorType}},
			{Status: http.StatusConflict, Bodies: scratchpadErrorBodies, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadMigrationRequired, protocol.ScratchpadUnsupported}},
			{Status: http.StatusServiceUnavailable, Bodies: scratchpadErrorBodies, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadUnavailable}},
		},
	}
	// UpdateScratchpad applies one revision-guarded replacement.
	UpdateScratchpad = Operation[SessionPath, protocol.UpdateScratchpadInput, protocol.Scratchpad]{
		ID: "updateScratchpad", Tag: "scratchpad", Method: http.MethodPut, Path: "/v1/sessions/{sessionID}/scratchpad", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Bodies: scratchpadErrorBodies, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadInvalidContent}},
			{Status: http.StatusNotFound, Bodies: []reflect.Type{stringErrorType}},
			{Status: http.StatusRequestEntityTooLarge, Bodies: []reflect.Type{typedScratchpadErrorType}, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadTooLarge}},
			{Status: http.StatusConflict, Bodies: scratchpadErrorBodies, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadRevisionConflict, protocol.ScratchpadRevisionExhausted, protocol.ScratchpadMigrationRequired, protocol.ScratchpadUnsupported}},
			{Status: http.StatusServiceUnavailable, Bodies: scratchpadErrorBodies, ScratchpadCodes: []protocol.ScratchpadErrorCode{protocol.ScratchpadUnavailable}},
		},
	}
)
