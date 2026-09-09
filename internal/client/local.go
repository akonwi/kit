// Package client composes concrete server and session client transports.
package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

type sessionMutationTransport interface {
	ConfigureSession(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	CompactSession(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	GetSessionSnapshot(context.Context, string) (protocol.SessionSnapshot, error)
}

type localSession struct {
	transport          *daemon.Client
	mutations          sessionMutationTransport
	id                 string
	mu                 sync.Mutex
	snapshot           protocol.SessionSnapshot
	cacheGeneration    uint64
	mutationGate       chan struct{}
	pendingCWDTarget   string
	pendingCWDMutation string
}

type localRun struct {
	transport *daemon.Client
	sessionID string
	turnID    string
	id        string
}

type localBashExecution struct {
	transport *daemon.Client
	sessionID string
	mu        sync.RWMutex
	state     protocol.BashExecution
}

type localEventStream struct {
	updates chan []protocol.SessionEvent
	done    chan struct{}
	err     error
}

var (
	errEventResyncRequired  = errors.New("session event replay requires snapshot resynchronization")
	errTerminalEventMissing = errors.New("session event stream ended without a terminal event")
)

var _ sessionclient.Server = (*localServer)(nil)
var _ sessionclient.Session = (*localSession)(nil)
var _ sessionclient.Run = (*localRun)(nil)
var _ sessionclient.BashExecution = (*localBashExecution)(nil)

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

func (c *localServer) RenameSession(ctx context.Context, sessionID, name string) (protocol.SessionInfo, error) {
	return c.transport.RenameSession(ctx, sessionID, name)
}

func (c *localServer) DeleteSession(ctx context.Context, sessionID string) error {
	return c.transport.DeleteSession(ctx, sessionID)
}

func (c *localServer) DisposeTemporarySession(ctx context.Context, sessionID string) error {
	return c.transport.DisposeTemporarySession(ctx, sessionID)
}

func (c *localServer) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	return c.transport.ListSessions(ctx, cwd)
}

func (c *localServer) Models(ctx context.Context) (protocol.ModelCatalog, error) {
	return c.transport.ListModels(ctx)
}

func (c *localServer) Attach(ctx context.Context, sessionID string) (sessionclient.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is empty")
	}
	mutationGate := make(chan struct{}, 1)
	mutationGate <- struct{}{}
	return &localSession{transport: c.transport, mutations: c.transport, id: sessionID, mutationGate: mutationGate}, nil
}

func (c *localSession) ID() string { return c.id }

func (c *localSession) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	c.mu.Lock()
	generation := c.cacheGeneration
	c.mu.Unlock()
	snapshot, err := c.transport.GetSessionSnapshot(ctx, c.id)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	c.mu.Lock()
	if c.cacheGeneration == generation {
		c.snapshot = snapshot
	}
	c.mu.Unlock()
	return snapshot, nil
}

func (c *localSession) ChangeCWD(ctx context.Context, target string) (protocol.SessionInfo, error) {
	if err := ctx.Err(); err != nil {
		return protocol.SessionInfo{}, err
	}
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-ctx.Done():
		return protocol.SessionInfo{}, ctx.Err()
	}
	target = strings.TrimSpace(target)
	c.mu.Lock()
	mutationID := c.pendingCWDMutation
	if mutationID != "" && c.pendingCWDTarget != target {
		pending := c.pendingCWDTarget
		c.mu.Unlock()
		return protocol.SessionInfo{}, fmt.Errorf("previous cwd change to %q has an unresolved outcome; retry it before changing targets", pending)
	}
	c.mu.Unlock()
	newMutation := mutationID == ""
	if newMutation {
		var err error
		mutationID, err = identifier.New("cwd_")
		if err != nil {
			return protocol.SessionInfo{}, err
		}
	}
	if err := (protocol.ChangeCWDInput{MutationID: mutationID, Path: target}).Validate(); err != nil {
		return protocol.SessionInfo{}, err
	}
	if newMutation {
		c.mu.Lock()
		c.pendingCWDTarget, c.pendingCWDMutation = target, mutationID
		c.mu.Unlock()
	}
	result, err := c.transport.ChangeSessionCWDWithID(ctx, c.id, mutationID, target)
	if err != nil {
		var apiError *daemon.APIError
		if errors.As(err, &apiError) && apiError.StatusCode < 500 {
			c.clearPendingCWD(mutationID)
			return protocol.SessionInfo{}, err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return protocol.SessionInfo{}, err
		}
		retryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var retryErr error
		result, retryErr = c.transport.ChangeSessionCWDWithID(retryContext, c.id, mutationID, target)
		cancel()
		if retryErr != nil {
			var retryAPIError *daemon.APIError
			if errors.As(retryErr, &retryAPIError) {
				if retryAPIError.StatusCode < 500 {
					c.clearPendingCWD(mutationID)
				}
				return protocol.SessionInfo{}, retryErr
			}
			return protocol.SessionInfo{}, err
		}
	}
	c.mu.Lock()
	c.cacheGeneration++
	c.snapshot.Session = result
	if c.pendingCWDMutation == mutationID {
		c.pendingCWDTarget, c.pendingCWDMutation = "", ""
	}
	c.mu.Unlock()
	return result, nil
}

