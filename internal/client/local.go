// Package client composes concrete server and session client transports.
package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

type localServer struct {
	transport *daemon.Client
}

type localSession struct {
	transport *daemon.Client
	id        string
}

type localRun struct {
	transport *daemon.Client
	sessionID string
	turnID    string
	id        string
}

var _ sessionclient.Server = (*localServer)(nil)
var _ sessionclient.Session = (*localSession)(nil)
var _ sessionclient.Run = (*localRun)(nil)

// NewLocalServer creates an authenticated loopback server client.
func NewLocalServer(paths apphome.Paths) sessionclient.Server {
	return &localServer{transport: daemon.NewClient(paths)}
}

func (c *localServer) CreateSession(
	ctx context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	return c.transport.CreateSession(ctx, input)
}

func (c *localServer) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	return c.transport.ListSessions(ctx, cwd)
}

func (c *localServer) Attach(ctx context.Context, sessionID string) (sessionclient.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is empty")
	}
	return &localSession{transport: c.transport, id: sessionID}, nil
}

func (c *localSession) ID() string { return c.id }

func (c *localSession) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	return c.transport.GetSessionSnapshot(ctx, c.id)
}

func (c *localSession) Run(ctx context.Context, runID string) (protocol.RunInfo, error) {
	return c.transport.GetRun(ctx, c.id, runID)
}

func (c *localSession) Abort(ctx context.Context, runID string) error {
	return c.transport.AbortSession(ctx, c.id, runID)
}

func (c *localSession) StartPrompt(ctx context.Context, text string) (sessionclient.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("prompt is empty")
	}
	runID, err := identifier.New("run_")
	if err != nil {
		return nil, err
	}
	reservation, err := c.transport.StartPrompt(ctx, c.id, runID, text)
	if err != nil {
		inspectContext, cancel := context.WithTimeout(context.Background(), time.Second)
		info, inspectErr := c.transport.GetRun(inspectContext, c.id, runID)
		cancel()
		if inspectErr != nil {
			return nil, err
		}
		reservation = protocol.RunReservation{
			SessionID: info.SessionID, TurnID: info.TurnID, RunID: info.RunID,
		}
	}
	return &localRun{
		transport: c.transport,
		sessionID: c.id,
		turnID:    reservation.TurnID,
		id:        runID,
	}, nil
}

func (r *localRun) ID() string { return r.id }

func (r *localRun) Wait(ctx context.Context) (protocol.PromptOutcome, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pollFailures := 0
	snapshotFailures := 0
	for {
		info, err := r.transport.GetRun(ctx, r.sessionID, r.id)
		if err != nil {
			pollFailures++
			if !retryablePollingError(err) || pollFailures >= 6 {
				return protocol.PromptOutcome{}, err
			}
			if err := waitForRetry(ctx, pollFailures); err != nil {
				return protocol.PromptOutcome{}, err
			}
			continue
		}
		pollFailures = 0
		switch info.Status {
		case protocol.RunStatusQueued, protocol.RunStatusRunning:
			if err := waitForPoll(ctx, ticker.C); err != nil {
				return protocol.PromptOutcome{}, err
			}
		case protocol.RunStatusCompleted:
			snapshot, err := r.transport.GetSessionSnapshot(ctx, r.sessionID)
			if err != nil {
				snapshotFailures++
				if !retryablePollingError(err) || snapshotFailures >= 6 {
					return protocol.PromptOutcome{}, err
				}
				if err := waitForRetry(ctx, snapshotFailures); err != nil {
					return protocol.PromptOutcome{}, err
				}
				continue
			}
			text := ""
			for index := len(snapshot.Messages) - 1; index >= 0; index-- {
				message := snapshot.Messages[index]
				if message.TurnID == r.turnID && message.Role == "assistant" {
					text = message.Text
					break
				}
			}
			return protocol.PromptOutcome{
				SessionID: r.sessionID, TurnID: r.turnID, RunID: r.id,
				Status: protocol.RunStatusCompleted, Text: text,
			}, nil
		case protocol.RunStatusFailed, protocol.RunStatusAborted, protocol.RunStatusInterrupted:
			return protocol.PromptOutcome{
				SessionID: r.sessionID, TurnID: r.turnID, RunID: r.id,
				Status: info.Status, ErrorMessage: info.ErrorMessage,
			}, nil
		default:
			return protocol.PromptOutcome{}, fmt.Errorf("daemon returned unknown run status %q", info.Status)
		}
	}
}

func retryablePollingError(err error) bool {
	if err == nil || errors.Is(err, daemon.ErrIncompatibleDaemon) {
		return false
	}
	var apiError *daemon.APIError
	if errors.As(err, &apiError) {
		return apiError.StatusCode >= 500
	}
	return true
}

func waitForRetry(ctx context.Context, failure int) error {
	delay := 100 * time.Millisecond * time.Duration(1<<min(failure-1, 4))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForPoll(ctx context.Context, tick <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tick:
		return nil
	}
}

func (r *localRun) Abort(ctx context.Context) error {
	return r.transport.AbortSession(ctx, r.sessionID, r.id)
}
