package droids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

func validID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+32 {
		return false
	}
	_, err := hex.DecodeString(value[len(prefix):])
	return err == nil
}

func newID(prefix string) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("droids: generate %s id: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(entropy[:]), nil
}

func newTurnID() (TurnID, error) {
	id, err := newID("turn_")
	return TurnID(id), err
}

func newAttemptID() (AttemptID, error) {
	id, err := newID("attempt_")
	return AttemptID(id), err
}

func newMessageID() (MessageID, error) {
	id, err := newID("message_")
	return MessageID(id), err
}

func newCheckpointID() (CheckpointID, error) {
	id, err := newID("checkpoint_")
	return CheckpointID(id), err
}
