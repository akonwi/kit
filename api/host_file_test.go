package kit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

type hostFileTransport struct {
	sessionTransport
	err error
}

func (t hostFileTransport) ReadHostFile(context.Context, string, protocol.ReadHostFileInput) (protocol.HostFileRead, error) {
	return protocol.HostFileRead{}, t.err
}

func TestReadHostFileProjectsAnOlderServerAndWorkspaceErrors(t *testing.T) {
	session := newSession(nil, hostFileTransport{err: httpapi.ErrOperationNotServed}, "session_test", protocol.SessionSnapshot{})
	_, err := session.ReadHostFile(t.Context(), protocol.ReadHostFileInput{Path: "/etc/hosts"})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != "readHostFile" || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("older server error = %v", err)
	}

	declared := &clienttransport.APIError{StatusCode: http.StatusUnsupportedMediaType, Code: "binary_file", Message: "only text files and PNG, JPEG, GIF, or WebP images can be shown"}
	session = newSession(nil, hostFileTransport{err: declared}, "session_test", protocol.SessionSnapshot{})
	_, err = session.ReadHostFile(t.Context(), protocol.ReadHostFileInput{Path: "/bin/ls"})
	var workspaceErr *protocol.WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != protocol.WorkspaceErrorBinary {
		t.Fatalf("declared error = %v", err)
	}
}

func TestWorkspaceOperationsProjectDeclaredFailures(t *testing.T) {
	limit := httpapi.DecodeOperationError(httpapi.ReadHostFile, http.StatusRequestEntityTooLarge,
		[]byte(`{"error":{"code":"limit_exceeded","message":"the image is too large","details":{"limit":"image_pixels"}}}`))
	session := newSession(nil, hostFileTransport{err: limit}, "session_test", protocol.SessionSnapshot{})
	_, err := session.ReadHostFile(t.Context(), protocol.ReadHostFileInput{Path: "/tmp/huge.png"})
	var workspaceErr *protocol.WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != protocol.WorkspaceErrorLimit || workspaceErr.Details["limit"] != "image_pixels" {
		t.Fatalf("limit error = %#v", err)
	}

	for _, test := range []struct {
		status int
		body   string
		want   ServerError
	}{
		{http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"invalid request"}}`, ServerError{Code: ErrorInvalidRequest, Message: "invalid request"}},
		{http.StatusInternalServerError, `{"error":{"code":"internal","message":"internal server error"}}`, ServerError{Code: ErrorInternal, Message: "internal server error"}},
	} {
		declared := httpapi.DecodeOperationError(httpapi.ReadHostFile, test.status, []byte(test.body))
		session := newSession(nil, hostFileTransport{err: declared}, "session_test", protocol.SessionSnapshot{})
		_, err := session.ReadHostFile(t.Context(), protocol.ReadHostFileInput{Path: "/tmp/file"})
		var server *ServerError
		if !errors.As(err, &server) || server.Code != test.want.Code || server.Message != test.want.Message {
			t.Fatalf("HTTP %d error = %#v, want %+v", test.status, err, test.want)
		}
	}
}

type downloadTransport struct {
	sessionTransport
	err error
}

func (t downloadTransport) DownloadFile(context.Context, string, protocol.DownloadFileInput, protocol.DownloadConditions) (protocol.FileDownloadResponse, io.ReadCloser, error) {
	return protocol.FileDownloadResponse{}, nil, t.err
}

func (t downloadTransport) CheckFile(context.Context, string, protocol.DownloadFileInput) (protocol.DownloadedFileInfo, error) {
	return protocol.DownloadedFileInfo{}, t.err
}

func TestDownloadFileProjectsAnOlderServerAndWorkspaceErrors(t *testing.T) {
	session := newSession(nil, downloadTransport{err: httpapi.ErrOperationNotServed}, "session_test", protocol.SessionSnapshot{})
	input := protocol.DownloadFileInput{Path: "/etc/hosts"}
	_, _, err := session.DownloadFile(t.Context(), input, protocol.DownloadConditions{})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != "downloadFile" {
		t.Fatalf("older server download = %v", err)
	}
	if _, err := session.CheckFile(t.Context(), input); !errors.As(err, &unsupported) || unsupported.Capability != "downloadFile" {
		t.Fatalf("older server check = %v", err)
	}
	stale := httpapi.DecodeOperationError(httpapi.DownloadFile, http.StatusConflict,
		[]byte(`{"error":{"code":"stale_workspace","message":"the session workspace changed","details":{"currentWorkspaceId":"workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","currentWorkspaceState":"ready"}}}`))
	session = newSession(nil, downloadTransport{err: stale}, "session_test", protocol.SessionSnapshot{})
	var workspaceErr *protocol.WorkspaceError
	if _, _, err := session.DownloadFile(t.Context(), input, protocol.DownloadConditions{}); !errors.As(err, &workspaceErr) || workspaceErr.Code != protocol.WorkspaceErrorStaleWorkspace {
		t.Fatalf("stale workspace = %#v", err)
	}
}
