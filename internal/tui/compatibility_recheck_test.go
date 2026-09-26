package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

type serverWithoutCompatibilityProbe struct{ sessionclient.Server }

func TestProbeAndReattachChecksCompatibilityBeforeBinding(t *testing.T) {
	t.Parallel()
	var probes, attachments int
	session := fakeSession{id: "session_test", snapshot: protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_test", CWD: "/workspace"}}}
	server := &fakeServer{
		probe: func(context.Context) error {
			probes++
			return &kitserver.DaemonCompatibilityError{Reason: kitserver.ClientProtocolOlder}
		},
		attach: func(string) (sessionclient.Session, error) {
			attachments++
			return session, nil
		},
	}
	if _, _, _, err := probeAndReattach(t.Context(), server, "session_test", nil); !errors.Is(err, kitserver.ErrIncompatibleDaemon) {
		t.Fatalf("reprobe error = %v, want incompatibility", err)
	}
	if probes != 1 || attachments != 0 {
		t.Fatalf("mismatched daemon: probes=%d attachments=%d, want 1 and 0", probes, attachments)
	}
	if _, _, _, err := probeAndReattach(t.Context(), serverWithoutCompatibilityProbe{server}, "session_test", nil); err == nil {
		t.Fatal("reattached without a compatibility probe")
	}
	if attachments != 0 {
		t.Fatalf("unsupported probe still attached %d times", attachments)
	}
	server.probe = func(context.Context) error { probes++; return nil }
	bound, snapshot, location, err := probeAndReattach(t.Context(), server, "session_test", nil)
	if err != nil || bound.ID() != "session_test" || snapshot.Session.ID != "session_test" || location != "/workspace" || probes != 2 || attachments != 1 {
		t.Fatalf("fresh attachment = %v, %+v, %q, %v (probes=%d attachments=%d)", bound, snapshot.Session, location, err, probes, attachments)
	}
}

func TestQueuedRunUpdateCannotChangeFrozenTranscript(t *testing.T) {
	application, state, session := mountRunAbort(t)
	state.attachmentCtx, state.attachmentCancel = context.WithCancel(state.ctx)
	updates := make(chan []protocol.SessionEvent, 1)
	session.stream = func(string) (sessionclient.EventStream, error) { return abortTestStream{updates: updates}, nil }
	state.watchSession(session, state.operation, "run_abort")
	select {
	case <-session.streams:
	case <-time.After(3 * time.Second):
		t.Fatal("run watcher did not connect")
	}
	updates <- []protocol.SessionEvent{{Sequence: 5, Kind: protocol.SessionEventUsageUpdated}}
	stale := receiveAbortCompletion(t, state)
	state.reportDaemonMismatch(state.Context().Runtime(), session, state.operation, &kitserver.APIError{StatusCode: 426, Message: "upgrade required"})
	freeze := receiveAbortCompletion(t, state)
	freeze()
	stale()
	application.Pump(120, 36)
	if state.liveSequence != 4 || state.recovery != footerDaemonIncompatible || state.attachmentCtx.Err() != context.Canceled {
		t.Fatalf("queued watcher update changed frozen attachment: cursor=%d recovery=%v context=%v", state.liveSequence, state.recovery, state.attachmentCtx.Err())
	}
}

func TestDaemonRecheckFailureKeepsTranscriptAndDraft(t *testing.T) {
	application, state, original := mountRunAbort(t)
	state.attachmentCtx, state.attachmentCancel = context.WithCancel(state.ctx)
	state.composer = "unsent draft"
	state.reportDaemonMismatch(state.Context().Runtime(), original, state.operation, &kitserver.APIError{StatusCode: 426, Message: "upgrade required"})
	receiveAbortCompletion(t, state)()
	application.Pump(120, 36)
	if state.attachmentCtx.Err() != context.Canceled {
		t.Fatalf("incompatible attachment still running: %v", state.attachmentCtx.Err())
	}
	for _, tc := range []struct {
		name   string
		server *fakeServer
	}{
		{"still incompatible", &fakeServer{probe: func(context.Context) error {
			return &kitserver.DaemonCompatibilityError{Reason: kitserver.ClientProtocolOlder}
		}, attach: func(string) (sessionclient.Session, error) { t.Fatal("attached incompatible daemon"); return nil, nil }}},
		{"session unavailable", &fakeServer{attachErr: errors.New("session not found")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state.recheckDaemonWith(tc.server, nil)
			if state.recovery != footerCheckingDaemon {
				t.Fatalf("recheck footer = %v, want checking", state.recovery)
			}
			receiveAbortCompletion(t, state)()
			application.Pump(120, 36)
			if state.bound != original || !state.daemonIncompatible || state.daemonRechecking || state.recovery != footerDaemonIncompatible || state.composer != "unsent draft" {
				t.Fatalf("failed recheck changed attachment: bound=%T incompatible=%t checking=%t recovery=%v draft=%q", state.bound, state.daemonIncompatible, state.daemonRechecking, state.recovery, state.composer)
			}
			if len(state.messages) != 1 || state.messages[0].Text != "Retained evidence" || len(state.liveMessages) != 1 || state.liveMessages[0].Text != "Current prompt" {
				t.Fatalf("failed recheck dropped transcript: messages=%+v live=%+v", state.messages, state.liveMessages)
			}
		})
	}
}
