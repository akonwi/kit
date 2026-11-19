package clienttransport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

// DownloadFile opens the content of a workspace or host file (ADR 0074). The
// body is nil when the response is not modified. Otherwise the caller closes
// it; a body that ends before the declared length fails with a transport
// error.
func (c *Client) DownloadFile(ctx context.Context, sessionID string, input protocol.DownloadFileInput, conditions protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error) {
	if err := input.Validate(); err != nil {
		return protocol.FileDownloadResponse{}, nil, fmt.Errorf("validate download request: %w", err)
	}
	if err := conditions.Validate(); err != nil {
		return protocol.FileDownloadResponse{}, nil, err
	}
	headers := http.Header{}
	if conditions.CachedRevision != "" {
		headers.Set("If-None-Match", `"`+conditions.CachedRevision+`"`)
	}
	if conditions.ResumeRevision != "" {
		headers.Set("Range", "bytes="+strconv.FormatInt(conditions.ResumeOffset, 10)+"-")
		headers.Set("If-Range", `"`+conditions.ResumeRevision+`"`)
	}
	ctx, cleanup := c.operationContext(ctx)
	keepContext := false
	defer func() {
		if !keepContext {
			cleanup()
		}
	}()
	response, err := c.sendFileRequest(ctx, http.MethodGet, sessionID, input, headers)
	if err != nil {
		return protocol.FileDownloadResponse{}, nil, err
	}
	closeBody := func() { _ = response.Body.Close() }
	result := protocol.FileDownloadResponse{}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusPartialContent:
		if conditions.ResumeRevision == "" {
			closeBody()
			return protocol.FileDownloadResponse{}, nil, protocolErrorf("daemon sent a range that wasn't requested")
		}
	case http.StatusNotModified:
		closeBody()
		if conditions.CachedRevision == "" {
			return protocol.FileDownloadResponse{}, nil, protocolErrorf("daemon sent not modified unrequested")
		}
		result.NotModified = true
	default:
		closeBody()
		return protocol.FileDownloadResponse{}, nil, protocolErrorf("daemon download status %d is not declared", response.StatusCode)
	}
	info, offset, err := downloadedFileInfo(response, sessionID, input)
	if err != nil {
		closeBody()
		return protocol.FileDownloadResponse{}, nil, err
	}
	if result.NotModified {
		if info.Revision != conditions.CachedRevision {
			return protocol.FileDownloadResponse{}, nil, protocolErrorf("daemon not-modified revision mismatch")
		}
		result.Info = info
		return result, nil, nil
	}
	if response.StatusCode == http.StatusPartialContent && (offset != conditions.ResumeOffset || info.Revision != conditions.ResumeRevision) {
		closeBody()
		return protocol.FileDownloadResponse{}, nil, protocolErrorf("daemon resumed a different range or revision")
	}
	result.Info, result.Offset = info, offset
	keepContext = true
	return result, &lifetimeReadCloser{ReadCloser: response.Body, operation: "download file", cleanup: cleanup}, nil
}

// CheckFile returns a file's current download metadata without its content.
func (c *Client) CheckFile(ctx context.Context, sessionID string, input protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error) {
	if err := input.Validate(); err != nil {
		return protocol.DownloadedFileInfo{}, fmt.Errorf("validate download request: %w", err)
	}
	ctx, cleanup := c.operationContext(ctx)
	defer cleanup()
	response, err := c.sendFileRequest(ctx, http.MethodHead, sessionID, input, nil)
	if err != nil {
		return protocol.DownloadedFileInfo{}, err
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return protocol.DownloadedFileInfo{}, protocolErrorf("daemon check status %d is not declared", response.StatusCode)
	}
	info, _, err := downloadedFileInfo(response, sessionID, input)
	return info, err
}

