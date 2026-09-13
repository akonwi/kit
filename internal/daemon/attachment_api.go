package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
)

const (
	maxAttachmentRequestBytes = protocol.MaxImageAttachmentBytes + 64<<10
	maxImagePixels            = 12_000_000
)

type attachmentService interface {
	Put(context.Context, string, attachment.PutInput) (protocol.AttachmentInfo, error)
	Open(context.Context, string, string) (protocol.AttachmentInfo, io.ReadCloser, error)
}

type runtimeAttachmentService struct {
	manager *kitsession.Manager
	store   attachment.Store
}

func (service runtimeAttachmentService) Put(ctx context.Context, sessionID string, input attachment.PutInput) (protocol.AttachmentInfo, error) {
	if _, err := service.manager.Get(ctx, sessionID); err != nil {
		return protocol.AttachmentInfo{}, err
	}
	input.SessionID = sessionID
	record, err := service.store.Put(ctx, input)
	if err != nil {
		return protocol.AttachmentInfo{}, err
	}
	// Close the upload/delete race: either this check observes deletion and
	// removes the just-published blob, or deletion's session sweep runs after it.
	if _, err := service.manager.Get(ctx, sessionID); err != nil {
		_ = service.store.Remove(context.Background(), sessionID, record.ID)
		return protocol.AttachmentInfo{}, err
	}
	return projectAttachment(record), nil
}

func (service runtimeAttachmentService) Open(ctx context.Context, sessionID, id string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	if _, err := service.manager.Get(ctx, sessionID); err != nil {
		return protocol.AttachmentInfo{}, nil, err
	}
	record, content, err := service.store.Open(ctx, sessionID, id)
	if err != nil {
		return protocol.AttachmentInfo{}, nil, err
	}
	return projectAttachment(record), content, nil
}

func projectAttachment(record attachment.Record) protocol.AttachmentInfo {
	return protocol.AttachmentInfo{
		ID: record.ID, SessionID: record.SessionID, Filename: record.Filename, MediaType: record.MediaType,
		Size: record.Size, SHA256: record.SHA256, CreatedAt: record.CreatedAt.Format(time.RFC3339Nano),
		Width: record.Width, Height: record.Height,
	}
}

func registerAttachmentRoutes(mux *http.ServeMux, service attachmentService) {
	mux.HandleFunc("POST /v1/sessions/{sessionID}/attachments", func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, maxAttachmentRequestBytes)
		mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
			writeSessionError(writer, fmt.Errorf("%w: attachment upload must be multipart/form-data", errInvalidSessionRequest))
			return
		}
		if err := request.ParseMultipartForm(protocol.MaxTextAttachmentBytes); err != nil {
			writeAttachmentError(writer, err)
			return
		}
		defer request.MultipartForm.RemoveAll()
		files := request.MultipartForm.File["file"]
		if len(files) != 1 || len(request.MultipartForm.File) != 1 || len(request.MultipartForm.Value) != 0 || files[0].Filename == "" {
			writeSessionError(writer, fmt.Errorf("%w: attachment upload requires exactly one file part", errInvalidSessionRequest))
			return
		}
		part, err := files[0].Open()
		if err != nil {
			writeAttachmentError(writer, err)
			return
		}
		defer part.Close()

		input, err := inspectAttachment(files[0].Filename, part)
		if err != nil {
			writeAttachmentError(writer, err)
			return
		}
		result, err := service.Put(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeAttachmentError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid attachment result: %w", err))
			return
		}
		writeJSON(writer, http.StatusCreated, result)
	})

	mux.HandleFunc("GET /v1/sessions/{sessionID}/attachments/{attachmentID}", func(writer http.ResponseWriter, request *http.Request) {
		info, content, err := service.Open(request.Context(), request.PathValue("sessionID"), request.PathValue("attachmentID"))
		if err != nil {
			writeAttachmentError(writer, err)
			return
		}
		defer content.Close()
		if err := info.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid attachment metadata: %w", err))
			return
		}
		writer.Header().Set("Content-Type", info.MediaType)
		writer.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
		writer.Header().Set("ETag", `"sha256:`+info.SHA256+`"`)
		writer.Header().Set("X-Kit-Attachment-ID", info.ID)
		writer.Header().Set("X-Kit-Session-ID", info.SessionID)
		writer.Header().Set("X-Kit-Attachment-Created-At", info.CreatedAt)
		writer.Header().Set("X-Kit-Image-Width", strconv.Itoa(info.Width))
		writer.Header().Set("X-Kit-Image-Height", strconv.Itoa(info.Height))
		writer.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": info.Filename}))
		writer.WriteHeader(http.StatusOK)
		_, _ = io.Copy(writer, content)
	})
}

func inspectAttachment(filename string, content io.Reader) (attachment.PutInput, error) {
	reader := bufio.NewReader(content)
	header, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return attachment.PutInput{}, fmt.Errorf("read attachment header: %w", err)
	}
	if len(header) == 0 {
		return attachment.PutInput{}, fmt.Errorf("%w: attachment is empty", attachment.ErrInvalidInput)
	}
	mediaType := http.DetectContentType(header)
	input := attachment.PutInput{Filename: filename, Content: reader, MediaType: mediaType}
	if strings.HasPrefix(mediaType, "text/plain") {
		input.MediaType = "text/plain"
		input.MaxBytes = protocol.MaxTextAttachmentBytes
		input.Validate = func(staged io.ReadSeeker) error {
			data, err := io.ReadAll(staged)
			if err != nil {
				return err
			}
			if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
				return errors.New("text attachment must be valid UTF-8 without NUL")
			}
			return nil
		}
		return input, nil
	}
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" && mediaType != "image/webp" {
		return attachment.PutInput{}, fmt.Errorf("%w: unsupported attachment type %q", attachment.ErrInvalidInput, mediaType)
	}

	return attachment.InspectImage(filename, reader, attachment.ImageLimits{
		MaxBytes: protocol.MaxImageAttachmentBytes, MaxWidth: 8192, MaxHeight: 8192, MaxPixels: maxImagePixels,
	})
}

func writeAttachmentError(writer http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.Is(err, attachment.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "attachment not found"})
	case errors.As(err, &maxBytesError), errors.Is(err, attachment.ErrTooLarge):
		writeJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
	case errors.Is(err, attachment.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeSessionError(writer, err)
	}
}
