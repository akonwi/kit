package kit

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

const testHostedInstance = "clientplugin_0123456789abcdef0123456789abcdef"

// hostingTransport records the order of attachments and bound stream opens.
type hostingTransport struct {
	sessionTransport
	mu    sync.Mutex
	steps []string
	opens chan struct{}
}

func (t *hostingTransport) record(step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.steps = append(t.steps, step)
}

func (t *hostingTransport) recorded() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.steps...)
}

func (t *hostingTransport) GetSessionSnapshot(context.Context, string) (SessionSnapshot, error) {
	return validWatchSnapshot("session_0123456789abcdef0123456789abcdef", "stream_test", 0), nil
}

func (t *hostingTransport) AttachClientPlugin(_ context.Context, _ string, instanceID string, input protocol.AttachClientPluginInput) error {
	t.record("attach " + instanceID + " " + input.PluginID + " " + input.Tools[0].ID)
	return nil
}

func (t *hostingTransport) StreamClientPluginSessionEvents(_ context.Context, _ string, streamID string, after int64, instanceID string) (io.ReadCloser, error) {
	t.record("stream " + instanceID + " " + streamID)
	t.opens <- struct{}{}
	// An empty body ends the connection, so the stream reconnects.
	return io.NopCloser(strings.NewReader("")), nil
}

func TestHostPluginAttachesBeforeEveryBoundConnection(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	transport := &hostingTransport{opens: make(chan struct{}, 4)}
	session := newSession(nil, transport, sessionID, validWatchSnapshot(sessionID, "stream_test", 0))
	input := protocol.AttachClientPluginInput{PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{{ID: "snapshot", Description: "Outline a tab", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	stream, err := session.HostPlugin(t.Context(), testHostedInstance, input)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if update := receiveSessionUpdate(t, stream.Updates()); update.Snapshot == nil {
		t.Fatalf("first update = %+v", update)
	}
	for range 2 {
		select {
		case <-transport.opens:
		case <-time.After(5 * time.Second):
			t.Fatal("bound stream did not open")
		}
	}
	attach := "attach " + testHostedInstance + " browser snapshot"
	open := "stream " + testHostedInstance + " stream_test"
	want := []string{attach, attach, open, attach, open}
	got := transport.recorded()
	if len(got) < len(want) || strings.Join(got[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps = %q, want prefix %q", got, want)
	}
}
