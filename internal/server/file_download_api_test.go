package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	kitsession "github.com/akonwi/kit/internal/session"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

// downloadRouteService opens downloads as the runtime service does, with a
// cwd the test can move.
type downloadRouteService struct {
	sessionService
	workspaces *kitworkspace.Service
	cwd        *atomic.Value
	// opened, when set, runs after the file is opened and before recheck.
	opened func()
}

func (s downloadRouteService) OpenFileDownload(ctx context.Context, sessionID string, input protocol.DownloadFileInput) (*kitworkspace.Download, func(context.Context) error, error) {
	if sessionID == "session_gone" {
		return nil, nil, kitsession.ErrNotFound
	}
	if !input.HasWorkspace {
		download, err := s.workspaces.OpenHostDownload(ctx, sessionID, input)
		return download, func(context.Context) error { return nil }, err
	}
	cwd := s.cwd.Load().(string)
	download, err := s.workspaces.OpenWorkspaceDownload(ctx, sessionID, cwd, input)
	if err != nil {
		return nil, nil, err
	}
	if s.opened != nil {
		s.opened()
	}
	return download, func(context.Context) error {
		if current := s.cwd.Load().(string); current != cwd {
			return staleWorkspaceError(s.workspaces.Ref(sessionID, current))
		}
		return nil
	}, nil
}

type downloadFixture struct {
	t         *testing.T
	cwd       string
	service   downloadRouteService
	mux       *http.ServeMux
	workspace string
}

func newDownloadFixture(t *testing.T) *downloadFixture {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &atomic.Value{}
	current.Store(cwd)
	service := downloadRouteService{workspaces: kitworkspace.NewService(), cwd: current}
	f := &downloadFixture{t: t, cwd: cwd, service: service, workspace: service.workspaces.Ref("session_test", cwd).WorkspaceID}
	f.mux = http.NewServeMux()
	registerSessionRoutes(f.mux, service)
	return f
}

func (f *downloadFixture) target(path string) string {
	return "/v1/sessions/session_test/files/content?workspaceId=" + f.workspace + "&path=" + url.QueryEscape(path)
}

func (f *downloadFixture) do(method, target string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Add(headers[i], headers[i+1])
	}
	response := httptest.NewRecorder()
	f.mux.ServeHTTP(response, request)
	return response
}

func (f *downloadFixture) revision(path string) string {
	f.t.Helper()
	read, err := f.service.workspaces.ReadLineRange(context.Background(), "session_test", f.cwd, kitworkspace.LineRangeInput{WorkspaceID: f.workspace, Path: path, StartLine: 1, EndLine: 1})
	if err != nil {
		f.t.Fatal(err)
	}
	return read.Revision
}

func expectHeaders(t *testing.T, name string, response *httptest.ResponseRecorder, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for key := range response.Header() {
		got[key] = response.Header().Get(key)
	}
	if len(got) != len(want) {
		t.Fatalf("%s headers = %v, want %v", name, got, want)
	}
	for key, value := range want {
		if got[http.CanonicalHeaderKey(key)] != value {
			t.Fatalf("%s header %s = %q, want %q (all: %v)", name, key, got[http.CanonicalHeaderKey(key)], value, got)
		}
	}
}

