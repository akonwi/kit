package kit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/identifier"
)

type sessionMutationTransport interface {
	ConfigureSession(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	CompactSession(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	GetSessionSnapshot(context.Context, string) (protocol.SessionSnapshot, error)
}

type subagentConfigurationTransport interface {
	ConfigureSubagent(context.Context, string, string, protocol.ConfigureSubagentInput) (protocol.ConfigureSubagentResult, error)
}

type scratchpadTransport interface {
	GetScratchpad(context.Context, string) (protocol.Scratchpad, error)
	UpdateScratchpad(context.Context, string, protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
}

type sessionTransport interface {
	sessionMutationTransport
	scratchpadTransport
	UploadAttachment(context.Context, string, string, io.Reader) (protocol.AttachmentInfo, error)
	OpenAttachment(context.Context, string, string) (protocol.AttachmentInfo, io.ReadCloser, error)
	ResolveAttachments(context.Context, string, []string) (protocol.AttachmentResolution, error)
	Subagent(context.Context, string, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	GetSubagentTranscript(context.Context, string, string, string) (protocol.SubagentTranscript, error)
	GetSubagentEvents(context.Context, string, string, string, int64) (protocol.SubagentLiveEventPage, error)
	GetSessionVCSStatus(context.Context, string) (protocol.SessionVCSStatus, error)
	StreamSessionVCS(context.Context, string) (io.ReadCloser, error)
	GetSessionFileIndex(context.Context, string) (protocol.SessionFileIndex, error)
	RefreshSessionFileIndex(context.Context, string) (protocol.SessionFileIndex, error)
	GetWorkspace(context.Context, string) (protocol.WorkspaceRef, error)
	ListWorkspaceDirectory(context.Context, string, protocol.ListDirectoryInput) (protocol.DirectoryPage, error)
	ReadWorkspaceFile(context.Context, string, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error)
	ListDiffTargets(context.Context, string, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error)
	ObserveDiff(context.Context, string, protocol.ObserveDiffInput) (protocol.DiffPage, error)
	ObserveWorkingTree(context.Context, string, protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error)
	ReadFileDiff(context.Context, string, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error)
	ListAnnotations(context.Context, string, protocol.ListAnnotationsInput) (protocol.AnnotationPage, error)
	CreateAnnotation(context.Context, string, protocol.CreateAnnotationInput) (protocol.Annotation, error)
	UpdateAnnotation(context.Context, string, protocol.UpdateAnnotationInput) (protocol.Annotation, error)
	DeleteAnnotation(context.Context, string, protocol.DeleteAnnotationInput) error
	GetMessagePage(context.Context, string, protocol.MessagePageQuery) (protocol.MessagePage, error)
	GetTranscriptPage(context.Context, string, string) (protocol.TranscriptPage, error)
	ChangeSessionWorkspaceCWDWithID(context.Context, string, string, string) (protocol.ChangeWorkspaceCWDResult, error)
	ReloadSession(context.Context, string) (protocol.ReloadSessionResult, error)
	GetTurn(context.Context, string, string) (protocol.TurnInfo, error)
	AbortSession(context.Context, string, string) error
	RespondInteraction(context.Context, string, protocol.InteractionResponse) error
	GetBash(context.Context, string, string) (protocol.BashExecution, error)
	StartBash(context.Context, string, protocol.BashExecutionInput) (protocol.BashExecution, error)
	AbortBash(context.Context, string, string) error
	GetBashHistory(context.Context, string, uint64, int) (protocol.BashHistoryPage, error)
	StreamSessionEvents(context.Context, string, string, int64) (io.ReadCloser, error)
	StartPromptInput(context.Context, string, protocol.PromptInput) (protocol.TurnReservation, error)
	SubmitPromptInput(context.Context, string, protocol.PromptInput) (protocol.PromptSubmission, error)
	RestoreFollowUps(context.Context, string) (protocol.RestoreFollowUpsResult, error)
	PromoteFollowUps(context.Context, string) (protocol.PromoteFollowUpsResult, error)
	SubmitPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.PromptSubmission, error)
	StartPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.TurnReservation, error)
	StreamPluginToasts(context.Context, string) (io.ReadCloser, error)
	ExecutePluginCommand(context.Context, string, protocol.PluginCommandInput) error
}

// Session is a stateful handle bound to one immutable session identity.
type Session struct {
	client             *Client
	transport          sessionTransport
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

// Turn is a submitted conversation turn.
type Turn struct {
	client    *Client
	transport sessionTransport
	sessionID string
	turnID    string
	id        string
}

// BashExecution is one stateful bash execution handle.
type BashExecution struct {
	client    *Client
	transport sessionTransport
	sessionID string
	mu        sync.RWMutex
	state     protocol.BashExecution
}

// EventStream delivers ordered batches for one turn-oriented event stream.
type EventStream struct {
	updates chan []protocol.SessionEvent
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
	err     error
}

type sessionReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (r *sessionReadCloser) Read(buffer []byte) (int, error) {
	count, err := r.ReadCloser.Read(buffer)
	return count, projectError(err)
}

func (r *sessionReadCloser) Close() error {
	r.once.Do(r.cancel)
	return r.ReadCloser.Close()
}

// SessionUpdate is either an ordered event batch or an authoritative
// replacement snapshot after attachment or replay resynchronization.
type SessionUpdate struct {
	Snapshot *SessionSnapshot
	Events   []SessionEvent
}

// SessionUpdateStream delivers a session attachment across reconnects.
type SessionUpdateStream struct {
	updates chan SessionUpdate
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
	err     error
}

// PromptSubmission reports whether a prompt started immediately or was queued.
type PromptSubmission struct {
	Turn   *Turn
	Queued bool
	Queue  protocol.FollowUpQueue
}

// StreamWatchTerminalError marks an error for which reconnecting cannot recover.
type StreamWatchTerminalError struct{ Err error }

func (e *StreamWatchTerminalError) Error() string {
	if e == nil || e.Err == nil {
		return "session stream terminated"
	}
	return e.Err.Error()
}
func (e *StreamWatchTerminalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var (
	errEventResyncRequired  = errors.New("session event replay requires snapshot resynchronization")
	errEventStreamFinished  = errors.New("session event stream reached its terminal event")
	errTerminalEventMissing = errors.New("session event stream ended without a terminal event")
	// ErrTranscriptCursorUnavailable indicates that the requested transcript cursor is no longer available.
	ErrTranscriptCursorUnavailable = errors.New("transcript cursor unavailable")
)

func newSession(client *Client, transport sessionTransport, sessionID string, snapshot protocol.SessionSnapshot) *Session {
	if concrete, ok := transport.(*clienttransport.Client); ok && client != nil {
		transport = concrete.WithLifetime(client.lifetime, ErrClientClosed)
	}
	mutationGate := make(chan struct{}, 1)
	mutationGate <- struct{}{}
	return &Session{
		client: client, transport: transport, mutations: transport, scratchpads: transport,
		id: sessionID, mutationGate: mutationGate, snapshot: snapshot,
	}
}

// ID returns the immutable bound session identity.
func (c *Session) ID() string {
	if c == nil {
		return ""
	}
	return c.id
}

func (c *Session) preflight(ctx context.Context) error {
	if c == nil {
		return errors.New("session handle is nil")
	}
	if c.client != nil {
		return c.client.preflight(ctx)
	}
	if ctx == nil {
		return errors.New("operation context is nil")
	}
	return ctx.Err()
}

func handleOperationContext(client *Client, ctx context.Context) (context.Context, func(), error) {
	if client != nil {
		return client.operationContext(ctx)
	}
	if ctx == nil {
		return nil, nil, errors.New("operation context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	operation, cancel := context.WithCancel(ctx)
	return operation, cancel, nil
}

func sessionResult[T any](c *Session, ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer cancel()
	return projectResult(call(operation))
}

func sessionProjectedResult[T any](c *Session, ctx context.Context, project func(error) error, call func(context.Context) (T, error)) (T, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer cancel()
	value, err := call(operation)
	return value, project(err)
}

func sessionError(c *Session, ctx context.Context, call func(context.Context) error) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return projectError(call(operation))
}

func (c *Session) operationContext(ctx context.Context) (context.Context, func(), error) {
	if err := c.preflight(ctx); err != nil {
		return nil, nil, err
	}
	if c.client != nil {
		return c.client.operationContext(ctx)
	}
	operation, cancel := context.WithCancel(ctx)
	return operation, cancel, operation.Err()
}

func (c *Session) UploadAttachment(ctx context.Context, filename string, content io.Reader) (protocol.AttachmentInfo, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.AttachmentInfo, error) {
		return c.transport.UploadAttachment(operation, c.id, filename, content)
	})
}

func (c *Session) OpenAttachment(ctx context.Context, attachmentID string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return protocol.AttachmentInfo{}, nil, err
	}
	info, content, err := c.transport.OpenAttachment(operation, c.id, attachmentID)
	if err != nil {
		cancel()
		return info, content, projectError(err)
	}
	return info, &sessionReadCloser{ReadCloser: content, cancel: cancel}, nil
}

func (c *Session) ResolveAttachments(ctx context.Context, attachmentIDs []string) (protocol.AttachmentResolution, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.AttachmentResolution, error) {
		return c.transport.ResolveAttachments(operation, c.id, attachmentIDs)
	})
}

