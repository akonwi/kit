package clienttransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

func TestSessionEventStreamCancellationClosesRequest(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
		response.(http.Flusher).Flush()
		<-request.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	client := NewEndpoint(server.URL, "secret", "instance_test")
	body, err := client.StreamSessionEvents(ctx, sessionID, "stream_test", 0)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	_, err = body.Read(make([]byte, 1))
	_ = body.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stream read error = %v", err)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled")
	}
}

func TestOpenAttachmentValidatesMetadataAndAppliesCredentials(t *testing.T) {
	const (
		sessionID    = "session_0123456789abcdef0123456789abcdef"
		attachmentID = "attachment_0123456789abcdef0123456789abcdef"
		instanceID   = "instance_test"
		content      = "attachment contents"
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/sessions/"+sessionID+"/attachments/"+attachmentID {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer secret" ||
			request.Header.Get(httpapi.InstanceHeader) != instanceID ||
			request.Header.Get(httpapi.ProtocolHeader) != strconv.Itoa(version.SessionProtocolVersion) {
			http.Error(response, "missing client credentials", http.StatusUnauthorized)
			return
		}
		writeAttachmentHeaders(response, sessionID, attachmentID, content)
		_, _ = io.WriteString(response, content)
	}))
	defer server.Close()

	client := NewEndpoint(server.URL, "secret", instanceID)
	info, body, err := client.OpenAttachment(t.Context(), sessionID, attachmentID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content || info.ID != attachmentID || info.SessionID != sessionID || info.Size != int64(len(content)) {
		t.Fatalf("attachment info=%+v content=%q", info, got)
	}
}

func TestOpenAttachmentRejectsIntegrityMetadataMismatch(t *testing.T) {
	const (
		sessionID    = "session_0123456789abcdef0123456789abcdef"
		attachmentID = "attachment_0123456789abcdef0123456789abcdef"
		content      = "attachment contents"
	)
	for name, mutate := range map[string]func(http.Header){
		"attachment identity": func(header http.Header) {
			header.Set("X-Kit-Attachment-ID", "attachment_ffffffffffffffffffffffffffffffff")
		},
		"session identity": func(header http.Header) { header.Set("X-Kit-Session-ID", "session_ffffffffffffffffffffffffffffffff") },
		"checksum": func(header http.Header) {
			digest := sha256.Sum256([]byte("different contents"))
			header.Set("ETag", `"sha256:`+hex.EncodeToString(digest[:])+`"`)
		},
		"size": func(header http.Header) { header.Set("Content-Length", "999") },
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				writeAttachmentHeaders(response, sessionID, attachmentID, content)
				mutate(response.Header())
				_, _ = io.WriteString(response, content)
			}))
			defer server.Close()

			client := NewEndpoint(server.URL, "secret", "instance_test")
			_, body, err := client.OpenAttachment(t.Context(), sessionID, attachmentID)
			if err == nil && body != nil {
				_, err = io.ReadAll(body)
			}
			if body != nil {
				body.Close()
			}
			var protocolFailure *ProtocolError
			if !errors.As(err, &protocolFailure) {
				t.Fatalf("error = %v, want *ProtocolError", err)
			}
		})
	}
}

func writeAttachmentHeaders(response http.ResponseWriter, sessionID, attachmentID, content string) {
	response.Header().Set("Content-Disposition", `inline; filename="note.txt"`)
	response.Header().Set("Content-Type", "text/plain")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	digest := sha256.Sum256([]byte(content))
	response.Header().Set("ETag", `"sha256:`+hex.EncodeToString(digest[:])+`"`)
	response.Header().Set("X-Kit-Attachment-ID", attachmentID)
	response.Header().Set("X-Kit-Session-ID", sessionID)
	response.Header().Set("X-Kit-Attachment-Created-At", time.Unix(1, 0).UTC().Format(time.RFC3339Nano))
	response.Header().Set("X-Kit-Image-Width", "0")
	response.Header().Set("X-Kit-Image-Height", "0")
}
