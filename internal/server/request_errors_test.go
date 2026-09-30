package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
)

type compactRouteService struct {
	sessionService
	err error
}

func (s compactRouteService) Compact(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	return protocol.CompactSessionResult{}, s.err
}

func serveCompactRoute(t *testing.T, err error) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return attr
	}}))
	mux := http.NewServeMux()
	registerSessionRoutes(mux, compactRouteService{err: err})
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/compact", strings.NewReader(`{"operationId":"compact_0123456789abcdef0123456789abcdef"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(withRequestErrorReporting(response, request, logger), request)
	return response, logs.String()
}

func TestCompactionFailureIsUnprocessableAndLogsCause(t *testing.T) {
	cause := fmt.Errorf("%w: droids: compaction model stopped with length: ", kitsession.ErrCompactionFailed)
	response, logs := serveCompactRoute(t, cause)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), `{"error":{"code":"unprocessable","message":"context compaction failed"}}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	want := `level=ERROR msg="request failed" method=POST path=/v1/sessions/session_test/compact status=422 error="context compaction failed: droids: compaction model stopped with length: "` + "\n"
	if logs != want {
		t.Fatalf("logs = %q, want %q", logs, want)
	}
}

func TestInternalSessionErrorLogsCause(t *testing.T) {
	response, logs := serveCompactRoute(t, errors.New("droid store is corrupt"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), `{"error":"internal server error"}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	want := `level=ERROR msg="request failed" method=POST path=/v1/sessions/session_test/compact status=500 error="droid store is corrupt"` + "\n"
	if logs != want {
		t.Fatalf("logs = %q, want %q", logs, want)
	}
}

func TestErrorReportingWriterPreservesFlusher(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := withRequestErrorReporting(recorder, httptest.NewRequest(http.MethodGet, "/v1/sessions/events", nil), slog.New(slog.DiscardHandler))
	flusher, ok := writer.(http.Flusher)
	if !ok {
		t.Fatal("wrapped writer is not an http.Flusher")
	}
	flusher.Flush()
	if !recorder.Flushed {
		t.Fatal("flush did not reach the underlying writer")
	}
}
