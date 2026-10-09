package session

import (
	"context"
	"testing"
	"time"
)

func TestSettledAdmissionWaitsForSettlingTransitionThenHoldsBoth(t *testing.T) {
	loaded := &runtime{}
	// A finished turn settles while holding transition and admission authority.
	loaded.transitionMu.Lock()
	loaded.admissionMu.Lock()
	go func() {
		time.Sleep(30 * time.Millisecond)
		loaded.admissionMu.Unlock()
		loaded.transitionMu.Unlock()
	}()
	if !acquireSettledAdmission(t.Context(), loaded) {
		t.Fatal("acquireSettledAdmission() = false, want admission after settling")
	}
	if loaded.transitionMu.TryLock() {
		t.Fatal("transition authority is not held after admission")
	}
	if loaded.admissionMu.TryLock() {
		t.Fatal("admission authority is not held after admission")
	}
	loaded.admissionMu.Unlock()
	loaded.transitionMu.RUnlock()
}

func TestSettledAdmissionRejectsHeldTransitionWithinCallerDeadline(t *testing.T) {
	loaded := &runtime{}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if acquireSettledAdmission(ctx, loaded) {
		t.Fatal("acquireSettledAdmission() = true during a runtime transition")
	}
	if !loaded.admissionMu.TryLock() {
		t.Fatal("rejected admission left admission authority held")
	}
	loaded.admissionMu.Unlock()
}

func TestSettledAdmissionYieldsImmediatelyToActiveTurnsAndQueuedFollowUps(t *testing.T) {
	for name, loaded := range map[string]*runtime{
		"active turn":       {activeRun: "turn_active"},
		"queued follow-ups": {followUps: []PromptInput{{Text: "queued"}}},
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			if acquireSettledAdmission(t.Context(), loaded) {
				t.Fatal("acquireSettledAdmission() = true")
			}
			if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
				t.Fatalf("yield waited %s", elapsed)
			}
			// Yielding never takes authority a queued follow-up needs.
			if !loaded.transitionMu.TryLock() || !loaded.admissionMu.TryLock() {
				t.Fatal("yield left authority held")
			}
		})
	}
}