func (c *Session) Subagent(ctx context.Context, input protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.SubagentOperationResult, error) {
		return c.transport.Subagent(operation, c.id, input)
	})
}

func (c *Session) SubagentTranscript(ctx context.Context, conversationID, before string) (protocol.SubagentTranscript, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return protocol.SubagentTranscript{}, err
	}
	defer cancel()
	page, err := c.transport.GetSubagentTranscript(operation, c.id, conversationID, before)
	var apiErr *clienttransport.APIError
	if errors.As(err, &apiErr) && apiErr.Code == string(ErrorTranscriptCursorUnavailable) {
		return protocol.SubagentTranscript{}, ErrTranscriptCursorUnavailable
	}
	return page, projectError(err)
}

func (c *Session) SubagentEvents(ctx context.Context, conversationID, streamID string, after int64) (protocol.SubagentLiveEventPage, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.SubagentLiveEventPage, error) {
		return c.transport.GetSubagentEvents(operation, c.id, conversationID, streamID, after)
	})
}

func (c *Session) VCSStatus(ctx context.Context) (protocol.SessionVCSStatus, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.SessionVCSStatus, error) {
		return c.transport.GetSessionVCSStatus(operation, c.id)
	})
}

// vcsStreamIdleLimit allows three missed 15-second heartbeats.
const vcsStreamIdleLimit = 45 * time.Second

