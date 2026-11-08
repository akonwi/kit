package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/codehighlight"
	"github.com/akonwi/kit/internal/highlight"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serveraccess"
	"github.com/akonwi/kit/internal/version"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

// countingHighlighter counts the requests that reach it, delegating to inner
// or, without one, waiting until the request's context ends.
type countingHighlighter struct {
	calls atomic.Int64
	inner highlight.Highlighter
}

func (h *countingHighlighter) Highlight(ctx context.Context, request highlight.Request) highlight.Result {
	h.calls.Add(1)
	if h.inner != nil {
		return h.inner.Highlight(ctx, request)
	}
	<-ctx.Done()
	return highlight.Plain(request, ctx.Err())
}

func newCountingHighlighter(t *testing.T) *countingHighlighter {
	t.Helper()
	scheduler := highlight.NewScheduler(highlight.DefaultWorkers, highlight.DefaultQueueDepth, 1<<20)
	t.Cleanup(scheduler.Close)
	return &countingHighlighter{inner: scheduler}
}

func highlightTestHandler(highlighter highlight.Highlighter, timeout time.Duration) http.Handler {
	return newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy([]string{"studio.tail3f2a.ts.net"}, nil), token: "test",
		registry:   localdiscovery.Registry{InstanceID: "instance_test"},
		highlights: codehighlight.New(highlighter, timeout, highlight.DefaultWorkers),
	})
}

func highlightTestClient(t *testing.T, handler http.Handler) *clienttransport.Client {
	t.Helper()
	server := httptest.NewServer(contractConformanceMiddleware(t, handler, func(err error) { t.Errorf("contract violation: %v", err) }))
	t.Cleanup(server.Close)
	return clienttransport.New(func() (clienttransport.Connection, error) {
		return clienttransport.Connection{URL: server.URL, Token: "test", InstanceID: "instance_test", ProtocolVersion: version.SessionProtocolVersion}, nil
	})
}

func TestHighlightCodeRoundTripsThroughTheTypedClient(t *testing.T) {
	highlighter := newCountingHighlighter(t)
	client := highlightTestClient(t, highlightTestHandler(highlighter, time.Second))
	input := protocol.HighlightCodeInput{Blocks: []protocol.HighlightCodeBlock{
		{Language: "golang", Source: "package main\r\n\r\n// 😀\tok\r\n"},
		{Language: "", Source: "plain words"},
		{Language: "cobol", Source: "DISPLAY 'HI'."},
		{Language: "ts", Source: "const x = \"日本\""},
	}}
	result, err := client.HighlightCode(t.Context(), input)
	if err != nil {
		t.Fatalf("HighlightCode() = %v", err)
	}
	want := []*protocol.Highlight{
		{Language: "go", Lines: [][]int{{0, 7, 0}, {}, {0, 8, 1}}, Roles: []string{"keyword", "comment"}},
		nil,
		nil,
		{Language: "typescript", Lines: [][]int{{0, 5, 0, 6, 1, 1, 8, 1, 2, 10, 4, 3}}, Roles: []string{"keyword", "variable", "operator", "string"}},
	}
	got := make([]*protocol.Highlight, len(result.Blocks))
	for i, block := range result.Blocks {
		got[i] = block.Highlight
	}
	if !reflect.DeepEqual(got, want) {
		encoded, _ := json.Marshal(result)
		t.Fatalf("HighlightCode() = %s", encoded)
	}
	// Blocks without a supported language never reach the highlighter.
	if calls := highlighter.calls.Load(); calls != 2 {
		t.Fatalf("highlighter calls = %d, want 2", calls)
	}
}

