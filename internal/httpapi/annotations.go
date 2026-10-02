package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

// AnnotationListParams binds a session identity and bounded annotation pagination.
type AnnotationListParams struct {
	SessionID string `path:"sessionID"`
	Cursor    string `query:"cursor,omitempty"`
	PageSize  int    `query:"pageSize,omitempty"`
}

func annotationErrors(statuses ...int) []ErrorResponse {
	responses := make([]ErrorResponse, 0, len(statuses))
	for _, status := range statuses {
		code := ErrorCode("")
		switch status {
		case http.StatusBadRequest:
			code = ErrorInvalidRequest
		case http.StatusNotFound:
			code = ErrorNotFound
		case http.StatusConflict:
			code = ErrorConflict
		case http.StatusRequestEntityTooLarge:
			code = ErrorLimitExceeded
		case http.StatusServiceUnavailable:
			code = ErrorUnavailable
		case http.StatusInternalServerError:
			code = ErrorInternal
		}
		responses = append(responses, ErrorResponse{Status: status, Codes: []ErrorCode{code}})
	}
	return responses
}

var (
	// ListAnnotations returns a bounded page of session annotations.
	ListAnnotations = Operation[AnnotationListParams, NoBody, protocol.AnnotationPage]{ID: "listAnnotations", Tag: "annotations", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/annotations", Success: http.StatusOK, Errors: annotationErrors(400, 404, 503, 500)}
	// CreateAnnotation creates an anchored annotation.
	CreateAnnotation = Operation[SessionPath, protocol.CreateAnnotationInput, protocol.Annotation]{ID: "createAnnotation", Tag: "annotations", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/annotations", Success: http.StatusCreated, Errors: annotationErrors(400, 404, 413, 409, 503, 500)}
	// UpdateAnnotation replaces an annotation body.
	UpdateAnnotation = Operation[SessionPath, protocol.UpdateAnnotationInput, protocol.Annotation]{ID: "updateAnnotation", Tag: "annotations", Method: http.MethodPatch, Path: "/v1/sessions/{sessionID}/annotations", Success: http.StatusOK, Errors: annotationErrors(400, 404, 413, 409, 503, 500)}
	// DeleteAnnotation deletes an annotation.
	DeleteAnnotation = Operation[SessionPath, protocol.DeleteAnnotationInput, NoBody]{ID: "deleteAnnotation", Tag: "annotations", Method: http.MethodDelete, Path: "/v1/sessions/{sessionID}/annotations", Success: http.StatusNoContent, Errors: annotationErrors(400, 404, 409, 503, 500)}
)