// WatchVCS delivers repository status updates until cancellation or a terminal
// server failure. Transient opens, idle timeouts, and disconnects reconnect
// internally with bounded backoff.
func (c *Session) WatchVCS(ctx context.Context, receive func(protocol.SessionVCSStatus)) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	failure := 0
	for {
		body, err := c.transport.StreamSessionVCS(operation, c.id)
		if err == nil {
			watched, stop := httpapi.WatchStreamIdle(operation, body, vcsStreamIdleLimit)
			err = readBoundVCS(operation, watched, c.id, receive)
			stop()
			_ = body.Close()
		}
		if operation.Err() != nil {
			return operation.Err()
		}
		err = classifyStreamWatchError(err)
		var terminal *StreamWatchTerminalError
		if errors.As(err, &terminal) {
			return err
		}
		failure++
		if err := waitForRetry(operation, failure); err != nil {
			return err
		}
	}
}

func readBoundVCS(ctx context.Context, body io.Reader, sessionID string, receive func(protocol.SessionVCSStatus)) error {
	return clienttransport.ReadSessionVCS(body, func(status protocol.SessionVCSStatus) error {
		if status.SessionID != sessionID {
			return &StreamWatchTerminalError{Err: fmt.Errorf("daemon session VCS stream identity mismatch")}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		receive(status)
		return nil
	})
}

func classifyStreamWatchError(err error) error {
	if errors.Is(err, clienttransport.ErrIncompatibleServer) {
		return &StreamWatchTerminalError{Err: projectError(err)}
	}
	var frame *clienttransport.StreamError
	if errors.As(err, &frame) {
		return &StreamWatchTerminalError{Err: projectError(err)}
	}
	var protocolFailure *clienttransport.ProtocolError
	if errors.As(err, &protocolFailure) {
		return &StreamWatchTerminalError{Err: projectError(err)}
	}
	var apiError *clienttransport.APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusUpgradeRequired:
			return &StreamWatchTerminalError{Err: projectError(err)}
		}
	}
	return err
}

func (c *Session) FileIndex(ctx context.Context) (protocol.SessionFileIndex, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.SessionFileIndex, error) {
		return c.transport.GetSessionFileIndex(operation, c.id)
	})
}

func (c *Session) RefreshFileIndex(ctx context.Context) (protocol.SessionFileIndex, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.SessionFileIndex, error) {
		return c.transport.RefreshSessionFileIndex(operation, c.id)
	})
}

func (c *Session) WorkspaceLimits() protocol.WorkspaceLimits {
	return protocol.DefaultWorkspaceLimits()
}

func (c *Session) Workspace(ctx context.Context) (protocol.WorkspaceRef, error) {
	return sessionProjectedResult(c, ctx, projectWorkspaceError, func(operation context.Context) (protocol.WorkspaceRef, error) {
		return c.transport.GetWorkspace(operation, c.id)
	})
}

func (c *Session) ListDirectory(ctx context.Context, input protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	return sessionProjectedResult(c, ctx, projectWorkspaceError, func(operation context.Context) (protocol.DirectoryPage, error) {
		return c.transport.ListWorkspaceDirectory(operation, c.id, input)
	})
}

func (c *Session) ReadWorkspaceFile(ctx context.Context, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	return sessionProjectedResult(c, ctx, projectWorkspaceError, func(operation context.Context) (protocol.WorkspaceFileRead, error) {
		return c.transport.ReadWorkspaceFile(operation, c.id, input)
	})
}

func (c *Session) ListDiffTargets(ctx context.Context, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	return sessionProjectedResult(c, ctx, projectDiffError, func(operation context.Context) (protocol.DiffTargetCatalog, error) {
		return c.transport.ListDiffTargets(operation, c.id, input)
	})
}
func (c *Session) ObserveDiff(ctx context.Context, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	return sessionProjectedResult(c, ctx, projectDiffError, func(operation context.Context) (protocol.DiffPage, error) {
		return c.transport.ObserveDiff(operation, c.id, input)
	})
}
func (c *Session) ObserveWorkingTree(ctx context.Context, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	return sessionProjectedResult(c, ctx, projectDiffError, func(operation context.Context) (protocol.WorkingTreePage, error) {
		return c.transport.ObserveWorkingTree(operation, c.id, input)
	})
}
func (c *Session) ReadFileDiff(ctx context.Context, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	return sessionProjectedResult(c, ctx, projectDiffError, func(operation context.Context) (protocol.FileDiffPage, error) {
		return c.transport.ReadFileDiff(operation, c.id, input)
	})
}

// projectDiffError projects a diff operation failure. Diff error codes become
// *protocol.DiffError values carrying their decoded details. The generic codes
// diff operations also declare, such as internal or invalid_request, are
// projected like any other server error; the transport has already rejected
// codes the operation does not declare.
func projectDiffError(err error) error {
	var apiError *clienttransport.APIError
	if !errors.As(err, &apiError) || !protocol.DiffErrorCode(apiError.Code).Valid() {
		return projectError(err)
	}
	details, ok := diffErrorDetails(apiError)
	projected := &protocol.DiffError{Code: protocol.DiffErrorCode(apiError.Code), Message: apiError.Message, Details: details}
	if !ok || projected.Validate() != nil {
		return fmt.Errorf("daemon returned malformed diff error")
	}
	return projected
}

