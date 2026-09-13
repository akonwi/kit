package daemon

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/protocol"
)

type recordingAttachmentService struct {
	sessionID string
	input     attachment.PutInput
	content   []byte
	info      protocol.AttachmentInfo
}

func (service *recordingAttachmentService) Put(_ context.Context, sessionID string, input attachment.PutInput) (protocol.AttachmentInfo, error) {
	service.sessionID, service.input = sessionID, input
	service.content, _ = io.ReadAll(input.Content)
	if input.Validate != nil {
		if err := input.Validate(bytes.NewReader(service.content)); err != nil {
			return protocol.AttachmentInfo{}, err
		}
	}
	return service.info, nil
}

func (service *recordingAttachmentService) Open(_ context.Context, sessionID, id string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	service.sessionID = sessionID
	return service.info, io.NopCloser(bytes.NewReader(service.content)), nil
}

func TestAttachmentRoutesUploadAndRetrieveImage(t *testing.T) {
	t.Parallel()
	var imageBytes bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 2, 3))
	source.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageBytes, source); err != nil {
		t.Fatal(err)
	}
	info := protocol.AttachmentInfo{
		ID: "attachment_0123456789abcdef0123456789abcdef", SessionID: "session_one", Filename: "photo.png", MediaType: "image/png",
		Size: int64(imageBytes.Len()), SHA256: strings.Repeat("a", 64), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Width: 2, Height: 3,
	}
	service := &recordingAttachmentService{info: info}
	mux := http.NewServeMux()
	registerAttachmentRoutes(mux, service)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(imageBytes.Bytes())
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_one/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d: %s", response.Code, response.Body.String())
	}
	if service.sessionID != "session_one" || service.input.MediaType != "image/png" || service.input.Width != 2 || service.input.Height != 3 || !bytes.Equal(service.content, imageBytes.Bytes()) {
		t.Fatalf("uploaded input = session %q, input %#v, bytes %d", service.sessionID, service.input, len(service.content))
	}

	service.content = imageBytes.Bytes()
	request = httptest.NewRequest(http.MethodGet, "/v1/sessions/session_one/attachments/"+info.ID, nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Kit-Attachment-ID") != info.ID || !bytes.Equal(response.Body.Bytes(), imageBytes.Bytes()) {
		t.Fatalf("retrieval = %d, headers %#v, bytes %d", response.Code, response.Header(), response.Body.Len())
	}
}

func TestAttachmentRouteRejectsMultipleParts(t *testing.T) {
	t.Parallel()
	service := &recordingAttachmentService{}
	mux := http.NewServeMux()
	registerAttachmentRoutes(mux, service)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, name := range []string{"one.txt", "two.txt"} {
		part, _ := writer.CreateFormFile("file", name)
		_, _ = part.Write([]byte("text"))
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_one/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || service.sessionID != "" {
		t.Fatalf("response = %d, service session = %q", response.Code, service.sessionID)
	}
}

func TestInspectAttachmentRejectsMalformedImage(t *testing.T) {
	t.Parallel()
	if _, err := inspectAttachment("bad.png", strings.NewReader("\x89PNG\r\n\x1a\nnot an image")); err == nil {
		t.Fatal("malformed PNG accepted")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	input, err := inspectAttachment("polyglot.png", bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Validate(bytes.NewReader(append(encoded.Bytes(), []byte("trailing")...))); err == nil {
		t.Fatal("PNG with trailing payload accepted")
	}
}
