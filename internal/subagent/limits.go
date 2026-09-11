package subagent

import "fmt"

// Limits bounds daemon-wide child execution and durable queueing.
type Limits struct {
	GlobalRunning        int
	PerSessionRunning    int
	PerConversationQueue int
	PerSessionQueue      int
	GlobalQueue          int
}

// DefaultLimits returns the centrally defined first-delivery limits.
func DefaultLimits() Limits {
	return Limits{GlobalRunning: 4, PerSessionRunning: 2, PerConversationQueue: 16, PerSessionQueue: 64, GlobalQueue: 256}
}

// Validate checks that limits are positive and internally coherent.
func (l Limits) Validate() error {
	if l.GlobalRunning < 1 || l.PerSessionRunning < 1 || l.PerConversationQueue < 1 || l.PerSessionQueue < 1 || l.GlobalQueue < 1 {
		return fmt.Errorf("subagent limits must be positive")
	}
	if l.PerSessionRunning > l.GlobalRunning {
		return fmt.Errorf("per-session running limit exceeds global limit")
	}
	if l.PerConversationQueue > l.PerSessionQueue || l.PerSessionQueue > l.GlobalQueue {
		return fmt.Errorf("subagent queue limits are not ordered conversation <= session <= global")
	}
	return nil
}
