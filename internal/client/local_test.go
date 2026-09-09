package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/protocol"
)

func TestLocalSessionConfigurationUpdatesCacheAndResynchronizesAmbiguousErrors(t *testing.T) {
	t.Parallel()

	initial := protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session_test", Model: "test/old", ThinkingLevel: "off", ConfigurationRevision: 1}, EventStreamID: "stream_old"}
	configured := initial
	configured.Session.Model = "test/new"
	configured.Session.ConfigurationRevision = 2
	configured.EventStreamID = "stream_new"
	transport := &scriptedMutationTransport{
		configureResult: protocol.ConfigureSessionResult{Session: configured.Session, EventStreamID: configured.EventStreamID},
		snapshot:        configured,
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	session := &localSession{id: "session_test", mutations: transport, mutationGate: gate, snapshot: initial}
	result, err := session.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/new"})
	if err != nil || result.Session != configured.Session {
		t.Fatalf("Configure() = %+v, %v", result, err)
	}
	if session.snapshot.Session != configured.Session || session.snapshot.EventStreamID != "stream_new" || session.cacheGeneration != 1 {
		t.Fatalf("configured cache = %+v generation=%d", session.snapshot, session.cacheGeneration)
	}

	transport.configureErr = errors.New("response lost")
	configured.Session.Model = "test/newer"
	configured.Session.ConfigurationRevision = 3
	configured.EventStreamID = "stream_newer"
	transport.snapshot = configured
	if _, err := session.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 2, Model: "test/newer"}); err == nil {
		t.Fatal("Configure() hid an ambiguous transport error")
	}
	if session.snapshot.Session.Model != "test/newer" || session.snapshot.Session.ConfigurationRevision != 3 || session.snapshot.EventStreamID != "stream_newer" {
		t.Fatalf("ambiguous configuration did not resynchronize cache: %+v", session.snapshot)
	}
	transport.configureErr = &daemon.APIError{StatusCode: 409, Message: "configuration revision conflict"}
	configured.Session.Model = "test/other-client"
	configured.Session.ConfigurationRevision = 4
	configured.EventStreamID = "stream_other"
	transport.snapshot = configured
	if _, err := session.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 2, Model: "test/conflict"}); err == nil {
		t.Fatal("Configure() hid a stale-revision conflict")
	}
	if session.snapshot.Session.Model != "test/other-client" || session.snapshot.Session.ConfigurationRevision != 4 {
		t.Fatalf("configuration conflict did not refresh cache: %+v", session.snapshot)
	}
}

