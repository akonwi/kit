package kit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	kitserver "github.com/akonwi/kit/internal/server"
)

type concurrentSessionTransport struct {
	sessionTransport
	started chan string
	release chan struct{}
}

func (t *concurrentSessionTransport) GetSessionSnapshot(_ context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	return protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: sessionID, ConfigurationRevision: 1}}, nil
}

func (t *concurrentSessionTransport) ConfigureSession(ctx context.Context, sessionID string, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	select {
	case t.started <- sessionID:
	case <-ctx.Done():
		return protocol.ConfigureSessionResult{}, ctx.Err()
	}
	select {
	case <-t.release:
	case <-ctx.Done():
		return protocol.ConfigureSessionResult{}, ctx.Err()
	}
	return protocol.ConfigureSessionResult{Session: protocol.SessionInfo{ID: sessionID, ConfigurationRevision: input.ExpectedRevision + 1}}, nil
}

func TestBoundSessionsSerializeIndependently(t *testing.T) {
	transport := &concurrentSessionTransport{started: make(chan string, 2), release: make(chan struct{})}
	backend := &fakeClientBackend{list: func(context.Context, string) ([]protocol.SessionInfo, error) { return nil, nil }}
	client := newClient(backend, nil)
	client.sessions = transport
	t.Cleanup(func() { _ = client.Close() })

	first, err := client.Attach(t.Context(), "session_first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Attach(t.Context(), "session_second")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() {
		_, err := first.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 1})
		done <- err
	}()
	go func() {
		_, err := second.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 1})
		done <- err
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-transport.started:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatal("sessions shared a mutation gate")
		}
	}
	close(transport.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if !seen["session_first"] || !seen["session_second"] {
		t.Fatalf("started sessions = %+v", seen)
	}
}

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
	session := &Session{id: "session_test", mutations: transport, mutationGate: gate, snapshot: initial}
	result, err := session.Configure(t.Context(), protocol.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/new"})
	if err != nil || !reflect.DeepEqual(result.Session, configured.Session) {
		t.Fatalf("Configure() = %+v, %v", result, err)
	}
	if !reflect.DeepEqual(session.snapshot.Session, configured.Session) || session.snapshot.EventStreamID != "stream_new" || session.cacheGeneration != 1 {
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
	transport.configureErr = &kitserver.APIError{StatusCode: 409, Message: "configuration revision conflict"}
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
	session := &Session{id: "session_test", mutations: transport, mutationGate: gate}
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

type scriptedScratchpadTransport struct {
	read         protocol.Scratchpad
	updated      protocol.Scratchpad
	readSession  string
	writeSession string
	input        protocol.UpdateScratchpadInput
}

func (transport *scriptedScratchpadTransport) GetScratchpad(_ context.Context, sessionID string) (protocol.Scratchpad, error) {
	transport.readSession = sessionID
	return transport.read, nil
}

func (transport *scriptedScratchpadTransport) UpdateScratchpad(_ context.Context, sessionID string, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	transport.writeSession = sessionID
	transport.input = input
	return transport.updated, nil
}

func TestTemporarySessionScratchpadReturnsTypedUnsupportedError(t *testing.T) {
	session := &Session{snapshot: protocol.SessionSnapshot{Session: protocol.SessionInfo{Temporary: true}}}
	if _, err := session.Scratchpad(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Scratchpad() error = %v", err)
	}
	if _, err := session.UpdateScratchpad(t.Context(), protocol.UpdateScratchpadInput{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("UpdateScratchpad() error = %v", err)
	}
}

func TestLocalSessionScratchpadUsesBoundSessionIdentity(t *testing.T) {
	t.Parallel()
	transport := &scriptedScratchpadTransport{
		read:    protocol.Scratchpad{OwnerSessionID: "session_root", Revision: 1},
		updated: protocol.Scratchpad{OwnerSessionID: "session_root", Content: "notes", Revision: 2},
	}
	session := &Session{id: "session_child", scratchpads: transport}
	base := session
	if got, err := session.Scratchpad(t.Context()); err != nil || got != transport.read {
		t.Fatalf("Scratchpad() = %+v, %v", got, err)
	}
	input := protocol.UpdateScratchpadInput{ExpectedRevision: 1, Content: "notes"}
	if got, err := session.UpdateScratchpad(t.Context(), input); err != nil || got != transport.updated {
		t.Fatalf("UpdateScratchpad() = %+v, %v", got, err)
	}
	if transport.readSession != base.id || transport.writeSession != base.id || transport.input != input {
		t.Fatalf("bound calls = read:%q write:%q input:%+v", transport.readSession, transport.writeSession, transport.input)
	}
	if base.snapshot.Scratchpad == nil || *base.snapshot.Scratchpad != transport.updated || base.cacheGeneration != 2 {
		t.Fatalf("scratchpad cache = %+v generation=%d", base.snapshot.Scratchpad, base.cacheGeneration)
	}
}

func TestLocalSessionScratchpadEventReducerIgnoresStaleDuplicateAndReversedEvents(t *testing.T) {
	t.Parallel()
	current := protocol.Scratchpad{OwnerSessionID: "session_root", Content: "revision three", Revision: 3}
	session := &Session{snapshot: protocol.SessionSnapshot{Scratchpad: &current}}
	revisionTwo := protocol.Scratchpad{OwnerSessionID: "session_root", Content: "revision two", Revision: 2}
	revisionFive := protocol.Scratchpad{OwnerSessionID: "session_root", Content: "revision five", Revision: 5}
	revisionFour := protocol.Scratchpad{OwnerSessionID: "session_root", Content: "revision four", Revision: 4}
	filtered, err := session.reduceScratchpadEvents([]protocol.SessionEvent{
		{Payload: protocol.ScratchpadChangedEvent{Scratchpad: &revisionTwo}},
		{Payload: protocol.ScratchpadChangedEvent{Scratchpad: &revisionFive}},
		{Payload: protocol.ScratchpadChangedEvent{Scratchpad: &revisionFive}},
		{Payload: protocol.ScratchpadChangedEvent{Scratchpad: &revisionFour}},
	})
	payload, ok := filtered[0].Payload.(protocol.ScratchpadChangedEvent)
	if err != nil || len(filtered) != 1 || !ok || payload.Scratchpad == nil || *payload.Scratchpad != revisionFive {
		t.Fatalf("filtered events = %+v, %v", filtered, err)
	}
	if session.snapshot.Scratchpad == nil || *session.snapshot.Scratchpad != revisionFive || session.cacheGeneration != 1 {
		t.Fatalf("reduced scratchpad = %+v generation=%d", session.snapshot.Scratchpad, session.cacheGeneration)
	}
	transport := &scriptedScratchpadTransport{read: revisionFour, updated: revisionFour}
	bound := session
	session.scratchpads = transport
	if got, err := bound.Scratchpad(t.Context()); err != nil || got != revisionFive {
		t.Fatalf("stale Scratchpad() = %+v, %v; want %+v", got, err, revisionFive)
	}
	if got, err := bound.UpdateScratchpad(t.Context(), protocol.UpdateScratchpadInput{ExpectedRevision: 3, Content: revisionFour.Content}); err != nil || got != revisionFive {
		t.Fatalf("stale UpdateScratchpad() = %+v, %v; want %+v", got, err, revisionFive)
	}
	inconsistent := revisionFive
	inconsistent.Content = "inconsistent"
	if _, err := session.cacheScratchpad(inconsistent); err == nil {
		t.Fatal("equal revision with inconsistent content was accepted")
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
			{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_one", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}},
			{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "run_one", Payload: protocol.ProviderRetryScheduledEvent{ProviderRetry: &protocol.ProviderRetry{Count: 1, RetryAt: "2026-01-02T03:04:05Z"}}},
			{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_one", Payload: protocol.ProviderRetryStartedEvent{ProviderRetry: &protocol.ProviderRetry{Count: 1}}},
			{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "run_one", Payload: protocol.AssistantCompletedEvent{MessageID: "message_one"}},
			{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "run_one", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusCompleted}},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader("event: session.events\nid: stream_test:5\ndata: " + string(encoded) + "\n\n"))
	stream := &EventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "", true, "", "stream_test", 0, nil, nil, true)
	var received []protocol.SessionEvent
	for events := range stream.Updates() {
		received = append(received, events...)
	}
	if len(received) != 5 || received[1].Kind() != protocol.SessionEventProviderRetryScheduled || received[2].Kind() != protocol.SessionEventProviderRetryStarted || received[4].Kind() != protocol.SessionEventTurnCompleted {
		t.Fatalf("attachment events = %#v", received)
	}
}

func TestLocalEventStreamReadsSSEUntilBoundRunFinishes(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 6,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_other", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}},
			{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}},
			{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_test", Payload: protocol.ProviderRetryScheduledEvent{ProviderRetry: &protocol.ProviderRetry{Count: 1, RetryAt: "2026-01-02T03:04:05Z"}}},
			{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "run_test", Payload: protocol.ProviderRetryStartedEvent{ProviderRetry: &protocol.ProviderRetry{Count: 1}}},
			{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", Payload: protocol.AnnotationSubmittedEvent{AnnotationIDs: []uint64{9}, AcceptedMessageID: "message_0123456789abcdef0123456789abcdef"}},
			{StreamID: "stream_test", Sequence: 6, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusCompleted}},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader(": connected\n\nevent: session.events\nid: stream_test:6\ndata: " + string(encoded) + "\n\n"))
	stream := &EventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "run_test", false, "", "", 0, nil, nil, false)

	var received []protocol.SessionEvent
	for events := range stream.Updates() {
		received = append(received, events...)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("event stream error = %v", err)
	}
	if len(received) != 5 || received[1].Kind() != protocol.SessionEventProviderRetryScheduled || received[2].Kind() != protocol.SessionEventProviderRetryStarted || received[3].Kind() != protocol.SessionEventAnnotationSubmitted || received[4].Kind() != protocol.SessionEventTurnCompleted {
		t.Fatalf("received = %+v", received)
	}
}

func TestLocalEventStreamResumesFromSnapshotBaseline(t *testing.T) {
	t.Parallel()

	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 42, LastSequence: 44,
		Events: []protocol.SessionEvent{
			{StreamID: "stream_test", Sequence: 42, SessionID: "session_test", TurnID: "run_test", Payload: protocol.AssistantTextDeltaEvent{MessageID: "message_test", Delta: "continued"}},
			{StreamID: "stream_test", Sequence: 43, SessionID: "session_test", TurnID: "run_test", Payload: protocol.AssistantCompletedEvent{MessageID: "message_test"}},
			{StreamID: "stream_test", Sequence: 44, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusCompleted}},
		},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	body := io.NopCloser(strings.NewReader("event: session.events\nid: stream_test:44\ndata: " + string(encoded) + "\n\n"))
	stream := &EventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	var cursor int64
	go stream.readSSE(t.Context(), body, "run_test", true, "message_test", "stream_test", 41, func(_ string, value int64, _ bool) {
		cursor = value
	}, nil, false)

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
	stream := &EventStream{updates: make(chan []protocol.SessionEvent, 1), done: make(chan struct{})}
	go stream.readSSE(t.Context(), body, "run_test", false, "", "", 0, nil, nil, false)
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
			StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning},
		}},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stream := &EventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	advanced := make(chan struct{}, 1)
	body := io.NopCloser(strings.NewReader("event: session.events\nid: stream_test:1\ndata: " + string(encoded) + "\n\n"))
	go stream.readSSE(ctx, body, "run_test", false, "", "", 0, func(string, int64, bool) { advanced <- struct{}{} }, nil, false)
	cancel()
	<-stream.done
	select {
	case <-advanced:
		t.Fatal("cursor advanced for an undelivered batch")
	default:
	}
}

