// Package client composes concrete server and session client transports.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

type localServer struct {
	transport *kitserver.Client
}

type sessionMutationTransport interface {
	ConfigureSession(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	CompactSession(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	GetSessionSnapshot(context.Context, string) (protocol.SessionSnapshot, error)
}

type scratchpadTransport interface {
	GetScratchpad(context.Context, string) (protocol.Scratchpad, error)
	UpdateScratchpad(context.Context, string, protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
}

type localSession struct {
	transport          *kitserver.Client
	mutations          sessionMutationTransport
	scratchpads        scratchpadTransport
	id                 string
	mu                 sync.Mutex
	snapshot           protocol.SessionSnapshot
	cacheGeneration    uint64
	mutationGate       chan struct{}
	pendingCWDTarget   string
	pendingCWDMutation string
	eventStreamID      string
	eventCursor        int64
	eventTurnID        string
	eventTurnStarted   bool
}

type scratchpadLocalSession struct{ *localSession }

var _ sessionclient.ScratchpadSession = (*scratchpadLocalSession)(nil)

type localTurn struct {
	transport *kitserver.Client
	sessionID string
	turnID    string
	id        string
}

type localBashExecution struct {
	transport *kitserver.Client
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
var _ sessionclient.CompatibilityProber = (*localServer)(nil)
var _ sessionclient.Session = (*localSession)(nil)
var _ sessionclient.SessionEventWatcher = (*localSession)(nil)
var _ sessionclient.WorkspaceFilesSession = (*localSession)(nil)
var _ sessionclient.DiffSession = (*localSession)(nil)
var _ sessionclient.WorkingTreeDiffSession = (*localSession)(nil)
var _ sessionclient.AttachmentSession = (*localSession)(nil)
var _ sessionclient.AttachmentMetadataSession = (*localSession)(nil)
var _ sessionclient.SubagentEventReader = (*localSession)(nil)
var _ sessionclient.BashHistorySession = (*localSession)(nil)
var _ sessionclient.Turn = (*localTurn)(nil)
var _ sessionclient.BashExecution = (*localBashExecution)(nil)

// NewLocalServer creates an authenticated loopback server client.
func NewLocalServer(paths apphome.Paths) sessionclient.Server {
	return &localServer{transport: kitserver.NewClient(paths)}
}

func (c *localServer) ProbeCompatibility(ctx context.Context) error {
	_, err := c.transport.ProbeCompatible(ctx)
	return err
}

func (c *localServer) CreateSession(
	ctx context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	return c.transport.CreateSession(ctx, input)
}

func (c *localServer) ForkSession(ctx context.Context, sourceSessionID string, input protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	return c.transport.ForkSession(ctx, sourceSessionID, input)
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

func (c *localServer) RefreshModels(ctx context.Context) (protocol.ModelCatalog, error) {
	return c.transport.RefreshModels(ctx)
}

func (c *localServer) Attach(ctx context.Context, sessionID string) (sessionclient.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is empty")
	}
	snapshot, err := c.transport.GetSessionSnapshot(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	mutationGate := make(chan struct{}, 1)
	mutationGate <- struct{}{}
	bound := &localSession{
		transport: c.transport, mutations: c.transport, scratchpads: c.transport, id: sessionID,
		mutationGate: mutationGate, snapshot: snapshot,
	}
	if snapshot.Session.Temporary {
		return bound, nil
	}
	return &scratchpadLocalSession{localSession: bound}, nil
}

func (c *localSession) ID() string { return c.id }

func (c *localSession) UploadAttachment(ctx context.Context, filename string, content io.Reader) (protocol.AttachmentInfo, error) {
	return c.transport.UploadAttachment(ctx, c.id, filename, content)
}

func (c *localSession) OpenAttachment(ctx context.Context, attachmentID string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	return c.transport.OpenAttachment(ctx, c.id, attachmentID)
}

func (c *localSession) ResolveAttachments(ctx context.Context, attachmentIDs []string) (protocol.AttachmentResolution, error) {
	return c.transport.ResolveAttachments(ctx, c.id, attachmentIDs)
}

func (c *localSession) Subagent(ctx context.Context, input protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	return c.transport.Subagent(ctx, c.id, input)
}

func (c *localSession) SubagentTranscript(ctx context.Context, conversationID, before string) (protocol.SubagentTranscript, error) {
	page, err := c.transport.GetSubagentTranscript(ctx, c.id, conversationID, before)
	var apiErr *kitserver.APIError
	if errors.As(err, &apiErr) && apiErr.Code == string(httpapi.ErrorTranscriptCursorUnavailable) {
		return protocol.SubagentTranscript{}, sessionclient.ErrTranscriptCursorUnavailable
	}
	return page, err
}

func (c *localSession) SubagentEvents(ctx context.Context, conversationID, streamID string, after int64) (protocol.SubagentLiveEventPage, error) {
	return c.transport.GetSubagentEvents(ctx, c.id, conversationID, streamID, after)
}

func (c *localSession) VCSStatus(ctx context.Context) (protocol.SessionVCSStatus, error) {
	return c.transport.GetSessionVCSStatus(ctx, c.id)
}

// vcsStreamIdleLimit allows three missed 15-second heartbeats.
const vcsStreamIdleLimit = 45 * time.Second

// WatchVCS consumes one fresh server-pushed VCS stream. Reconnection policy is
// owned by the attachment; this adapter only classifies terminal failures.
func (c *localSession) WatchVCS(ctx context.Context, receive func(protocol.SessionVCSStatus)) error {
	body, err := c.transport.StreamSessionVCS(ctx, c.id)
	if err != nil {
		return classifyStreamWatchError(err)
	}
	watched, stop := httpapi.WatchStreamIdle(ctx, body, vcsStreamIdleLimit)
	defer stop()
	err = readBoundVCS(ctx, watched, c.id, receive)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		// The stream has no terminal record; an end is a transient failure.
		return fmt.Errorf("daemon session VCS stream ended: %w", io.ErrUnexpectedEOF)
	}
	var terminal *sessionclient.StreamWatchTerminalError
	if errors.As(err, &terminal) {
		return err
	}
	var frame *kitserver.StreamError
	if errors.As(err, &frame) {
		return &sessionclient.StreamWatchTerminalError{Err: err}
	}
	return err
}

func readBoundVCS(ctx context.Context, body io.Reader, sessionID string, receive func(protocol.SessionVCSStatus)) error {
	return kitserver.ReadSessionVCS(body, func(status protocol.SessionVCSStatus) error {
		if status.SessionID != sessionID {
			return &sessionclient.StreamWatchTerminalError{Err: fmt.Errorf("daemon session VCS stream identity mismatch")}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		receive(status)
		return nil
	})
}

func classifyStreamWatchError(err error) error {
	if errors.Is(err, kitserver.ErrIncompatibleDaemon) || sessionclient.IsIncompatibleDaemon(err) {
		return &sessionclient.StreamWatchTerminalError{Err: err}
	}
	var frame *kitserver.StreamError
	if errors.As(err, &frame) {
		return &sessionclient.StreamWatchTerminalError{Err: err}
	}
	var apiError *kitserver.APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			return &sessionclient.StreamWatchTerminalError{Err: err}
		}
	}
	return err
}

func (c *localSession) FileIndex(ctx context.Context) (protocol.SessionFileIndex, error) {
	return c.transport.GetSessionFileIndex(ctx, c.id)
}

func (c *localSession) RefreshFileIndex(ctx context.Context) (protocol.SessionFileIndex, error) {
	return c.transport.RefreshSessionFileIndex(ctx, c.id)
}

func (c *localSession) WorkspaceLimits() protocol.WorkspaceLimits {
	return protocol.DefaultWorkspaceLimits()
}

func (c *localSession) Workspace(ctx context.Context) (protocol.WorkspaceRef, error) {
	result, err := c.transport.GetWorkspace(ctx, c.id)
	return result, projectWorkspaceError(err)
}

func (c *localSession) ListDirectory(ctx context.Context, input protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	result, err := c.transport.ListWorkspaceDirectory(ctx, c.id, input)
	return result, projectWorkspaceError(err)
}

func (c *localSession) ReadWorkspaceFile(ctx context.Context, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	result, err := c.transport.ReadWorkspaceFile(ctx, c.id, input)
	return result, projectWorkspaceError(err)
}

func (c *localSession) ListDiffTargets(ctx context.Context, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	result, err := c.transport.ListDiffTargets(ctx, c.id, input)
	return result, projectDiffError(err)
}
func (c *localSession) ObserveDiff(ctx context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	result, err := c.transport.ObserveDiff(ctx, c.id, input)
	return result, projectDiffError(err)
}
func (c *localSession) ObserveWorkingTree(ctx context.Context, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	result, err := c.transport.ObserveWorkingTree(ctx, c.id, input)
	return result, projectDiffError(err)
}
func (c *localSession) ReadFileDiff(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	result, err := c.transport.ReadFileDiff(ctx, c.id, input)
	return result, projectDiffError(err)
}
func projectDiffError(err error) error {
	var apiError *kitserver.APIError
	if !errors.As(err, &apiError) || apiError.Code == "" {
		return err
	}
	projected := &protocol.DiffError{Code: protocol.DiffErrorCode(apiError.Code), Message: apiError.Message, Details: apiError.Details}
	if projected.Validate() != nil {
		return fmt.Errorf("daemon returned malformed diff error")
	}
	return projected
}

func (c *localSession) ListAnnotations(ctx context.Context, input protocol.ListAnnotationsInput) (protocol.AnnotationPage, error) {
	return c.transport.ListAnnotations(ctx, c.id, input)
}

func (c *localSession) CreateAnnotation(ctx context.Context, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
	return c.transport.CreateAnnotation(ctx, c.id, input)
}

func (c *localSession) UpdateAnnotation(ctx context.Context, input protocol.UpdateAnnotationInput) (protocol.Annotation, error) {
	return c.transport.UpdateAnnotation(ctx, c.id, input)
}

func (c *localSession) DeleteAnnotation(ctx context.Context, input protocol.DeleteAnnotationInput) error {
	return c.transport.DeleteAnnotation(ctx, c.id, input)
}

func projectWorkspaceError(err error) error {
	var apiError *kitserver.APIError
	if !errors.As(err, &apiError) || apiError.Code == "" {
		return err
	}
	projected := &protocol.WorkspaceError{Code: protocol.WorkspaceErrorCode(apiError.Code), Message: apiError.Message, Details: apiError.Details}
	if projected.Validate() != nil {
		return fmt.Errorf("daemon returned malformed workspace error")
	}
	return projected
}

func (c *localSession) MessagePage(ctx context.Context, query protocol.MessagePageQuery) (protocol.MessagePage, error) {
	return c.transport.GetMessagePage(ctx, c.id, query)
}

func (c *localSession) TranscriptPage(ctx context.Context, before string) (protocol.TranscriptPage, error) {
	page, err := c.transport.GetTranscriptPage(ctx, c.id, before)
	var apiErr *kitserver.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return protocol.TranscriptPage{}, sessionclient.ErrTranscriptCursorUnavailable
	}
	return page, err
}

func (c *localSession) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	c.mu.Lock()
	generation := c.cacheGeneration
	c.mu.Unlock()
	snapshot, err := c.transport.GetSessionSnapshot(ctx, c.id)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	c.mu.Lock()
	mergedScratchpad, scratchpadChanged, mergeErr := reconcileScratchpad(c.snapshot.Scratchpad, snapshot.Scratchpad)
	if mergeErr != nil {
		c.mu.Unlock()
		return protocol.SessionSnapshot{}, fmt.Errorf("reconcile snapshot scratchpad: %w", mergeErr)
	}
	snapshot.Scratchpad = mergedScratchpad
	if c.cacheGeneration == generation {
		c.snapshot = snapshot
		if c.eventStreamID != snapshot.EventStreamID {
			c.eventStreamID = ""
			c.eventCursor = 0
			c.eventTurnID = ""
			c.eventTurnStarted = false
		}
		if snapshot.ActiveTurnID != "" && !snapshot.EventReplayAvailable {
			if c.eventStreamID != snapshot.EventStreamID || c.eventTurnID != snapshot.ActiveTurnID || c.eventCursor < snapshot.EventCursor {
				c.eventStreamID = snapshot.EventStreamID
				c.eventCursor = snapshot.EventCursor
				c.eventTurnID = snapshot.ActiveTurnID
				c.eventTurnStarted = true
			}
		}
	} else if scratchpadChanged {
		c.snapshot.Scratchpad = mergedScratchpad
		c.cacheGeneration++
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
	result, err := c.transport.ChangeSessionWorkspaceCWDWithID(ctx, c.id, mutationID, target)
	if err != nil {
		var apiError *kitserver.APIError
		if errors.As(err, &apiError) && apiError.StatusCode < 500 {
			c.clearPendingCWD(mutationID)
			return protocol.SessionInfo{}, err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return protocol.SessionInfo{}, err
		}
		retryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var retryErr error
		result, retryErr = c.transport.ChangeSessionWorkspaceCWDWithID(retryContext, c.id, mutationID, target)
		cancel()
		if retryErr != nil {
			var retryAPIError *kitserver.APIError
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
	c.snapshot.Session = result.Session
	c.snapshot.Workspace = &result.Workspace
	if c.pendingCWDMutation == mutationID {
		c.pendingCWDTarget, c.pendingCWDMutation = "", ""
	}
	c.mu.Unlock()
	return result.Session, nil
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
		var apiError *kitserver.APIError
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
	c.snapshot.ActiveTurnID = ""
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
		var apiError *kitserver.APIError
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
	c.snapshot.ActiveTurnID = ""
	c.snapshot.EventCursor = 0
	c.snapshot.EventReplayFrom = 0
	c.snapshot.EventReplayAvailable = false
	c.snapshot.Warnings = append([]string(nil), result.Warnings...)
	c.mu.Unlock()
	return result, nil
}

func (c *scratchpadLocalSession) Scratchpad(ctx context.Context) (protocol.Scratchpad, error) {
	transport := c.scratchpads
	if transport == nil {
		transport = c.transport
	}
	record, err := transport.GetScratchpad(ctx, c.id)
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	return c.cacheScratchpad(record)
}

func (c *scratchpadLocalSession) UpdateScratchpad(ctx context.Context, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	transport := c.scratchpads
	if transport == nil {
		transport = c.transport
	}
	record, err := transport.UpdateScratchpad(ctx, c.id, input)
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	return c.cacheScratchpad(record)
}

func (c *scratchpadLocalSession) cacheScratchpad(record protocol.Scratchpad) (protocol.Scratchpad, error) {
	return c.localSession.cacheScratchpad(record)
}

func (c *localSession) cacheScratchpad(record protocol.Scratchpad) (protocol.Scratchpad, error) {
	c.mu.Lock()
	merged, changed, err := reconcileScratchpad(c.snapshot.Scratchpad, &record)
	if err == nil && changed {
		c.snapshot.Scratchpad = merged
		c.cacheGeneration++
	}
	c.mu.Unlock()
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	return *merged, nil
}

func reconcileScratchpad(current, incoming *protocol.Scratchpad) (*protocol.Scratchpad, bool, error) {
	if incoming == nil {
		return current, false, nil
	}
	if current == nil {
		copy := *incoming
		return &copy, true, nil
	}
	if current.OwnerSessionID != incoming.OwnerSessionID {
		return nil, false, fmt.Errorf("scratchpad owner identity changed")
	}
	if incoming.Revision < current.Revision {
		copy := *current
		return &copy, false, nil
	}
	if incoming.Revision == current.Revision {
		if *incoming != *current {
			return nil, false, fmt.Errorf("scratchpad revision %d has inconsistent records", incoming.Revision)
		}
		copy := *current
		return &copy, false, nil
	}
	copy := *incoming
	return &copy, true, nil
}

func (c *localSession) reduceScratchpadEvents(events []protocol.SessionEvent) ([]protocol.SessionEvent, error) {
	filtered := make([]protocol.SessionEvent, 0, len(events))
	for _, event := range events {
		payload, ok := event.Payload.(protocol.ScratchpadChangedEvent)
		if event.Kind() != protocol.SessionEventScratchpadChanged || !ok || payload.Scratchpad == nil {
			filtered = append(filtered, event)
			continue
		}
		c.mu.Lock()
		merged, changed, err := reconcileScratchpad(c.snapshot.Scratchpad, payload.Scratchpad)
		if err == nil && changed {
			c.snapshot.Scratchpad = merged
			c.cacheGeneration++
		}
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if changed {
			copy := event
			copy.Payload = protocol.ScratchpadChangedEvent{Scratchpad: merged}
			filtered = append(filtered, copy)
		}
	}
	return filtered, nil
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
	c.snapshot.ActiveTurnID = ""
	c.snapshot.EventCursor = 0
	c.snapshot.EventReplayFrom = 0
	c.snapshot.EventReplayAvailable = false
	c.mu.Unlock()
	return result, nil
}

func (c *localSession) Turn(ctx context.Context, turnID string) (protocol.TurnInfo, error) {
	return c.transport.GetTurn(ctx, c.id, turnID)
}

func (c *localSession) Abort(ctx context.Context, turnID string) error {
	return c.transport.AbortSession(ctx, c.id, turnID)
}

func (c *localSession) RespondInteraction(ctx context.Context, response protocol.InteractionResponse) error {
	return c.transport.RespondInteraction(ctx, c.id, response)
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

func (c *localSession) BashHistory(ctx context.Context, before uint64, limit int) (protocol.BashHistoryPage, error) {
	return c.transport.GetBashHistory(ctx, c.id, before, limit)
}

// Watch returns an exact baseline and all subsequent attachment-scoped events.
func (c *localSession) Watch(ctx context.Context) (protocol.SessionSnapshot, sessionclient.EventStream, error) {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return protocol.SessionSnapshot{}, nil, err
	}
	body, err := c.transport.StreamSessionEvents(ctx, c.id, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil {
		return protocol.SessionSnapshot{}, nil, err
	}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	go stream.readSSE(ctx, body, "", true, "", snapshot.EventStreamID, snapshot.EventCursor, nil, c.reduceScratchpadEvents, true)
	return snapshot, stream, nil
}

func (c *localSession) Stream(ctx context.Context, turnID string) (sessionclient.EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(turnID) == "" {
		return nil, fmt.Errorf("run id is empty")
	}
	c.mu.Lock()
	snapshot := c.snapshot
	streamID := ""
	after := int64(0)
	seenTurnStart := false
	resumedAssistantMessageID := ""
	if snapshot.ActiveTurnID == turnID {
		streamID = snapshot.EventStreamID
		after = snapshot.EventReplayFrom
		if !snapshot.EventReplayAvailable {
			after = snapshot.EventCursor
			seenTurnStart = true
			for index := len(snapshot.Messages) - 1; index >= 0; index-- {
				message := snapshot.Messages[index]
				if message.TurnID == turnID && message.Role == "assistant" && message.StopReason == "" {
					resumedAssistantMessageID = message.ID
					break
				}
			}
		}
		if c.eventStreamID == streamID && c.eventTurnID == turnID && c.eventCursor >= after {
			after = c.eventCursor
			seenTurnStart = c.eventTurnStarted
		}
	}
	c.mu.Unlock()
	body, err := c.transport.StreamSessionEvents(ctx, c.id, streamID, after)
	if err != nil {
		return nil, err
	}
	stream := &localEventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
	go stream.readSSE(ctx, body, turnID, seenTurnStart, resumedAssistantMessageID, streamID, after, func(streamID string, cursor int64, turnStarted bool) {
		c.mu.Lock()
		if c.eventStreamID == "" || (c.eventStreamID == streamID && cursor >= c.eventCursor) {
			c.eventStreamID = streamID
			c.eventCursor = cursor
			c.eventTurnID = turnID
			c.eventTurnStarted = turnStarted
		}
		c.mu.Unlock()
	}, c.reduceScratchpadEvents, false)
	return stream, nil
}

func (c *localSession) StartPrompt(ctx context.Context, text string) (sessionclient.Turn, error) {
	return c.StartPromptInput(ctx, protocol.PromptInput{Text: text})
}

func (c *localSession) StartPromptInput(ctx context.Context, input protocol.PromptInput) (sessionclient.Turn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Text) == "" && len(input.AttachmentIDs) == 0 && len(input.AnnotationIDs) == 0 {
		return nil, fmt.Errorf("prompt is empty")
	}
	reservation, err := c.transport.StartPromptInput(ctx, c.id, input)
	if err != nil {
		return nil, err
	}
	return c.turnFromReservation(reservation), nil
}

func (c *localSession) SubmitPrompt(ctx context.Context, text string) (sessionclient.PromptSubmission, error) {
	return c.SubmitPromptInput(ctx, protocol.PromptInput{Text: text})
}

func (c *localSession) SubmitPromptInput(ctx context.Context, input protocol.PromptInput) (sessionclient.PromptSubmission, error) {
	if err := ctx.Err(); err != nil {
		return sessionclient.PromptSubmission{}, err
	}
	result, err := c.transport.SubmitPromptInput(ctx, c.id, input)
	if err != nil {
		return sessionclient.PromptSubmission{}, err
	}
	output := sessionclient.PromptSubmission{Queued: result.Queued, Queue: result.Queue}
	if result.Reservation != nil {
		output.Turn = c.turnFromReservation(*result.Reservation)
	}
	return output, nil
}

func (c *localSession) RestoreFollowUps(ctx context.Context) (protocol.RestoreFollowUpsResult, error) {
	return c.transport.RestoreFollowUps(ctx, c.id)
}

func (c *localSession) PromoteFollowUps(ctx context.Context) (protocol.PromoteFollowUpsResult, error) {
	return c.transport.PromoteFollowUps(ctx, c.id)
}

func (c *localSession) StartPromptCommand(ctx context.Context, name, args string) (sessionclient.Turn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reservation, err := c.transport.StartPromptCommand(ctx, c.id, protocol.PromptCommandInput{Name: name, Args: args})
	if err != nil {
		return nil, err
	}
	return c.turnFromReservation(reservation), nil
}

func (c *localSession) turnFromReservation(reservation protocol.TurnReservation) sessionclient.Turn {
	return &localTurn{
		transport: c.transport, sessionID: c.id,
		turnID: reservation.TurnID, id: reservation.TurnID,
	}
}

func (r *localTurn) ID() string { return r.id }

func (r *localTurn) Wait(ctx context.Context) (protocol.PromptOutcome, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pollFailures := 0
	snapshotFailures := 0
	for {
		info, err := r.transport.GetTurn(ctx, r.sessionID, r.id)
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
		case protocol.TurnStatusQueued, protocol.TurnStatusRunning:
			if err := waitForPoll(ctx, ticker.C); err != nil {
				return protocol.PromptOutcome{}, err
			}
		case protocol.TurnStatusCompleted:
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
				SessionID: r.sessionID, TurnID: r.turnID,
				Status: protocol.TurnStatusCompleted, Text: text,
			}, nil
		case protocol.TurnStatusFailed, protocol.TurnStatusAborted, protocol.TurnStatusInterrupted:
			return protocol.PromptOutcome{
				SessionID: r.sessionID, TurnID: r.turnID,
				Status: info.Status, ErrorMessage: info.ErrorMessage,
			}, nil
		default:
			return protocol.PromptOutcome{}, fmt.Errorf("daemon returned unknown turn status %q", info.Status)
		}
	}
}

func retryablePollingError(err error) bool {
	if err == nil || errors.Is(err, kitserver.ErrIncompatibleDaemon) {
		return false
	}
	var apiError *kitserver.APIError
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

func (r *localTurn) Abort(ctx context.Context) error {
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

func isSessionScopedEvent(kind protocol.SessionEventKind) bool {
	switch kind {
	case protocol.SessionEventAnnotationCreated, protocol.SessionEventAnnotationUpdated,
		protocol.SessionEventAnnotationDeleted, protocol.SessionEventAnnotationSubmitted,
		protocol.SessionEventScratchpadChanged:
		return true
	default:
		return false
	}
}

func (s *localEventStream) readSSE(
	ctx context.Context,
	body io.ReadCloser,
	turnID string,
	seenTurnStart bool,
	activeAssistantMessageID string,
	expectedStreamID string,
	after int64,
	recordCursor func(string, int64, bool),
	reduceEvents func([]protocol.SessionEvent) ([]protocol.SessionEvent, error),
	allRuns bool,
) {
	defer body.Close()
	defer close(s.updates)
	defer close(s.done)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var data strings.Builder
	finished := false
	resumedFromSnapshot := seenTurnStart
	consume := func() bool {
		if data.Len() == 0 {
			return true
		}
		var batch protocol.SessionEventBatch
		if err := json.Unmarshal([]byte(data.String()), &batch); err != nil {
			s.err = fmt.Errorf("decode session event stream: %w", err)
			return false
		}
		data.Reset()
		if err := batch.Validate(); err != nil {
			s.err = fmt.Errorf("validate session event stream: %w", err)
			return false
		}
		if batch.ResyncRequired {
			s.err = errEventResyncRequired
			return false
		}
		if expectedStreamID != "" && batch.StreamID != expectedStreamID {
			s.err = errEventResyncRequired
			return false
		}
		if expectedStreamID == "" {
			expectedStreamID = batch.StreamID
		}
		if len(batch.Events) > 0 && batch.Events[0].Sequence != after+1 {
			s.err = errEventResyncRequired
			return false
		}
		receivedEvents := batch.Events
		cursor := int64(0)
		for _, event := range receivedEvents {
			if event.Sequence > cursor {
				cursor = event.Sequence
			}
		}
		if reduceEvents != nil {
			var reduceErr error
			batch.Events, reduceErr = reduceEvents(receivedEvents)
			if reduceErr != nil {
				s.err = fmt.Errorf("%w: %v", errEventResyncRequired, reduceErr)
				return false
			}
		}
		matching := make([]protocol.SessionEvent, 0, len(batch.Events))
		for _, event := range batch.Events {
			if allRuns {
				matching = append(matching, event)
				continue
			}
			if event.TurnID != turnID {
				if isSessionScopedEvent(event.Kind()) {
					matching = append(matching, event)
				}
				continue
			}
			if !seenTurnStart {
				if event.Kind() != protocol.SessionEventTurnStarted {
					s.err = errEventResyncRequired
					return false
				}
				seenTurnStart = true
			}
			if resumedFromSnapshot && activeAssistantMessageID == "" {
				switch event.Kind() {
				case protocol.SessionEventAssistantTextDelta, protocol.SessionEventThinkingDelta, protocol.SessionEventToolPlanned:
					activeAssistantMessageID = sessionEventMessageID(event)
				case protocol.SessionEventAssistantCompleted:
					matching = append(matching, event)
					continue
				}
			}
			var err error
			activeAssistantMessageID, err = reduceAssistantMessageID(activeAssistantMessageID, event)
			if err != nil {
				s.err = fmt.Errorf("%w: %v", errEventResyncRequired, err)
				return false
			}
			matching = append(matching, event)
			if event.Kind() == protocol.SessionEventTurnStarted || event.Kind() == protocol.SessionEventAssistantStarted {
				resumedFromSnapshot = false
			}
			finished = finished || event.Kind() == protocol.SessionEventTurnCompleted
		}
		if len(matching) > 0 {
			select {
			case s.updates <- matching:
			case <-ctx.Done():
				s.err = ctx.Err()
				return false
			}
		}
		if cursor > 0 {
			after = cursor
			if recordCursor != nil {
				recordCursor(batch.StreamID, cursor, seenTurnStart)
			}
		}
		return !finished
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !consume() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		s.err = fmt.Errorf("read session event stream: %w", err)
		return
	}
	if ctx.Err() != nil {
		s.err = ctx.Err()
		return
	}
	if !finished {
		s.err = errTerminalEventMissing
	}
}

func sessionEventMessageID(event protocol.SessionEvent) string {
	switch payload := event.Payload.(type) {
	case protocol.AssistantStartedEvent:
		return payload.MessageID
	case protocol.AssistantTextDeltaEvent:
		return payload.MessageID
	case protocol.ThinkingDeltaEvent:
		return payload.MessageID
	case protocol.AssistantCompletedEvent:
		return payload.MessageID
	case protocol.ToolPlannedEvent:
		return payload.MessageID
	}
	return ""
}

func reduceAssistantMessageID(current string, event protocol.SessionEvent) (string, error) {
	switch event.Kind() {
	case protocol.SessionEventTurnStarted:
		return "", nil
	case protocol.SessionEventAssistantStarted:
		if current != "" {
			return current, fmt.Errorf("assistant message %q started before %q completed", sessionEventMessageID(event), current)
		}
		return sessionEventMessageID(event), nil
	case protocol.SessionEventAssistantTextDelta, protocol.SessionEventThinkingDelta, protocol.SessionEventToolPlanned:
		if current == "" || sessionEventMessageID(event) != current {
			return current, fmt.Errorf("assistant update message id %q does not match active message %q", sessionEventMessageID(event), current)
		}
		return current, nil
	case protocol.SessionEventAssistantCompleted:
		if current == "" || sessionEventMessageID(event) != current {
			return current, fmt.Errorf("completed assistant message id %q does not match active message %q", sessionEventMessageID(event), current)
		}
		return "", nil
	case protocol.SessionEventTurnCompleted:
		if current != "" {
			return current, fmt.Errorf("run finished before assistant message %q completed", current)
		}
	}
	return current, nil
}

var _ sessionclient.PluginCommandSession = (*localSession)(nil)

func (c *localSession) ExecutePluginCommand(ctx context.Context, input protocol.PluginCommandInput) error {
	return c.transport.ExecutePluginCommand(ctx, c.id, input)
}