// sendFileRequest sends a download or check request and decodes a declared
// failure. A response it returns has a success or conditional status.
func (c *Client) sendFileRequest(ctx context.Context, method, sessionID string, input protocol.DownloadFileInput, headers http.Header) (*http.Response, error) {
	connection, err := c.connection(ctx)
	if err != nil {
		return nil, err
	}
	if err := CheckCompatibility(connection.ProtocolVersion, connection.KitVersion); err != nil {
		return nil, err
	}
	query := url.Values{"path": {input.Path}}
	if input.HasWorkspace {
		query.Set("workspaceId", input.WorkspaceID)
	}
	target := connection.URL + "/v1/sessions/" + url.PathEscape(sessionID) + "/files/content?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	for key, values := range headers {
		request.Header[key] = values
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(instanceHeader, connection.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return nil, transportError("download file", err)
	}
	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
		return response, nil
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if rejectedInstance(response.StatusCode, body) {
		c.instanceRejected(connection)
	}
	if response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// The client sends no If-Match, and resumes only within the file
		// it already has.
		return nil, protocolErrorf("daemon download status %d answers no condition the client sent", response.StatusCode)
	}
	return nil, httpapi.DecodeOperationError(httpapi.DownloadFile, response.StatusCode, body)
}

// downloadedFileInfo reads a response's download metadata and the offset
// its body starts at, and checks them against the request.
func downloadedFileInfo(response *http.Response, sessionID string, input protocol.DownloadFileInput) (protocol.DownloadedFileInfo, int64, error) {
	header := response.Header
	path, pathErr := url.PathUnescape(header.Get(httpapi.FilePathHeader))
	etag := header.Get("ETag")
	revision := strings.TrimSuffix(strings.TrimPrefix(etag, `"`), `"`)
	if etag != "" && etag != `"`+revision+`"` {
		return protocol.DownloadedFileInfo{}, 0, protocolErrorf("daemon download ETag is malformed")
	}
	info := protocol.DownloadedFileInfo{
		SessionID: header.Get(httpapi.SessionIDHeader), WorkspaceID: header.Get(httpapi.FileWorkspaceHeader),
		Path: path, Revision: revision, Size: -1, ModifiedAt: header.Get(httpapi.FileModifiedAtHeader),
	}
	var offset int64
	switch {
	case response.StatusCode == http.StatusPartialContent:
		start, size, ok := parseContentRange(header.Get("Content-Range"), response.ContentLength)
		if !ok {
			return protocol.DownloadedFileInfo{}, 0, protocolErrorf("daemon download range is malformed")
		}
		offset, info.Size = start, size
	case response.StatusCode == http.StatusOK && revision != "":
		info.Size = response.ContentLength
	}
	if pathErr != nil || info.Validate() != nil {
		return protocol.DownloadedFileInfo{}, 0, protocolErrorf("daemon download metadata is invalid")
	}
	matchesPath := info.Path == input.Path
	if !input.HasWorkspace {
		// Only the server can expand "~".
		matchesPath = strings.HasPrefix(input.Path, "~") || info.Path == filepath.Clean(input.Path)
	}
	if info.SessionID != sessionID || info.WorkspaceID != input.WorkspaceID || !matchesPath {
		return protocol.DownloadedFileInfo{}, 0, protocolErrorf("daemon download identity mismatch")
	}
	return info, offset, nil
}

// parseContentRange reads "bytes start-end/size" and checks it against the
// body length.
func parseContentRange(value string, length int64) (start, size int64, ok bool) {
	spec, found := strings.CutPrefix(value, "bytes ")
	if !found {
		return 0, 0, false
	}
	span, total, found := strings.Cut(spec, "/")
	first, last, found2 := strings.Cut(span, "-")
	if !found || !found2 {
		return 0, 0, false
	}
	start, err1 := strconv.ParseInt(first, 10, 64)
	end, err2 := strconv.ParseInt(last, 10, 64)
	size, err3 := strconv.ParseInt(total, 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || start < 0 || end < start || end >= size || length != end-start+1 {
		return 0, 0, false
	}
	return start, size, true
}