func TestDownloadServesAWorkspaceFileAsAnHTTPResource(t *testing.T) {
	f := newDownloadFixture(t)
	revision := f.revision("notes.txt")
	info, _ := os.Stat(filepath.Join(f.cwd, "notes.txt"))
	identity := map[string]string{
		"X-Kit-Session-ID": "session_test", "X-Kit-Workspace-ID": f.workspace, "X-Kit-File-Path": "notes.txt",
		"X-Kit-File-Modified-At": info.ModTime().UTC().Format(time.RFC3339Nano), "ETag": `"` + revision + `"`, "Cache-Control": "private, no-cache",
	}
	body := func(extra map[string]string) map[string]string {
		headers := map[string]string{
			"Accept-Ranges": "bytes", "Content-Type": "application/octet-stream", "X-Content-Type-Options": "nosniff",
			"Content-Security-Policy": "sandbox; default-src 'none'", "Content-Disposition": `attachment; filename="notes.txt"; filename*=UTF-8''notes.txt`,
		}
		for key, value := range identity {
			headers[key] = value
		}
		for key, value := range extra {
			headers[key] = value
		}
		return headers
	}

	response := f.do(http.MethodGet, f.target("notes.txt"))
	if response.Code != http.StatusOK || response.Body.String() != "0123456789" {
		t.Fatalf("GET = %d %q", response.Code, response.Body.String())
	}
	expectHeaders(t, "GET", response, body(map[string]string{"Content-Length": "10"}))

	response = f.do(http.MethodHead, f.target("notes.txt"))
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %q", response.Code, response.Body.String())
	}
	expectHeaders(t, "HEAD", response, body(map[string]string{"Content-Length": "10"}))

	etag := `"` + revision + `"`
	stale := `"file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`
	for _, test := range []struct {
		name    string
		headers []string
		status  int
		body    string
		want    map[string]string
	}{
		{"current If-None-Match", []string{"If-None-Match", etag}, http.StatusNotModified, "", identity},
		{"stale If-None-Match", []string{"If-None-Match", stale}, http.StatusOK, "0123456789", body(map[string]string{"Content-Length": "10"})},
		{"stale If-Match", []string{"If-Match", stale}, http.StatusPreconditionFailed, "", identity},
		{"current If-Match", []string{"If-Match", etag}, http.StatusOK, "0123456789", body(map[string]string{"Content-Length": "10"})},
		{"resumed range", []string{"Range", "bytes=4-", "If-Range", etag}, http.StatusPartialContent, "456789", body(map[string]string{"Content-Length": "6", "Content-Range": "bytes 4-9/10"})},
		{"bounded range", []string{"Range", "bytes=2-4"}, http.StatusPartialContent, "234", body(map[string]string{"Content-Length": "3", "Content-Range": "bytes 2-4/10"})},
		{"suffix range", []string{"Range", "bytes=-3"}, http.StatusPartialContent, "789", body(map[string]string{"Content-Length": "3", "Content-Range": "bytes 7-9/10"})},
		{"stale If-Range", []string{"Range", "bytes=4-", "If-Range", stale}, http.StatusOK, "0123456789", body(map[string]string{"Content-Length": "10"})},
		{"weak If-Range", []string{"Range", "bytes=4-", "If-Range", "W/" + etag}, http.StatusOK, "0123456789", body(map[string]string{"Content-Length": "10"})},
		{"several ranges", []string{"Range", "bytes=0-1,4-5"}, http.StatusOK, "0123456789", body(map[string]string{"Content-Length": "10"})},
		{"unsatisfiable range", []string{"Range", "bytes=10-"}, http.StatusRequestedRangeNotSatisfiable, "", func() map[string]string {
			headers := map[string]string{"Content-Range": "bytes */10", "Content-Length": "0"}
			for key, value := range identity {
				headers[key] = value
			}
			return headers
		}()},
	} {
		response := f.do(http.MethodGet, f.target("notes.txt"), test.headers...)
		if response.Code != test.status || response.Body.String() != test.body {
			t.Fatalf("%s = %d %q, want %d %q", test.name, response.Code, response.Body.String(), test.status, test.body)
		}
		expectHeaders(t, test.name, response, test.want)
	}
}

