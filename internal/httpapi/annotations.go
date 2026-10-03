package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
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
		codes := []ErrorCode{ErrorInternal}
		switch status {
		case http.StatusBadRequest:
			codes = []ErrorCode{ErrorInvalidRequest, ErrorCode(protocol.AnnotationEvidenceErrorInvalid)}
		case http.StatusForbidden:
			codes = []ErrorCode{ErrorCode(protocol.AnnotationEvidenceErrorPermission)}
		case http.StatusNotFound:
			codes = []ErrorCode{ErrorNotFound}
		case http.StatusConflict:
			codes = []ErrorCode{ErrorConflict, ErrorCode(protocol.AnnotationEvidenceErrorStaleWorkspace), ErrorCode(protocol.AnnotationEvidenceErrorStaleTarget), ErrorCode(protocol.AnnotationEvidenceErrorStaleFile)}
		case http.StatusRequestEntityTooLarge:
			codes = []ErrorCode{ErrorLimitExceeded, ErrorCode(protocol.AnnotationEvidenceErrorLimit)}
		case http.StatusServiceUnavailable:
			codes = []ErrorCode{ErrorUnavailable, ErrorCode(protocol.AnnotationEvidenceErrorUnavailable)}
		}
		responses = append(responses, ErrorResponse{Status: status, Codes: codes})
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
