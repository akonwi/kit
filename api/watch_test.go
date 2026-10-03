package kit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

type scriptedWatchTransport struct {
	sessionTransport
	mu            sync.Mutex
	snapshotReads int
	queries       chan watchQuery
}

type watchQuery struct {
	streamID string
	after    int64
}

func (t *scriptedWatchTransport) GetSessionSnapshot(context.Context, string) (SessionSnapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snapshotReads++
	if t.snapshotReads == 1 {
		return validWatchSnapshot("session_0123456789abcdef0123456789abcdef", "stream_old", 1), nil
	}
	return validWatchSnapshot("session_0123456789abcdef0123456789abcdef", "stream_new", 5), nil
}

func (t *scriptedWatchTransport) StreamSessionEvents(_ context.Context, _ string, streamID string, after int64) (io.ReadCloser, error) {
	t.queries <- watchQuery{streamID: streamID, after: after}
	if streamID == "stream_old" {
		return io.NopCloser(strings.NewReader("event: session.resync\ndata: {\"streamId\":\"stream_old\",\"firstSequence\":1,\"lastSequence\":1,\"resyncRequired\":true,\"events\":[]}\n\n")), nil
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func TestSessionWatchEmitsReplacementSnapshotAfterReplayGap(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	transport := &scriptedWatchTransport{queries: make(chan watchQuery, 4)}
	session := newSession(nil, transport, sessionID, validWatchSnapshot(sessionID, "stream_old", 1))
	stream, err := session.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first := receiveSessionUpdate(t, stream.Updates())
	if first.Snapshot == nil || first.Snapshot.EventStreamID != "stream_old" {
		t.Fatalf("first update = %+v", first)
	}
	second := receiveSessionUpdate(t, stream.Updates())
	if second.Snapshot == nil || second.Snapshot.EventStreamID != "stream_new" || second.Snapshot.EventCursor != 5 {
		t.Fatalf("replacement update = %+v", second)
	}

	if query := <-transport.queries; query != (watchQuery{streamID: "stream_old", after: 1}) {
		t.Fatalf("initial query = %+v", query)
	}
	select {
	case query := <-transport.queries:
		if query != (watchQuery{streamID: "stream_new", after: 5}) {
			t.Fatalf("resynchronized query = %+v", query)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement stream was not opened")
	}
}

type cursorWatchTransport struct {
	sessionTransport
	body    string
	queries chan watchQuery
}

func (t *cursorWatchTransport) GetSessionSnapshot(context.Context, string) (SessionSnapshot, error) {
	return validWatchSnapshot("session_0123456789abcdef0123456789abcdef", "stream_test", 0), nil
}

func (t *cursorWatchTransport) StreamSessionEvents(_ context.Context, _ string, streamID string, after int64) (io.ReadCloser, error) {
	t.queries <- watchQuery{streamID: streamID, after: after}
	body := t.body
	t.body = ""
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestSessionWatchReconnectsAfterLastDeliveredCursor(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 2,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 1, SessionID: sessionID, TurnID: "turn_test", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}},
			{StreamID: "stream_test", Sequence: 2, SessionID: sessionID, TurnID: "turn_test", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusCompleted}},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	transport := &cursorWatchTransport{
		body:    "event: session.events\nid: stream_test:2\ndata: " + string(encoded) + "\n\n",
		queries: make(chan watchQuery, 4),
	}
	session := newSession(nil, transport, sessionID, validWatchSnapshot(sessionID, "stream_test", 0))
	stream, err := session.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_ = receiveSessionUpdate(t, stream.Updates())
	update := receiveSessionUpdate(t, stream.Updates())
	if len(update.Events) != 2 || update.Events[1].Sequence != 2 {
		t.Fatalf("event update = %+v", update)
	}
	if first := <-transport.queries; first.after != 0 {
		t.Fatalf("initial cursor = %+v", first)
	}
	select {
	case reconnect := <-transport.queries:
		if reconnect.after != 2 {
			t.Fatalf("reconnect cursor = %+v", reconnect)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not reconnect")
	}
}

func TestSessionWatchStopsOnProtocolViolation(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	transport := &cursorWatchTransport{
		body:    "data: {not-json}\n\n",
		queries: make(chan watchQuery, 2),
	}
	session := newSession(nil, transport, sessionID, validWatchSnapshot(sessionID, "stream_test", 0))
	stream, err := session.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_ = receiveSessionUpdate(t, stream.Updates())
	for range stream.Updates() {
	}
	var terminal *StreamWatchTerminalError
	var protocolFailure *ProtocolError
	if err := stream.Err(); !errors.As(err, &terminal) || !errors.As(err, &protocolFailure) {
		t.Fatalf("stream error = %v", err)
	}
}

func TestClientCloseStopsSessionWatchWithoutAbortingServerWork(t *testing.T) {
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	transport := &cursorWatchTransport{queries: make(chan watchQuery, 4)}
	backend := &fakeClientBackend{list: func(context.Context, string) ([]SessionInfo, error) { return nil, nil }}
	client := newClient(backend, nil)
	session := newSession(client, transport, sessionID, validWatchSnapshot(sessionID, "stream_test", 0))
	stream, err := session.Watch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_ = receiveSessionUpdate(t, stream.Updates())
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-stream.Updates():
		if ok {
			t.Fatal("watch delivered an update after client close")
		}
	case <-time.After(time.Second):
		t.Fatal("client close did not stop watch")
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("detached watch error = %v", err)
	}
}

func validWatchSnapshot(sessionID, streamID string, cursor int64) SessionSnapshot {
	now := time.Unix(1, 0).UTC().Format(time.RFC3339Nano)
	return protocol.SessionSnapshot{
		Session: protocol.SessionInfo{
			ID: sessionID, CWD: "/workspace", Model: "test/model", ThinkingLevel: "off",
			ConfigurationRevision: 1, CreatedAt: now, UpdatedAt: now,
		},
		EventStreamID: streamID,
		EventCursor:   cursor,
	}
}

func receiveSessionUpdate(t *testing.T, updates <-chan SessionUpdate) SessionUpdate {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session update")
		return SessionUpdate{}
	}
}
