package kit

import (
	"context"
	"errors"
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