func TestHighlightCodeRejectsRequestsOverItsBounds(t *testing.T) {
	highlighter := newCountingHighlighter(t)
	handler := highlightTestHandler(highlighter, time.Second)
	blocks := func(count, size int) protocol.HighlightCodeInput {
		input := protocol.HighlightCodeInput{Blocks: make([]protocol.HighlightCodeBlock, count)}
		for i := range input.Blocks {
			input.Blocks[i] = protocol.HighlightCodeBlock{Language: "go", Source: strings.Repeat("a", size)}
		}
		return input
	}
	encode := func(input protocol.HighlightCodeInput) string {
		encoded, _ := json.Marshal(input)
		return string(encoded)
	}
	tests := map[string]struct {
		body   string
		status int
		code   httpapi.ErrorCode
		limit  protocol.HighlightCodeLimit
	}{
		"too many blocks":    {encode(blocks(protocol.MaxHighlightCodeBlocks+1, 1)), http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, protocol.HighlightCodeLimitBlocks},
		"block too large":    {encode(blocks(1, protocol.MaxHighlightCodeBlockBytes+1)), http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, protocol.HighlightCodeLimitBlockBytes},
		"total too large":    {encode(blocks(2, 400<<10)), http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, protocol.HighlightCodeLimitTotalBytes},
		"body over 1 MiB":    {encode(blocks(3, 400<<10)), http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, protocol.HighlightCodeLimitTotalBytes},
		"language too long":  {encode(protocol.HighlightCodeInput{Blocks: []protocol.HighlightCodeBlock{{Language: strings.Repeat("g", 65), Source: "x"}}}), http.StatusRequestEntityTooLarge, httpapi.ErrorLimitExceeded, protocol.HighlightCodeLimitLanguageBytes},
		"control language":   {`{"blocks":[{"language":"g\u001bo","source":"x"}]}`, http.StatusBadRequest, httpapi.ErrorInvalidRequest, ""},
		"unknown member":     {`{"blocks":[],"extra":1}`, http.StatusBadRequest, httpapi.ErrorInvalidRequest, ""},
		"malformed JSON":     {`{"blocks":[`, http.StatusBadRequest, httpapi.ErrorInvalidRequest, ""},
		"multiple documents": {`{"blocks":[]}{}`, http.StatusBadRequest, httpapi.ErrorInvalidRequest, ""},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7001/v1/highlight", strings.NewReader(test.body))
			addContractRequestHeaders(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d %s, want %d", response.Code, response.Body.String(), test.status)
			}
			var apiError *httpapi.APIError
			if err := httpapi.DecodeOperationError(httpapi.HighlightCode, response.Code, response.Body.Bytes()); !errors.As(err, &apiError) || apiError.Code != string(test.code) {
				t.Fatalf("decoded error = %v, want %s", err, test.code)
			}
			if test.limit != "" && !reflect.DeepEqual(apiError.TypedDetails, protocol.HighlightCodeLimitDetails{Limit: test.limit}) {
				t.Fatalf("details = %#v, want limit %q", apiError.TypedDetails, test.limit)
			}
		})
	}
	if calls := highlighter.calls.Load(); calls != 0 {
		t.Fatalf("rejected requests reached the highlighter %d times", calls)
	}
}

