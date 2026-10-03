package httpapi

import (
	"net/http"
	"reflect"

	protocol "github.com/akonwi/kit/api/contract"
)

func scratchpadCode(code protocol.ScratchpadErrorCode) ErrorCode { return ErrorCode(code) }

var scratchpadConflictDetails = reflect.TypeOf(protocol.ScratchpadErrorDetails{})

var (
	// GetScratchpad reads the authoritative shared scratchpad.
	GetScratchpad = Operation[SessionPath, NoBody, protocol.Scratchpad]{
		ID: "getScratchpad", Tag: "scratchpad", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/scratchpad", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusConflict, Codes: []ErrorCode{scratchpadCode(protocol.ScratchpadMigrationRequired), scratchpadCode(protocol.ScratchpadUnsupported)}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{scratchpadCode(protocol.ScratchpadUnavailable)}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
	// UpdateScratchpad applies one revision-guarded replacement.
	UpdateScratchpad = Operation[SessionPath, protocol.UpdateScratchpadInput, protocol.Scratchpad]{
		ID: "updateScratchpad", Tag: "scratchpad", Method: http.MethodPut, Path: "/v1/sessions/{sessionID}/scratchpad", Success: http.StatusOK,
		Errors: []ErrorResponse{
			{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest, scratchpadCode(protocol.ScratchpadInvalidContent)}},
			{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
			{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{scratchpadCode(protocol.ScratchpadTooLarge), ErrorLimitExceeded}},
			{Status: http.StatusConflict, Codes: []ErrorCode{scratchpadCode(protocol.ScratchpadRevisionConflict), scratchpadCode(protocol.ScratchpadRevisionExhausted), scratchpadCode(protocol.ScratchpadMigrationRequired), scratchpadCode(protocol.ScratchpadUnsupported)}, Details: map[ErrorCode]reflect.Type{scratchpadCode(protocol.ScratchpadRevisionConflict): scratchpadConflictDetails}},
			{Status: http.StatusServiceUnavailable, Codes: []ErrorCode{scratchpadCode(protocol.ScratchpadUnavailable)}},
			{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
		},
	}
)
