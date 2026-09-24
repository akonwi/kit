package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

func TestReadBoundVCSRejectsWrongSessionIdentity(t *testing.T) {
	frame := `{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":null}` + "\n"
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
	frame := `{"sessionId":"` + id + `","cwd":"/repo","status":null}` + "\n"
	called := false
	if err := readBoundVCS(t.Context(), strings.NewReader(frame), id, func(value protocol.SessionVCSStatus) {
		called = value.SessionID == id && value.CWD == "/repo"
	}); err != nil || !called {
		t.Fatalf("delivery: called=%t err=%v", called, err)
	}
}

func TestClassifyVCSWatchErrorStopsOnlyTerminalOpenFailures(t *testing.T) {
	for _, err := range []error{
		daemon.ErrIncompatibleDaemon,
		&daemon.VCSFrameError{Err: errors.New("bad content type")},
		&daemon.APIError{StatusCode: http.StatusUnauthorized},
		&daemon.APIError{StatusCode: http.StatusNotFound},
	} {
		var terminal *sessionclient.VCSWatchTerminalError
		if !errors.As(classifyVCSWatchError(err), &terminal) {
			t.Fatalf("%v was not terminal", err)
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
	frame := `{"sessionId":"` + id + `","cwd":"/repo","status":null}` + "\n"
	if err := readBoundVCS(ctx, strings.NewReader(frame), id, func(protocol.SessionVCSStatus) {
		t.Fatal("canceled update delivered")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