// highlightCode is not bound to a session, so it is easy to mistake for a
// public route. It keeps every check the session routes have, and a request
// failing one never reaches the highlighter.
func TestHighlightCodeKeepsTheAPIRouteChecks(t *testing.T) {
	highlighter := newCountingHighlighter(t)
	handler := highlightTestHandler(highlighter, time.Second)
	body := `{"blocks":[{"language":"go","source":"package main"}]}`
	requests := 0
	for name, test := range map[string]struct {
		status int
		code   string
		mutate func(*http.Request)
	}{
		"no credential":      {http.StatusUnauthorized, "unauthorized", func(r *http.Request) { r.Header.Del("Authorization") }},
		"wrong credential":   {http.StatusUnauthorized, "unauthorized", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }},
		"unlisted host":      {http.StatusMisdirectedRequest, "invalid_host", func(r *http.Request) { r.Host = "evil.example" }},
		"foreign origin":     {http.StatusForbidden, "forbidden", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }},
		"no instance":        {http.StatusConflict, "instance_mismatch", func(r *http.Request) { r.Header.Del(instanceHeader) }},
		"newer protocol":     {http.StatusUpgradeRequired, "protocol_mismatch", func(r *http.Request) { r.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion+1)) }},
		"no protocol header": {http.StatusUpgradeRequired, "protocol_mismatch", func(r *http.Request) { r.Header.Del(protocolHeader) }},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7001/v1/highlight", strings.NewReader(body))
			requests++
			request.RemoteAddr = "10.1.0." + strconv.Itoa(requests) + ":40000"
			addContractRequestHeaders(request)
			test.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(response.Body.Bytes(), &envelope)
			if response.Code != test.status || envelope.Error.Code != test.code {
				t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), test.status, test.code)
			}
		})
	}
	if calls := highlighter.calls.Load(); calls != 0 {
		t.Fatalf("rejected requests reached the highlighter %d times", calls)
	}
}

// A highlighter that cannot answer, whether saturated or stuck, leaves the
// blocks plain once the request's deadline passes rather than holding it.
func TestHighlightCodeAnswersPlainWhenTheHighlighterCannot(t *testing.T) {
	stuck := &countingHighlighter{}
	const timeout = 50 * time.Millisecond
	client := highlightTestClient(t, highlightTestHandler(stuck, timeout))
	input := protocol.HighlightCodeInput{Blocks: make([]protocol.HighlightCodeBlock, 6)}
	for i := range input.Blocks {
		input.Blocks[i] = protocol.HighlightCodeBlock{Language: "go", Source: "package main\n"}
	}
	started := time.Now()
	result, err := client.HighlightCode(t.Context(), input)
	if err != nil {
		t.Fatalf("HighlightCode() = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 10*timeout {
		t.Fatalf("HighlightCode() took %v with a %v deadline", elapsed, timeout)
	}
	if want := (protocol.HighlightCodeResult{Blocks: make([]protocol.HighlightedCode, 6)}); !reflect.DeepEqual(result, want) {
		t.Fatalf("HighlightCode() = %+v, want every block plain", result)
	}
}

type highlightReadService struct {
	sessionService
	workspaces *kitworkspace.Service
}

const highlightTestWorkspaceID = "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func (highlightReadService) ReadWorkspaceFile(_ context.Context, sessionID string, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	content := "package main\n\nfunc main() {}\n"
	if strings.HasSuffix(input.Path, ".txt") {
		content = "plain notes\n"
	}
	return protocol.WorkspaceFileRead{
		SessionID: sessionID, Workspace: protocol.WorkspaceRef{SessionID: sessionID, CWD: "/workspace", WorkspaceID: highlightTestWorkspaceID, State: protocol.WorkspaceReady, Limits: protocol.DefaultWorkspaceLimits()},
		Path: input.Path, Revision: "file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Size: int64(len(content)), Encoding: "utf-8",
		Content: content, ReturnedBytes: len(content), ReturnedLines: strings.Count(content, "\n"),
	}, nil
}

func (s highlightReadService) ReadHostFile(ctx context.Context, sessionID string, input protocol.ReadHostFileInput) (protocol.HostFileRead, func(), error) {
	return s.workspaces.ReadHostFile(ctx, sessionID, input)
}

func postJSON(t *testing.T, handler http.Handler, target string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(input)
	request := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("POST %s = %d %s", target, response.Code, response.Body.String())
	}
	return response
}