func (c *localSession) clearPendingCWD(mutationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingCWDMutation == mutationID {
		c.pendingCWDTarget, c.pendingCWDMutation = "", ""
	}
}

func (c *localSession) Reload(ctx context.Context) (protocol.ReloadSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ReloadSessionResult{}, err
	}
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-ctx.Done():
		return protocol.ReloadSessionResult{}, ctx.Err()
	}
	result, err := c.transport.ReloadSession(ctx, c.id)
	if err != nil {
		var apiError *daemon.APIError
		if !errors.As(err, &apiError) {
			// A transport failure may detach after the server committed reload.
			// Reconcile the cache through an independent bounded snapshot attempt.
			inspectContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			snapshot, inspectErr := c.transport.GetSessionSnapshot(inspectContext, c.id)
			cancel()
			if inspectErr == nil {
				c.mu.Lock()
				c.cacheGeneration++
				c.snapshot = snapshot
				c.mu.Unlock()
			}
		}
		return protocol.ReloadSessionResult{}, err
	}
	c.mu.Lock()
	c.cacheGeneration++
	c.snapshot.EventStreamID = result.EventStreamID
	c.snapshot.ActiveRunID = ""
	c.snapshot.EventCursor = 0
	c.snapshot.EventReplayFrom = 0
	c.snapshot.EventReplayAvailable = false
	c.mu.Unlock()
	return result, nil
}

func (c *localSession) Configure(ctx context.Context, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ConfigureSessionResult{}, err
	}
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-ctx.Done():
		return protocol.ConfigureSessionResult{}, ctx.Err()
	}
	transport := c.mutations
	if transport == nil {
		transport = c.transport
	}
	result, err := transport.ConfigureSession(ctx, c.id, input)
	if err != nil {
		var apiError *daemon.APIError
		if !errors.As(err, &apiError) || apiError.StatusCode == 409 || apiError.StatusCode >= 500 {
			inspectContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			snapshot, inspectErr := transport.GetSessionSnapshot(inspectContext, c.id)
			cancel()
			if inspectErr == nil {
				c.mu.Lock()
				c.cacheGeneration++
				c.snapshot = snapshot
				c.mu.Unlock()
			}
		}
		return protocol.ConfigureSessionResult{}, err
	}
	c.mu.Lock()
	c.cacheGeneration++
	c.snapshot.Session = result.Session
	c.snapshot.EventStreamID = result.EventStreamID
	c.snapshot.ActiveRunID = ""
	c.snapshot.EventCursor = 0
	c.snapshot.EventReplayFrom = 0
	c.snapshot.EventReplayAvailable = false
	c.snapshot.Warnings = append([]string(nil), result.Warnings...)
	c.mu.Unlock()
	return result, nil
}

func (c *localSession) Compact(ctx context.Context, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CompactSessionResult{}, err
	}
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-ctx.Done():
		return protocol.CompactSessionResult{}, ctx.Err()
	}
	transport := c.mutations
	if transport == nil {
		transport = c.transport
	}
	result, err := transport.CompactSession(ctx, c.id, input)
	if err != nil {
		return protocol.CompactSessionResult{}, err
	}
	c.mu.Lock()
	c.cacheGeneration++
	c.snapshot.EventStreamID = result.EventStreamID
	c.snapshot.ActiveRunID = ""
	c.snapshot.EventCursor = 0
	c.snapshot.EventReplayFrom = 0
	c.snapshot.EventReplayAvailable = false
	c.mu.Unlock()
	return result, nil
}

