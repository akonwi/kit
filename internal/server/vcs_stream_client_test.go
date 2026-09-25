package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestSessionVCSWireReaderDeliversFramesAndSkipsHeartbeats(t *testing.T) {
	t.Parallel()
	loading := protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"}
	ready := protocol.SessionVCSStatus{
		SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo",
		Status: &protocol.VCSStatus{
			Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}, Dirty: true,
			PullRequest: &protocol.GitHubPullRequest{Number: 42, URL: "https://github.com/akonwi/kit/pull/42"},
		},
	}
	first, _ := json.Marshal(loading)
	second, _ := json.Marshal(ready)
	// Initial latest snapshot (status nil while loading), heartbeat blanks,
	// then the deduplicated update.
	body := "\n" + string(first) + "\n\n\n" + string(second) + "\n"
	var received []protocol.SessionVCSStatus
	if err := ReadSessionVCS(strings.NewReader(body), func(status protocol.SessionVCSStatus) error {
		received = append(received, status)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0].Status != nil || received[1].Status == nil ||
		received[1].Status.PullRequest == nil || received[1].Status.PullRequest.Number != 42 {
		t.Fatalf("decoded = %#v", received)
	}
}

func TestSessionVCSWireReaderRejectsInvalidFramesAsProtocolViolations(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"})
	frames := []string{
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"}}}` + "\n",
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":null}}` + "\n",
		strings.Repeat(`{"nested":`, 10) + `null` + strings.Repeat(`}`, 10) + "\n",

		`{}` + "\n", // missing identity
		`{"unknown":true}` + "\n",
		string(valid) + ` {}` + "\n", // trailing garbage
		strings.Replace(string(valid), "repo", "\xff", 1) + "\n",                                                                                                                                                              // invalid UTF-8
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","sessionId":"session_other","cwd":"/repo","status":null}` + "\n",                                                                                             // duplicate top-level key
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","root":"/other","head":{"kind":"branch","name":"main"},"dirty":false}}` + "\n",                                        // duplicate nested key
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"relative","head":{"kind":"branch","name":"main"},"dirty":false}}` + "\n",                                                     // fails Validate
		`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"pullRequest":{"number":1,"url":"javascript:alert(1)"},"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":false}}` + "\n", // unsafe PR
		string(valid), // valid JSON without the required terminating newline
		strings.Repeat(" ", maxVCSFrameBytes+1) + "x\n", // beyond the 64KiB frame bound
	}
	for _, frame := range frames {
		err := ReadSessionVCS(strings.NewReader(frame), func(protocol.SessionVCSStatus) error {
			t.Error("invalid frame delivered")
			return nil
		})
		var violation *VCSFrameError
		if err == nil || !errors.As(err, &violation) {
			t.Fatalf("frame of %d bytes produced %v, want *VCSFrameError", len(frame), err)
		}
	}
}

func TestSessionVCSWireReaderAcceptsMaximumCompleteFrame(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"})
	frame := append([]byte(nil), valid...)
	frame = append(frame, strings.Repeat(" ", maxVCSFrameBytes-len(frame)-1)...)
	frame = append(frame, '\n')
	called := false
	if err := ReadSessionVCS(strings.NewReader(string(frame)), func(protocol.SessionVCSStatus) error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("maximum frame: called=%t err=%v", called, err)
	}
}

func TestSessionVCSWireReaderPropagatesReceiveErrors(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"})
	stop := errors.New("stop consuming")
	err := ReadSessionVCS(strings.NewReader(string(valid)+"\n"+string(valid)+"\n"), func(protocol.SessionVCSStatus) error {
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("receive error = %v, want %v", err, stop)
	}
	var violation *VCSFrameError
	if errors.As(err, &violation) {
		t.Fatal("consumer error must not be classified as a protocol violation")
	}
}