// diffErrorDetails flattens the details of a diff error into the string map
// carried by protocol.DiffError. The transport decodes declared details into
// typed records, so those are re-encoded through their JSON field names.
func diffErrorDetails(apiError *clienttransport.APIError) (map[string]string, bool) {
	if apiError.TypedDetails == nil {
		return apiError.Details, true
	}
	encoded, err := json.Marshal(apiError.TypedDetails)
	if err != nil {
		return nil, false
	}
	var details map[string]string
	if err := json.Unmarshal(encoded, &details); err != nil {
		return nil, false
	}
	return details, true
}

func (c *Session) ListAnnotations(ctx context.Context, input protocol.ListAnnotationsInput) (protocol.AnnotationPage, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.AnnotationPage, error) {
		return c.transport.ListAnnotations(operation, c.id, input)
	})
}

func (c *Session) CreateAnnotation(ctx context.Context, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.Annotation, error) {
		return c.transport.CreateAnnotation(operation, c.id, input)
	})
}

func (c *Session) UpdateAnnotation(ctx context.Context, input protocol.UpdateAnnotationInput) (protocol.Annotation, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.Annotation, error) {
		return c.transport.UpdateAnnotation(operation, c.id, input)
	})
}

func (c *Session) DeleteAnnotation(ctx context.Context, input protocol.DeleteAnnotationInput) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.DeleteAnnotation(operation, c.id, input)
	})
}

func projectWorkspaceError(err error) error {
	var apiError *clienttransport.APIError
	if !errors.As(err, &apiError) || apiError.Code == "" {
		return projectError(err)
	}
	projected := &protocol.WorkspaceError{Code: protocol.WorkspaceErrorCode(apiError.Code), Message: apiError.Message, Details: apiError.Details}
	if projected.Validate() != nil {
		return fmt.Errorf("daemon returned malformed workspace error")
	}
	return projected
}

func (c *Session) MessagePage(ctx context.Context, query protocol.MessagePageQuery) (protocol.MessagePage, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.MessagePage, error) {
		return c.transport.GetMessagePage(operation, c.id, query)
	})
}

func (c *Session) TranscriptPage(ctx context.Context, before string) (protocol.TranscriptPage, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return protocol.TranscriptPage{}, err
	}
	defer cancel()
	page, err := c.transport.GetTranscriptPage(operation, c.id, before)
	var apiErr *clienttransport.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return protocol.TranscriptPage{}, ErrTranscriptCursorUnavailable
	}
	return page, projectError(err)
}

