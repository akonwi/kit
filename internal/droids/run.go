package droids

import "sync"

// Run is one agent run's event stream and final result. Its Events
// channel closes when that run ends, so callers can range over it directly and
// then call Result. Events must be consumed continuously; delivery applies
// bounded backpressure to the run.
type Run interface {
	Events() <-chan Event
	// Result waits for the final assistant message. Call it after ranging over
	// Events, or while another goroutine continuously consumes Events.
	Result() (AssistantMessage, error)
}

type agentRun struct {
	events chan Event
	done   chan struct{}
	once   sync.Once
	result runResult
}

func newAgentRun() *agentRun {
	return &agentRun{
		events: make(chan Event, 64),
		done:   make(chan struct{}),
	}
}

func (s *agentRun) Events() <-chan Event { return s.events }

func (s *agentRun) Result() (AssistantMessage, error) {
	<-s.done
	return s.result.message, s.result.err
}

func (s *agentRun) emit(event Event) {
	s.events <- event
}

func (s *agentRun) complete(result runResult) {
	s.once.Do(func() {
		s.result = result
		close(s.events)
		close(s.done)
	})
}

func (p queuedPrompt) complete(result runResult) {
	if p.result != nil {
		p.result <- result
	}
	if p.stream != nil {
		p.stream.complete(result)
	}
}
