package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

// downloadWriteTimeout bounds each write of a download body, as the stream
// write timeout of ADR 0035 bounds each stream write.
var downloadWriteTimeout = 10 * time.Second

// downloadBlockBytes is the size of each read and write of a download body.
const downloadBlockBytes = 64 << 10

// fileContentSecurityPolicy stops a browser that opens a download directly
// from running it with the server's origin (ADR 0074).
const fileContentSecurityPolicy = "sandbox; default-src 'none'"

// OpenFileDownload opens a workspace or host file to download (ADR 0074).
// For a workspace file, recheck fails with stale_workspace when the session's
// cwd has changed since the file was opened; the caller calls it immediately
// before sending headers.
func (s runtimeSessionService) OpenFileDownload(ctx context.Context, sessionID string, input protocol.DownloadFileInput) (*kitworkspace.Download, func(context.Context) error, error) {
	workspaces, err := s.workspaceService()
	if err != nil {
		return nil, nil, err
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if !input.HasWorkspace {
		download, err := workspaces.OpenHostDownload(ctx, sessionID, input)
		return download, func(context.Context) error { return nil }, err
	}
	download, err := workspaces.OpenWorkspaceDownload(ctx, sessionID, record.CWD, input)
	if err != nil {
		return nil, nil, err
	}
	recheck := func(ctx context.Context) error {
		current, err := s.manager.Get(ctx, sessionID)
		if err != nil {
			return err
		}
		if current.CWD != record.CWD {
			return staleWorkspaceError(workspaces.Ref(sessionID, current.CWD))
		}
		return nil
	}
	return download, recheck, nil
}

// registerFileDownloadRoutes serves downloadFile and checkFile. Open
// downloads end when streamShutdown is done.
func registerFileDownloadRoutes(mux *http.ServeMux, service sessionService, streamShutdown context.Context) {
	handler := func(writer http.ResponseWriter, request *http.Request) {
		serveFileDownload(writer, request, service, streamShutdown)
	}
	httpapi.HandleRaw(mux, httpapi.DownloadFile, handler)
	httpapi.HandleRaw(mux, httpapi.CheckFile, handler)
}

func serveFileDownload(writer http.ResponseWriter, request *http.Request, service sessionService, shutdown context.Context) {
	sessionID := request.PathValue("sessionID")
	input, ok := parseDownloadQuery(request.URL.Query())
	if !ok {
		writeSessionError(writer, invalidAPIError())
		return
	}
	download, recheck, err := service.OpenFileDownload(request.Context(), sessionID, input)
	if err != nil {
		writeSessionError(writer, hostFileAPIError(err))
		return
	}
	defer download.Close()
	if shutdown != nil {
		stop := context.AfterFunc(shutdown, download.Close)
		defer stop()
	}
	info := download.Info
	if err := info.Validate(); err != nil {
		writeSessionError(writer, internalAPIErrorCausedBy(fmt.Errorf("invalid download metadata: %w", err)))
		return
	}
	plan := planDownload(request, info)
	if plan.status == http.StatusOK || plan.status == http.StatusPartialContent {
		if request.Method == http.MethodGet {
			if err := download.StartBody(); err != nil {
				writeSessionError(writer, err)
				return
			}
		}
	}
	if err := recheck(request.Context()); err != nil {
		writeSessionError(writer, hostFileAPIError(err))
		return
	}
	writeDownloadHeaders(writer.Header(), info, plan)
	writer.WriteHeader(plan.status)
	if request.Method != http.MethodGet || plan.status != http.StatusOK && plan.status != http.StatusPartialContent {
		return
	}
	copyDownload(request.Context(), writer, download.Section(plan.start, plan.length), plan.length)
}

// parseDownloadQuery reads the download's query. A parameter counts as
// present even when empty, and a repeated one is malformed.
func parseDownloadQuery(query url.Values) (protocol.DownloadFileInput, bool) {
	paths, hasPath := query["path"]
	workspaces, hasWorkspace := query["workspaceId"]
	if !hasPath || len(paths) != 1 || hasWorkspace && len(workspaces) != 1 {
		return protocol.DownloadFileInput{}, false
	}
	input := protocol.DownloadFileInput{HasWorkspace: hasWorkspace, Path: paths[0]}
	if hasWorkspace {
		if workspaces[0] == "" {
			return protocol.DownloadFileInput{}, false
		}
		input.WorkspaceID = workspaces[0]
	}
	return input, true
}

// downloadPlan is the response a download request gets.
type downloadPlan struct {
	status int
	// start and length are the byte range of a 200 or 206 body, for a
	// known-length file.
	start, length int64
}

// planDownload evaluates the request's preconditions and range against the
// opened file, as ADR 0074 describes. Only one range is served; a request
// for several gets the whole file.
func planDownload(request *http.Request, info protocol.DownloadedFileInfo) downloadPlan {
	whole := downloadPlan{status: http.StatusOK, length: info.Size}
	if !info.KnownLength() {
		if request.Header.Get("If-Match") != "" {
			return downloadPlan{status: http.StatusPreconditionFailed}
		}
		return downloadPlan{status: http.StatusOK, length: -1}
	}
	etag := `"` + info.Revision + `"`
	if match := request.Header.Values("If-Match"); len(match) > 0 && !etagListMatches(match, etag) {
		return downloadPlan{status: http.StatusPreconditionFailed}
	}
	if none := request.Header.Values("If-None-Match"); len(none) > 0 && etagListMatches(none, etag) {
		return downloadPlan{status: http.StatusNotModified}
	}
	ranges := request.Header.Values("Range")
	if request.Method != http.MethodGet || len(ranges) == 0 {
		return whole
	}
	if ifRange := request.Header.Get("If-Range"); ifRange != "" && ifRange != etag {
		return whole
	}
	if len(ranges) != 1 {
		return whole
	}
	start, length, ok, satisfiable := parseSingleRange(ranges[0], info.Size)
	if !ok {
		return whole
	}
	if !satisfiable {
		return downloadPlan{status: http.StatusRequestedRangeNotSatisfiable}
	}
	return downloadPlan{status: http.StatusPartialContent, start: start, length: length}
}

// etagListMatches reports whether a strong comparison of etag matches one of
// the listed validators, or the list is "*". Weak validators never match.
func etagListMatches(values []string, etag string) bool {
	for _, value := range values {
		for _, candidate := range strings.Split(value, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || candidate == etag {
				return true
			}
		}
	}
	return false
}

// parseSingleRange parses one "bytes=" range. ok is false for a malformed
// or multiple range, which the server ignores. satisfiable is false for a
// range that starts past the end of the file.
func parseSingleRange(value string, size int64) (start, length int64, ok, satisfiable bool) {
	spec, found := strings.CutPrefix(value, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false, false
	}
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix < 0 {
			return 0, 0, false, false
		}
		if suffix == 0 {
			return 0, 0, true, false
		}
		suffix = min(suffix, size)
		return size - suffix, suffix, true, true
	}
	begin, err := strconv.ParseInt(first, 10, 64)
	if err != nil || begin < 0 {
		return 0, 0, false, false
	}
	end := size - 1
	if last != "" {
		end, err = strconv.ParseInt(last, 10, 64)
		if err != nil || end < begin {
			return 0, 0, false, false
		}
		end = min(end, size-1)
	}
	if begin >= size {
		return 0, 0, true, false
	}
	return begin, end - begin + 1, true, true
}

