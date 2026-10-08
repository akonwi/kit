package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type streamPayload struct {
	Value string   `json:"value"`
	Flag  bool     `json:"flag"`
	Items []string `json:"items"`
	Note  string   `json:"note,omitempty"`
}

func validateStreamPayload(payload streamPayload) error {
	if payload.Value == "" || payload.Value == "invalid" {
		return errors.New("invalid value")
	}
	return nil
}

var testStream = StreamOperation[SessionPath, streamPayload]{
	ID: "streamTest", Tag: "test", Path: "/items/{sessionID}/events",
	Records: []string{"test.value", "test.reset"}, MaxRecordBytes: 256, Validate: validateStreamPayload,
	Errors: []ErrorResponse{{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}}},
}

// scriptedSource replays steps; a nil step waits for the heartbeat deadline.
type scriptedSource struct {
	steps  []*StreamRecord[streamPayload]
	closed bool
}

func (s *scriptedSource) Next(ctx context.Context) (StreamRecord[streamPayload], error) {
	if len(s.steps) == 0 {
		return StreamRecord[streamPayload]{}, io.EOF
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step == nil {
		<-ctx.Done()
		return StreamRecord[streamPayload]{}, ctx.Err()
	}
	return *step, nil
}
func (s *scriptedSource) Close() { s.closed = true }

func serveTestStream(t *testing.T, op StreamOperation[SessionPath, streamPayload], source *scriptedSource, openErr error) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	options := ServeOptions{StreamHeartbeat: 5 * time.Millisecond, WriteError: func(w http.ResponseWriter, err error) {
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("unexpected error type %T", err)
		}
		WriteError(w, apiErr)
	}}
	HandleStream(mux, options, op, func(_ context.Context, params SessionPath) (StreamSource[streamPayload], error) {
		if params.SessionID != "a/b" {
			t.Fatalf("params = %+v", params)
		}
		if openErr != nil {
			return nil, openErr
		}
		return source, nil
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/a%2Fb/events", nil))
	return response
}

func record(name, id string, payload streamPayload) *StreamRecord[streamPayload] {
	return &StreamRecord[streamPayload]{Name: name, ID: id, Payload: payload}
}

func TestStreamWriterFramesRecordsAndHeartbeats(t *testing.T) {
	source := &scriptedSource{steps: []*StreamRecord[streamPayload]{
		record("test.value", "", streamPayload{Value: "<a&b>", Flag: true, Items: []string{"x"}}),
		nil,
		record("test.reset", "", streamPayload{Value: "b"}),
	}}
	response := serveTestStream(t, testStream, source, nil)
	want := ": connected\n\n" +
		"event: test.value\ndata: {\"value\":\"<a&b>\",\"flag\":true,\"items\":[\"x\"]}\n\n" +
		": heartbeat\n\n" +
		"event: test.reset\ndata: {\"value\":\"b\",\"flag\":false,\"items\":null}\n\n"
	if response.Code != http.StatusOK || response.Body.String() != want {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	for header, value := range map[string]string{"Content-Type": "text/event-stream; charset=utf-8", "Cache-Control": "no-cache", "X-Accel-Buffering": "no"} {
		if got := response.Header().Get(header); got != value {
			t.Fatalf("%s = %q, want %q", header, got, value)
		}
	}
	if !source.closed {
		t.Fatal("source was not closed")
	}
}

func TestStreamWriterWritesIDsOnlyForResumableStreams(t *testing.T) {
	resumable := testStream
	resumable.Resumable = true
	response := serveTestStream(t, resumable, &scriptedSource{steps: []*StreamRecord[streamPayload]{record("test.value", "7", streamPayload{Value: "a"})}}, nil)
	if want := ": connected\n\nevent: test.value\nid: 7\ndata: {\"value\":\"a\",\"flag\":false,\"items\":null}\n\n"; response.Body.String() != want {
		t.Fatalf("resumable body = %q", response.Body.String())
	}
	response = serveTestStream(t, testStream, &scriptedSource{steps: []*StreamRecord[streamPayload]{record("test.value", "7", streamPayload{Value: "a"})}}, nil)
	if response.Body.String() != ": connected\n\n" {
		t.Fatalf("non-resumable id body = %q", response.Body.String())
	}
}

func TestStreamWriterEndsInsteadOfSendingViolatingRecords(t *testing.T) {
	for name, step := range map[string]*StreamRecord[streamPayload]{
		"oversized":  record("test.value", "", streamPayload{Value: strings.Repeat("x", 256)}),
		"undeclared": record("test.other", "", streamPayload{Value: "a"}),
		"invalid":    record("test.value", "", streamPayload{Value: "invalid"}),
	} {
		t.Run(name, func(t *testing.T) {
			follow := record("test.value", "", streamPayload{Value: "after"})
			source := &scriptedSource{steps: []*StreamRecord[streamPayload]{step, follow}}
			response := serveTestStream(t, testStream, source, nil)
			if response.Code != http.StatusOK || response.Body.String() != ": connected\n\n" || !source.closed {
				t.Fatalf("status=%d body=%q closed=%t", response.Code, response.Body.String(), source.closed)
			}
		})
	}
}

func TestStreamWriterRecordBoundIncludesFieldLines(t *testing.T) {
	// event line (18) + data prefix and newline (7) + payload.
	payload := streamPayload{Value: "a"}
	frame, err := encodeStreamRecord(testStream, StreamRecord[streamPayload]{Name: "test.value", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	size := len(frame) - 1
	exact := testStream
	exact.MaxRecordBytes = size
	if _, err := encodeStreamRecord(exact, StreamRecord[streamPayload]{Name: "test.value", Payload: payload}); err != nil {
		t.Fatalf("record at bound rejected: %v", err)
	}
	exact.MaxRecordBytes = size - 1
	if _, err := encodeStreamRecord(exact, StreamRecord[streamPayload]{Name: "test.value", Payload: payload}); err == nil {
		t.Fatal("record beyond bound accepted")
	}
}

func TestStreamPreStreamErrorsUseErrorBody(t *testing.T) {
	response := serveTestStream(t, testStream, nil, NewAPIError(http.StatusTooManyRequests, ErrorCapacityExceeded, "too many subscribers", nil))
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Content-Type") != "application/json" ||
		response.Body.String() != "{\"error\":{\"code\":\"capacity_exceeded\",\"message\":\"too many subscribers\"}}\n" {
		t.Fatalf("status=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

// blockingSource waits for its context, as an idle live stream does.
type blockingSource struct{ closed chan struct{} }

func (s *blockingSource) Next(ctx context.Context) (StreamRecord[streamPayload], error) {
	<-ctx.Done()
	return StreamRecord[streamPayload]{}, ctx.Err()
}
func (s *blockingSource) Close() { close(s.closed) }

func TestStreamShutdownEndsOpenStreamsSoGracefulShutdownCompletes(t *testing.T) {
	t.Parallel()
	streamShutdown, endStreams := context.WithCancel(context.Background())
	defer endStreams()
	source := &blockingSource{closed: make(chan struct{})}
	mux := http.NewServeMux()
	HandleStream(mux, ServeOptions{StreamShutdown: streamShutdown, WriteError: func(w http.ResponseWriter, err error) {
		t.Errorf("unexpected open error: %v", err)
	}}, testStream, func(context.Context, SessionPath) (StreamSource[streamPayload], error) {
		return source, nil
	})
	server := httptest.NewUnstartedServer(mux)
	server.Config.RegisterOnShutdown(endStreams)
	server.Start()
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/items/a%2Fb/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	connected := make([]byte, len(": connected\n\n"))
	if _, err := io.ReadFull(response.Body, connected); err != nil || string(connected) != ": connected\n\n" {
		t.Fatalf("stream preamble = %q, %v", connected, err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Config.Shutdown(shutdownContext); err != nil {
		t.Fatalf("graceful shutdown = %v, want nil with an attached stream", err)
	}
	select {
	case <-source.closed:
	default:
		t.Fatal("stream source was not closed after shutdown")
	}
	rest, err := io.ReadAll(response.Body)
	if err != nil || len(rest) != 0 {
		t.Fatalf("stream tail = %q, %v; want clean end", rest, err)
	}
}
