package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const validPayload = `{"value":"a","flag":true,"items":["x"]}`

func readTestStream(t *testing.T, op StreamOperation[SessionPath, streamPayload], body string) ([]StreamRecord[streamPayload], error) {
	t.Helper()
	var records []StreamRecord[streamPayload]
	err := ReadStream(strings.NewReader(body), op, func(record StreamRecord[streamPayload]) error {
		records = append(records, record)
		return nil
	})
	return records, err
}

func TestStreamReaderParsesRecordsAndIgnoresComments(t *testing.T) {
	body := ": connected\n\n" +
		"event: test.value\ndata: " + validPayload + "\n\n" +
		": heartbeat\r\n\r\n" +
		"event:test.reset\r\ndata:{\"value\":\"b\",\"flag\":false,\"items\":null}\r\n\r\n"
	records, err := readTestStream(t, testStream, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 ||
		records[0].Name != "test.value" || records[0].Payload.Value != "a" || !records[0].Payload.Flag || len(records[0].Payload.Items) != 1 ||
		records[1].Name != "test.reset" || records[1].Payload.Value != "b" || records[1].Payload.Items != nil {
		t.Fatalf("records = %+v", records)
	}
}

func TestStreamReaderRoundTripsWriterOutput(t *testing.T) {
	resumable := testStream
	resumable.Resumable = true
	source := &scriptedSource{steps: []*StreamRecord[streamPayload]{
		record("test.value", "1", streamPayload{Value: "a", Items: []string{}}), nil, record("test.reset", "2", streamPayload{Value: "b", Note: "n"}),
	}}
	response := serveTestStream(t, resumable, source, nil)
	records, err := readTestStream(t, resumable, response.Body.String())
	if err != nil || len(records) != 2 || records[0].ID != "1" || records[1].ID != "2" || records[1].Payload.Note != "n" {
		t.Fatalf("records = %+v err = %v", records, err)
	}
}

func TestStreamReaderRejectsViolations(t *testing.T) {
	oversized := "event: test.value\ndata: {\"value\":\"" + strings.Repeat("x", 256) + "\",\"flag\":true,\"items\":null}\n\n"
	for name, body := range map[string]string{
		"oversized record":        oversized,
		"oversized comment":       ":" + strings.Repeat("x", 256) + "\n\n",
		"record lines over bound": "event: test.value\n:" + strings.Repeat("x", 200) + "\ndata: " + validPayload + "\n\n",
		"unknown record":          "event: test.other\ndata: " + validPayload + "\n\n",
		"missing record name":     "data: " + validPayload + "\n\n",
		"missing data":            "event: test.value\n\n",
		"multi-data record":       "event: test.value\ndata: {\"value\":\"a\",\ndata: \"flag\":true,\"items\":null}\n\n",
		"repeated event":          "event: test.value\nevent: test.value\ndata: " + validPayload + "\n\n",
		"id on non-resumable":     "event: test.value\nid: 1\ndata: " + validPayload + "\n\n",
		"undeclared field":        "event: test.value\nretry: 5\ndata: " + validPayload + "\n\n",
		"fieldless line":          "event: test.value\ndata\n\n",
		"malformed JSON":          "event: test.value\ndata: {\"value\":\n\n",
		"trailing garbage":        "event: test.value\ndata: " + validPayload + " {}\n\n",
		"non-object payload":      "event: test.value\ndata: [1]\n\n",
		"unknown payload field":   "event: test.value\ndata: {\"value\":\"a\",\"flag\":true,\"items\":null,\"extra\":1}\n\n",
		"duplicate key":           "event: test.value\ndata: {\"value\":\"a\",\"value\":\"b\",\"flag\":true,\"items\":null}\n\n",
		"missing required field":  "event: test.value\ndata: {\"value\":\"a\",\"items\":null}\n\n",
		"null required boolean":   "event: test.value\ndata: {\"value\":\"a\",\"flag\":null,\"items\":null}\n\n",
		"missing nullable array":  "event: test.value\ndata: {\"value\":\"a\",\"flag\":true}\n\n",
		"validation failure":      "event: test.value\ndata: {\"value\":\"invalid\",\"flag\":true,\"items\":null}\n\n",
		"invalid UTF-8":           "event: test.value\ndata: {\"value\":\"\xff\",\"flag\":true,\"items\":null}\n\n",
		"unterminated record":     "event: test.value\ndata: " + validPayload + "\n",
		"truncated line":          "event: test.value\ndata: " + validPayload,
	} {
		t.Run(name, func(t *testing.T) {
			records, err := readTestStream(t, testStream, body)
			var streamErr *StreamError
			if !errors.As(err, &streamErr) || len(records) != 0 {
				t.Fatalf("records=%+v err=%v, want *StreamError", records, err)
			}
		})
	}
}

func TestStreamReaderAcceptsRecordAtBound(t *testing.T) {
	prefix := "event: test.value\ndata: {\"value\":\""
	suffix := "\",\"flag\":true,\"items\":null}\n"
	value := strings.Repeat("x", testStream.MaxRecordBytes-len(prefix)-len(suffix))
	records, err := readTestStream(t, testStream, prefix+value+suffix+"\n")
	if err != nil || len(records) != 1 || records[0].Payload.Value != value {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	if _, err := readTestStream(t, testStream, prefix+value+"x"+suffix+"\n"); err == nil {
		t.Fatal("record one byte beyond bound accepted")
	}
}

func TestStreamReaderPropagatesReceiveErrorsAndCleanEnd(t *testing.T) {
	if records, err := readTestStream(t, testStream, ""); err != nil || len(records) != 0 {
		t.Fatalf("empty stream = %v %v", records, err)
	}
	stop := errors.New("stop")
	err := ReadStream(strings.NewReader("event: test.value\ndata: "+validPayload+"\n\n"), testStream, func(StreamRecord[streamPayload]) error { return stop })
	var streamErr *StreamError
	if !errors.Is(err, stop) || errors.As(err, &streamErr) {
		t.Fatalf("receive error = %v", err)
	}
}

func TestOpenStreamClassifiesPreStreamResponses(t *testing.T) {
	respond := func(status int, contentType, body string) testTransport {
		return func(_ context.Context, method, path string, input io.Reader, jsonBody bool) (*http.Response, error) {
			if method != http.MethodGet || path != "/items/a%2Fb/events" || input != nil || jsonBody {
				t.Fatalf("request = %s %s", method, path)
			}
			header := http.Header{}
			header.Set("Content-Type", contentType)
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
	}
	body, err := OpenStream(t.Context(), respond(http.StatusOK, "text/event-stream; charset=utf-8", ": connected\n\n"), testStream, SessionPath{SessionID: "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	body.Close()

	_, err = OpenStream(t.Context(), respond(http.StatusTooManyRequests, "application/json", `{"error":{"code":"capacity_exceeded","message":"full"}}`), testStream, SessionPath{SessionID: "a/b"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests || apiErr.Code != string(ErrorCapacityExceeded) {
		t.Fatalf("capacity error = %#v", err)
	}
	_, err = OpenStream(t.Context(), respond(http.StatusUpgradeRequired, "application/json", `{"error":{"code":"protocol_mismatch","message":"mismatch"}}`), testStream, SessionPath{SessionID: "a/b"})
	if !errors.As(err, &apiErr) || !apiErr.IncompatibleDaemon() {
		t.Fatalf("protocol error = %#v", err)
	}
	for name, transport := range map[string]testTransport{
		"plain text":        respond(http.StatusTooManyRequests, "text/plain", "VCS subscriber limit exceeded\n"),
		"plain-text 404":    respond(http.StatusNotFound, "text/plain", "404 page not found\n"),
		"undeclared code":   respond(http.StatusTooManyRequests, "application/json", `{"error":{"code":"unavailable","message":"x"}}`),
		"undeclared status": respond(http.StatusGone, "application/json", `{"error":{"code":"not_found","message":"x"}}`),
		"missing message":   respond(http.StatusTooManyRequests, "application/json", `{"error":{"code":"capacity_exceeded"}}`),
	} {
		_, err := OpenStream(t.Context(), transport, testStream, SessionPath{SessionID: "a/b"})
		var violation *StreamError
		if !errors.As(err, &violation) || errors.As(err, &apiErr) {
			t.Fatalf("%s = %#v, want *StreamError", name, err)
		}
	}
	_, err = OpenStream(t.Context(), respond(http.StatusOK, "application/x-ndjson", "{}\n"), testStream, SessionPath{SessionID: "a/b"})
	var streamErr *StreamError
	if !errors.As(err, &streamErr) {
		t.Fatalf("content type error = %#v", err)
	}
}