func (c *Session) Snapshot(ctx context.Context) (protocol.SessionSnapshot, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	defer cancel()
	c.mu.Lock()
	generation := c.cacheGeneration
	c.mu.Unlock()
	snapshot, err := c.transport.GetSessionSnapshot(operation, c.id)
	if err != nil {
		return protocol.SessionSnapshot{}, projectError(err)
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

func (c *Session) ChangeCWD(ctx context.Context, target string) (protocol.SessionInfo, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	defer cleanup()
	ctx = operation
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
			return protocol.SessionInfo{}, projectError(err)
		}
	}
	if err := (protocol.ChangeCWDInput{MutationID: mutationID, Path: target}).Validate(); err != nil {
		return protocol.SessionInfo{}, projectError(err)
	}
	if newMutation {
		c.mu.Lock()
		c.pendingCWDTarget, c.pendingCWDMutation = target, mutationID
		c.mu.Unlock()
	}
	result, err := c.transport.ChangeSessionWorkspaceCWDWithID(ctx, c.id, mutationID, target)
	if err != nil {
		var apiError *clienttransport.APIError
		if errors.As(err, &apiError) && apiError.StatusCode < 500 {
			c.clearPendingCWD(mutationID)
			return protocol.SessionInfo{}, projectError(err)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return protocol.SessionInfo{}, projectError(err)
		}
		retryContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		var retryErr error
		result, retryErr = c.transport.ChangeSessionWorkspaceCWDWithID(retryContext, c.id, mutationID, target)
		cancel()
		if retryErr != nil {
			var retryAPIError *clienttransport.APIError
			if errors.As(retryErr, &retryAPIError) {
				if retryAPIError.StatusCode < 500 {
					c.clearPendingCWD(mutationID)
				}
				return protocol.SessionInfo{}, projectError(retryErr)
			}
			return protocol.SessionInfo{}, projectError(err)
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

func (c *Session) clearPendingCWD(mutationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingCWDMutation == mutationID {
		c.pendingCWDTarget, c.pendingCWDMutation = "", ""
	}
}

func (c *Session) Reload(ctx context.Context) (protocol.ReloadSessionResult, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.ReloadSessionResult{}, err
	}
	defer cleanup()
	ctx = operation
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-ctx.Done():
		return protocol.ReloadSessionResult{}, ctx.Err()
	}
	result, err := c.transport.ReloadSession(ctx, c.id)
	if err != nil {
		var apiError *clienttransport.APIError
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
		return protocol.ReloadSessionResult{}, projectError(err)
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

func (c *Session) Configure(ctx context.Context, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.ConfigureSessionResult{}, err
	}
	defer cleanup()
	ctx = operation
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
		var apiError *clienttransport.APIError
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
		return protocol.ConfigureSessionResult{}, projectError(err)
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

// ConfigureSubagent patches one retained child conversation's model and/or thinking.
func (c *Session) ConfigureSubagent(ctx context.Context, conversationID string, input protocol.ConfigureSubagentInput) (protocol.ConfigureSubagentResult, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.ConfigureSubagentResult{}, err
	}
	defer cleanup()
	select {
	case <-c.mutationGate:
		defer func() { c.mutationGate <- struct{}{} }()
	case <-operation.Done():
		return protocol.ConfigureSubagentResult{}, operation.Err()
	}
	transport, ok := c.transport.(subagentConfigurationTransport)
	if !ok {
		return protocol.ConfigureSubagentResult{}, &UnsupportedError{Capability: "subagent configuration"}
	}
	result, err := transport.ConfigureSubagent(operation, c.id, conversationID, input)
	if err != nil {
		var apiError *clienttransport.APIError
		if !errors.As(err, &apiError) || apiError.StatusCode == 409 || apiError.StatusCode >= 500 {
			inspectContext, cancel := context.WithTimeout(context.WithoutCancel(operation), 3*time.Second)
			snapshot, inspectErr := c.transport.GetSessionSnapshot(inspectContext, c.id)
			cancel()
			if inspectErr == nil {
				c.mu.Lock()
				c.cacheGeneration++
				c.snapshot = snapshot
				c.mu.Unlock()
			}
		}
		return protocol.ConfigureSubagentResult{}, projectError(err)
	}
	c.mu.Lock()
	c.cacheGeneration++
	updated := false
	for index := range c.snapshot.SubagentConversations {
		if c.snapshot.SubagentConversations[index].ID == conversationID {
			c.snapshot.SubagentConversations[index] = result.Conversation
			updated = true
			break
		}
	}
	if !updated {
		c.snapshot.SubagentConversations = append(c.snapshot.SubagentConversations, result.Conversation)
	}
	c.mu.Unlock()
	return result, nil
}

func (c *Session) Scratchpad(ctx context.Context) (protocol.Scratchpad, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	defer cleanup()
	if c.isTemporary() {
		return protocol.Scratchpad{}, &UnsupportedError{Capability: "scratchpad"}
	}
	transport := c.scratchpads
	if transport == nil {
		transport = c.transport
	}
	record, err := transport.GetScratchpad(operation, c.id)
	if err != nil {
		return protocol.Scratchpad{}, projectError(err)
	}
	return c.cacheScratchpad(record)
}

func (c *Session) UpdateScratchpad(ctx context.Context, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	defer cleanup()
	if c.isTemporary() {
		return protocol.Scratchpad{}, &UnsupportedError{Capability: "scratchpad"}
	}
	transport := c.scratchpads
	if transport == nil {
		transport = c.transport
	}
	record, err := transport.UpdateScratchpad(operation, c.id, input)
	if err != nil {
		return protocol.Scratchpad{}, projectError(err)
	}
	return c.cacheScratchpad(record)
}

func (c *Session) isTemporary() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot.Session.Temporary
}

func (c *Session) cacheScratchpad(record protocol.Scratchpad) (protocol.Scratchpad, error) {
	c.mu.Lock()
	merged, changed, err := reconcileScratchpad(c.snapshot.Scratchpad, &record)
	if err == nil && changed {
		c.snapshot.Scratchpad = merged
		c.cacheGeneration++
	}
	c.mu.Unlock()
	if err != nil {
		return protocol.Scratchpad{}, projectError(err)
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

func (c *Session) reduceScratchpadEvents(events []protocol.SessionEvent) ([]protocol.SessionEvent, error) {
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
			return nil, projectError(err)
		}
		if changed {
			copy := event
			copy.Payload = protocol.ScratchpadChangedEvent{Scratchpad: merged}
			filtered = append(filtered, copy)
		}
	}
	return filtered, nil
}

func (c *Session) Compact(ctx context.Context, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return protocol.CompactSessionResult{}, err
	}
	defer cleanup()
	ctx = operation
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
		return protocol.CompactSessionResult{}, projectError(err)
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

func (c *Session) Turn(ctx context.Context, turnID string) (protocol.TurnInfo, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.TurnInfo, error) {
		return c.transport.GetTurn(operation, c.id, turnID)
	})
}

func (c *Session) Abort(ctx context.Context, turnID string) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.AbortSession(operation, c.id, turnID)
	})
}

func (c *Session) RespondInteraction(ctx context.Context, response protocol.InteractionResponse) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.RespondInteraction(operation, c.id, response)
	})
}

func (c *Session) Bash(ctx context.Context, executionID string) (*BashExecution, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	requestContext, cancel := context.WithTimeout(operation, 3*time.Second)
	defer cancel()
	state, err := c.transport.GetBash(requestContext, c.id, executionID)
	if err != nil {
		return nil, projectError(err)
	}
	return &BashExecution{client: c.client, transport: c.transport, sessionID: c.id, state: state}, nil
}

// RunBash starts or resumes an idempotent direct shell execution.
func (c *Session) RunBash(ctx context.Context, input BashExecutionInput) (*BashExecution, error) {
	return c.StartBash(ctx, input.ExecutionID, input.Command, input.ExcludeFromContext)
}