func TestLocalSessionCompactPreservesOperationIdentityAndSerializesCancellation(t *testing.T) {
	t.Parallel()

	transport := &scriptedMutationTransport{compactResult: protocol.CompactSessionResult{OperationID: "compact_test", EventStreamID: "stream_new"}}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	session := &localSession{id: "session_test", mutations: transport, mutationGate: gate}
	result, err := session.Compact(t.Context(), protocol.CompactSessionInput{OperationID: "compact_test"})
	if err != nil || result.OperationID != "compact_test" || session.snapshot.EventStreamID != "stream_new" {
		t.Fatalf("Compact() = %+v, %v cache=%+v", result, err, session.snapshot)
	}
	<-gate
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := session.Compact(ctx, protocol.CompactSessionInput{OperationID: "compact_test"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Compact() error = %v", err)
	}
	gate <- struct{}{}
	if transport.compactCalls != 1 {
		t.Fatalf("compact transport calls = %d, want 1", transport.compactCalls)
	}
}

type scriptedMutationTransport struct {
	configureResult protocol.ConfigureSessionResult
	configureErr    error
	compactResult   protocol.CompactSessionResult
	compactErr      error
	snapshot        protocol.SessionSnapshot
	compactCalls    int
}

func (transport *scriptedMutationTransport) ConfigureSession(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	return transport.configureResult, transport.configureErr
}
func (transport *scriptedMutationTransport) CompactSession(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	transport.compactCalls++
	return transport.compactResult, transport.compactErr
}
func (transport *scriptedMutationTransport) GetSessionSnapshot(context.Context, string) (protocol.SessionSnapshot, error) {
	return transport.snapshot, nil
}

func TestLocalEventStreamReportsMissingTerminalEvent(t *testing.T) {
	t.Parallel()

	transport := &scriptedEventTransport{runStatus: protocol.RunStatusCompleted}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	initial := protocol.SessionEventBatch{StreamID: "stream_test", Events: []protocol.SessionEvent{
		{Sequence: 1, RunID: "run_test", Kind: protocol.SessionEventRunStarted},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go stream.poll(ctx, transport, "session_test", "run_test", initial)

	var received []protocol.SessionEvent
	for batch := range stream.Updates() {
		received = append(received, batch...)
	}
	if err := stream.Err(); !errors.Is(err, errTerminalEventMissing) {
		t.Fatalf("event stream error = %v, want terminal-event failure", err)
	}
	if len(received) != 1 || received[0].Kind != protocol.SessionEventRunStarted || transport.runCalls != 3 {
		t.Fatalf("received = %+v, run status checks = %d", received, transport.runCalls)
	}
}

func TestLocalEventStreamDeliversOnlyTheBoundRunInOrder(t *testing.T) {
	t.Parallel()

	transport := &scriptedEventTransport{pages: []protocol.SessionEventBatch{{
		StreamID: "stream_test",
		Events: []protocol.SessionEvent{
			{Sequence: 3, RunID: "run_test", MessageID: "message_test", Kind: protocol.SessionEventAssistantStarted},
			{Sequence: 4, RunID: "run_test", MessageID: "message_test", Kind: protocol.SessionEventAssistantTextDelta, Delta: "hello"},
			{Sequence: 5, RunID: "run_test", MessageID: "message_test", Kind: protocol.SessionEventAssistantCompleted},
			{Sequence: 6, RunID: "run_test", Kind: protocol.SessionEventRunFinished, Status: protocol.RunStatusCompleted},
		},
	}}}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	initial := protocol.SessionEventBatch{StreamID: "stream_test", Events: []protocol.SessionEvent{
		{Sequence: 1, RunID: "run_other", Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, RunID: "run_test", Kind: protocol.SessionEventRunStarted},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go stream.poll(ctx, transport, "session_test", "run_test", initial)

	var received []protocol.SessionEvent
	for batch := range stream.Updates() {
		received = append(received, batch...)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("event stream error = %v", err)
	}
	if len(received) != 5 {
		t.Fatalf("received event count = %d, want 5: %+v", len(received), received)
	}
	wantKinds := []protocol.SessionEventKind{
		protocol.SessionEventRunStarted,
		protocol.SessionEventAssistantStarted,
		protocol.SessionEventAssistantTextDelta,
		protocol.SessionEventAssistantCompleted,
		protocol.SessionEventRunFinished,
	}
	for index, want := range wantKinds {
		if received[index].Kind != want || received[index].RunID != "run_test" {
			t.Errorf("received event %d = %+v, want kind %q for run_test", index, received[index], want)
		}
	}
	if len(transport.cursors) != 1 || transport.cursors[0] != 2 {
		t.Fatalf("poll cursors = %v, want [2]", transport.cursors)
	}
}

func TestLocalEventStreamRejectsAssistantIdentityChangeAcrossPages(t *testing.T) {
	t.Parallel()

	transport := &scriptedEventTransport{pages: []protocol.SessionEventBatch{{
		StreamID: "stream_test",
		Events: []protocol.SessionEvent{
			{Sequence: 3, RunID: "run_test", MessageID: "message_two", Kind: protocol.SessionEventAssistantTextDelta, Delta: "wrong"},
		},
	}}}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	initial := protocol.SessionEventBatch{StreamID: "stream_test", Events: []protocol.SessionEvent{
		{Sequence: 1, RunID: "run_test", Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, RunID: "run_test", MessageID: "message_one", Kind: protocol.SessionEventAssistantStarted},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go stream.poll(ctx, transport, "session_test", "run_test", initial)

	for range stream.Updates() {
	}
	if err := stream.Err(); !errors.Is(err, errEventResyncRequired) {
		t.Fatalf("event stream error = %v, want resynchronization", err)
	}
}

type scriptedEventTransport struct {
	pages     []protocol.SessionEventBatch
	cursors   []int64
	runStatus protocol.RunStatus
	runCalls  int
}

func (t *scriptedEventTransport) GetSessionEvents(_ context.Context, _ string, _ string, after int64) (protocol.SessionEventBatch, error) {
	t.cursors = append(t.cursors, after)
	if len(t.pages) == 0 {
		return protocol.SessionEventBatch{StreamID: "stream_test"}, nil
	}
	page := t.pages[0]
	t.pages = t.pages[1:]
	return page, nil
}

func (t *scriptedEventTransport) GetRun(context.Context, string, string) (protocol.RunInfo, error) {
	t.runCalls++
	status := t.runStatus
	if status == "" {
		status = protocol.RunStatusRunning
	}
	return protocol.RunInfo{Status: status}, nil
}
