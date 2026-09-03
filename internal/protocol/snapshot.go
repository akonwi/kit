package protocol

import (
	"fmt"
	"time"
)

// Validate checks a session snapshot received across a transport boundary.
func (snapshot SessionSnapshot) Validate() error {
	if err := snapshot.Session.Validate(); err != nil {
		return fmt.Errorf("snapshot session: %w", err)
	}
	if snapshot.ContextTokens < 0 || snapshot.ContextWindow < 0 {
		return fmt.Errorf("snapshot context usage cannot be negative")
	}
	if snapshot.ContextWindow == 0 && snapshot.ContextTokens != 0 {
		return fmt.Errorf("snapshot context tokens require a context window")
	}
	previousSequence := int64(-1)
	for index, message := range snapshot.Messages {
		if message.ID == "" || message.TurnID == "" {
			return fmt.Errorf("snapshot message %d requires message and turn ids", index)
		}
		if message.Sequence <= previousSequence {
			return fmt.Errorf("snapshot message %d sequence %d is not increasing", index, message.Sequence)
		}
		previousSequence = message.Sequence
		switch message.Role {
		case "user", "assistant", "tool":
		default:
			return fmt.Errorf("snapshot message %d role %q is invalid", index, message.Role)
		}
		if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
			return fmt.Errorf("snapshot message %d createdAt is invalid: %w", index, err)
		}
	}
	return nil
}