func (c *localSession) Run(ctx context.Context, runID string) (protocol.RunInfo, error) {
	return c.transport.GetRun(ctx, c.id, runID)
}

func (c *localSession) Abort(ctx context.Context, runID string) error {
	return c.transport.AbortSession(ctx, c.id, runID)
}

func (c *localSession) Bash(ctx context.Context, executionID string) (sessionclient.BashExecution, error) {
	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	state, err := c.transport.GetBash(requestContext, c.id, executionID)
	if err != nil {
		return nil, err
	}
	return &localBashExecution{transport: c.transport, sessionID: c.id, state: state}, nil
}

func (c *localSession) StartBash(ctx context.Context, executionID, command string, excludeFromContext bool) (sessionclient.BashExecution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input := protocol.BashExecutionInput{
		ExecutionID: executionID, Command: command, ExcludeFromContext: excludeFromContext,
	}
	state, err := c.transport.StartBash(ctx, c.id, input)
	if err != nil {
		inspectContext, cancel := context.WithTimeout(context.Background(), time.Second)
		resolved, inspectErr := c.transport.GetBash(inspectContext, c.id, executionID)
		cancel()
		if inspectErr != nil {
			return nil, err
		}
		state = resolved
	}
	return &localBashExecution{transport: c.transport, sessionID: c.id, state: state}, nil
}

func (c *localSession) AbortBash(ctx context.Context, executionID string) error {
	return c.transport.AbortBash(ctx, c.id, executionID)
}

func (c *localSession) Stream(ctx context.Context, runID string) (sessionclient.EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("run id is empty")
	}
	c.mu.Lock()
	snapshot := c.snapshot
	c.mu.Unlock()
	streamID := ""
	after := int64(0)
	if snapshot.ActiveRunID == runID {
		if !snapshot.EventReplayAvailable {
			return nil, errEventResyncRequired
		}
		streamID = snapshot.EventStreamID
		after = snapshot.EventReplayFrom
	}
	initial, err := fetchSessionEvents(ctx, c.transport, c.id, streamID, after)
	if err != nil {
		return nil, err
	}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent, 8), done: make(chan struct{})}
	go stream.poll(ctx, c.transport, c.id, runID, initial)
	return stream, nil
}

func (c *localSession) StartPrompt(ctx context.Context, text string) (sessionclient.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("prompt is empty")
	}
	reservation, err := c.transport.StartPrompt(ctx, c.id, text)
	if err != nil {
		return nil, err
	}
	return c.runFromReservation(reservation), nil
}

func (c *localSession) StartPromptCommand(ctx context.Context, name, args string) (sessionclient.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reservation, err := c.transport.StartPromptCommand(ctx, c.id, protocol.PromptCommandInput{Name: name, Args: args})
	if err != nil {
		return nil, err
	}
	return c.runFromReservation(reservation), nil
}

func (c *localSession) runFromReservation(reservation protocol.RunReservation) sessionclient.Run {
	return &localRun{
		transport: c.transport, sessionID: c.id,
		turnID: reservation.TurnID, id: reservation.RunID,
	}
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
					text = message.TextContent()
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

func (b *localBashExecution) ID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state.ID
}

func (b *localBashExecution) State() protocol.BashExecution {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *localBashExecution) Wait(ctx context.Context) (protocol.BashExecution, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	failures := 0
	for {
		requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		state, err := b.transport.GetBash(requestContext, b.sessionID, b.ID())
		cancel()
		if err != nil {
			failures++
			if !retryablePollingError(err) || failures >= 6 {
				return protocol.BashExecution{}, err
			}
			if err := waitForRetry(ctx, failures); err != nil {
				return protocol.BashExecution{}, err
			}
			continue
		}
		failures = 0
		b.mu.Lock()
		b.state = state
		b.mu.Unlock()
		if state.Status != protocol.BashExecutionRunning {
			return state, nil
		}
		if err := waitForPoll(ctx, ticker.C); err != nil {
			return protocol.BashExecution{}, err
		}
	}
}

func (b *localBashExecution) Abort(ctx context.Context) error {
	return b.transport.AbortBash(ctx, b.sessionID, b.ID())
}

func (s *localEventStream) Updates() <-chan []protocol.SessionEvent { return s.updates }

