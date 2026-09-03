// Package client composes concrete server and session client transports.
package client

import (
	"context"
	"fmt"
	"strings"

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
	done      chan struct{}
	outcome   protocol.PromptOutcome
	err       error
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
	reservation, err := c.transport.ReserveRun(ctx, c.id, runID)
	if err != nil {
		return nil, err
	}
	run := &localRun{
		transport: c.transport,
		sessionID: c.id,
		turnID:    reservation.TurnID,
		id:        runID,
		done:      make(chan struct{}),
	}
	go func() {
		run.outcome, run.err = c.transport.RunPrompt(
			context.Background(), c.id, runID, text,
		)
		if run.err == nil && run.outcome.TurnID != run.turnID {
			run.err = fmt.Errorf("daemon prompt turn identity mismatch")
		}
		close(run.done)
	}()
	return run, nil
}

func (r *localRun) ID() string { return r.id }

func (r *localRun) Wait(ctx context.Context) (protocol.PromptOutcome, error) {
	select {
	case <-r.done:
		return r.outcome, r.err
	case <-ctx.Done():
		return protocol.PromptOutcome{}, ctx.Err()
	}
}

func (r *localRun) Abort(ctx context.Context) error {
	return r.transport.AbortSession(ctx, r.sessionID, r.id)
}
