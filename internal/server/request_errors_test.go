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

	protocol "github.com/akonwi/kit/api/contract"
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

func TestCompactionFailureReturnsItsReasonAndLogsCause(t *testing.T) {
	cause := &kitsession.CompactionFailedError{Reason: kitsession.CompactionFailureSummaryTruncated}
	response, logs := serveCompactRoute(t, fmt.Errorf("compact: %w", cause))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), `{"error":{"code":"compaction_failed","details":{"reason":"summary_truncated"},"message":"context compaction failed"}}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	want := `level=ERROR msg="request failed" method=POST path=/v1/sessions/session_test/compact status=422 error="compact: context compaction failed: summary_truncated"` + "\n"
	if logs != want {
		t.Fatalf("logs = %q, want %q", logs, want)
	}

	// A failure the session layer did not classify is an internal one.
	response, _ = serveCompactRoute(t, fmt.Errorf("%w: droids: compaction model stopped with length: provider text", kitsession.ErrCompactionFailed))
	if got, want := response.Body.String(), `{"error":{"code":"compaction_failed","details":{"reason":"internal_error"},"message":"context compaction failed"}}`+"\n"; got != want {
		t.Fatalf("unclassified body = %q, want %q", got, want)
	}
}

func TestInternalSessionErrorLogsCause(t *testing.T) {
	response, logs := serveCompactRoute(t, errors.New("droid store is corrupt"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), `{"error":{"code":"internal","message":"internal server error"}}`+"\n"; got != want {
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
