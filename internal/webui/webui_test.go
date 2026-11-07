package webui

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/fstest"
)

const (
	testIndex    = "<!doctype html><title>Kit</title><script type=\"module\" src=\"/assets/app-3f2a.js\"></script>\n"
	testScript   = "console.log(\"kit\")\n"
	testManifest = `{"name":"Kit","icons":[{"src":"/icon-192.png","sizes":"192x192"},{"src":"/assets/icon-512-9c1d.png"},{"src":"https://cdn.example/x.png"}]}`
)

func gzipped(t *testing.T, data string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func builtHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := New(fstest.MapFS{
		"index.html":                 {Data: []byte(testIndex)},
		"manifest.webmanifest":       {Data: []byte(testManifest)},
		"favicon.ico":                {Data: []byte("ico")},
		"icon-192.png":               {Data: []byte("png192")},
		"icon-unlisted.png":          {Data: []byte("unlisted")},
		"stats.json":                 {Data: []byte("{}")},
		".gitignore":                 {Data: []byte("*\n")},
		"assets/app-3f2a.js":         {Data: []byte(testScript)},
		"assets/app-3f2a.js.gz":      {Data: gzipped(t, testScript)},
		"assets/app-3f2a.css":        {Data: []byte("body{}")},
		"assets/icon-512-9c1d.png":   {Data: []byte("png512")},
		"assets/app-3f2a.js.map":     {Data: []byte("{}")},
		"assets/.hidden.js":          {Data: []byte("hidden")},
		"assets/nested/deep-1a2b.js": {Data: []byte("deep")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func serve(handler *Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy":    "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' blob: data:; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for name, value := range want {
		if got := response.Header().Get(name); got != value {
			t.Fatalf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestPagesServeTheIndexForRevalidation(t *testing.T) {
	handler := builtHandler(t)
	for _, page := range []string{"/", "/pair", "/sessions/session_0123456789abcdef0123456789abcdef"} {
		t.Run(page, func(t *testing.T) {
			response := serve(handler, http.MethodGet, page, nil)
			if response.Code != http.StatusOK || response.Body.String() != testIndex {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("Cache-Control = %q", got)
			}
			assertSecurityHeaders(t, response)

			etag := response.Header().Get("ETag")
			revalidated := serve(handler, http.MethodGet, page, map[string]string{"If-None-Match": etag})
			if etag == "" || revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
				t.Fatalf("revalidation with %q = %d %q", etag, revalidated.Code, revalidated.Body.String())
			}
		})
	}
}

func TestHeadSendsHeadersWithoutABody(t *testing.T) {
	response := serve(builtHandler(t), http.MethodHead, "/", nil)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Length") != strconv.Itoa(len(testIndex)) {
		t.Fatalf("response = %d length=%q body=%q", response.Code, response.Header().Get("Content-Length"), response.Body.String())
	}
}

func TestAssetsAreImmutableAndPreferPrecompressedGzip(t *testing.T) {
	handler := builtHandler(t)

	plain := serve(handler, http.MethodGet, "/assets/app-3f2a.js", nil)
	if plain.Code != http.StatusOK || plain.Body.String() != testScript || plain.Header().Get("Content-Encoding") != "" {
		t.Fatalf("plain = %d %q encoding=%q", plain.Code, plain.Body.String(), plain.Header().Get("Content-Encoding"))
	}
	if got := plain.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := plain.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := plain.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q", got)
	}
	assertSecurityHeaders(t, plain)

	compressed := serve(handler, http.MethodGet, "/assets/app-3f2a.js", map[string]string{"Accept-Encoding": "br, gzip;q=0.8"})
	if compressed.Code != http.StatusOK || compressed.Header().Get("Content-Encoding") != "gzip" ||
		!bytes.Equal(compressed.Body.Bytes(), gzipped(t, testScript)) {
		t.Fatalf("compressed = %d encoding=%q", compressed.Code, compressed.Header().Get("Content-Encoding"))
	}
	if compressed.Header().Get("ETag") == plain.Header().Get("ETag") {
		t.Fatalf("compressed and plain bodies share ETag %q", plain.Header().Get("ETag"))
	}

	refused := serve(handler, http.MethodGet, "/assets/app-3f2a.js", map[string]string{"Accept-Encoding": "gzip;q=0"})
	if refused.Header().Get("Content-Encoding") != "" || refused.Body.String() != testScript {
		t.Fatalf("q=0 response encoding=%q body=%q", refused.Header().Get("Content-Encoding"), refused.Body.String())
	}

	stylesheet := serve(handler, http.MethodGet, "/assets/app-3f2a.css", map[string]string{"Accept-Encoding": "gzip"})
	if stylesheet.Body.String() != "body{}" || stylesheet.Header().Get("Content-Encoding") != "" || stylesheet.Header().Get("Vary") != "" {
		t.Fatalf("stylesheet = %q encoding=%q vary=%q", stylesheet.Body.String(), stylesheet.Header().Get("Content-Encoding"), stylesheet.Header().Get("Vary"))
	}
}

func TestRootFilesAreTheFixedHomeScreenSet(t *testing.T) {
	handler := builtHandler(t)
	tests := []struct {
		path, contentType, body string
	}{
		{"/manifest.webmanifest", "application/manifest+json", testManifest},
		{"/favicon.ico", "image/x-icon", "ico"},
		{"/icon-192.png", "image/png", "png192"},
		{"/robots.txt", "text/plain; charset=utf-8", "User-agent: *\nDisallow: /\n"},
	}
	for _, test := range tests {
		response := serve(handler, http.MethodGet, test.path, nil)
		if response.Code != http.StatusOK || response.Body.String() != test.body ||
			response.Header().Get("Content-Type") != test.contentType || response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s = %d %q type=%q cache=%q", test.path, response.Code, response.Body.String(),
				response.Header().Get("Content-Type"), response.Header().Get("Cache-Control"))
		}
	}
}

func TestMatchAcceptsOnlyThePagesAndBuildFiles(t *testing.T) {
	handler := builtHandler(t)
	matched := []string{
		"/", "/pair", "/sessions/session_1", "/manifest.webmanifest", "/favicon.ico", "/icon-192.png",
		"/robots.txt", "/assets/app-3f2a.js", "/assets/app-3f2a.css", "/assets/icon-512-9c1d.png",
	}
	unmatched := []string{
		"/index.html", "/icon-unlisted.png", "/stats.json", "/.gitignore", "/assets/app-3f2a.js.gz",
		"/assets/app-3f2a.js.map", "/assets/.hidden.js", "/assets/nested/deep-1a2b.js", "/assets/",
		"/assets/missing.js", "/sessions/", "/sessions/a/b", "/sessions/.hidden", "/pair/", "/v1/health",
		"/v1/sessions", "/unknown", "/assets/app-3f2a%2Ejs",
	}
	for _, target := range matched {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			if !handler.Match(httptest.NewRequest(method, target, nil)) {
				t.Errorf("%s %s did not match", method, target)
			}
		}
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			if handler.Match(httptest.NewRequest(method, target, nil)) {
				t.Errorf("%s %s matched", method, target)
			}
		}
	}
	for _, target := range unmatched {
		if handler.Match(httptest.NewRequest(http.MethodGet, target, nil)) {
			t.Errorf("GET %s matched", target)
		}
	}
}

