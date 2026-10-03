package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

func vcsRecord(data string) string { return "event: vcs.status\ndata: " + data + "\n\n" }

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
	// Initial latest snapshot (status nil while loading), heartbeat comments,
	// then the deduplicated update.
	body := ": connected\n\n" + vcsRecord(string(first)) + ": heartbeat\n\n: heartbeat\n\n" + vcsRecord(string(second))
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
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"}}}`),
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":null}}`),
		vcsRecord(strings.Repeat(`{"nested":`, 40) + `null` + strings.Repeat(`}`, 40)),
		vcsRecord(`{}`), // missing identity
		vcsRecord(`{"unknown":true}`),
		vcsRecord(string(valid) + ` {}`),                             // trailing garbage
		vcsRecord(strings.Replace(string(valid), "repo", "\xff", 1)), // invalid UTF-8
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","sessionId":"session_other","cwd":"/repo","status":null}`),                                                                                             // duplicate top-level key
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"/repo","root":"/other","head":{"kind":"branch","name":"main"},"dirty":false}}`),                                        // duplicate nested key
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"root":"relative","head":{"kind":"branch","name":"main"},"dirty":false}}`),                                                     // fails Validate
		vcsRecord(`{"sessionId":"session_0123456789abcdef0123456789abcdef","cwd":"/repo","status":{"pullRequest":{"number":1,"url":"javascript:alert(1)"},"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":false}}`), // unsafe PR
		strings.TrimSuffix(vcsRecord(string(valid)), "\n"),                                                    // record without its dispatching blank line
		"event: vcs.status\ndata: " + string(valid),                                                           // truncated line
		"event: vcs.other\ndata: " + string(valid) + "\n\n",                                                   // undeclared record name
		"data: " + string(valid) + "\n\n",                                                                     // missing record name
		"event: vcs.status\n\n",                                                                               // missing data
		"event: vcs.status\ndata: {\ndata: }\n\n",                                                             // multi-data record
		"event: vcs.status\nid: 1\ndata: " + string(valid) + "\n\n",                                           // id on a non-resumable stream
		"event: vcs.status\nretry: 10\ndata: " + string(valid) + "\n\n",                                       // undeclared field
		"event: vcs.status\ndata: {\"sessionId\":\n\n",                                                        // malformed JSON
		"event: vcs.status\ndata: " + string(valid) + strings.Repeat(" ", httpapi.MaxVCSRecordBytes) + "\n\n", // beyond the 64 KiB record bound
	}
	for _, frame := range frames {
		err := ReadSessionVCS(strings.NewReader(frame), func(protocol.SessionVCSStatus) error {
			t.Error("invalid frame delivered")
			return nil
		})
		var violation *StreamError
		if err == nil || !errors.As(err, &violation) {
			t.Fatalf("frame %.120q produced %v, want *StreamError", frame, err)
		}
	}
}

func TestSessionVCSWireReaderAcceptsMaximumCompleteRecord(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"})
	record := vcsRecord(string(valid))
	padding := strings.Repeat(" ", httpapi.MaxVCSRecordBytes-(len(record)-1))
	record = strings.Replace(record, string(valid), string(valid)+padding, 1)
	called := false
	if err := ReadSessionVCS(strings.NewReader(record), func(protocol.SessionVCSStatus) error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("maximum record: called=%t err=%v", called, err)
	}
}

func TestSessionVCSWireReaderPropagatesReceiveErrors(t *testing.T) {
	t.Parallel()
	valid, _ := json.Marshal(protocol.SessionVCSStatus{SessionID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo"})
	stop := errors.New("stop consuming")
	err := ReadSessionVCS(strings.NewReader(vcsRecord(string(valid))+vcsRecord(string(valid))), func(protocol.SessionVCSStatus) error {
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("receive error = %v, want %v", err, stop)
	}
	var violation *StreamError
	if errors.As(err, &violation) {
		t.Fatal("consumer error must not be classified as a protocol violation")
	}
}