func (c *Session) StartBash(ctx context.Context, executionID, command string, excludeFromContext bool) (*BashExecution, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ctx = operation
	input := protocol.BashExecutionInput{
		ExecutionID: executionID, Command: command, ExcludeFromContext: excludeFromContext,
	}
	state, err := c.transport.StartBash(ctx, c.id, input)
	if err != nil {
		inspectContext, cancel := context.WithTimeout(context.Background(), time.Second)
		resolved, inspectErr := c.transport.GetBash(inspectContext, c.id, executionID)
		cancel()
		if inspectErr != nil {
			return nil, projectError(err)
		}
		state = resolved
	}
	return &BashExecution{client: c.client, transport: c.transport, sessionID: c.id, state: state}, nil
}

func (c *Session) AbortBash(ctx context.Context, executionID string) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.AbortBash(operation, c.id, executionID)
	})
}

func (c *Session) BashHistory(ctx context.Context, before uint64, limit int) (protocol.BashHistoryPage, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.BashHistoryPage, error) {
		return c.transport.GetBashHistory(operation, c.id, before, limit)
	})
}

// Watch returns a session update stream. Its first value is an authoritative
// snapshot; replay gaps produce later replacement snapshots before events
// continue. Transient disconnects reconnect from the last delivered cursor.
func (c *Session) Watch(ctx context.Context) (*SessionUpdateStream, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := c.Snapshot(operation)
	if err != nil {
		cancel()
		return nil, projectError(err)
	}
	stream := &SessionUpdateStream{
		updates: make(chan SessionUpdate, 16), done: make(chan struct{}), cancel: cancel,
	}
	go stream.run(operation, c, snapshot)
	return stream, nil
}

func (s *SessionUpdateStream) run(ctx context.Context, session *Session, snapshot SessionSnapshot) {
	defer close(s.done)
	defer close(s.updates)
	defer s.once.Do(s.cancel)

	streamID, cursor := snapshot.EventStreamID, snapshot.EventCursor
	if !s.deliver(ctx, SessionUpdate{Snapshot: snapshotCopy(snapshot)}) {
		return
	}
	failure := 0
	for {
		body, err := session.transport.StreamSessionEvents(ctx, session.id, streamID, cursor)
		if err != nil {
			if s.handleStreamFailure(ctx, session, &streamID, &cursor, err, &failure) {
				continue
			}
			return
		}
		one := &EventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{})}
		cursorUpdates := make(chan int64)
		startCursor := cursor
		go func() {
			one.readSSE(ctx, body, "", true, "", streamID, startCursor, func(_ string, delivered int64, _ bool) {
				select {
				case cursorUpdates <- delivered:
				case <-ctx.Done():
				}
			}, session.reduceScratchpadEvents, true)
			close(cursorUpdates)
		}()
		eventsChannel := one.Updates()
		for eventsChannel != nil || cursorUpdates != nil {
			select {
			case events, ok := <-eventsChannel:
				if !ok {
					eventsChannel = nil
					continue
				}
				if !s.deliver(ctx, SessionUpdate{Events: events}) {
					s.once.Do(s.cancel)
					_ = one.rawErr()
					return
				}
				failure = 0
			case delivered, ok := <-cursorUpdates:
				if !ok {
					cursorUpdates = nil
					continue
				}
				cursor = delivered
				session.recordDeliveredCursor(streamID, cursor)
			}
		}
		err = one.rawErr()
		if s.handleStreamFailure(ctx, session, &streamID, &cursor, err, &failure) {
			continue
		}
		return
	}
}

func (s *SessionUpdateStream) handleStreamFailure(
	ctx context.Context,
	session *Session,
	streamID *string,
	cursor *int64,
	err error,
	failure *int,
) bool {
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, errEventResyncRequired) {
		snapshot, snapshotErr := session.Snapshot(ctx)
		if snapshotErr != nil {
			s.err = projectError(snapshotErr)
			return false
		}
		*streamID, *cursor = snapshot.EventStreamID, snapshot.EventCursor
		*failure = 0
		return s.deliver(ctx, SessionUpdate{Snapshot: snapshotCopy(snapshot)})
	}
	classified := classifyStreamWatchError(err)
	var terminal *StreamWatchTerminalError
	if errors.As(classified, &terminal) {
		s.err = classified
		return false
	}
	*failure++
	if waitErr := waitForRetry(ctx, *failure); waitErr != nil {
		return false
	}
	return true
}

func (s *SessionUpdateStream) deliver(ctx context.Context, update SessionUpdate) bool {
	select {
	case s.updates <- update:
		return true
	case <-ctx.Done():
		return false
	}
}

func snapshotCopy(snapshot SessionSnapshot) *SessionSnapshot {
	copy := snapshot
	return &copy
}

func (c *Session) recordDeliveredCursor(streamID string, cursor int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventStreamID == "" || c.eventStreamID == streamID {
		c.eventStreamID = streamID
		c.eventCursor = cursor
		if c.snapshot.EventStreamID == streamID && cursor >= c.snapshot.EventCursor {
			c.snapshot.EventCursor = cursor
		}
	}
}

// Updates returns ordered session updates.
func (s *SessionUpdateStream) Updates() <-chan SessionUpdate { return s.updates }

// Err waits for the stream to finish and returns its terminal error.
func (s *SessionUpdateStream) Err() error {
	<-s.done
	return s.err
}

// Close detaches the watcher without aborting a turn or bash execution.
func (s *SessionUpdateStream) Close() error {
	if s != nil {
		s.once.Do(s.cancel)
	}
	return nil
}

