package clienttransport

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

const (
	downloadSession   = "session_0123456789abcdef0123456789abcdef"
	downloadWorkspace = "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	downloadRevision  = "file_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	downloadContent   = "0123456789"
)

// downloadServer serves notes.txt in one workspace as ADR 0074 describes,
// recording each request.
func downloadServer(t *testing.T, requests *[]*http.Request) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		*requests = append(*requests, request.Clone(request.Context()))
		query := request.URL.Query()
		if request.URL.Path != "/v1/sessions/"+downloadSession+"/files/content" || query.Get("workspaceId") != downloadWorkspace || query.Get("path") != "notes.txt" ||
			request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get(httpapi.ProtocolHeader) != strconv.Itoa(version.SessionProtocolVersion) {
			http.Error(response, "unexpected request", http.StatusTeapot)
			return
		}
		header := response.Header()
		header.Set("ETag", `"`+downloadRevision+`"`)
		header.Set(httpapi.SessionIDHeader, downloadSession)
		header.Set(httpapi.FileWorkspaceHeader, downloadWorkspace)
		header.Set(httpapi.FilePathHeader, "notes.txt")
		header.Set(httpapi.FileModifiedAtHeader, "2026-11-18T12:00:00.123456789Z")
		if request.Header.Get("If-None-Match") == `"`+downloadRevision+`"` {
			response.WriteHeader(http.StatusNotModified)
			return
		}
		if value := request.Header.Get("Range"); value != "" && request.Header.Get("If-Range") == `"`+downloadRevision+`"` {
			start, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-"))
			header.Set("Content-Range", "bytes "+strconv.Itoa(start)+"-9/10")
			header.Set("Content-Length", strconv.Itoa(10-start))
			response.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(response, downloadContent[start:])
			return
		}
		header.Set("Content-Length", "10")
		_, _ = io.WriteString(response, downloadContent)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDownloadFileReadsMetadataRevalidatesAndResumes(t *testing.T) {
	var requests []*http.Request
	client := fixedEndpoint(downloadServer(t, &requests).URL, "secret", "instance_test")
	input := protocol.DownloadFileInput{HasWorkspace: true, WorkspaceID: downloadWorkspace, Path: "notes.txt"}
	want := protocol.DownloadedFileInfo{SessionID: downloadSession, WorkspaceID: downloadWorkspace, Path: "notes.txt", Revision: downloadRevision, Size: 10, ModifiedAt: "2026-11-18T12:00:00.123456789Z"}

	response, body, err := client.DownloadFile(t.Context(), downloadSession, input, protocol.DownloadConditions{})
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(content) != downloadContent || response != (protocol.FileDownloadResponse{Info: want}) {
		t.Fatalf("download = %+v %q %v", response, content, err)
	}

	response, body, err = client.DownloadFile(t.Context(), downloadSession, input, protocol.DownloadConditions{CachedRevision: downloadRevision})
	notModified := want
	notModified.Size = -1
	if err != nil || body != nil || response != (protocol.FileDownloadResponse{Info: notModified, NotModified: true}) {
		t.Fatalf("revalidation = %+v %v %v", response, body, err)
	}

	response, body, err = client.DownloadFile(t.Context(), downloadSession, input, protocol.DownloadConditions{ResumeRevision: downloadRevision, ResumeOffset: 6})
	if err != nil {
		t.Fatal(err)
	}
	content, err = io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(content) != "6789" || response != (protocol.FileDownloadResponse{Info: want, Offset: 6}) {
		t.Fatalf("resume = %+v %q %v", response, content, err)
	}
	if got := requests[2].Header; got.Get("Range") != "bytes=6-" || got.Get("If-Range") != `"`+downloadRevision+`"` {
		t.Fatalf("resume request headers = %v", got)
	}
}

func TestDownloadFileReportsAnOlderServerAsNotServed(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := fixedEndpoint(server.URL, "secret", "instance_test")
	_, _, err := client.DownloadFile(t.Context(), downloadSession, protocol.DownloadFileInput{Path: "/etc/hosts"}, protocol.DownloadConditions{})
	if !errors.Is(err, httpapi.ErrOperationNotServed) {
		t.Fatalf("older server = %v, want ErrOperationNotServed", err)
	}
}

func TestDownloadFileRejectsMismatchedIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set(httpapi.SessionIDHeader, downloadSession)
		response.Header().Set(httpapi.FilePathHeader, "/etc/passwd")
		response.Header().Set(httpapi.FileModifiedAtHeader, "2026-11-18T12:00:00Z")
		response.Header().Set("ETag", `"hostfile_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"`)
		_, _ = io.WriteString(response, "root")
	}))
	defer server.Close()
	client := fixedEndpoint(server.URL, "secret", "instance_test")
	_, _, err := client.DownloadFile(t.Context(), downloadSession, protocol.DownloadFileInput{Path: "/etc/hosts"}, protocol.DownloadConditions{})
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("mismatched path = %v, want a protocol error", err)
	}
}
