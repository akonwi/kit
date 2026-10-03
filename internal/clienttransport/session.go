package clienttransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/scratchpad"
	"github.com/akonwi/kit/internal/version"
)

const maxSessionResponseBytes = 8 << 20

// APIError is a non-success response from the local session protocol.
type APIError = httpapi.APIError

// CreateSession creates a persisted or temporary session through the local daemon.
func (c *Client) CreateSession(ctx context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	if err := input.Validate(); err != nil {
		return protocol.SessionInfo{}, fmt.Errorf("validate session request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.CreateSession, httpapi.NoBody{}, input)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionInfo{}, protocolErrorf("validate daemon session response: %w", err)
	}
	if (input.ID != "" && output.ID != input.ID) || output.Temporary != input.Temporary {
		return protocol.SessionInfo{}, protocolErrorf("daemon session creation identity mismatch")
	}
	return output, nil
}

// ForkSession creates a linked child from one settled persistent session.
func (c *Client) ForkSession(ctx context.Context, sourceSessionID string, input protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	if err := input.Validate(); err != nil {
		return protocol.SessionInfo{}, fmt.Errorf("validate session fork: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ForkSession, httpapi.SessionPath{SessionID: sourceSessionID}, input)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionInfo{}, protocolErrorf("validate forked daemon session: %w", err)
	}
	if output.Temporary || output.ParentSessionID != sourceSessionID || (input.ID != "" && output.ID != input.ID) {
		return protocol.SessionInfo{}, protocolErrorf("daemon session fork identity mismatch")
	}
	return output, nil
}

// RenameSession replaces one persisted session's display name.
func (c *Client) RenameSession(ctx context.Context, sessionID, name string) (protocol.SessionInfo, error) {
	input := protocol.RenameSessionInput{Name: name}
	if err := input.Validate(); err != nil {
		return protocol.SessionInfo{}, fmt.Errorf("validate session rename: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.RenameSession, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionInfo{}, protocolErrorf("validate renamed daemon session: %w", err)
	}
	if output.ID != sessionID {
		return protocol.SessionInfo{}, protocolErrorf("daemon session rename identity mismatch")
	}
	if output.Name != strings.TrimSpace(name) {
		return protocol.SessionInfo{}, protocolErrorf("daemon session rename value mismatch")
	}
	return output, nil
}

// DeleteSession permanently deletes one persisted session and its stored history.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.DeleteSession, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	return err
}

// DisposeTemporarySession revokes and removes one process-local session.
func (c *Client) DisposeTemporarySession(ctx context.Context, sessionID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.DisposeTemporarySession, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	return err
}

// ListSessions lists daemon sessions, optionally filtered to one cwd.
func (c *Client) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ListSessions, httpapi.ListSessionsPath{CWD: cwd}, httpapi.NoBody{})
	if err != nil {
		return nil, err
	}
	if err := output.Validate(); err != nil {
		return nil, protocolErrorf("validate daemon session list: %w", err)
	}
	return output.Sessions, nil
}

// ListModels returns the selectable model catalog and authentication availability.
func (c *Client) ListModels(ctx context.Context) (protocol.ModelCatalog, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ListModels, httpapi.ServerPath{}, httpapi.NoBody{})
	if err != nil {
		return protocol.ModelCatalog{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ModelCatalog{}, protocolErrorf("validate daemon model catalog: %w", err)
	}
	return output, nil
}

// RefreshModels fetches and returns the latest selectable model catalog.
func (c *Client) RefreshModels(ctx context.Context) (protocol.ModelCatalog, error) {
	output, err := httpapi.Call(ctx, c, httpapi.RefreshModels, httpapi.ServerPath{}, httpapi.NoBody{})
	if err != nil {
		return protocol.ModelCatalog{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ModelCatalog{}, protocolErrorf("validate refreshed daemon model catalog: %w", err)
	}
	return output, nil
}

// GetWorkspace returns the current logical workspace for a session.
func (c *Client) GetWorkspace(ctx context.Context, sessionID string) (protocol.WorkspaceRef, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetWorkspace, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.WorkspaceRef{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.WorkspaceRef{}, protocolErrorf("validate daemon workspace: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.WorkspaceRef{}, protocolErrorf("daemon workspace identity mismatch")
	}
	return output, nil
}

// ListWorkspaceDirectory returns a bounded page of immediate workspace children.
func (c *Client) ListWorkspaceDirectory(ctx context.Context, sessionID string, input protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ListWorkspaceDirectory, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.DirectoryPage{}, protocolErrorf("validate daemon directory page: %w", err)
	}
	if output.SessionID != sessionID || output.Workspace.WorkspaceID != input.WorkspaceID || output.Path != input.Path {
		return protocol.DirectoryPage{}, protocolErrorf("daemon directory identity mismatch")
	}
	return output, nil
}

// ReadWorkspaceFile returns a bounded guarded UTF-8 workspace preview.
func (c *Client) ReadWorkspaceFile(ctx context.Context, sessionID string, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ReadWorkspaceFile, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.WorkspaceFileRead{}, protocolErrorf("validate daemon workspace file: %w", err)
	}
	if output.SessionID != sessionID || output.Workspace.WorkspaceID != input.WorkspaceID || output.Path != input.Path {
		return protocol.WorkspaceFileRead{}, protocolErrorf("daemon workspace file identity mismatch")
	}
	return output, nil
}

