package protocol

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Validate checks a session projection received across a transport boundary.
func (session SessionInfo) Validate() error {
	if session.ID == "" {
		return fmt.Errorf("session id is empty")
	}
	if !filepath.IsAbs(session.CWD) {
		return fmt.Errorf("session cwd %q is not absolute", session.CWD)
	}
	provider, model, ok := strings.Cut(session.Model, "/")
	if !ok || provider == "" || model == "" {
		return fmt.Errorf("session model %q is not namespaced", session.Model)
	}
	if _, err := time.Parse(time.RFC3339Nano, session.CreatedAt); err != nil {
		return fmt.Errorf("session createdAt is invalid: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, session.UpdatedAt); err != nil {
		return fmt.Errorf("session updatedAt is invalid: %w", err)
	}
	return nil
}

// Validate checks a run reservation received across a transport boundary.
func (reservation RunReservation) Validate() error {
	if reservation.SessionID == "" || reservation.TurnID == "" || reservation.RunID == "" {
		return fmt.Errorf("run reservation session, turn, and run ids are required")
	}
	return nil
}

// Validate checks a durable run projection received across a transport boundary.
func (run RunInfo) Validate() error {
	if run.SessionID == "" || run.TurnID == "" || run.RunID == "" {
		return fmt.Errorf("run info session, turn, and run ids are required")
	}
	switch run.Status {
	case RunStatusQueued, RunStatusRunning, RunStatusCompleted,
		RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
	default:
		return fmt.Errorf("run info status %q is invalid", run.Status)
	}
	if run.Status != RunStatusQueued && run.Status != RunStatusRunning && run.Status != RunStatusCompleted && run.ErrorMessage == "" {
		return fmt.Errorf("run info status %q requires an error message", run.Status)
	}
	return nil
}

// Validate checks a terminal prompt outcome received across a transport boundary.
func (outcome PromptOutcome) Validate() error {
	if outcome.SessionID == "" || outcome.TurnID == "" || outcome.RunID == "" {
		return fmt.Errorf("prompt outcome session, turn, and run ids are required")
	}
	switch outcome.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
	default:
		return fmt.Errorf("prompt outcome status %q is invalid", outcome.Status)
	}
	if outcome.Status != RunStatusCompleted && outcome.ErrorMessage == "" {
		return fmt.Errorf("prompt outcome status %q requires an error message", outcome.Status)
	}
	switch outcome.ErrorKind {
	case "", ProviderErrorAuthentication, ProviderErrorEntitlement,
		ProviderErrorUsageLimit, ProviderErrorRateLimit, ProviderErrorTransport,
		ProviderErrorProtocol:
	default:
		return fmt.Errorf("prompt outcome error kind %q is invalid", outcome.ErrorKind)
	}
	if outcome.Status == RunStatusCompleted && outcome.ErrorKind != "" {
		return fmt.Errorf("completed prompt outcome has error kind %q", outcome.ErrorKind)
	}
	return nil
}