func TestWithoutABuildPagesServeThePlaceholder(t *testing.T) {
	handler, err := New(fstest.MapFS{".gitignore": {Data: []byte("*\n")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"/", "/pair", "/sessions/session_1"} {
		response := serve(handler, http.MethodGet, page, nil)
		if response.Code != http.StatusNotFound || response.Body.String() != placeholder {
			t.Fatalf("%s = %d %q", page, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s type=%q cache=%q", page, response.Header().Get("Content-Type"), response.Header().Get("Cache-Control"))
		}
		assertSecurityHeaders(t, response)
	}
	if head := serve(handler, http.MethodHead, "/", nil); head.Code != http.StatusNotFound || head.Body.Len() != 0 {
		t.Fatalf("HEAD / = %d %q", head.Code, head.Body.String())
	}
	if robots := serve(handler, http.MethodGet, "/robots.txt", nil); robots.Code != http.StatusOK {
		t.Fatalf("robots.txt = %d", robots.Code)
	}
	for _, target := range []string{"/manifest.webmanifest", "/favicon.ico", "/assets/app.js"} {
		if handler.Match(httptest.NewRequest(http.MethodGet, target, nil)) {
			t.Fatalf("GET %s matched without a build", target)
		}
	}
}

func TestEmbeddedCheckoutBuildHasOnlyItsIgnoreFile(t *testing.T) {
	entries, err := fs.ReadDir(build, "build")
	if err != nil {
		t.Fatal(err)
	}
	handler := Embedded()
	if handler.index == nil {
		if len(entries) != 1 || entries[0].Name() != ".gitignore" {
			t.Fatalf("build without index.html contains %v", entries)
		}
		if response := serve(handler, http.MethodGet, "/", nil); response.Code != http.StatusNotFound {
			t.Fatalf("GET / = %d", response.Code)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	tests := map[string]bool{
		"":                  false,
		"gzip":              true,
		"GZIP":              true,
		"br, gzip":          true,
		"gzip;q=0.5":        true,
		"gzip; q=0":         false,
		"gzip;q=0.000":      false,
		"identity, deflate": false,
		"x-gzip":            false,
	}
	for value, want := range tests {
		if got := acceptsGzip([]string{value}); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", value, got, want)
		}
	}
}
