package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/getkin/kin-openapi/openapi3"
)

type pluginWireTestService struct {
	sessionService
	source   pluginToastSource
	err      error
	executed *protocol.PluginCommandInput
}

func (s pluginWireTestService) SubscribePluginToasts(context.Context, string) (pluginToastSource, error) {
	return s.source, s.err
}

func (s pluginWireTestService) ExecutePluginCommand(_ context.Context, _ string, input protocol.PluginCommandInput) error {
	if s.executed != nil {
		*s.executed = input
	}
	return s.err
}

type pluginToastSequence struct {
	values []protocol.PluginToast
	closed bool
}

func (s *pluginToastSequence) Next(context.Context) (protocol.PluginToast, error) {
	if len(s.values) == 0 {
		return protocol.PluginToast{}, io.EOF
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, value.Validate()
}
func (s *pluginToastSequence) Close() { s.closed = true }

func pluginContractToasts() []protocol.PluginToast {
	return []protocol.PluginToast{
		{PluginID: "demo", Instance: "owner:1", Title: "Notice", Variant: protocol.PluginToastInfo},
		{PluginID: "demo", Instance: "owner:1", Title: "Careful", Subtitle: "first\nsecond\tthird", Variant: protocol.PluginToastWarning},
		{PluginID: "git-tools", Instance: "host.2", Title: "Failed", Subtitle: "details", Variant: protocol.PluginToastError, Persistent: true},
	}
}

const pluginContractCommand = `{"id":"demo.run","instance":"owner:1","args":"--fast"}`

func servePluginContract(t *testing.T, service sessionService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	registerSessionRoutes(mux, service)
	var contractErr error
	handler := contractConformanceMiddleware(t, mux, func(err error) { contractErr = err })
	request := httptest.NewRequest(method, "/v1/sessions/"+vcsTestSessionID+path, strings.NewReader(body))
	addContractRequestHeaders(request)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if contractErr != nil {
		t.Fatalf("contract: %v body=%s", contractErr, response.Body.String())
	}
	return response
}

func TestExecutePluginCommandConformsToContract(t *testing.T) {
	var executed protocol.PluginCommandInput
	response := servePluginContract(t, pluginWireTestService{executed: &executed}, http.MethodPost, "/plugin-commands", pluginContractCommand)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "" {
		t.Fatalf("response = %d %v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if want := (protocol.PluginCommandInput{ID: "demo.run", Instance: "owner:1", Args: "--fast"}); executed != want {
		t.Fatalf("executed = %#v, want %#v", executed, want)
	}
}

func TestPluginErrorResponsesConformToContract(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		body   string
		status int
		code   string
		stream bool
	}{
		{"invalid selection", nil, `{"id":"demo","instance":"owner:1","args":""}`, http.StatusBadRequest, "invalid_request", false},
		{"missing", kitsession.ErrNotFound, "", http.StatusNotFound, "not_found", false},
		{"deleting", kitsession.ErrDeleteBusy, "", http.StatusConflict, "conflict", false},
		{"unavailable command", kitsession.ErrPluginCommandUnavailable, "", http.StatusConflict, "plugin_command_unavailable", false},
		{"failed command", errors.Join(kitsession.ErrPluginCommandFailed, errors.New("callback")), "", http.StatusUnprocessableEntity, "plugin_command_failed", false},
		{"closed", kitsession.ErrClosed, "", http.StatusServiceUnavailable, "unavailable", false},
		{"internal", errors.New("boom"), "", http.StatusInternalServerError, "internal", false},
		{"stream missing", kitsession.ErrNotFound, "", http.StatusNotFound, "not_found", true},
		{"stream deleting", kitsession.ErrDeleteBusy, "", http.StatusConflict, "conflict", true},
		{"stream subscriber limit", kitsession.ErrPluginNotificationCapacity, "", http.StatusTooManyRequests, "capacity_exceeded", true},
		{"stream closed", kitsession.ErrClosed, "", http.StatusServiceUnavailable, "unavailable", true},
		{"stream internal", errors.New("boom"), "", http.StatusInternalServerError, "internal", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method, path, body := http.MethodPost, "/plugin-commands", pluginContractCommand
			if test.body != "" {
				body = test.body
			}
			if test.stream {
				method, path, body = http.MethodGet, "/plugin-toasts", ""
			}
			response := servePluginContract(t, pluginWireTestService{err: test.err}, method, path, body)
			if response.Code != test.status || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response = %d %v body=%s", response.Code, response.Header(), response.Body.String())
			}
			var envelope struct {
				Error struct {
					Code    string          `json:"code"`
					Message string          `json:"message"`
					Details json.RawMessage `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != test.code || envelope.Error.Message == "" || envelope.Error.Details != nil {
				t.Fatalf("error body = %s, want code %s", response.Body.String(), test.code)
			}
			if test.status == http.StatusInternalServerError && envelope.Error.Message != "internal server error" {
				t.Fatalf("internal message = %q", envelope.Error.Message)
			}
		})
	}
}

func TestPluginToastStreamRecordsConformToPublishedPayloadSchema(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile("../../api/kit-session.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	operation := document.Paths.Find("/v1/sessions/{sessionID}/plugin-toasts").Get
	schema := operation.Responses.Status(http.StatusOK).Value.Content.Get("text/event-stream").Schema.Value
	source := &pluginToastSequence{values: pluginContractToasts()}
	response := servePluginContract(t, pluginWireTestService{source: source}, http.MethodGet, "/plugin-toasts", "")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" || !source.closed {
		t.Fatalf("response = %d %v closed=%t", response.Code, response.Header(), source.closed)
	}
	records := strings.Split(strings.TrimSuffix(response.Body.String(), "\n\n"), "\n\n")
	if len(records) != 1+len(pluginContractToasts()) || records[0] != ": connected" {
		t.Fatalf("records = %q", records)
	}
	for index, record := range records[1:] {
		name, data, ok := strings.Cut(record, "\n")
		if !ok || name != "event: plugin.toast" || !strings.HasPrefix(data, "data: ") || strings.Contains(data, "\n") {
			t.Fatalf("record %d = %q", index, record)
		}
		var value any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &value); err != nil {
			t.Fatal(err)
		}
		if err := schema.VisitJSON(value); err != nil {
			t.Fatalf("record %d does not match PluginToast: %v", index, err)
		}
	}
	var decoded []protocol.PluginToast
	if err := ReadPluginToasts(response.Body, func(value protocol.PluginToast) error { decoded = append(decoded, value); return nil }); err != nil {
		t.Fatal(err)
	}
	if want := pluginContractToasts(); len(decoded) != len(want) || decoded[0] != want[0] || decoded[1] != want[1] || decoded[2] != want[2] {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestPluginToastStreamKeepsMaximumEscapedToastWithinRecordBound(t *testing.T) {
	// Quotes, backslashes, and U+2028 expand the most under JSON escaping.
	toast := protocol.PluginToast{PluginID: "p" + strings.Repeat("a", 31), Instance: strings.Repeat("i", 128),
		Title: strings.Repeat(`"`, 1024), Subtitle: strings.Repeat(`\`, 4096-3) + "\u2028", Variant: protocol.PluginToastWarning, Persistent: true}
	if err := toast.Validate(); err != nil {
		t.Fatal(err)
	}
	source := &pluginToastSequence{values: []protocol.PluginToast{toast}}
	response := servePluginContract(t, pluginWireTestService{source: source}, http.MethodGet, "/plugin-toasts", "")
	record := strings.TrimPrefix(response.Body.String(), ": connected\n\n")
	if !source.closed || !strings.HasPrefix(record, "event: plugin.toast\ndata: ") || len(record)-1 > 32<<10 {
		t.Fatalf("closed=%t record bytes=%d", source.closed, len(record)-1)
	}
	var got protocol.PluginToast
	if err := ReadPluginToasts(response.Body, func(value protocol.PluginToast) error { got = value; return nil }); err != nil || got != toast {
		t.Fatalf("decoded = %#v err=%v", got, err)
	}
}

func TestPluginToastStreamEndsInsteadOfSendingInvalidToast(t *testing.T) {
	invalid := protocol.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Notice", Variant: "loud"}
	source := &pluginToastSequence{values: []protocol.PluginToast{pluginContractToasts()[0], invalid, pluginContractToasts()[1]}}
	response := servePluginContract(t, pluginWireTestService{source: source}, http.MethodGet, "/plugin-toasts", "")
	want := ": connected\n\nevent: plugin.toast\ndata: {\"pluginId\":\"demo\",\"instance\":\"owner:1\",\"title\":\"Notice\",\"variant\":\"info\"}\n\n"
	if !source.closed || response.Code != http.StatusOK || response.Body.String() != want {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
