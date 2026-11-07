// Package webui serves Kit's browser client: the compiled output of apps/web
// embedded into the executable, and the browser security headers it runs
// under (ADR 0059).
//
// It answers only a fixed set of paths outside the session API. Every other
// request, including any path under /v1, is left to the server's ordinary
// credential, instance, and protocol checks.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/pairing"
)

// build holds the browser client's build output. The apps/web build writes
// it; a checkout holds only build/.gitignore, so `go build` never needs the
// web toolchain.
//
//go:embed all:build
var build embed.FS

// Security headers sent with every browser client response. Scripts, styles,
// fonts, and connections are limited to the client's own origin, so any
// script that could read the stored device token is one the server shipped.
const (
	contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
		"img-src 'self' blob: data:; connect-src 'self'; manifest-src 'self'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	immutableCache = "public, max-age=31536000, immutable"
	revalidate     = "no-cache"
)

// robots keeps crawlers away from a server that is reachable beyond loopback.
const robots = "User-agent: *\nDisallow: /\n"

// placeholder is served for the client's pages when the executable was built
// without the browser client.
const placeholder = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Kit</title></head>
<body>
<h1>Kit</h1>
<p>This Kit build does not include the browser client.</p>
</body>
</html>
`

// manifestPath, faviconPath, and robotsPath are the fixed root files a phone
// reads to add the client to its home screen.
const (
	manifestPath = "/manifest.webmanifest"
	faviconPath  = "/favicon.ico"
	robotsPath   = "/robots.txt"
	assetsPrefix = "/assets/"
	sessionsPath = "/sessions/"
)

// contentTypes lists the only file types the client serves. A build file of
// another type is never served.
var contentTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".gif":         "image/gif",
	".html":        "text/html; charset=utf-8",
	".ico":         "image/x-icon",
	".jpg":         "image/jpeg",
	".js":          "text/javascript; charset=utf-8",
	".json":        "application/json",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".txt":         "text/plain; charset=utf-8",
	".webmanifest": "application/manifest+json",
	".webp":        "image/webp",
	".woff2":       "font/woff2",
}

// file is one servable response body.
type file struct {
	data         []byte
	gzip         []byte
	contentType  string
	cacheControl string
	etag         string
}

// Handler serves the browser client. Its zero value is not usable; use New
// or Embedded.
type Handler struct {
	// index is the client's HTML entry point, or nil when the build is absent.
	index *file
	// files maps the request path of each servable non-page file.
	files map[string]*file
}

// Embedded returns a Handler for the build compiled into this executable. A
// malformed embedded build panics, since the release build produced it.
func Embedded() *Handler {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(fmt.Sprintf("webui: embedded build: %v", err))
	}
	handler, err := New(sub)
	if err != nil {
		panic(fmt.Sprintf("webui: embedded build: %v", err))
	}
	return handler
}

// New returns a Handler for the build output in files. index.html marks a
// build; without it, the client's pages serve a placeholder and no other
// build file is served. Build files are read once.
func New(files fs.FS) (*Handler, error) {
	handler := &Handler{files: map[string]*file{robotsPath: newFile([]byte(robots), contentTypes[".txt"], revalidate, nil)}}
	index, err := readFile(files, "index.html", revalidate)
	if errors.Is(err, fs.ErrNotExist) {
		return handler, nil
	}
	if err != nil {
		return nil, err
	}
	handler.index = index

	for _, name := range []string{"manifest.webmanifest", "favicon.ico"} {
		if err := handler.addRootFile(files, name); err != nil {
			return nil, err
		}
	}
	if manifest := handler.files[manifestPath]; manifest != nil {
		icons, err := manifestIcons(manifest.data)
		if err != nil {
			return nil, err
		}
		for _, icon := range icons {
			if err := handler.addRootFile(files, icon); err != nil {
				return nil, err
			}
		}
	}

	assets, err := fs.ReadDir(files, "assets")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, entry := range assets {
		name := entry.Name()
		if entry.IsDir() || !servableName(name) || strings.HasSuffix(name, ".gz") {
			continue
		}
		asset, err := readFile(files, "assets/"+name, immutableCache)
		if err != nil {
			return nil, err
		}
		if asset != nil {
			handler.files[assetsPrefix+name] = asset
		}
	}
	return handler, nil
}

// addRootFile serves the build file name at /name when it exists.
func (h *Handler) addRootFile(files fs.FS, name string) error {
	root, err := readFile(files, name, revalidate)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if root != nil {
		h.files["/"+name] = root
	}
	return nil
}

// manifestIcons returns the root icon files a web app manifest names. Icons
// elsewhere are served only if they are assets.
func manifestIcons(data []byte) ([]string, error) {
	var manifest struct {
		Icons []struct {
			Src string `json:"src"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest.webmanifest: %w", err)
	}
	var icons []string
	for _, icon := range manifest.Icons {
		name := strings.TrimPrefix(icon.Src, "/")
		if path.Ext(name) == ".png" && servableName(name) {
			icons = append(icons, name)
		}
	}
	return icons, nil
}