func (s *localEventStream) Err() error {
	<-s.done
	return s.err
}

type sessionEventTransport interface {
	GetSessionEvents(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	GetRun(context.Context, string, string) (protocol.RunInfo, error)
}

func (s *localEventStream) poll(
	ctx context.Context,
	transport sessionEventTransport,
	sessionID, runID string,
	batch protocol.SessionEventBatch,
) {
	defer close(s.updates)
	defer close(s.done)
	const (
		pollInterval  = 50 * time.Millisecond
		eventPageSize = 32
	)
	after := int64(0)
	streamID := ""
	failures := 0
	polls := 0
	terminalChecks := 0
	seenRunStart := false
	activeAssistantMessageID := ""
	for {
		if batch.ResyncRequired {
			s.err = errEventResyncRequired
			return
		}
		if batch.StreamID != "" {
			if streamID != "" && batch.StreamID != streamID {
				after = 0
				seenRunStart = false
				activeAssistantMessageID = ""
			}
			streamID = batch.StreamID
		}
		matching := make([]protocol.SessionEvent, 0, len(batch.Events))
		finished := false
		for _, event := range batch.Events {
			if event.Sequence > after {
				after = event.Sequence
			}
			if event.RunID != runID {
				continue
			}
			if !seenRunStart {
				if event.Kind != protocol.SessionEventRunStarted {
					s.err = errEventResyncRequired
					return
				}
				seenRunStart = true
			}
			var err error
			activeAssistantMessageID, err = reduceAssistantMessageID(activeAssistantMessageID, event)
			if err != nil {
				s.err = fmt.Errorf("%w: %v", errEventResyncRequired, err)
				return
			}
			matching = append(matching, event)
			if event.Kind == protocol.SessionEventRunFinished {
				finished = true
			}
		}
		if len(matching) > 0 {
			select {
			case s.updates <- matching:
			case <-ctx.Done():
				s.err = ctx.Err()
				return
			}
		}
		if finished {
			return
		}
		if len(batch.Events) < eventPageSize {
			timer := time.NewTimer(pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.err = ctx.Err()
				return
			case <-timer.C:
			}
		}
		next, err := fetchSessionEvents(ctx, transport, sessionID, streamID, after)
		if err != nil {
			failures++
			if !retryablePollingError(err) || failures >= 6 {
				s.err = err
				return
			}
			if err := waitForRetry(ctx, failures); err != nil {
				s.err = err
				return
			}
			batch = protocol.SessionEventBatch{StreamID: streamID}
			continue
		}
		failures = 0
		batch = next
		polls++
		if polls%4 == 0 {
			requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			info, err := transport.GetRun(requestContext, sessionID, runID)
			cancel()
			if err == nil && info.Status != protocol.RunStatusQueued && info.Status != protocol.RunStatusRunning {
				terminalChecks++
				if terminalChecks >= 3 {
					s.err = errTerminalEventMissing
					return
				}
			} else if err == nil {
				terminalChecks = 0
			}
		}
	}
}

func reduceAssistantMessageID(current string, event protocol.SessionEvent) (string, error) {
	switch event.Kind {
	case protocol.SessionEventRunStarted:
		return "", nil
	case protocol.SessionEventAssistantStarted:
		if current != "" {
			return current, fmt.Errorf("assistant message %q started before %q completed", event.MessageID, current)
		}
		return event.MessageID, nil
	case protocol.SessionEventAssistantTextDelta, protocol.SessionEventThinkingDelta, protocol.SessionEventToolPlanned:
		if current == "" || event.MessageID != current {
			return current, fmt.Errorf("assistant update message id %q does not match active message %q", event.MessageID, current)
		}
		return current, nil
	case protocol.SessionEventAssistantCompleted:
		if current == "" || event.MessageID != current {
			return current, fmt.Errorf("completed assistant message id %q does not match active message %q", event.MessageID, current)
		}
		return "", nil
	case protocol.SessionEventRunFinished:
		if current != "" {
			return current, fmt.Errorf("run finished before assistant message %q completed", current)
		}
	}
	return current, nil
}

func fetchSessionEvents(
	ctx context.Context,
	transport sessionEventTransport,
	sessionID, streamID string,
	after int64,
) (protocol.SessionEventBatch, error) {
	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return transport.GetSessionEvents(requestContext, sessionID, streamID, after)
}
