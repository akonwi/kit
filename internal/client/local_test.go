package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

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

func (t *scriptedEventTransport) GetSessionEvents(_ context.Context, _ string, after int64) (protocol.SessionEventBatch, error) {
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
