package droids

import "sync"

// pipeStream is a bounded provider event stream. Calling Result signals that
// the caller no longer needs pending events, which lets a Result-only caller or
// a caller abandoning Events unblock the producer without unbounded buffering.
type pipeStream struct {
	events  chan StreamEvent
	done    chan struct{}
	discard chan struct{}

	discardOnce sync.Once
	final       AssistantMessage
}

func newPipeStream() *pipeStream {
	return &pipeStream{
		events:  make(chan StreamEvent, 32),
		done:    make(chan struct{}),
		discard: make(chan struct{}),
	}
}

func (s *pipeStream) emit(event StreamEvent) {
	select {
	case <-s.discard:
		return
	default:
	}
	select {
	case s.events <- event:
	case <-s.discard:
	}
}

func (s *pipeStream) finish() {
	close(s.events)
	close(s.done)
}

func (s *pipeStream) Events() <-chan StreamEvent { return s.events }

func (s *pipeStream) Result() AssistantMessage {
	// Result is the explicit signal that pending deltas may be abandoned. The
	// normal consume-both pattern ranges Events to closure before calling this.
	s.discardOnce.Do(func() { close(s.discard) })
	<-s.done
	return s.final
}