func writeDownloadHeaders(header http.Header, info protocol.DownloadedFileInfo, plan downloadPlan) {
	header.Set(httpapi.SessionIDHeader, info.SessionID)
	if info.WorkspaceID != "" {
		header.Set(httpapi.FileWorkspaceHeader, info.WorkspaceID)
	}
	header.Set(httpapi.FilePathHeader, (&url.URL{Path: info.Path}).EscapedPath())
	header.Set(httpapi.FileModifiedAtHeader, info.ModifiedAt)
	if info.KnownLength() {
		header.Set("ETag", `"`+info.Revision+`"`)
		header.Set("Cache-Control", "private, no-cache")
	} else {
		header.Set("Cache-Control", "no-store")
	}
	switch plan.status {
	case http.StatusNotModified, http.StatusPreconditionFailed:
		return
	case http.StatusRequestedRangeNotSatisfiable:
		header.Set("Content-Range", "bytes */"+strconv.FormatInt(info.Size, 10))
		header.Set("Content-Length", "0")
		return
	}
	if info.KnownLength() {
		header.Set("Accept-Ranges", "bytes")
		header.Set("Content-Length", strconv.FormatInt(plan.length, 10))
	} else {
		header.Set("Accept-Ranges", "none")
	}
	if plan.status == http.StatusPartialContent {
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", plan.start, plan.start+plan.length-1, info.Size))
	}
	header.Set("Content-Type", "application/octet-stream")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", fileContentSecurityPolicy)
	header.Set("Content-Disposition", attachmentDisposition(path.Base(info.Path)))
}

// attachmentDisposition names a download with an RFC 6266 ASCII filename and
// an RFC 5987 UTF-8 filename*.
func attachmentDisposition(name string) string {
	var fallback strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '%' {
			fallback.WriteByte('_')
			continue
		}
		fallback.WriteRune(r)
	}
	return `attachment; filename="` + fallback.String() + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// copyDownload copies a body of length bytes, or to end of file when length
// is negative, in bounded blocks. Every write gets a fresh deadline, so a
// client that stops reading loses its download rather than holding a slot.
// The copy stops when ctx ends or a read or write fails. A body that ends
// early, as when the file is truncated or a read fails, aborts the response,
// so the client sees a failed transfer rather than a short file.
func copyDownload(ctx context.Context, writer http.ResponseWriter, body io.Reader, length int64) {
	controller := http.NewResponseController(writer)
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	buffer := make([]byte, downloadBlockBytes)
	var written int64
	for ctx.Err() == nil {
		n, err := body.Read(buffer)
		if n > 0 {
			_ = controller.SetWriteDeadline(time.Now().Add(downloadWriteTimeout))
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				return
			}
			written += int64(n)
		}
		if errors.Is(err, io.EOF) && (length < 0 || written == length) {
			return
		}
		if err != nil {
			break
		}
	}
	panic(http.ErrAbortHandler)
}
