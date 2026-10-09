package droids

import (
	"testing"
	"time"
)

func TestPipeStreamResultDoesNotRequireEventConsumer(t *testing.T) {
	stream := newPipeStream()
	go func() {
		for i := range 100 {
			stream.emit(StreamTextDelta{ContentIndex: 0, Delta: string(rune('a' + i%26))})
		}
		stream.final = AssistantMessage{Model: "test", StopReason: StopReasonStop}
		stream.finish()
	}()

	result := make(chan AssistantMessage, 1)
	go func() { result <- stream.Result() }()
	select {
	case message := <-result:
		if message.Model != "test" {
			t.Fatalf("message = %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("Result blocked without an Events consumer")
	}
}

func TestPipeStreamResultAbandonsPartiallyConsumedEvents(t *testing.T) {
	stream := newPipeStream()
	go func() {
		for i := range 100 {
			stream.emit(StreamTextDelta{ContentIndex: 0, Delta: string(rune('a' + i%26))})
		}
		stream.final = AssistantMessage{Model: "test", StopReason: StopReasonStop}
		stream.finish()
	}()
	<-stream.Events()
	result := make(chan AssistantMessage, 1)
	go func() { result <- stream.Result() }()
	select {
	case message := <-result:
		if message.Model != "test" {
			t.Fatalf("message = %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("Result blocked after Events consumption was abandoned")
	}
	for range stream.Events() {
	}
}

func TestPipeStreamPreservesEventsForConsumer(t *testing.T) {
	stream := newPipeStream()
	events := stream.Events()
	go func() {
		for i := range 100 {
			stream.emit(StreamTextDelta{ContentIndex: 0, Delta: string(rune('a' + i%26))})
		}
		stream.final = AssistantMessage{Model: "test", StopReason: StopReasonStop}
		stream.finish()
	}()
	count := 0
	for range events {
		count++
	}
	if count != 100 {
		t.Fatalf("event count = %d", count)
	}
	if message := stream.Result(); message.Model != "test" {
		t.Fatalf("message = %#v", message)
	}
}
