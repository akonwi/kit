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

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

func TestConfigureSubagentRejectsMismatchedConversationIdentity(t *testing.T) {
	const (
		sessionID      = "session_0123456789abcdef0123456789abcdef"
		conversationID = "subagent_0123456789abcdef0123456789abcdef"
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"conversation":{"id":"subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agentName":"reviewer","model":"test/model","thinkingLevel":"off","state":"idle","generation":1,"queuedTasks":0,"updatedAt":"2025-01-01T00:00:00Z"}}`)
	}))
	defer server.Close()
	model := "test/model"
	_, err := NewEndpoint(server.URL, "secret", "instance_test").ConfigureSubagent(t.Context(), sessionID, conversationID, protocol.ConfigureSubagentInput{Generation: 1, Model: &model})
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("ConfigureSubagent() error = %T %v, want protocol error", err, err)
	}
}

func TestForkSessionReturnsFirstTurnErrorWithPublishedChild(t *testing.T) {
	const sourceID = "session_0123456789abcdef0123456789abcdef"
	const child = `"session":{"id":"session_cccccccccccccccccccccccccccccccc","cwd":"/work","model":"test/echo","thinkingLevel":"","configurationRevision":1,"parentSessionId":"session_0123456789abcdef0123456789abcdef","createdAt":"2026-03-23T12:34:56Z","updatedAt":"2026-03-23T12:34:56Z"}`
	const failure = `"firstTurnError":{"code":"unavailable","message":"session is unavailable"}`
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(response, "{"+child+","+failure+"}")
	}))
	defer server.Close()
	endpoint := NewEndpoint(server.URL, "secret", "instance_test")

	result, err := endpoint.ForkSession(t.Context(), sourceID, protocol.ForkSessionInput{Prompt: &protocol.PromptInput{Text: "explore the other approach"}})
	if err != nil {
		t.Fatalf("ForkSession() error = %v, want the published child", err)
	}
	want := protocol.FirstTurnError{Code: protocol.FirstTurnUnavailable, Message: "session is unavailable"}
	if result.Session.ID != "session_cccccccccccccccccccccccccccccccc" || result.FirstTurnError == nil || *result.FirstTurnError != want {
		t.Fatalf("ForkSession() = %+v, want the child with first turn error %+v", result, want)
	}

	_, err = endpoint.ForkSession(t.Context(), sourceID, protocol.ForkSessionInput{})
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("ForkSession() without a prompt error = %T %v, want protocol error for an unrequested first turn", err, err)
	}
}

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
