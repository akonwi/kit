package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

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

func TestAttachmentEventStreamIncludesRunLifecycleEvents(t *testing.T) {
	t.Parallel()
	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 5,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_one", RunID: "run_one", Kind: protocol.SessionEventRunStarted, Status: protocol.RunStatusRunning},
			{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "run_one", RunID: "run_one", Kind: protocol.SessionEventProviderRetryScheduled, ProviderRetry: &protocol.ProviderRetry{Count: 1, RetryAt: "2026-01-02T03:04:05Z"}},
			{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_one", RunID: "run_one", Kind: protocol.SessionEventProviderRetryStarted, ProviderRetry: &protocol.ProviderRetry{Count: 1}},
			{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "run_one", RunID: "run_one", Kind: protocol.SessionEventAssistantCompleted, MessageID: "message_one"},
			{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "run_one", RunID: "run_one", Kind: protocol.SessionEventRunFinished, Status: protocol.RunStatusCompleted},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader("event: session.events\nid: stream_test:5\ndata: " + string(encoded) + "\n\n"))
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "", true, "", "stream_test", 0, nil, true)
	var received []protocol.SessionEvent
	for events := range stream.Updates() {
		received = append(received, events...)
	}
	if len(received) != 5 || received[1].Kind != protocol.SessionEventProviderRetryScheduled || received[2].Kind != protocol.SessionEventProviderRetryStarted || received[4].Kind != protocol.SessionEventRunFinished {
		t.Fatalf("attachment events = %#v", received)
	}
}

func TestLocalEventStreamReadsSSEUntilBoundRunFinishes(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 5,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_other", RunID: "run_other", Kind: protocol.SessionEventRunStarted, Status: protocol.RunStatusRunning},
			{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventRunStarted, Status: protocol.RunStatusRunning},
			{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventProviderRetryScheduled, ProviderRetry: &protocol.ProviderRetry{Count: 1, RetryAt: "2026-01-02T03:04:05Z"}},
			{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventProviderRetryStarted, ProviderRetry: &protocol.ProviderRetry{Count: 1}},
			{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventRunFinished, Status: protocol.RunStatusCompleted},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader(": connected\n\nevent: session.events\nid: stream_test:5\ndata: " + string(encoded) + "\n\n"))
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "run_test", false, "", "", 0, nil, false)

	var received []protocol.SessionEvent
	for events := range stream.Updates() {
		received = append(received, events...)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("event stream error = %v", err)
	}
	if len(received) != 4 || received[1].Kind != protocol.SessionEventProviderRetryScheduled || received[2].Kind != protocol.SessionEventProviderRetryStarted || received[3].Kind != protocol.SessionEventRunFinished {
		t.Fatalf("received = %+v", received)
	}
}

func TestLocalEventStreamResumesFromSnapshotBaseline(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 42, LastSequence: 44,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 42, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", MessageID: "message_test", Kind: protocol.SessionEventAssistantTextDelta, Delta: "continued"},
			{StreamID: "stream_test", Sequence: 43, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", MessageID: "message_test", Kind: protocol.SessionEventAssistantCompleted},
			{StreamID: "stream_test", Sequence: 44, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventRunFinished, Status: protocol.RunStatusCompleted},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader("event: session.events\nid: stream_test:44\ndata: " + string(encoded) + "\n\n"))
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	var cursor int64
	go stream.readSSE(t.Context(), body, "run_test", true, "message_test", "stream_test", 41, func(_ string, value int64, _ bool) {
		cursor = value
	}, false)

	var received []protocol.SessionEvent
	for events := range stream.Updates() {
		received = append(received, events...)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("event stream error = %v", err)
	}
	if len(received) != 3 || cursor != 44 {
		t.Fatalf("received = %+v, cursor = %d", received, cursor)
	}
}

func TestLocalEventStreamReportsResynchronizationRecord(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{StreamID: "stream_new", ResyncRequired: true}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader("event: session.resync\ndata: " + string(encoded) + "\n\n"))
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 1), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "run_test", false, "", "", 0, nil, false)
	for range stream.Updates() {
	}
	if err := stream.Err(); !errors.Is(err, errEventResyncRequired) {
		t.Fatalf("event stream error = %v, want resynchronization", err)
	}
}

func TestLocalEventStreamDoesNotAdvanceCursorBeforeDelivery(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 1,
		Events: []protocol.SessionEvent{{
			StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_test", RunID: "run_test",
			Kind: protocol.SessionEventRunStarted, Status: protocol.RunStatusRunning,
		}},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	advanced := make(chan struct{}, 1)
	body := io.NopCloser(strings.NewReader("data: " + string(encoded) + "\n\n"))
	go stream.readSSE(ctx, body, "run_test", false, "", "", 0, func(string, int64, bool) { advanced <- struct{}{} }, false)
	cancel()
	<-stream.done
	select {
	case <-advanced:
		t.Fatal("cursor advanced for an undelivered batch")
	default:
	}
}

func TestLocalEventStreamRejectsSequenceGapAcrossRecords(t *testing.T) {
	t.Parallel()

	batches := []protocol.SessionEventBatch{
		{StreamID: "stream_test", FirstSequence: 1, LastSequence: 1, Events: []protocol.SessionEvent{{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventRunStarted, Status: protocol.RunStatusRunning}}},
		{StreamID: "stream_test", FirstSequence: 3, LastSequence: 3, Events: []protocol.SessionEvent{{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_test", RunID: "run_test", Kind: protocol.SessionEventRunFinished, Status: protocol.RunStatusCompleted}}},
	}
	var payload strings.Builder
	for _, batch := range batches {
		encoded, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		payload.WriteString("data: " + string(encoded) + "\n\n")
	}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	go stream.readSSE(t.Context(), io.NopCloser(strings.NewReader(payload.String())), "run_test", false, "", "stream_test", 0, nil, false)
	for range stream.Updates() {
	}
	if err := stream.Err(); !errors.Is(err, errEventResyncRequired) {
		t.Fatalf("event stream error = %v, want resynchronization", err)
	}
}
