package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

type AttachmentPath struct {
	SessionID    string `path:"sessionID"`
	AttachmentID string `path:"attachmentID"`
}

// AttachmentUpload is the multipart attachment request schema.
type AttachmentUpload struct {
	File []byte `json:"file"`
}

// AttachmentContent is the raw attachment response body.
type AttachmentContent []byte

func attachmentErrors() []ErrorResponse {
	return []ErrorResponse{
		{Status: http.StatusBadRequest, Codes: []ErrorCode{ErrorInvalidRequest}},
		{Status: http.StatusNotFound, Codes: []ErrorCode{ErrorNotFound}},
		{Status: http.StatusRequestEntityTooLarge, Codes: []ErrorCode{ErrorLimitExceeded}},
		{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}},
	}
}

var (
	UploadAttachment = Operation[SessionPath, AttachmentUpload, protocol.AttachmentInfo]{
		ID: "uploadAttachment", Tag: "attachments", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/attachments", Success: http.StatusCreated,
		Errors: attachmentErrors(), RequestMediaType: "multipart/form-data",
	}
	ResolveAttachments = Operation[SessionPath, protocol.AttachmentResolutionInput, protocol.AttachmentResolution]{
		ID: "resolveAttachments", Tag: "attachments", Method: http.MethodPost, Path: "/v1/sessions/{sessionID}/attachments/resolve", Success: http.StatusOK, Errors: attachmentErrors(),
	}
	ReadAttachment = Operation[AttachmentPath, NoBody, AttachmentContent]{
		ID: "readAttachment", Tag: "attachments", Method: http.MethodGet, Path: "/v1/sessions/{sessionID}/attachments/{attachmentID}", Success: http.StatusOK,
		Errors: attachmentErrors(), ResponseMediaTypes: []string{"text/plain", "image/png", "image/jpeg", "image/gif", "image/webp"},
	}
)
