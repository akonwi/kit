package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serveraccess"
	"github.com/akonwi/kit/internal/version"
	"github.com/akonwi/kit/internal/webui"
)

const webTestIndex = "<!doctype html><title>Kit</title>\n"

func webTestHandler(t *testing.T, built bool) http.Handler {
	t.Helper()
	files := fstest.MapFS{".gitignore": {Data: []byte("*\n")}}
	if built {
		files["index.html"] = &fstest.MapFile{Data: []byte(webTestIndex)}
		files["assets/app-3f2a.js"] = &fstest.MapFile{Data: []byte("console.log(1)\n")}
		files["v1/health"] = &fstest.MapFile{Data: []byte("not served")}
	}
	web, err := webui.New(files)
	if err != nil {
		t.Fatal(err)
	}
	return newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy([]string{"studio.tail3f2a.ts.net"}, nil), token: "test",
		registry: localdiscovery.Registry{InstanceID: "instance_test"}, sessions: scratchpadWireTestService{}, web: web,
	})
}

func TestBrowserClientPagesNeedNoCredentialButKeepTheHostAndOriginChecks(t *testing.T) {
	handler := webTestHandler(t, true)
	tests := []struct {
		name, target, host, origin string
		status                     int
		body                       string
	}{
		{name: "loopback root", target: "/", host: "127.0.0.1:7001", status: http.StatusOK, body: webTestIndex},
		{name: "allowed host pairing link", target: "/pair", host: "studio.tail3f2a.ts.net", status: http.StatusOK, body: webTestIndex},
		{name: "own origin session", target: "/sessions/session_1", host: "studio.tail3f2a.ts.net", origin: "https://studio.tail3f2a.ts.net", status: http.StatusOK, body: webTestIndex},
		{name: "asset", target: "/assets/app-3f2a.js", host: "127.0.0.1:7001", status: http.StatusOK, body: "console.log(1)\n"},
		{name: "unlisted host", target: "/", host: "evil.example", status: http.StatusMisdirectedRequest},
		{name: "unlisted origin", target: "/pair", host: "studio.tail3f2a.ts.net", origin: "https://evil.example", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://example.com"+test.target, nil)
			request.Host = test.host
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || (test.body != "" && response.Body.String() != test.body) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestBrowserClientWithoutABuildServesThePlaceholderPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/pair", nil)
	response := httptest.NewRecorder()
	webTestHandler(t, false).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("response = %d type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

// Every session API operation keeps its credential, instance, and protocol
// checks with the browser client present, even when its build contains a
// file at an API path.
func TestEverySessionAPIPathStillNeedsCredentialInstanceAndProtocol(t *testing.T) {
	handler := webTestHandler(t, true)
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	// Each request comes from its own address so the failed-authentication
	// limiter does not answer in place of the check under test.
	requests := 0
	for _, descriptor := range httpapi.Catalog() {
		target := "http://127.0.0.1:7001" + parameter.ReplaceAllString(descriptor.Path, "x")
		t.Run(descriptor.Method+" "+descriptor.Path, func(t *testing.T) {
			check := func(status int, mutate func(*http.Request)) {
				t.Helper()
				request := httptest.NewRequest(descriptor.Method, target, nil)
				requests++
				request.RemoteAddr = fmt.Sprintf("10.0.%d.%d:40000", requests/250, requests%250+1)
				addContractRequestHeaders(request)
				mutate(request)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != status {
					t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
				}
			}
			if descriptor.Unauthenticated {
				return
			}
			check(http.StatusUnauthorized, func(r *http.Request) { r.Header.Del("Authorization") })
			if !descriptor.OptionalInstance {
				check(http.StatusConflict, func(r *http.Request) { r.Header.Del(instanceHeader) })
			}
			if !descriptor.NoProtocol {
				check(http.StatusUpgradeRequired, func(r *http.Request) {
					r.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion+1))
				})
			}
		})
	}
}

// A path outside the browser client's pages and files is served exactly as
// it is without the client.
func TestUnlistedPathsBehaveAsWithoutTheBrowserClient(t *testing.T) {
	withWeb := webTestHandler(t, true)
	without := newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy([]string{"studio.tail3f2a.ts.net"}, nil), token: "test",
		registry: localdiscovery.Registry{InstanceID: "instance_test"}, sessions: scratchpadWireTestService{},
	})
	for _, target := range []string{"/index.html", "/assets/missing.js", "/favicon.ico", "/sessions/a/b", "/unknown", "/v1/unknown"} {
		for _, authenticated := range []bool{false, true} {
			serve := func(handler http.Handler) *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001"+target, nil)
				if authenticated {
					addContractRequestHeaders(request)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			}
			got, want := serve(withWeb), serve(without)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Errorf("GET %s (authenticated %v) = %d %q, want %d %q", target, authenticated, got.Code, got.Body.String(), want.Code, want.Body.String())
			}
		}
	}
}

// Loading the browser client never counts as a failed authentication, so a
// browser that reloads it cannot lock out its own address.
func TestBrowserClientRequestsDoNotSpendTheFailedAuthenticationLimit(t *testing.T) {
	handler := webTestHandler(t, true)
	for range 500 {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/", nil)
		request.Header.Set("Authorization", "Bearer wrong")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET / = %d", response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/v1/sessions", nil)
	addContractRequestHeaders(request)
	request.Header.Set("Authorization", "Bearer wrong")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("first failed authentication = %d %s", response.Code, response.Body.String())
	}
}
