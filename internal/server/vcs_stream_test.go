package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/session"
)

type vcsFrameSource struct {
	value        protocol.SessionVCSStatus
	sent, closed bool
}

func (s *vcsFrameSource) Next(context.Context) (protocol.SessionVCSStatus, error) {
	if s.sent {
		return protocol.SessionVCSStatus{}, io.EOF
	}
	s.sent = true
	return s.value, nil
}
func (s *vcsFrameSource) Close() { s.closed = true }

type vcsStreamTestService struct {
	sessionService
	source vcsSource
}

func (s vcsStreamTestService) SubscribeVCS(context.Context, string) (vcsSource, error) {
	return s.source, nil
}
func serveVCSStreamForTest(source vcsSource) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	registerVCSRoutes(mux, httpapi.ServeOptions{WriteError: writeSessionError}, vcsStreamTestService{source: source})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/v1/sessions/session_0123456789abcdef0123456789abcdef/vcs/events", nil))
	return response
}

func TestVCSStreamKeepsValidEscapedPathsWithinRecordBound(t *testing.T) {
	value := protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/" + strings.Repeat("<", 4095), Status: &protocol.VCSStatus{Root: "/" + strings.Repeat("<", 4095), Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: strings.Repeat("<", 4096)}}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	source := &vcsFrameSource{value: value}
	response := serveVCSStreamForTest(source)
	if !source.closed || response.Body.Len() > 64<<10 || response.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("closed=%v bytes=%d headers=%v", source.closed, response.Body.Len(), response.Header())
	}
	encoded, _ := json.Marshal(value)
	want := ": connected\n\nevent: vcs.status\ndata: " + strings.ReplaceAll(string(encoded), `\u003c`, "<") + "\n\n"
	if response.Body.String() != want {
		t.Fatalf("body = %.200q", response.Body.String())
	}
	var got protocol.SessionVCSStatus
	if err := ReadSessionVCS(response.Body, func(value protocol.SessionVCSStatus) error { got = value; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.CWD != value.CWD || got.Status == nil || got.Status.Root != value.Status.Root || got.Status.Head.Name != value.Status.Head.Name {
		t.Fatal("stream dropped valid bounded metadata")
	}
}

func TestVCSStreamEndsInsteadOfSendingInvalidStatus(t *testing.T) {
	source := &vcsFrameSource{value: protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "relative"}}
	response := serveVCSStreamForTest(source)
	if !source.closed || response.Code != http.StatusOK || response.Body.String() != ": connected\n\n" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestProjectVCSStatusKeepsCWDWhenDroppingStatus(t *testing.T) {
	value := projectVCSStatus("session_0123456789abcdef0123456789abcdef", session.VCSUpdate{CWD: "/repo", Status: &session.VCSStatus{Root: "relative", HeadKind: "branch", HeadName: "main"}})
	if value.CWD != "/repo" || value.Status != nil || value.Validate() != nil {
		t.Fatalf("projection = %+v", value)
	}
}