func (c *Session) Stream(ctx context.Context, turnID string) (*EventStream, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(turnID) == "" {
		cancel()
		return nil, fmt.Errorf("turn id is empty")
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
	body, err := c.transport.StreamSessionEvents(operation, c.id, streamID, after)
	if err != nil {
		cancel()
		return nil, projectError(err)
	}
	stream := &EventStream{updates: make(chan []protocol.SessionEvent), done: make(chan struct{}), cancel: cancel}
	go stream.readSSE(operation, body, turnID, seenTurnStart, resumedAssistantMessageID, streamID, after, func(streamID string, cursor int64, turnStarted bool) {
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

func (c *Session) StartPrompt(ctx context.Context, text string) (*Turn, error) {
	return c.StartPromptInput(ctx, protocol.PromptInput{Text: text})
}

func (c *Session) StartPromptInput(ctx context.Context, input protocol.PromptInput) (*Turn, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ctx = operation
	if strings.TrimSpace(input.Text) == "" && len(input.AttachmentIDs) == 0 && len(input.AnnotationIDs) == 0 {
		return nil, fmt.Errorf("prompt is empty")
	}
	reservation, err := c.transport.StartPromptInput(ctx, c.id, input)
	if err != nil {
		return nil, projectError(err)
	}
	return c.turnFromReservation(reservation), nil
}

// SendMessage submits a typed message, either starting a turn or joining the follow-up queue.
func (c *Session) SendMessage(ctx context.Context, message Message) (PromptSubmission, error) {
	return c.SubmitPromptInput(ctx, message)
}

// SubmitPrompt is a convenience wrapper for sending one text-only message.
func (c *Session) SubmitPrompt(ctx context.Context, text string) (PromptSubmission, error) {
	return c.SendMessage(ctx, protocol.PromptInput{Text: text})
}

func (c *Session) SubmitPromptInput(ctx context.Context, input protocol.PromptInput) (PromptSubmission, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return PromptSubmission{}, err
	}
	defer cleanup()
	ctx = operation
	result, err := c.transport.SubmitPromptInput(ctx, c.id, input)
	if err != nil {
		return PromptSubmission{}, projectError(err)
	}
	return c.promptSubmission(result), nil
}

func (c *Session) RestoreFollowUps(ctx context.Context) (protocol.RestoreFollowUpsResult, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.RestoreFollowUpsResult, error) {
		return c.transport.RestoreFollowUps(operation, c.id)
	})
}

func (c *Session) PromoteFollowUps(ctx context.Context) (protocol.PromoteFollowUpsResult, error) {
	return sessionResult(c, ctx, func(operation context.Context) (protocol.PromoteFollowUpsResult, error) {
		return c.transport.PromoteFollowUps(operation, c.id)
	})
}

// SubmitPromptCommand submits a prompt command, either starting a turn or
// joining the follow-up queue.
func (c *Session) SubmitPromptCommand(ctx context.Context, name, args string) (PromptSubmission, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return PromptSubmission{}, err
	}
	defer cleanup()
	result, err := c.transport.SubmitPromptCommand(operation, c.id, protocol.PromptCommandInput{Name: name, Args: args})
	if err != nil {
		return PromptSubmission{}, projectError(err)
	}
	return c.promptSubmission(result), nil
}

func (c *Session) promptSubmission(result protocol.PromptSubmission) PromptSubmission {
	output := PromptSubmission{Queued: result.Queued, Queue: result.Queue}
	if result.Reservation != nil {
		output.Turn = c.turnFromReservation(*result.Reservation)
	}
	return output
}

// StartPromptCommand starts a prompt command immediately and returns its turn.
func (c *Session) StartPromptCommand(ctx context.Context, name, args string) (*Turn, error) {
	operation, cleanup, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ctx = operation
	reservation, err := c.transport.StartPromptCommand(ctx, c.id, protocol.PromptCommandInput{Name: name, Args: args})
	if err != nil {
		return nil, projectError(err)
	}
	return c.turnFromReservation(reservation), nil
}

func (c *Session) turnFromReservation(reservation protocol.TurnReservation) *Turn {
	return &Turn{
		client: c.client, transport: c.transport, sessionID: c.id,
		turnID: reservation.TurnID, id: reservation.TurnID,
	}
}

func (r *Turn) ID() string {
	if r == nil {
		return ""
	}
	return r.id
}