func TestDownloadOfAZeroSizeFileHasNoValidatorOrRange(t *testing.T) {
	f := newDownloadFixture(t)
	if err := os.WriteFile(filepath.Join(f.cwd, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(f.cwd, "empty"))
	identity := map[string]string{
		"X-Kit-Session-ID": "session_test", "X-Kit-Workspace-ID": f.workspace, "X-Kit-File-Path": "empty",
		"X-Kit-File-Modified-At": info.ModTime().UTC().Format(time.RFC3339Nano), "Cache-Control": "no-store",
	}
	whole := map[string]string{
		"Accept-Ranges": "none", "Content-Type": "application/octet-stream", "X-Content-Type-Options": "nosniff",
		"Content-Security-Policy": "sandbox; default-src 'none'", "Content-Disposition": `attachment; filename="empty"; filename*=UTF-8''empty`,
	}
	for key, value := range identity {
		whole[key] = value
	}
	for _, headers := range [][]string{nil, {"If-None-Match", "*"}, {"Range", "bytes=0-1", "If-Range", `"file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`}} {
		response := f.do(http.MethodGet, f.target("empty"), headers...)
		if response.Code != http.StatusOK || response.Body.Len() != 0 {
			t.Fatalf("GET %v = %d %q", headers, response.Code, response.Body.String())
		}
		expectHeaders(t, "zero-size GET", response, whole)
	}
	response := f.do(http.MethodGet, f.target("empty"), "If-Match", "*")
	if response.Code != http.StatusPreconditionFailed {
		t.Fatalf("If-Match = %d", response.Code)
	}
	expectHeaders(t, "zero-size If-Match", response, identity)
}

func TestDownloadServesHostFilesWithoutAWorkspace(t *testing.T) {
	f := newDownloadFixture(t)
	name := filepath.Join(f.cwd, "notes.txt")
	response := f.do(http.MethodGet, "/v1/sessions/session_test/files/content?path="+url.QueryEscape(name))
	if response.Code != http.StatusOK || response.Body.String() != "0123456789" {
		t.Fatalf("GET = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("ETag"); !strings.HasPrefix(got, `"hostfile_`) || response.Header().Get("X-Kit-Workspace-ID") != "" ||
		response.Header().Get("X-Kit-File-Path") != (&url.URL{Path: name}).EscapedPath() {
		t.Fatalf("host headers = %v", response.Header())
	}
}

func TestDownloadRejectsMalformedRequestsWithDeclaredErrors(t *testing.T) {
	f := newDownloadFixture(t)
	if err := os.Mkdir(filepath.Join(f.cwd, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	base := "/v1/sessions/session_test/files/content"
	for _, test := range []struct {
		name, target, code string
		status             int
	}{
		{"no path", base + "?workspaceId=" + f.workspace, "invalid_request", http.StatusBadRequest},
		{"repeated path", f.target("notes.txt") + "&path=notes.txt", "invalid_request", http.StatusBadRequest},
		{"empty workspace", base + "?workspaceId=&path=notes.txt", "invalid_request", http.StatusBadRequest},
		{"relative path without a workspace", base + "?path=notes.txt", "invalid_path", http.StatusBadRequest},
		{"absolute path with a workspace", f.target("/etc/hosts"), "invalid_path", http.StatusBadRequest},
		{"missing file", f.target("missing.txt"), "not_found", http.StatusNotFound},
		{"directory", f.target("dir"), "not_file", http.StatusBadRequest},
		{"other workspace", base + "?workspaceId=workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&path=notes.txt", "stale_workspace", http.StatusConflict},
		{"missing host file", base + "?path=" + url.QueryEscape(filepath.Join(f.cwd, "missing.txt")), "not_found", http.StatusNotFound},
		{"host directory", base + "?path=" + url.QueryEscape(filepath.Join(f.cwd, "dir")), "not_file", http.StatusBadRequest},
		{"missing session", "/v1/sessions/session_gone/files/content?path=%2Fetc%2Fhosts", "not_found", http.StatusNotFound},
	} {
		response := f.do(http.MethodGet, test.target)
		err := httpapi.DecodeOperationError(httpapi.DownloadFile, response.Code, response.Body.Bytes())
		var apiError *httpapi.APIError
		if response.Code != test.status || !errors.As(err, &apiError) || apiError.Code != test.code {
			t.Fatalf("%s = %d %s (%v), want %d %s", test.name, response.Code, response.Body.String(), err, test.status, test.code)
		}
	}
}

func TestDownloadRechecksTheWorkspaceBeforeHeaders(t *testing.T) {
	f := newDownloadFixture(t)
	moved := t.TempDir()
	f.service.opened = func() { f.service.cwd.Store(moved) }
	f.mux = http.NewServeMux()
	registerSessionRoutes(f.mux, f.service)
	response := f.do(http.MethodGet, f.target("notes.txt"))
	err := httpapi.DecodeOperationError(httpapi.DownloadFile, response.Code, response.Body.Bytes())
	var apiError *httpapi.APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("download after the cwd moved = %d %s (%v)", response.Code, response.Body.String(), err)
	}
	details := fmt.Sprint(apiError.Details, apiError.TypedDetails)
	if response.Code != http.StatusConflict || apiError.Code != "stale_workspace" || !strings.Contains(details, f.service.workspaces.Ref("session_test", moved).WorkspaceID) {
		t.Fatalf("download after the cwd moved = %d %s", response.Code, response.Body.String())
	}
}

// startStalledDownload sends a GET on a raw connection and never reads the
// body, so the server's writes stall once the socket buffers fill.
func startStalledDownload(t *testing.T, address, target string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, "GET "+target+" HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("stalled download status = %q, %v", status, err)
	}
	return conn
}

func TestStalledDownloadsLoseTheirSlotsAtTheWriteDeadline(t *testing.T) {
	previous := downloadWriteTimeout
	downloadWriteTimeout = 300 * time.Millisecond
	t.Cleanup(func() { downloadWriteTimeout = previous })
	f := newDownloadFixture(t)
	large, err := os.Create(filepath.Join(f.cwd, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(256 << 20); err != nil {
		t.Fatal(err)
	}
	_ = large.Close()
	server := httptest.NewServer(f.mux)
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	startStalledDownload(t, address, f.target("large"))
	startStalledDownload(t, address, f.target("large"))
	response, err := http.Get(server.URL + f.target("notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third download while two stall = %d, want 429", response.StatusCode)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get(server.URL + f.target("notes.txt"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode == http.StatusOK && string(body) == "0123456789" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("download after the stalled ones timed out = %d", response.StatusCode)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDownloadEndsShortWhenTheFileIsTruncatedAndStopsAtItsLengthWhenItGrows(t *testing.T) {
	f := newDownloadFixture(t)
	name := filepath.Join(f.cwd, "log")
	if err := os.WriteFile(name, make([]byte, 8<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.mux)
	defer server.Close()

	response, err := http.Get(server.URL + f.target("log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(response.Body, make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(name, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated download ended with %v, want unexpected EOF", err)
	}
	_ = response.Body.Close()

	response, err = http.Get(server.URL + f.target("log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(response.Body, make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	appended, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appended.Write(make([]byte, 1<<20)); err != nil {
		t.Fatal(err)
	}
	_ = appended.Close()
	rest, err := io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err != nil || rest != 1<<20-64<<10 {
		t.Fatalf("grown download = %d more bytes, %v; want the length at open", rest, err)
	}
}
