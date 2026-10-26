package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	kitsession "github.com/akonwi/kit/internal/session"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

type hostFileRouteService struct {
	sessionService
	workspaces *kitworkspace.Service
}

func (s hostFileRouteService) ReadHostFile(ctx context.Context, sessionID string, input protocol.ReadHostFileInput) (protocol.HostFileRead, func(), error) {
	if sessionID == "session_gone" {
		return protocol.HostFileRead{}, func() {}, kitsession.ErrNotFound
	}
	return s.workspaces.ReadHostFile(ctx, sessionID, input)
}

func TestHostFileRouteReadsFilesAndDeclaresItsErrors(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(name, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "blob"), []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerSessionRoutes(mux, hostFileRouteService{workspaces: kitworkspace.NewService()})
	readIn := func(session, path string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(protocol.ReadHostFileInput{Path: path})
		request := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+session+"/host-files/read", strings.NewReader(string(body)))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}
	read := func(path string) *httptest.ResponseRecorder { return readIn("session_test", path) }

	response := read(name)
	if response.Code != http.StatusOK {
		t.Fatalf("read = %d %s", response.Code, response.Body.String())
	}
	var decoded protocol.HostFileRead
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("decoded = %+v, %v", decoded, err)
	}
	if text := decoded.Text(); text == nil || text.Path != name || text.Content != "hello\n" || text.SessionID != "session_test" {
		t.Fatalf("decoded = %+v", decoded)
	}

	for path, want := range map[string]struct {
		status int
		code   string
	}{
		filepath.Join(root, "blob"):        {http.StatusUnsupportedMediaType, "binary_file"},
		filepath.Join(root, "missing.txt"): {http.StatusNotFound, "not_found"},
		root:                               {http.StatusBadRequest, "not_file"},
		"relative.txt":                     {http.StatusBadRequest, "invalid_path"},
	} {
		response := read(path)
		if response.Code != want.status {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body.String())
		}
		err := httpapi.DecodeOperationError(httpapi.ReadHostFile, response.Code, response.Body.Bytes())
		var apiError *httpapi.APIError
		if !errors.As(err, &apiError) || apiError.Code != want.code {
			t.Fatalf("%s decoded error = %v, want %s", path, err, want.code)
		}
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"missing session": readIn("session_gone", "/etc/hosts"),
		"malformed body": func() *httptest.ResponseRecorder {
			request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/host-files/read", strings.NewReader(`{"path":1}`))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			return response
		}(),
	} {
		err := httpapi.DecodeOperationError(httpapi.ReadHostFile, response.Code, response.Body.Bytes())
		var apiError *httpapi.APIError
		if !errors.As(err, &apiError) {
			t.Fatalf("%s: %d %s decoded as %v", name, response.Code, response.Body.String(), err)
		}
		if name == "missing session" && (apiError.StatusCode != http.StatusNotFound || apiError.Code != "not_found") ||
			name == "malformed body" && (apiError.StatusCode != http.StatusBadRequest || apiError.Code != "invalid_request") {
			t.Fatalf("%s = %+v", name, apiError)
		}
	}
}