// ListDiffTargets returns a bounded authoritative target catalog.
func (c *Client) ListDiffTargets(ctx context.Context, sessionID string, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	if err := input.Validate(); err != nil {
		return protocol.DiffTargetCatalog{}, fmt.Errorf("validate diff target request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ListDiffTargets, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	if err := ValidateDiffTargetCatalogResponse(sessionID, input, output); err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	return output, nil
}

// ValidateDiffTargetCatalogResponse cross-checks a catalog against its request.
func ValidateDiffTargetCatalogResponse(sessionID string, input protocol.ListDiffTargetsInput, output protocol.DiffTargetCatalog) error {
	if err := output.Validate(); err != nil {
		return protocolErrorf("validate daemon diff target catalog: %w", err)
	}
	if output.SessionID != sessionID || output.WorkspaceID != input.WorkspaceID {
		return protocolErrorf("daemon diff target catalog identity does not match request")
	}
	return nil
}

// ObserveDiff observes one server-issued target reference.
func (c *Client) ObserveDiff(ctx context.Context, sessionID string, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	if err := input.Validate(); err != nil {
		return protocol.DiffPage{}, fmt.Errorf("validate diff observation request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ObserveDiff, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if err := ValidateObserveDiffResponse(sessionID, input, output); err != nil {
		return protocol.DiffPage{}, err
	}
	return output, nil
}

// ValidateObserveDiffResponse cross-checks a diff page against its request.
func ValidateObserveDiffResponse(sessionID string, input protocol.ObserveDiffInput, output protocol.DiffPage) error {
	if err := output.Validate(); err != nil {
		return protocolErrorf("validate daemon diff observation page: %w", err)
	}
	if output.Observation.SessionID != sessionID || output.Observation.Target.WorkspaceID != input.WorkspaceID || output.Observation.Target.ID != input.ExpectedTargetID || input.ExpectedTargetRevision != "" && output.Observation.Revision != input.ExpectedTargetRevision {
		return protocolErrorf("daemon diff observation identity does not match request")
	}
	return nil
}

// ObserveWorkingTree returns a stable page from a retained working-tree observation.
func (c *Client) ObserveWorkingTree(ctx context.Context, sessionID string, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	if err := input.Validate(); err != nil {
		return protocol.WorkingTreePage{}, fmt.Errorf("validate working-tree request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ObserveWorkingTree, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.WorkingTreePage{}, protocolErrorf("validate daemon working-tree page: %w", err)
	}
	if output.Observation.SessionID != sessionID {
		return protocol.WorkingTreePage{}, protocolErrorf("daemon working-tree page identity mismatch")
	}
	return output, nil
}

// ReadFileDiff returns guarded semantic hunk fragments.
func (c *Client) ReadFileDiff(ctx context.Context, sessionID string, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	if err := input.Validate(); err != nil {
		return protocol.FileDiffPage{}, fmt.Errorf("validate file diff request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ReadFileDiff, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.FileDiffPage{}, err
	}
	if err := ValidateFileDiffResponse(sessionID, input, output); err != nil {
		return protocol.FileDiffPage{}, err
	}
	return output, nil
}

// ValidateFileDiffResponse cross-checks a file diff page against its request.
func ValidateFileDiffResponse(sessionID string, input protocol.ReadFileDiffInput, output protocol.FileDiffPage) error {
	if err := output.Validate(); err != nil {
		return protocolErrorf("validate daemon file diff page: %w", err)
	}
	if output.Observation.SessionID != sessionID || output.Observation.Target.ID != input.TargetID || output.Observation.Revision != input.TargetRevision || output.File.Path != input.Path || input.ExpectedFileRevision != "" && output.File.FileRevision != input.ExpectedFileRevision {
		return protocolErrorf("daemon file diff identity does not match request")
	}
	return nil
}

// ListAnnotations returns one bounded page of live session annotation drafts.
func (c *Client) ListAnnotations(ctx context.Context, sessionID string, input protocol.ListAnnotationsInput) (protocol.AnnotationPage, error) {
	if err := input.Validate(); err != nil {
		return protocol.AnnotationPage{}, fmt.Errorf("validate annotation page request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ListAnnotations, httpapi.AnnotationListParams{SessionID: sessionID, Cursor: input.Cursor, PageSize: input.PageSize}, httpapi.NoBody{})
	if err != nil {
		return protocol.AnnotationPage{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.AnnotationPage{}, protocolErrorf("validate daemon annotation page: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.AnnotationPage{}, protocolErrorf("daemon annotation page identity mismatch")
	}
	return output, nil
}

// CreateAnnotation creates one guarded live session annotation.
func (c *Client) CreateAnnotation(ctx context.Context, sessionID string, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
	if err := input.Validate(); err != nil {
		return protocol.Annotation{}, fmt.Errorf("validate annotation request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.CreateAnnotation, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.Annotation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.Annotation{}, protocolErrorf("validate daemon annotation: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.Annotation{}, protocolErrorf("daemon annotation identity mismatch")
	}
	return output, nil
}

// UpdateAnnotation replaces the body of one live session annotation.
func (c *Client) UpdateAnnotation(ctx context.Context, sessionID string, input protocol.UpdateAnnotationInput) (protocol.Annotation, error) {
	if err := input.Validate(); err != nil {
		return protocol.Annotation{}, fmt.Errorf("validate annotation update: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.UpdateAnnotation, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.Annotation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.Annotation{}, protocolErrorf("validate daemon annotation update: %w", err)
	}
	if output.SessionID != sessionID || output.ID != input.AnnotationID {
		return protocol.Annotation{}, protocolErrorf("daemon annotation update identity mismatch")
	}
	return output, nil
}

// DeleteAnnotation removes one live session annotation.
func (c *Client) DeleteAnnotation(ctx context.Context, sessionID string, input protocol.DeleteAnnotationInput) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("validate annotation delete: %w", err)
	}
	_, err := httpapi.Call(ctx, c, httpapi.DeleteAnnotation, httpapi.SessionPath{SessionID: sessionID}, input)
	return err
}

// GetSessionFileIndex returns project paths indexed on the session host.
func (c *Client) GetSessionFileIndex(ctx context.Context, sessionID string) (protocol.SessionFileIndex, error) {
	return c.getSessionFileIndex(ctx, sessionID, false)
}

// RefreshSessionFileIndex forces the session host to rebuild its project-path index.
func (c *Client) RefreshSessionFileIndex(ctx context.Context, sessionID string) (protocol.SessionFileIndex, error) {
	return c.getSessionFileIndex(ctx, sessionID, true)
}

func (c *Client) getSessionFileIndex(ctx context.Context, sessionID string, refresh bool) (protocol.SessionFileIndex, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSessionFileIndex, httpapi.FileIndexParams{SessionID: sessionID, Refresh: refresh}, httpapi.NoBody{})
	if err != nil {
		return protocol.SessionFileIndex{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionFileIndex{}, protocolErrorf("validate daemon session file index: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.SessionFileIndex{}, protocolErrorf("daemon session file index identity mismatch")
	}
	return output, nil
}

// GetSessionSnapshot returns an authoritative transcript and active-run snapshot.
func (c *Client) GetSessionSnapshot(ctx context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSession, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionSnapshot{}, protocolErrorf("validate daemon session snapshot: %w", err)
	}
	if output.Session.ID != sessionID {
		return protocol.SessionSnapshot{}, protocolErrorf("daemon session snapshot identity mismatch")
	}
	return output, nil
}

// GetBashHistory returns one newest-first page of direct shell history.
func (c *Client) GetBashHistory(ctx context.Context, sessionID string, before uint64, limit int) (protocol.BashHistoryPage, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetBashHistory, httpapi.BashHistoryPath{SessionID: sessionID, Before: before, Limit: limit}, httpapi.NoBody{})
	if err != nil {
		return protocol.BashHistoryPage{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.BashHistoryPage{}, protocolErrorf("validate daemon bash history page: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.BashHistoryPage{}, protocolErrorf("daemon bash history page identity mismatch")
	}
	return output, nil
}

// GetMessagePage returns newest-first durable messages matching query.
func (c *Client) GetMessagePage(ctx context.Context, sessionID string, query protocol.MessagePageQuery) (protocol.MessagePage, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetMessagePage, httpapi.MessagePagePath{SessionID: sessionID, Before: query.Before, Limit: query.Limit, Roles: query.Roles}, httpapi.NoBody{})
	if err != nil {
		return protocol.MessagePage{}, err
	}
	before := ""
	if query.Before > 0 {
		before = strconv.FormatUint(query.Before, 10)
	}
	if err := output.ValidateBefore(before); err != nil {
		return protocol.MessagePage{}, protocolErrorf("validate daemon message page: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.MessagePage{}, protocolErrorf("daemon message page identity mismatch")
	}
	return output, nil
}

// GetTranscriptPage returns the complete-turn page preceding before.
func (c *Client) GetTranscriptPage(ctx context.Context, sessionID, before string) (protocol.TranscriptPage, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetTranscriptPage, httpapi.TranscriptPagePath{SessionID: sessionID, Before: before}, httpapi.NoBody{})
	if err != nil {
		return protocol.TranscriptPage{}, err
	}
	if err := output.ValidateBefore(before); err != nil {
		return protocol.TranscriptPage{}, protocolErrorf("validate daemon transcript page: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.TranscriptPage{}, protocolErrorf("daemon transcript page identity mismatch")
	}
	return output, nil
}

// GetSessionVCSStatus returns volatile repository status for a session workspace.
func (c *Client) GetSessionVCSStatus(ctx context.Context, sessionID string) (protocol.SessionVCSStatus, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSessionVCS, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.SessionVCSStatus{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionVCSStatus{}, protocolErrorf("validate daemon session VCS status: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.SessionVCSStatus{}, protocolErrorf("daemon session VCS identity mismatch")
	}
	return output, nil
}

// GetSessionEvents returns the next ordered page after a session stream sequence.
func (c *Client) GetSessionEvents(ctx context.Context, sessionID, streamID string, after int64) (protocol.SessionEventBatch, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSessionEventPage, httpapi.EventPagePath{SessionID: sessionID, StreamID: streamID, After: after}, httpapi.NoBody{})
	if err != nil {
		return protocol.SessionEventBatch{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SessionEventBatch{}, protocolErrorf("validate daemon session events: %w", err)
	}
	if !output.ResyncRequired && after > 0 && len(output.Events) > 0 && output.Events[0].Sequence != after+1 {
		return protocol.SessionEventBatch{}, protocolErrorf("daemon session event sequence gap after %d", after)
	}
	for _, event := range output.Events {
		if event.SessionID != sessionID {
			return protocol.SessionEventBatch{}, protocolErrorf("daemon session event identity mismatch")
		}
	}
	return output, nil
}

// ChangeSessionCWD changes one session's relative filesystem scope.
func (c *Client) ChangeSessionCWD(ctx context.Context, sessionID, target string) (protocol.SessionInfo, error) {
	mutationID, err := identifier.New("cwd_")
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return c.ChangeSessionCWDWithID(ctx, sessionID, mutationID, target)
}

// ChangeSessionCWDWithID changes one session's relative filesystem scope using
// a client-selected idempotency identity.
func (c *Client) ChangeSessionCWDWithID(ctx context.Context, sessionID, mutationID, target string) (protocol.SessionInfo, error) {
	result, err := c.ChangeSessionWorkspaceCWDWithID(ctx, sessionID, mutationID, target)
	return result.Session, err
}

// ChangeSessionWorkspaceCWDWithID returns both session metadata and workspace identity.
func (c *Client) ChangeSessionWorkspaceCWDWithID(ctx context.Context, sessionID, mutationID, target string) (protocol.ChangeWorkspaceCWDResult, error) {
	input := protocol.ChangeCWDInput{MutationID: mutationID, Path: target}
	if err := input.Validate(); err != nil {
		return protocol.ChangeWorkspaceCWDResult{}, fmt.Errorf("validate session cwd request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ChangeSessionCWD, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.ChangeWorkspaceCWDResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ChangeWorkspaceCWDResult{}, protocolErrorf("validate daemon session cwd result: %w", err)
	}
	if output.Session.ID != sessionID {
		return protocol.ChangeWorkspaceCWDResult{}, protocolErrorf("daemon session cwd identity mismatch")
	}
	return output, nil
}

// ReloadSession refreshes one idle session's authoritative prompt and tools.
func (c *Client) ReloadSession(ctx context.Context, sessionID string) (protocol.ReloadSessionResult, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ReloadSession, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.ReloadSessionResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ReloadSessionResult{}, protocolErrorf("validate daemon session reload: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.ReloadSessionResult{}, protocolErrorf("daemon session reload identity mismatch")
	}
	return output, nil
}

// ConfigureSession applies one revision-guarded exact model/thinking transition.
func (c *Client) ConfigureSession(ctx context.Context, sessionID string, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	if err := input.Validate(); err != nil {
		return protocol.ConfigureSessionResult{}, fmt.Errorf("validate session configuration request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.ConfigureSession, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.ConfigureSessionResult{}, err
	}
	if err := output.ValidateApplied(input); err != nil {
		return protocol.ConfigureSessionResult{}, protocolErrorf("validate daemon session configuration: %w", err)
	}
	if output.Session.ID != sessionID {
		return protocol.ConfigureSessionResult{}, protocolErrorf("daemon session configuration identity mismatch")
	}
	return output, nil
}

// GetScratchpad reads the authoritative shared scratchpad through one bound session identity.
func (c *Client) GetScratchpad(ctx context.Context, sessionID string) (protocol.Scratchpad, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetScratchpad, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.Scratchpad{}, protocolErrorf("validate daemon scratchpad: %w", err)
	}
	return output, nil
}

// UpdateScratchpad applies one revision-guarded content replacement.
func (c *Client) UpdateScratchpad(ctx context.Context, sessionID string, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	if err := input.Validate(); err != nil {
		code := protocol.ScratchpadInvalidContent
		message := "scratchpad content is invalid"
		if errors.Is(err, scratchpad.ErrContentTooLarge) {
			code = protocol.ScratchpadTooLarge
			message = "scratchpad content is too large"
		} else if !errors.Is(err, scratchpad.ErrInvalidContent) {
			return protocol.Scratchpad{}, fmt.Errorf("validate scratchpad update: %w", err)
		}
		return protocol.Scratchpad{}, &protocol.ScratchpadError{Code: code, Message: message}
	}
	output, err := httpapi.Call(ctx, c, httpapi.UpdateScratchpad, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.Scratchpad{}, err
	}
	if err := output.ValidateApplied(input); err != nil {
		return protocol.Scratchpad{}, protocolErrorf("validate daemon scratchpad update: %w", err)
	}
	return output, nil
}

// CompactSession applies one idempotent explicit context compaction.
func (c *Client) CompactSession(ctx context.Context, sessionID string, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	if err := input.Validate(); err != nil {
		return protocol.CompactSessionResult{}, fmt.Errorf("validate session compaction request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.CompactSession, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.CompactSessionResult{}, err
	}
	if err := output.ValidateApplied(input); err != nil {
		return protocol.CompactSessionResult{}, protocolErrorf("validate daemon session compaction: %w", err)
	}
	return output, nil
}

// GetSubagentEvents loads a bounded child event page.
func (c *Client) GetSubagentEvents(ctx context.Context, sessionID, conversationID, streamID string, after int64) (protocol.SubagentLiveEventPage, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSubagentEvents, httpapi.SubagentEventsParams{SessionID: sessionID, ConversationID: conversationID, StreamID: streamID, After: after}, httpapi.NoBody{})
	if err != nil {
		return protocol.SubagentLiveEventPage{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SubagentLiveEventPage{}, protocolErrorf("validate daemon subagent events: %w", err)
	}
	return output, nil
}

// GetSubagentTranscript loads one complete-turn child history page.
// An empty before selects the newest page; otherwise before is an exclusive
// durable sequence.
func (c *Client) GetSubagentTranscript(ctx context.Context, sessionID, conversationID, before string) (protocol.SubagentTranscript, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetSubagentTranscript, httpapi.SubagentTranscriptParams{SessionID: sessionID, ConversationID: conversationID, Before: before}, httpapi.NoBody{})
	if err != nil {
		return protocol.SubagentTranscript{}, err
	}
	if err := output.ValidateBefore(before); err != nil {
		return protocol.SubagentTranscript{}, protocolErrorf("validate daemon subagent transcript: %w", err)
	}
	if output.ConversationID != conversationID {
		return protocol.SubagentTranscript{}, protocolErrorf("daemon subagent transcript identity mismatch")
	}
	return output, nil
}

// Subagent performs one asynchronous child lifecycle operation.
func (c *Client) Subagent(ctx context.Context, sessionID string, input protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	if err := input.Validate(); err != nil {
		return protocol.SubagentOperationResult{}, fmt.Errorf("validate subagent operation: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.OperateSubagent, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.SubagentOperationResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.SubagentOperationResult{}, protocolErrorf("validate daemon subagent response: %w", err)
	}
	return output, nil
}

// UploadAttachment streams one image or text file into session-owned storage.
func (c *Client) UploadAttachment(ctx context.Context, sessionID, filename string, content io.Reader) (protocol.AttachmentInfo, error) {
	if content == nil || filename == "" {
		return protocol.AttachmentInfo{}, fmt.Errorf("attachment filename and content are required")
	}
	ctx, cleanup := c.operationContext(ctx)
	defer cleanup()
	connection, err := c.connection()
	if err != nil {
		return protocol.AttachmentInfo{}, err
	}
	if err := compatible(connection); err != nil {
		return protocol.AttachmentInfo{}, err
	}

	bodyReader, bodyWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(bodyWriter)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.URL+"/v1/sessions/"+url.PathEscape(sessionID)+"/attachments", bodyReader)
	if err != nil {
		bodyReader.Close()
		bodyWriter.Close()
		return protocol.AttachmentInfo{}, fmt.Errorf("create attachment upload request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(instanceHeader, connection.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	go func() {
		part, partErr := multipartWriter.CreateFormFile("file", filename)
		if partErr == nil {
			_, partErr = io.Copy(part, content)
		}
		if closeErr := multipartWriter.Close(); partErr == nil {
			partErr = closeErr
		}
		_ = bodyWriter.CloseWithError(partErr)
	}()

	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		bodyReader.CloseWithError(err)
		return protocol.AttachmentInfo{}, transportError("upload attachment", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxSessionResponseBytes)
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(limited)
		return protocol.AttachmentInfo{}, DecodeAPIError(response.StatusCode, body)
	}
	var output protocol.AttachmentInfo
	if err := json.NewDecoder(limited).Decode(&output); err != nil {
		return protocol.AttachmentInfo{}, protocolErrorf("decode attachment response: %w", err)
	}
	if err := output.Validate(); err != nil {
		return protocol.AttachmentInfo{}, protocolErrorf("validate daemon attachment: %w", err)
	}
	if output.SessionID != sessionID {
		return protocol.AttachmentInfo{}, protocolErrorf("daemon attachment session identity mismatch")
	}
	return output, nil
}

// ResolveAttachments returns bounded metadata for session-owned attachments.
func (c *Client) ResolveAttachments(ctx context.Context, sessionID string, attachmentIDs []string) (protocol.AttachmentResolution, error) {
	input := protocol.AttachmentResolutionInput{AttachmentIDs: attachmentIDs}
	if err := input.Validate(); err != nil {
		return protocol.AttachmentResolution{}, err
	}
	output, err := httpapi.Call(ctx, c, httpapi.ResolveAttachments, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.AttachmentResolution{}, err
	}
	if err := output.Validate(sessionID, attachmentIDs); err != nil {
		return protocol.AttachmentResolution{}, protocolErrorf("validate daemon attachment resolution: %w", err)
	}
	return output, nil
}

// OpenAttachment opens verified session-owned attachment bytes from the daemon.
func (c *Client) OpenAttachment(ctx context.Context, sessionID, attachmentID string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	ctx, cleanup := c.operationContext(ctx)
	keepContext := false
	defer func() {
		if !keepContext {
			cleanup()
		}
	}()
	connection, err := c.connection()
	if err != nil {
		return protocol.AttachmentInfo{}, nil, err
	}
	if err := compatible(connection); err != nil {
		return protocol.AttachmentInfo{}, nil, err
	}
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/attachments/" + url.PathEscape(attachmentID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.URL+path, nil)
	if err != nil {
		return protocol.AttachmentInfo{}, nil, fmt.Errorf("create attachment request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(instanceHeader, connection.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return protocol.AttachmentInfo{}, nil, transportError("open attachment", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes))
		return protocol.AttachmentInfo{}, nil, DecodeAPIError(response.StatusCode, body)
	}
	disposition, parameters, dispositionErr := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	width, widthErr := strconv.Atoi(response.Header.Get("X-Kit-Image-Width"))
	height, heightErr := strconv.Atoi(response.Header.Get("X-Kit-Image-Height"))
	info := protocol.AttachmentInfo{
		ID: response.Header.Get("X-Kit-Attachment-ID"), SessionID: response.Header.Get("X-Kit-Session-ID"), Filename: parameters["filename"],
		MediaType: response.Header.Get("Content-Type"), Size: response.ContentLength,
		SHA256:    strings.TrimSuffix(strings.TrimPrefix(response.Header.Get("ETag"), `"sha256:`), `"`),
		CreatedAt: response.Header.Get("X-Kit-Attachment-Created-At"), Width: width, Height: height,
	}
	if dispositionErr != nil || disposition != "inline" || widthErr != nil || heightErr != nil {
		response.Body.Close()
		return protocol.AttachmentInfo{}, nil, protocolErrorf("daemon attachment returned malformed metadata")
	}
	if err := info.Validate(); err != nil {
		response.Body.Close()
		return protocol.AttachmentInfo{}, nil, protocolErrorf("validate daemon attachment: %w", err)
	}
	if info.ID != attachmentID || info.SessionID != sessionID {
		response.Body.Close()
		return protocol.AttachmentInfo{}, nil, protocolErrorf("daemon attachment identity mismatch")
	}
	keepContext = true
	return info, &lifetimeReadCloser{ReadCloser: response.Body, operation: "read attachment", cleanup: cleanup}, nil
}

// DecodeAPIError decodes one declared session API failure.
func DecodeAPIError(statusCode int, body []byte) error {
	return httpapi.DecodeError(statusCode, body)
}

// DecodeStrictJSONObject decodes exactly one strict JSON object.
func DecodeStrictJSONObject(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return fmt.Errorf("expected one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// SubmitPrompt starts an idle prompt or queues it behind active work.
func (c *Client) SubmitPrompt(ctx context.Context, sessionID, text string) (protocol.PromptSubmission, error) {
	return c.SubmitPromptInput(ctx, sessionID, protocol.PromptInput{Text: text})
}

func (c *Client) SubmitPromptInput(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptSubmission, error) {
	output, err := httpapi.Call(ctx, c, httpapi.SubmitPrompt, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.PromptSubmission{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PromptSubmission{}, protocolErrorf("validate daemon prompt submission: %w", err)
	}
	if output.Reservation != nil && output.Reservation.SessionID != sessionID {
		return protocol.PromptSubmission{}, protocolErrorf("daemon prompt submission identity mismatch")
	}
	return output, nil
}

// RestoreFollowUps atomically drains one session's deferred prompts.
func (c *Client) RestoreFollowUps(ctx context.Context, sessionID string) (protocol.RestoreFollowUpsResult, error) {
	output, err := httpapi.Call(ctx, c, httpapi.RestoreTurnFollowUps, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.RestoreFollowUpsResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.RestoreFollowUpsResult{}, protocolErrorf("validate daemon follow-up restoration: %w", err)
	}
	return output, nil
}

// PromoteFollowUps moves one session's deferred prompts into active steering.
func (c *Client) PromoteFollowUps(ctx context.Context, sessionID string) (protocol.PromoteFollowUpsResult, error) {
	output, err := httpapi.Call(ctx, c, httpapi.PromoteTurnFollowUps, httpapi.SessionPath{SessionID: sessionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.PromoteFollowUpsResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PromoteFollowUpsResult{}, protocolErrorf("validate daemon follow-up promotion: %w", err)
	}
	return output, nil
}

// StartPrompt admits a droid-owned turn and returns its canonical identity.
func (c *Client) StartPrompt(ctx context.Context, sessionID, text string) (protocol.TurnReservation, error) {
	return c.StartPromptInput(ctx, sessionID, protocol.PromptInput{Text: text})
}

func (c *Client) StartPromptInput(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.TurnReservation, error) {
	output, err := httpapi.Call(ctx, c, httpapi.StartPrompt, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.TurnReservation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.TurnReservation{}, protocolErrorf("validate daemon prompt reservation: %w", err)
	}
	if output.SessionID != sessionID || output.TurnID == "" {
		return protocol.TurnReservation{}, protocolErrorf("daemon prompt reservation identity mismatch")
	}
	return output, nil
}

// SubmitPromptCommand expands one discovered prompt command and either starts it or queues it.
func (c *Client) SubmitPromptCommand(ctx context.Context, sessionID string, input protocol.PromptCommandInput) (protocol.PromptSubmission, error) {
	if err := input.Validate(); err != nil {
		return protocol.PromptSubmission{}, fmt.Errorf("validate prompt command request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.SubmitPromptCommand, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.PromptSubmission{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PromptSubmission{}, protocolErrorf("validate daemon prompt command submission: %w", err)
	}
	if output.Reservation != nil && output.Reservation.SessionID != sessionID {
		return protocol.PromptSubmission{}, protocolErrorf("daemon prompt command submission identity mismatch")
	}
	return output, nil
}

// StartPromptCommand expands and admits one discovered prompt command.
func (c *Client) StartPromptCommand(ctx context.Context, sessionID string, input protocol.PromptCommandInput) (protocol.TurnReservation, error) {
	if err := input.Validate(); err != nil {
		return protocol.TurnReservation{}, fmt.Errorf("validate prompt command request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.StartPromptCommand, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.TurnReservation{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.TurnReservation{}, protocolErrorf("validate daemon prompt command reservation: %w", err)
	}
	if output.SessionID != sessionID || output.TurnID == "" {
		return protocol.TurnReservation{}, protocolErrorf("daemon prompt command reservation identity mismatch")
	}
	return output, nil
}

// GetTurn returns a loaded droid turn's transient protocol projection.
func (c *Client) GetTurn(ctx context.Context, sessionID, turnID string) (protocol.TurnInfo, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetTurn, httpapi.TurnPath{SessionID: sessionID, TurnID: turnID}, httpapi.NoBody{})
	if err != nil {
		return protocol.TurnInfo{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.TurnInfo{}, protocolErrorf("validate daemon turn response: %w", err)
	}
	if output.SessionID != sessionID || output.TurnID != turnID {
		return protocol.TurnInfo{}, protocolErrorf("daemon turn response identity mismatch")
	}
	return output, nil
}

// Prompt admits and waits for one droid turn.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (protocol.PromptOutcome, error) {
	return c.PromptInput(ctx, sessionID, protocol.PromptInput{Text: text})
}

func (c *Client) PromptInput(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptOutcome, error) {
	output, err := httpapi.Call(ctx, c, httpapi.Prompt, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.PromptOutcome{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PromptOutcome{}, protocolErrorf("validate daemon prompt response: %w", err)
	}
	if output.SessionID != sessionID || output.TurnID == "" {
		return protocol.PromptOutcome{}, protocolErrorf("daemon prompt response identity mismatch")
	}
	return output, nil
}

// StartBash starts an idempotent daemon-owned direct shell execution.
func (c *Client) StartBash(ctx context.Context, sessionID string, input protocol.BashExecutionInput) (protocol.BashExecution, error) {
	output, err := httpapi.Call(ctx, c, httpapi.StartBash, httpapi.SessionPath{SessionID: sessionID}, input)
	if err != nil {
		return protocol.BashExecution{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.BashExecution{}, protocolErrorf("validate daemon bash execution: %w", err)
	}
	if output.SessionID != sessionID || output.ID != input.ExecutionID {
		return protocol.BashExecution{}, protocolErrorf("daemon bash execution identity mismatch")
	}
	return output, nil
}

// GetBash returns one durable direct shell execution.
func (c *Client) GetBash(ctx context.Context, sessionID, executionID string) (protocol.BashExecution, error) {
	output, err := httpapi.Call(ctx, c, httpapi.GetBash, httpapi.BashExecutionPath{SessionID: sessionID, ExecutionID: executionID}, httpapi.NoBody{})
	if err != nil {
		return protocol.BashExecution{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.BashExecution{}, protocolErrorf("validate daemon bash execution: %w", err)
	}
	if output.SessionID != sessionID || output.ID != executionID {
		return protocol.BashExecution{}, protocolErrorf("daemon bash execution identity mismatch")
	}
	return output, nil
}

// AbortBash requests cancellation of one direct shell generation.
func (c *Client) AbortBash(ctx context.Context, sessionID, executionID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.AbortBash, httpapi.BashExecutionPath{SessionID: sessionID, ExecutionID: executionID}, httpapi.NoBody{})
	return err
}

// RespondInteraction atomically settles one pending model-user interaction.
func (c *Client) RespondInteraction(ctx context.Context, sessionID string, response protocol.InteractionResponse) error {
	result, err := httpapi.Call(ctx, c, httpapi.RespondInteraction, httpapi.InteractionPath{SessionID: sessionID, InteractionID: response.RequestID}, response)
	if err != nil {
		return err
	}
	if !result.Settled {
		return protocolErrorf("daemon did not settle interaction")
	}
	return nil
}

// AbortSession requests cancellation of a loaded session's active turn.
func (c *Client) AbortSession(ctx context.Context, sessionID, turnID string) error {
	result, err := httpapi.Call(ctx, c, httpapi.AbortTurn, httpapi.TurnPath{SessionID: sessionID, TurnID: turnID}, httpapi.NoBody{})
	if err != nil {
		return err
	}
	if !result.Aborting {
		return protocolErrorf("daemon did not acknowledge turn abort")
	}
	return nil
}

// StreamSessionEvents opens the session's authenticated SSE event response.
func (c *Client) StreamSessionEvents(ctx context.Context, sessionID, streamID string, after int64) (io.ReadCloser, error) {
	ctx, cleanup := c.operationContext(ctx)
	keepContext := false
	defer func() {
		if !keepContext {
			cleanup()
		}
	}()
	connection, err := c.connection()
	if err != nil {
		return nil, err
	}
	if err := compatible(connection); err != nil {
		return nil, err
	}
	values := url.Values{}
	if streamID != "" {
		values.Set("stream", streamID)
	}
	values.Set("after", strconv.FormatInt(after, 10))
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/events/stream?" + values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.URL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create daemon event stream request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(instanceHeader, connection.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return nil, transportError("open session event stream", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes))
		return nil, &APIError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil || mediaType != "text/event-stream" {
		response.Body.Close()
		return nil, protocolErrorf("daemon event stream returned content type %q", response.Header.Get("Content-Type"))
	}
	keepContext = true
	return &lifetimeReadCloser{ReadCloser: response.Body, operation: "read session event stream", cleanup: cleanup}, nil
}

// DoSessionRequest performs an authenticated, compatibility-checked session request.
func (c *Client) DoSessionRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Response, error) {
	ctx, cleanup := c.operationContext(ctx)
	connection, err := c.connection()
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := compatible(connection); err != nil {
		cleanup()
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, connection.URL+path, body)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create daemon session request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(instanceHeader, connection.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	if hasJSONBody {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		cleanup()
		return nil, transportError("call session API", err)
	}
	response.Body = &lifetimeReadCloser{ReadCloser: response.Body, operation: "read session response", cleanup: cleanup}
	return response, nil
}

func (c *Client) sessionJSON(ctx context.Context, method, path string, input any, expectedStatus int, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode daemon request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	response, err := c.DoSessionRequest(ctx, method, path, body, input != nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxSessionResponseBytes)
	if response.StatusCode != expectedStatus {
		body, _ := io.ReadAll(limited)
		return DecodeAPIError(response.StatusCode, body)
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, limited)
		return nil
	}
	if err := json.NewDecoder(limited).Decode(output); err != nil {
		return fmt.Errorf("decode daemon session response: %w", err)
	}
	return nil
}

// ExecutePluginCommand dispatches a selected command without starting a model run.
// Failed requests are never retried automatically because effects may have occurred.
func (c *Client) ExecutePluginCommand(ctx context.Context, sessionID string, input protocol.PluginCommandInput) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("validate plugin command request: %w", err)
	}
	_, err := httpapi.Call(ctx, c, httpapi.ExecutePluginCommand, httpapi.SessionPath{SessionID: sessionID}, input)
	var apiError *APIError
	if err != nil && !errors.As(err, &apiError) {
		return fmt.Errorf("plugin command did not complete (effects may have partially completed): %w", err)
	}
	return err
}