func TestWorkspaceFileReadsCarryHighlightOnlyWhenAsked(t *testing.T) {
	mux := http.NewServeMux()
	service := highlightReadService{}
	registerSessionRoutes(mux, service)
	target := "/v1/sessions/session_test/workspace/files/read"

	// A client that does not ask, such as one built before ADR 0062, gets
	// exactly the bytes it always did.
	plain := postJSON(t, mux, target, protocol.ReadWorkspaceFileInput{WorkspaceID: highlightTestWorkspaceID, Path: "main.go"})
	read, _ := service.ReadWorkspaceFile(t.Context(), "session_test", protocol.ReadWorkspaceFileInput{Path: "main.go"})
	want, _ := json.Marshal(read)
	if got := strings.TrimSuffix(plain.Body.String(), "\n"); got != string(want) {
		t.Fatalf("unhighlighted read = %s\nwant %s", got, want)
	}

	highlighted := postJSON(t, mux, target, protocol.ReadWorkspaceFileInput{WorkspaceID: highlightTestWorkspaceID, Path: "main.go", Highlight: true})
	var decoded protocol.WorkspaceFileRead
	if err := json.Unmarshal(highlighted.Body.Bytes(), &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("highlighted read = %s, %v", highlighted.Body.String(), err)
	}
	wantHighlight := &protocol.Highlight{Language: "go", Lines: [][]int{{0, 7, 0}, {}, {0, 4, 0, 5, 4, 1}}, Roles: []string{"keyword", "function"}}
	if !reflect.DeepEqual(decoded.Highlight, wantHighlight) {
		t.Fatalf("highlight = %+v, want %+v", decoded.Highlight, wantHighlight)
	}
	decoded.Highlight = nil
	if !reflect.DeepEqual(decoded, read) {
		t.Fatalf("highlighting changed the read: %+v", decoded)
	}

	unsupported := postJSON(t, mux, target, protocol.ReadWorkspaceFileInput{WorkspaceID: highlightTestWorkspaceID, Path: "notes.txt", Highlight: true})
	read, _ = service.ReadWorkspaceFile(t.Context(), "session_test", protocol.ReadWorkspaceFileInput{Path: "notes.txt"})
	want, _ = json.Marshal(read)
	if got := strings.TrimSuffix(unsupported.Body.String(), "\n"); got != string(want) {
		t.Fatalf("unsupported language read = %s\nwant %s", got, want)
	}
}

func TestHostTextReadsCarryHighlightOnlyWhenAsked(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "main.go")
	if err := os.WriteFile(source, []byte("package main\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(root, "dot.gif")
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	if err := os.WriteFile(image, gif, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, highlightReadService{workspaces: kitworkspace.NewService()})
	target := "/v1/sessions/session_test/host-files/read"

	plain := postJSON(t, mux, target, protocol.ReadHostFileInput{Path: source})
	if strings.Contains(plain.Body.String(), "highlight") {
		t.Fatalf("unhighlighted read = %s", plain.Body.String())
	}
	var decoded protocol.HostFileRead
	highlighted := postJSON(t, mux, target, protocol.ReadHostFileInput{Path: source, Highlight: true})
	if err := json.Unmarshal(highlighted.Body.Bytes(), &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("highlighted read = %s, %v", highlighted.Body.String(), err)
	}
	want := &protocol.Highlight{Language: "go", Lines: [][]int{{0, 7, 0}}, Roles: []string{"keyword"}}
	if text := decoded.Text(); text == nil || !reflect.DeepEqual(text.Highlight, want) {
		t.Fatalf("highlighted read = %s", highlighted.Body.String())
	}
	var plainDecoded protocol.HostFileRead
	_ = json.Unmarshal(plain.Body.Bytes(), &plainDecoded)
	text := decoded.Text()
	text.Highlight = nil
	if !reflect.DeepEqual(*text, *plainDecoded.Text()) {
		t.Fatalf("highlighting changed the read: %+v", text)
	}

	images := postJSON(t, mux, target, protocol.ReadHostFileInput{Path: image, Highlight: true})
	if err := json.Unmarshal(images.Body.Bytes(), &decoded); err != nil || decoded.Image() == nil {
		t.Fatalf("image read = %s, %v", images.Body.String(), err)
	}
}