func TestEventStreamRejectsFramingAndBoundViolations(t *testing.T) {
	t.Parallel()
	batch := protocol.SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 1,
		Events: []protocol.SessionEvent{{
			StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_test",
			Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning},
		}},
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"undeclared record": "event: session.other\nid: stream_test:1\ndata: " + string(encoded) + "\n\n",
		"mismatched id":     "event: session.events\nid: stream_test:2\ndata: " + string(encoded) + "\n\n",
		"oversized record":  ":" + strings.Repeat("x", httpapi.MaxSessionEventRecordBytes) + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			stream := &EventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
			go stream.readSSE(t.Context(), io.NopCloser(strings.NewReader(body)), "run_test", false, "", "stream_test", 0, nil, nil, false)
			for range stream.Updates() {
			}
			var protocolFailure *ProtocolError
			if err := stream.Err(); !errors.As(err, &protocolFailure) {
				t.Fatalf("stream error = %v, want *ProtocolError", err)
			}
		})
	}
}

func TestLocalEventStreamRejectsSequenceGapAcrossRecords(t *testing.T) {
	t.Parallel()

	batches := []protocol.SessionEventBatch{
		{StreamID: "stream_test", FirstSequence: 1, LastSequence: 1, Events: []protocol.SessionEvent{{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}}}},
		{StreamID: "stream_test", FirstSequence: 3, LastSequence: 3, Events: []protocol.SessionEvent{{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "run_test", Payload: protocol.TurnCompletedEvent{Status: protocol.TurnStatusCompleted}}}},
	}
	var payload strings.Builder
	for _, batch := range batches {
		encoded, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		payload.WriteString(fmt.Sprintf("event: session.events\nid: stream_test:%d\ndata: %s\n\n", batch.LastSequence, encoded))
	}
	stream := &EventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	go stream.readSSE(t.Context(), io.NopCloser(strings.NewReader(payload.String())), "run_test", false, "", "stream_test", 0, nil, nil, false)
	for range stream.Updates() {
	}
	if err := stream.Err(); !errors.Is(err, errEventResyncRequired) {
		t.Fatalf("event stream error = %v, want resynchronization", err)
	}
}
