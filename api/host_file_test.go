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