func (r *Turn) Wait(ctx context.Context) (protocol.PromptOutcome, error) {
	if r == nil {
		return protocol.PromptOutcome{}, errors.New("turn handle is nil")
	}
	operation, cleanup, err := handleOperationContext(r.client, ctx)
	if err != nil {
		return protocol.PromptOutcome{}, err
	}
	defer cleanup()
	ctx = operation
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	pollFailures := 0
	snapshotFailures := 0
	for {
		info, err := r.transport.GetTurn(ctx, r.sessionID, r.id)
		if err != nil {
			pollFailures++
			if !retryablePollingError(err) || pollFailures >= 6 {
				return protocol.PromptOutcome{}, projectError(err)
			}
			if err := waitForRetry(ctx, pollFailures); err != nil {
				return protocol.PromptOutcome{}, projectError(err)
			}
			continue
		}
		pollFailures = 0
		switch info.Status {
		case protocol.TurnStatusQueued, protocol.TurnStatusRunning:
			if err := waitForPoll(ctx, ticker.C); err != nil {
				return protocol.PromptOutcome{}, projectError(err)
			}
		case protocol.TurnStatusCompleted:
			snapshot, err := r.transport.GetSessionSnapshot(ctx, r.sessionID)
			if err != nil {
				snapshotFailures++
				if !retryablePollingError(err) || snapshotFailures >= 6 {
					return protocol.PromptOutcome{}, projectError(err)
				}
				if err := waitForRetry(ctx, snapshotFailures); err != nil {
					return protocol.PromptOutcome{}, projectError(err)
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
	if err == nil || errors.Is(err, clienttransport.ErrIncompatibleServer) {
		return false
	}
	var apiError *clienttransport.APIError
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

func (r *Turn) Abort(ctx context.Context) error {
	if r == nil {
		return errors.New("turn handle is nil")
	}
	operation, cleanup, err := handleOperationContext(r.client, ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return projectError(r.transport.AbortSession(operation, r.sessionID, r.id))
}

func (b *BashExecution) ID() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state.ID
}

func (b *BashExecution) State() protocol.BashExecution {
	if b == nil {
		return protocol.BashExecution{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *BashExecution) Wait(ctx context.Context) (protocol.BashExecution, error) {
	if b == nil {
		return protocol.BashExecution{}, errors.New("bash execution handle is nil")
	}
	operation, cleanup, err := handleOperationContext(b.client, ctx)
	if err != nil {
		return protocol.BashExecution{}, err
	}
	defer cleanup()
	ctx = operation
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
				return protocol.BashExecution{}, projectError(err)
			}
			if err := waitForRetry(ctx, failures); err != nil {
				return protocol.BashExecution{}, projectError(err)
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
			return protocol.BashExecution{}, projectError(err)
		}
	}
}

func (b *BashExecution) Abort(ctx context.Context) error {
	if b == nil {
		return errors.New("bash execution handle is nil")
	}
	operation, cleanup, err := handleOperationContext(b.client, ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return projectError(b.transport.AbortBash(operation, b.sessionID, b.ID()))
}

// Updates returns ordered event batches.
func (s *EventStream) Updates() <-chan []protocol.SessionEvent { return s.updates }

func (s *EventStream) rawErr() error {
	<-s.done
	return s.err
}

// Err waits for the stream to finish and returns its terminal error.
func (s *EventStream) Err() error { return projectError(s.rawErr()) }

// Close detaches the stream without aborting its turn.
func (s *EventStream) Close() error {
	if s != nil && s.cancel != nil {
		s.once.Do(s.cancel)
	}
	return nil
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

func (s *EventStream) readSSE(
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
	defer close(s.done)
	defer close(s.updates)
	defer body.Close()
	if s.cancel != nil {
		defer s.once.Do(s.cancel)
	}

	finished := false
	resumedFromSnapshot := seenTurnStart
	err := clienttransport.ReadSessionEvents(body, func(name, id string, batch protocol.SessionEventBatch) error {
		if batch.ResyncRequired {
			if name != httpapi.SessionResyncRecord || id != "" {
				return &clienttransport.StreamError{Err: errors.New("session resync record has invalid framing")}
			}
			return errEventResyncRequired
		}
		if name != httpapi.SessionEventsRecord {
			return &clienttransport.StreamError{Err: errors.New("session event batch has invalid record name")}
		}
		if expectedStreamID != "" && batch.StreamID != expectedStreamID {
			return errEventResyncRequired
		}
		if expectedStreamID == "" {
			expectedStreamID = batch.StreamID
		}
		if len(batch.Events) > 0 && batch.Events[0].Sequence != after+1 {
			return errEventResyncRequired
		}
		receivedEvents := batch.Events
		cursor := after
		for _, event := range receivedEvents {
			if event.Sequence > cursor {
				cursor = event.Sequence
			}
		}
		expectedID := batch.StreamID + ":" + strconv.FormatInt(cursor, 10)
		if id != expectedID {
			return &clienttransport.StreamError{Err: fmt.Errorf("session event record id %q does not match %q", id, expectedID)}
		}
		if reduceEvents != nil {
			var reduceErr error
			batch.Events, reduceErr = reduceEvents(receivedEvents)
			if reduceErr != nil {
				return fmt.Errorf("%w: %v", errEventResyncRequired, reduceErr)
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
					return errEventResyncRequired
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
			var reduceErr error
			activeAssistantMessageID, reduceErr = reduceAssistantMessageID(activeAssistantMessageID, event)
			if reduceErr != nil {
				return fmt.Errorf("%w: %v", errEventResyncRequired, reduceErr)
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
				return ctx.Err()
			}
		}
		if cursor > after {
			after = cursor
			if recordCursor != nil {
				recordCursor(batch.StreamID, cursor, seenTurnStart)
			}
		}
		if finished {
			return errEventStreamFinished
		}
		return nil
	})
	switch {
	case errors.Is(err, errEventStreamFinished):
		return
	case err != nil:
		s.err = err
		return
	case ctx.Err() != nil:
		s.err = ctx.Err()
		return
	case !finished:
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

func (c *Session) ExecutePluginCommand(ctx context.Context, input protocol.PluginCommandInput) error {
	return sessionError(c, ctx, func(operation context.Context) error {
		return c.transport.ExecutePluginCommand(operation, c.id, input)
	})
}
