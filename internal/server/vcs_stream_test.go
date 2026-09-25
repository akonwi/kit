package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
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
func TestVCSStreamKeepsValidEscapedPathsWithinFrameBound(t *testing.T) {
	value := protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/" + strings.Repeat("<", 4095), Status: &protocol.VCSStatus{Root: "/" + strings.Repeat("<", 4095), Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: strings.Repeat("<", 4096)}}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	source := &vcsFrameSource{value: value}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/vcs/events", nil)
	serveVCS(response, request, vcsStreamTestService{source: source})
	if !source.closed || response.Body.Len() > 64<<10 || response.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("closed=%v bytes=%d headers=%v", source.closed, response.Body.Len(), response.Header())
	}
	var got protocol.SessionVCSStatus
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CWD != value.CWD || got.Status == nil || got.Status.Root != value.Status.Root || got.Status.Head.Name != value.Status.Head.Name {
		t.Fatal("stream dropped valid bounded metadata")
	}
}