// readFile reads a servable build file and its precompressed .gz sibling. It
// returns nil for a file type the client does not serve.
func readFile(files fs.FS, name, cacheControl string) (*file, error) {
	contentType, ok := contentTypes[path.Ext(name)]
	if !ok {
		return nil, nil
	}
	data, err := fs.ReadFile(files, name)
	if err != nil {
		return nil, err
	}
	compressed, err := fs.ReadFile(files, name+".gz")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return newFile(data, contentType, cacheControl, compressed), nil
}

func newFile(data []byte, contentType, cacheControl string, compressed []byte) *file {
	sum := sha256.Sum256(data)
	return &file{data: data, gzip: compressed, contentType: contentType, cacheControl: cacheControl, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
}

// servableName reports whether name is one plain path segment: no
// separators, no leading dot, and only characters a build emits.
func servableName(name string) bool {
	if name == "" || len(name) > 128 || name[0] == '.' {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Match reports whether request is for the browser client: a GET or HEAD of
// one of its pages or one of the files its build contains. A request it does
// not match must be served exactly as if the client did not exist.
func (h *Handler) Match(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	if request.URL.RawPath != "" {
		return false
	}
	if isPage(request.URL.Path) {
		return true
	}
	_, ok := h.files[request.URL.Path]
	return ok
}

// isPage reports whether p is a path the client's HTML entry point answers:
// the root, the pairing link, or a session.
func isPage(p string) bool {
	if p == "/" || p == pairing.LinkPath {
		return true
	}
	id, ok := strings.CutPrefix(p, sessionsPath)
	return ok && servableName(id)
}

// ServeHTTP serves a request Match accepted, and 404 otherwise.
func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	header := writer.Header()
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	if !h.Match(request) {
		http.NotFound(writer, request)
		return
	}
	target := h.files[request.URL.Path]
	if isPage(request.URL.Path) {
		target = h.index
	}
	if target == nil {
		header.Set("Content-Type", contentTypes[".html"])
		header.Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusNotFound)
		if request.Method != http.MethodHead {
			_, _ = writer.Write([]byte(placeholder))
		}
		return
	}
	body, etag := target.data, target.etag
	if target.gzip != nil {
		header.Add("Vary", "Accept-Encoding")
		if acceptsGzip(request.Header.Values("Accept-Encoding")) {
			header.Set("Content-Encoding", "gzip")
			body = target.gzip
			etag = strings.TrimSuffix(etag, `"`) + `-gzip"`
		}
	}
	header.Set("Content-Type", target.contentType)
	header.Set("Cache-Control", target.cacheControl)
	header.Set("ETag", etag)
	http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(body))
}

// acceptsGzip reports whether Accept-Encoding header values allow gzip.
func acceptsGzip(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			coding, params, _ := strings.Cut(part, ";")
			if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
				continue
			}
			name, quality, ok := strings.Cut(strings.TrimSpace(params), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
				return true
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(quality), 64)
			return err == nil && q > 0
		}
	}
	return false
}
