package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/localimage"
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

func (service *recordingAttachmentService) Resolve(_ context.Context, sessionID string, ids []string) (protocol.AttachmentResolution, error) {
	service.sessionID = sessionID
	result := protocol.AttachmentResolution{}
	for _, id := range ids {
		if id == service.info.ID {
			result.Attachments = append(result.Attachments, service.info)
		} else {
			result.MissingAttachmentIDs = append(result.MissingAttachmentIDs, id)
		}
	}
	return result, nil
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
	if got := response.Header().Get("Content-Security-Policy"); got != "sandbox; default-src 'none'" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `inline; filename=photo.png` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestAttachmentRouteResolvesMetadataAndReportsMissingIDs(t *testing.T) {
	t.Parallel()
	info := protocol.AttachmentInfo{
		ID: "attachment_0123456789abcdef0123456789abcdef", SessionID: "session_one", Filename: "photo.png", MediaType: "image/png",
		Size: 42, SHA256: strings.Repeat("a", 64), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Width: 2, Height: 3,
	}
	missing := "attachment_abcdef0123456789abcdef0123456789"
	service := &recordingAttachmentService{info: info}
	mux := http.NewServeMux()
	registerAttachmentRoutes(mux, service)
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_one/attachments/resolve", strings.NewReader(`{"attachmentIds":["`+info.ID+`","`+missing+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"filename":"photo.png"`) || !strings.Contains(response.Body.String(), missing) {
		t.Fatalf("resolution = %d: %s", response.Code, response.Body.String())
	}
}

func TestResolveAttachmentMetadataDistinguishesMissingFromStorageFailure(t *testing.T) {
	t.Parallel()
	foundID := "attachment_0123456789abcdef0123456789abcdef"
	missingID := "attachment_abcdef0123456789abcdef0123456789"
	record := attachment.Record{ID: foundID, SessionID: "session_one", Filename: "photo.png"}
	result, err := resolveAttachmentMetadata(context.Background(), "session_one", []string{foundID, missingID}, func(_ context.Context, _, id string) (attachment.Record, error) {
		if id == missingID {
			return attachment.Record{}, attachment.ErrNotFound
		}
		return record, nil
	})
	if err != nil || len(result.Attachments) != 1 || result.Attachments[0].ID != foundID || len(result.MissingAttachmentIDs) != 1 || result.MissingAttachmentIDs[0] != missingID {
		t.Fatalf("resolution = %#v, %v", result, err)
	}
	if _, err := resolveAttachmentMetadata(context.Background(), "session_one", []string{foundID}, func(context.Context, string, string) (attachment.Record, error) {
		return attachment.Record{}, io.ErrUnexpectedEOF
	}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("storage failure = %v, want unexpected EOF", err)
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

// storeAttachmentService serves a real attachment store without a session
// manager, so route tests cover what the store records.
type storeAttachmentService struct{ store attachment.Store }

func (service storeAttachmentService) Put(ctx context.Context, sessionID string, input attachment.PutInput) (protocol.AttachmentInfo, error) {
	input.SessionID = sessionID
	record, err := service.store.Put(ctx, input)
	return projectAttachment(record), err
}

func (service storeAttachmentService) Open(ctx context.Context, sessionID, id string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	record, content, err := service.store.Open(ctx, sessionID, id)
	return projectAttachment(record), content, err
}

func (service storeAttachmentService) Resolve(ctx context.Context, sessionID string, ids []string) (protocol.AttachmentResolution, error) {
	return resolveAttachmentMetadata(ctx, sessionID, ids, service.store.Stat)
}

// A show_image image may be larger than one a model can see, as a Paper
// export at 2x often is; the routes still resolve and serve it.
func TestAttachmentRoutesServeAShownImageLargerThanAModelImage(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	const filename = "Explore · shell commands@2x.png"
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 2880, 4800))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, filename), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	// show_image's limits.
	record, err := localimage.Persist(context.Background(), localimage.Options{
		SessionID: "session_one", CWD: func() string { return cwd }, Store: store,
		Limits: attachment.ImageLimits{MaxBytes: protocol.MaxStoredImageBytes, MaxPixels: protocol.MaxStoredImagePixels},
	}, filename)
	if err != nil {
		t.Fatal(err)
	}
	details := struct{ AttachmentID string }{record.ID}
	mux := http.NewServeMux()
	registerAttachmentRoutes(mux, storeAttachmentService{store: store})

	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_one/attachments/resolve", strings.NewReader(`{"attachmentIds":["`+details.AttachmentID+`"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("resolution = %d: %s", response.Code, response.Body.String())
	}
	var resolution protocol.AttachmentResolution
	if err := json.Unmarshal(response.Body.Bytes(), &resolution); err != nil {
		t.Fatal(err)
	}
	want := protocol.AttachmentInfo{
		ID: details.AttachmentID, SessionID: "session_one", Filename: filename, MediaType: "image/png",
		Size: int64(encoded.Len()), Width: 2880, Height: 4800,
	}
	if len(resolution.Attachments) != 1 || len(resolution.MissingAttachmentIDs) != 0 {
		t.Fatalf("resolution = %#v", resolution)
	}
	got := resolution.Attachments[0]
	got.SHA256, got.CreatedAt = "", ""
	if got != want {
		t.Fatalf("resolved = %#v, want %#v", got, want)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/sessions/session_one/attachments/"+details.AttachmentID, nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), encoded.Bytes()) {
		t.Fatalf("retrieval = %d, %d bytes: %.200s", response.Code, response.Body.Len(), response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); got != `inline; filename*=utf-8''Explore%20%C2%B7%20shell%20commands%402x.png` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}
