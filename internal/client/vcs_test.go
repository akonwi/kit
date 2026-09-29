package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

func TestReadBoundVCSRejectsWrongSessionIdentity(t *testing.T) {
	frame := "event: vcs.status\ndata: " + `{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo"}` + "\n\n"
	err := readBoundVCS(t.Context(), strings.NewReader(frame), "session_ffffffffffffffffffffffffffffffff", func(protocol.SessionVCSStatus) {
		t.Fatal("wrong-session update delivered")
	})
	var terminal *sessionclient.VCSWatchTerminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("identity error = %v, want terminal error", err)
	}
}

func TestReadBoundVCSDeliversValidatedUpdate(t *testing.T) {
	const id = "session_0123456789abcdef0123456789abcdef"
	frame := "event: vcs.status\ndata: " + `{"sessionId":"` + id + `","cwd":"/repo"}` + "\n\n"
	called := false
	if err := readBoundVCS(t.Context(), strings.NewReader(frame), id, func(value protocol.SessionVCSStatus) {
		called = value.SessionID == id && value.CWD == "/repo"
	}); err != nil || !called {
		t.Fatalf("delivery: called=%t err=%v", called, err)
	}
}

func TestClassifyVCSWatchErrorStopsOnlyTerminalOpenFailures(t *testing.T) {
	for _, err := range []error{
		kitserver.ErrIncompatibleDaemon,
		&kitserver.StreamError{Err: errors.New("bad content type")},
		&kitserver.APIError{StatusCode: http.StatusUnauthorized},
		&kitserver.APIError{StatusCode: http.StatusNotFound},
		&kitserver.APIError{StatusCode: http.StatusUpgradeRequired, Code: "protocol_mismatch"},
	} {
		var terminal *sessionclient.VCSWatchTerminalError
		if !errors.As(classifyVCSWatchError(err), &terminal) {
			t.Fatalf("%v was not terminal", err)
		}
	}
	for _, err := range []error{
		&kitserver.APIError{StatusCode: http.StatusTooManyRequests, Code: "capacity_exceeded"},
		&kitserver.APIError{StatusCode: http.StatusServiceUnavailable, Code: "unavailable"},
		&kitserver.APIError{StatusCode: http.StatusConflict, Code: "conflict"},
	} {
		var terminal *sessionclient.VCSWatchTerminalError
		if errors.As(classifyVCSWatchError(err), &terminal) {
			t.Fatalf("%v must reconnect", err)
		}
	}
	transient := errors.New("connection reset")
	if got := classifyVCSWatchError(transient); !errors.Is(got, transient) {
		t.Fatalf("transient error changed: %v", got)
	} else {
		var terminal *sessionclient.VCSWatchTerminalError
		if errors.As(got, &terminal) {
			t.Fatal("transient error classified terminal")
		}
	}
}

func TestReadBoundVCSHonorsCanceledAttachment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const id = "session_0123456789abcdef0123456789abcdef"
	frame := "event: vcs.status\ndata: " + `{"sessionId":"` + id + `","cwd":"/repo"}` + "\n\n"
	if err := readBoundVCS(ctx, strings.NewReader(frame), id, func(protocol.SessionVCSStatus) {
		t.Fatal("canceled update delivered")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestReadBoundVCSClassifiesProtocolViolationsAsStreamErrors(t *testing.T) {
	const id = "session_0123456789abcdef0123456789abcdef"
	for _, body := range []string{
		"event: vcs.status\ndata: {\"sessionId\":\"" + id + "\",\"cwd\":\"/repo\"}\n", // truncated record
		"event: vcs.other\ndata: {\"sessionId\":\"" + id + "\",\"cwd\":\"/repo\"}\n\n",
		"event: vcs.status\ndata: {\"sessionId\":\"" + id + "\",\"cwd\":\"" + strings.Repeat("a", 70_000) + "\"}\n\n",
	} {
		err := readBoundVCS(t.Context(), strings.NewReader(body), id, func(protocol.SessionVCSStatus) { t.Fatal("violation delivered") })
		var violation *kitserver.StreamError
		if !errors.As(err, &violation) {
			t.Fatalf("violation = %v", err)
		}
	}
}
