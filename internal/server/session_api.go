package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/akonwi/kit/api/contract"
	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/fileindex"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/identifier"
	kitscratchpad "github.com/akonwi/kit/internal/scratchpad"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
	kitworkingdiff "github.com/akonwi/kit/internal/workingdiff"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

type annotationWorkspaceReader struct{ service *kitworkspace.Service }
type annotationDiffReader struct{ service *kitworkingdiff.Service }

func (r annotationWorkspaceReader) ReadFile(ctx context.Context, sessionID, cwd string, anchor kitannotation.WorkspaceFileAnchor) (kitannotation.FileEvidence, error) {
	read, err := r.service.ReadLineRange(ctx, sessionID, cwd, kitworkspace.LineRangeInput{
		WorkspaceID: anchor.WorkspaceID, Path: anchor.Path, ExpectedFileRevision: anchor.FileRevision,
		StartLine: anchor.StartLine, EndLine: anchor.EndLine,
	})
	if err != nil {
		var workspaceErr *kitworkspace.Error
		if errors.As(err, &workspaceErr) {
			kind := kitannotation.EvidenceUnavailable
			switch workspaceErr.Code {
			case kitworkspace.StaleWorkspace:
				kind = kitannotation.EvidenceStaleWorkspace
			case kitworkspace.StaleFile, kitworkspace.NotFound:
				kind = kitannotation.EvidenceStaleFile
			case kitworkspace.PermissionDenied, kitworkspace.OutsideWorkspace:
				kind = kitannotation.EvidencePermission
			case kitworkspace.LimitExceeded, kitworkspace.CapacityExceeded:
				kind = kitannotation.EvidenceLimit
			case kitworkspace.InvalidPath, kitworkspace.NotFile, kitworkspace.BinaryFile, kitworkspace.SymlinkTraversal:
				kind = kitannotation.EvidenceInvalid
			}
			return kitannotation.FileEvidence{}, &kitannotation.EvidenceError{Kind: kind}
		}
		return kitannotation.FileEvidence{}, err
	}
	return kitannotation.FileEvidence{
		Content: read.Content, ContentStartLine: anchor.StartLine, CompleteLineCount: anchor.EndLine,
	}, nil
}

func (r annotationDiffReader) ReadDiff(ctx context.Context, sessionID, cwd string, anchor kitannotation.WorkingTreeDiffAnchor, target *protocol.PinnedDiffTarget, deriveTarget bool) (kitannotation.FileEvidence, error) {
	read, err := r.service.ReadLineRange(ctx, sessionID, cwd, kitworkingdiff.LineRangeInput{
		TargetID: anchor.TargetID, TargetRevision: anchor.TargetRevision, Target: target, DeriveTarget: deriveTarget, Path: anchor.Path, FileRevision: anchor.FileRevision,
		Side: anchor.Side, StartLine: anchor.StartLine, EndLine: anchor.EndLine,
	})
	if err != nil {
		var diffErr *kitworkingdiff.Error
		if errors.As(err, &diffErr) {
			kind := kitannotation.EvidenceUnavailable
			switch diffErr.Code {
			case kitworkingdiff.StaleWorkspace:
				kind = kitannotation.EvidenceStaleWorkspace
			case kitworkingdiff.StaleTarget, kitworkingdiff.StaleCursor:
				kind = kitannotation.EvidenceStaleTarget
			case kitworkingdiff.StaleFile, kitworkingdiff.NotFound:
				kind = kitannotation.EvidenceStaleFile
			case kitworkingdiff.PermissionDenied:
				kind = kitannotation.EvidencePermission
			case kitworkingdiff.LimitExceeded, kitworkingdiff.CapacityExceeded:
				kind = kitannotation.EvidenceLimit
			case kitworkingdiff.InvalidPath:
				kind = kitannotation.EvidenceInvalid
			}
			return kitannotation.FileEvidence{}, &kitannotation.EvidenceError{Kind: kind}
		}
		return kitannotation.FileEvidence{}, err
	}
	return kitannotation.FileEvidence{Content: read.Content, ContentStartLine: anchor.StartLine, CompleteLineCount: read.EndLine, DiffTarget: read.Target}, nil
}

const (
	maxSessionRequestBytes   = httpapi.MaxRequestBytes
	maxSessionEventPageBytes = 512 << 10
)

var errInvalidSessionRequest = errors.New("invalid session request")

type sessionService interface {
	SubscribePluginToasts(context.Context, string) (pluginToastSource, error)
	Create(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	Fork(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error)
	ChangeCWD(context.Context, string, protocol.ChangeCWDInput) (protocol.ChangeWorkspaceCWDResult, error)
	Rename(context.Context, string, protocol.RenameSessionInput) (protocol.SessionInfo, error)
	Delete(context.Context, string) error
	DisposeTemporary(context.Context, string) error
	List(context.Context, string) ([]protocol.SessionInfo, error)
	Models(context.Context) (protocol.ModelCatalog, error)
	RefreshModels(context.Context) (protocol.ModelCatalog, error)
	Snapshot(context.Context, string) (protocol.SessionSnapshot, error)
	MessagePage(context.Context, string, protocol.MessagePageQuery) (protocol.MessagePage, error)
	TranscriptPage(context.Context, string, string) (protocol.TranscriptPage, error)
	VCS(context.Context, string) (protocol.SessionVCSStatus, error)
	SubscribeVCS(context.Context, string) (vcsSource, error)
	FileIndex(context.Context, string, bool) (protocol.SessionFileIndex, error)
	Workspace(context.Context, string) (protocol.WorkspaceRef, error)
	ListDirectory(context.Context, string, protocol.ListDirectoryInput) (protocol.DirectoryPage, error)
	ReadWorkspaceFile(context.Context, string, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error)
	ListDiffTargets(context.Context, string, protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error)
	ObserveDiff(context.Context, string, protocol.ObserveDiffInput) (protocol.DiffPage, error)
	ObserveWorkingTree(context.Context, string, protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error)
	ReadFileDiff(context.Context, string, protocol.ReadFileDiffInput) (protocol.FileDiffPage, error)
	ListAnnotations(context.Context, string, protocol.ListAnnotationsInput) (protocol.AnnotationPage, error)
	CreateAnnotation(context.Context, string, protocol.CreateAnnotationInput) (protocol.Annotation, error)
	UpdateAnnotation(context.Context, string, protocol.UpdateAnnotationInput) (protocol.Annotation, error)
	DeleteAnnotation(context.Context, string, protocol.DeleteAnnotationInput) error
	Events(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	WaitEvents(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	Reload(context.Context, string) (protocol.ReloadSessionResult, error)
	Configure(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Scratchpad(context.Context, string) (protocol.Scratchpad, error)
	UpdateScratchpad(context.Context, string, protocol.UpdateScratchpadInput) (protocol.Scratchpad, error)
	Compact(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	StartPrompt(context.Context, string, protocol.PromptInput) (protocol.TurnReservation, error)
	SubmitPrompt(context.Context, string, protocol.PromptInput) (protocol.PromptSubmission, error)
	RestoreFollowUps(context.Context, string) (protocol.RestoreFollowUpsResult, error)
	PromoteFollowUps(context.Context, string) (protocol.PromoteFollowUpsResult, error)
	ExecutePluginCommand(context.Context, string, protocol.PluginCommandInput) error
	StartPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.TurnReservation, error)
	SubmitPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.PromptSubmission, error)
	Turn(context.Context, string, string) (protocol.TurnInfo, error)
	Prompt(context.Context, string, protocol.PromptInput) (protocol.PromptOutcome, error)
	Abort(context.Context, string, string) error
	RespondInteraction(context.Context, string, protocol.InteractionResponse) error
	StartBash(context.Context, string, protocol.BashExecutionInput) (protocol.BashExecution, error)
	Bash(context.Context, string, string) (protocol.BashExecution, error)
	AbortBash(context.Context, string, string) error
	BashHistory(context.Context, string, uint64, int) (protocol.BashHistoryPage, error)
	Subagent(context.Context, string, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	SubagentTranscript(context.Context, string, string, string) (protocol.SubagentTranscript, error)
	SubagentEvents(context.Context, string, string, string, int64) (protocol.SubagentLiveEventPage, error)
}

type runtimeSessionService struct {
	manager             *kitsession.Manager
	availableProviders  func(context.Context) []string
	modelContextWindow  func(string) int
	fileIndexes         *sessionFileIndexCache
	workspaces          *kitworkspace.Service
	diffs               *kitworkingdiff.Service
	annotations         *kitannotation.Service
	annotationCursorKey []byte
	subagents           *subagent.Supervisor
	subagentTools       *subagent.ToolService
	attachments         attachment.Store
}

func (s runtimeSessionService) Create(
	ctx context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	record, err := s.manager.Create(ctx, kitsession.CreateInput{
		ID: input.ID, CWD: input.CWD, Name: input.Name, Model: input.Model,
		ThinkingLevel: input.ThinkingLevel, Temporary: input.Temporary,
	})
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return s.projectSession(record), nil
}

func (s runtimeSessionService) Fork(
	ctx context.Context,
	sourceSessionID string,
	input protocol.ForkSessionInput,
) (protocol.SessionInfo, error) {
	result, err := s.manager.Fork(ctx, sourceSessionID, kitsession.ForkInput{ID: input.ID, Name: input.Name})
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return s.projectSession(result.Session), nil
}

func (s runtimeSessionService) workspaceService() (*kitworkspace.Service, error) {
	if s.workspaces == nil {
		return nil, &kitworkspace.Error{Code: kitworkspace.Unavailable, Message: "workspace service is unavailable"}
	}
	return s.workspaces, nil
}

func (s runtimeSessionService) ChangeCWD(
	ctx context.Context,
	sessionID string,
	input protocol.ChangeCWDInput,
) (protocol.ChangeWorkspaceCWDResult, error) {
	workspaces, err := s.workspaceService()
	if err != nil {
		return protocol.ChangeWorkspaceCWDResult{}, err
	}
	result, err := s.manager.ChangeCWDWithID(ctx, sessionID, input.MutationID, input.Path)
	if err != nil {
		return protocol.ChangeWorkspaceCWDResult{}, err
	}
	sessionInfo := s.projectSession(result.Session)
	workspaceRef := workspaces.Ref(sessionID, result.Session.CWD)
	return protocol.ChangeWorkspaceCWDResult{Session: sessionInfo, Workspace: workspaceRef}, nil
}

func (s runtimeSessionService) Rename(
	ctx context.Context,
	sessionID string,
	input protocol.RenameSessionInput,
) (protocol.SessionInfo, error) {
	record, err := s.manager.Rename(ctx, sessionID, input.Name)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return s.projectSession(record), nil
}

func (s runtimeSessionService) Delete(ctx context.Context, sessionID string) error {
	if err := s.manager.Delete(ctx, sessionID); err != nil {
		return err
	}
	if s.workspaces != nil {
		s.workspaces.RemoveSession(sessionID)
	}
	if s.diffs != nil {
		s.diffs.RemoveSession(sessionID)
	}
	if s.fileIndexes != nil {
		s.fileIndexes.removeSession(sessionID)
	}
	if s.annotations != nil {
		s.annotations.ForgetSession(sessionID)
	}
	return nil
}

func (s runtimeSessionService) DisposeTemporary(ctx context.Context, sessionID string) error {
	if err := s.manager.DisposeTemporary(ctx, sessionID); err != nil {
		return err
	}
	if s.workspaces != nil {
		s.workspaces.RemoveSession(sessionID)
	}
	if s.diffs != nil {
		s.diffs.RemoveSession(sessionID)
	}
	if s.annotations != nil {
		s.annotations.ForgetSession(sessionID)
	}
	if s.attachments != nil {
		return s.attachments.RemoveSession(ctx, sessionID)
	}
	return nil
}

func (s runtimeSessionService) List(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	records, err := s.manager.List(ctx, cwd)
	if err != nil {
		return nil, err
	}
	result := make([]protocol.SessionInfo, 0, len(records))
	for _, record := range records {
		result = append(result, s.projectSession(record))
	}
	return result, nil
}

func (s runtimeSessionService) RefreshModels(ctx context.Context) (protocol.ModelCatalog, error) {
	if err := s.manager.RefreshModelCatalog(ctx); err != nil {
		return protocol.ModelCatalog{}, err
	}
	return s.Models(ctx)
}

func (s runtimeSessionService) Models(ctx context.Context) (protocol.ModelCatalog, error) {
	models, err := s.manager.ModelCapabilities(ctx)
	if err != nil {
		return protocol.ModelCatalog{}, err
	}
	available := map[string]bool{}
	if s.availableProviders == nil {
		for _, model := range models {
			available[model.Provider] = true
		}
	} else {
		for _, provider := range s.availableProviders(ctx) {
			available[provider] = true
		}
	}
	result := protocol.ModelCatalog{Models: make([]protocol.ModelCapability, 0, len(models))}
	for _, model := range models {
		if s.modelContextWindow != nil {
			if contextWindow := s.modelContextWindow(model.ID); contextWindow > 0 {
				model.ContextWindow = contextWindow
				model.MaxInputTokens = contextWindow
			}
		}
		thinking := make([]protocol.ThinkingLevel, 0, len(model.ThinkingLevels))
		for _, level := range model.ThinkingLevels {
			thinking = append(thinking, protocol.ThinkingLevel(level))
		}
		inputs := make([]protocol.ModelInputKind, 0, len(model.Inputs))
		for _, input := range model.Inputs {
			inputs = append(inputs, protocol.ModelInputKind(input))
		}
		result.Models = append(result.Models, protocol.ModelCapability{
			ID: model.ID, Name: model.Name, Provider: model.Provider, API: model.API,
			ContextWindow: model.ContextWindow, MaxInputTokens: model.MaxInputTokens, MaxOutputTokens: model.MaxOutputTokens,
			ThinkingLevels: thinking, Inputs: inputs, Available: available[model.Provider],
		})
	}
	return result, nil
}

func (s runtimeSessionService) Workspace(ctx context.Context, sessionID string) (protocol.WorkspaceRef, error) {
	workspaces, err := s.workspaceService()
	if err != nil {
		return protocol.WorkspaceRef{}, err
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.WorkspaceRef{}, err
	}
	return workspaces.Ref(sessionID, record.CWD), nil
}

func (s runtimeSessionService) ListDirectory(ctx context.Context, sessionID string, input protocol.ListDirectoryInput) (protocol.DirectoryPage, error) {
	workspaces, err := s.workspaceService()
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	result, err := workspaces.List(ctx, sessionID, record.CWD, input)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DirectoryPage{}, err
	}
	if current.CWD != record.CWD {
		return protocol.DirectoryPage{}, &kitworkspace.Error{Code: kitworkspace.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) ReadWorkspaceFile(ctx context.Context, sessionID string, input protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error) {
	workspaces, err := s.workspaceService()
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	result, err := workspaces.Read(ctx, sessionID, record.CWD, input)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.WorkspaceFileRead{}, err
	}
	if current.CWD != record.CWD {
		return protocol.WorkspaceFileRead{}, &kitworkspace.Error{Code: kitworkspace.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) ListDiffTargets(ctx context.Context, sessionID string, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	if s.diffs == nil {
		return protocol.DiffTargetCatalog{}, &kitworkingdiff.Error{Code: kitworkingdiff.Unavailable, Message: "diff service is unavailable"}
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	result, err := s.diffs.ListTargets(ctx, sessionID, record.CWD, input)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	if current.CWD != record.CWD {
		return protocol.DiffTargetCatalog{}, &kitworkingdiff.Error{Code: kitworkingdiff.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) ObserveDiff(ctx context.Context, sessionID string, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	if s.diffs == nil {
		return protocol.DiffPage{}, &kitworkingdiff.Error{Code: kitworkingdiff.Unavailable, Message: "diff service is unavailable"}
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	result, err := s.diffs.ObserveTarget(ctx, sessionID, record.CWD, input)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if current.CWD != record.CWD {
		return protocol.DiffPage{}, &kitworkingdiff.Error{Code: kitworkingdiff.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) ObserveWorkingTree(ctx context.Context, sessionID string, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	if s.diffs == nil {
		return protocol.WorkingTreePage{}, &kitworkingdiff.Error{Code: kitworkingdiff.Unavailable, Message: "diff service is unavailable"}
	}
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	result, err := s.diffs.Observe(ctx, sessionID, record.CWD, input)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	if current.CWD != record.CWD {
		return protocol.WorkingTreePage{}, &kitworkingdiff.Error{Code: kitworkingdiff.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) ReadFileDiff(ctx context.Context, sessionID string, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	if s.diffs == nil {
		return protocol.FileDiffPage{}, &kitworkingdiff.Error{Code: kitworkingdiff.Unavailable, Message: "diff service is unavailable"}
	}
	var record kitsession.SessionRecord
	var err error
	if input.AnnotationID != 0 {
		var release func()
		record, release, err = s.manager.BeginSessionOperationRecord(ctx, sessionID)
		if err != nil {
			return protocol.FileDiffPage{}, err
		}
		defer release()
	} else {
		record, err = s.manager.Get(ctx, sessionID)
		if err != nil {
			return protocol.FileDiffPage{}, err
		}
	}
	var result protocol.FileDiffPage
	if input.AnnotationID != 0 {
		if s.annotations == nil {
			return protocol.FileDiffPage{}, &kitworkingdiff.Error{Code: kitworkingdiff.Unavailable, Message: "annotation evidence is unavailable"}
		}
		target, authorizeErr := s.annotations.AuthorizeDiffRead(ctx, sessionID, record.CWD, input.AnnotationID, input)
		if authorizeErr != nil {
			return protocol.FileDiffPage{}, authorizeErr
		}
		result, err = s.diffs.ReadFileForAnnotation(ctx, sessionID, record.CWD, input, target)
	} else {
		result, err = s.diffs.ReadFile(ctx, sessionID, record.CWD, input)
	}
	if err != nil {
		return protocol.FileDiffPage{}, err
	}
	current, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.FileDiffPage{}, err
	}
	if current.CWD != record.CWD {
		return protocol.FileDiffPage{}, &kitworkingdiff.Error{Code: kitworkingdiff.StaleWorkspace, Message: "the session workspace changed"}
	}
	return result, nil
}

func (s runtimeSessionService) prepareAnnotationSession(ctx context.Context, record kitsession.SessionRecord) error {
	if s.annotations == nil {
		return nil
	}
	if !record.Persistent {
		s.annotations.SetTemporary(record.ID)
	}
	return s.manager.ReconcileAnnotationSubmissions(ctx, record.ID)
}

func (s runtimeSessionService) ListAnnotations(ctx context.Context, sessionID string, input protocol.ListAnnotationsInput) (protocol.AnnotationPage, error) {
	if s.annotations == nil {
		return protocol.AnnotationPage{}, fmt.Errorf("annotation service is unavailable")
	}
	record, release, err := s.manager.BeginSessionOperationRecord(ctx, sessionID)
	if err != nil {
		return protocol.AnnotationPage{}, err
	}
	defer release()
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return protocol.AnnotationPage{}, err
	}
	after, err := s.decodeAnnotationCursor(sessionID, input.Cursor)
	if err != nil {
		return protocol.AnnotationPage{}, fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = protocol.DefaultAnnotationPageSize
	}
	records, stale, err := s.annotations.List(ctx, sessionID, record.CWD, after, pageSize+1)
	if err != nil {
		return protocol.AnnotationPage{}, err
	}
	page := protocol.AnnotationPage{SessionID: sessionID, Entries: make([]protocol.Annotation, 0, min(len(records), pageSize))}
	for _, annotation := range records[:min(len(records), pageSize)] {
		page.Entries = append(page.Entries, projectAnnotation(annotation, stale[annotation.ID]))
	}
	if len(records) > pageSize {
		page.NextCursor = s.encodeAnnotationCursor(sessionID, records[pageSize-1].ID)
	}
	return page, nil
}

func (s runtimeSessionService) CreateAnnotation(ctx context.Context, sessionID string, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
	if err := input.Validate(); err != nil {
		return protocol.Annotation{}, fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	if s.annotations == nil {
		return protocol.Annotation{}, fmt.Errorf("annotation service is unavailable")
	}
	record, release, err := s.manager.BeginSessionOperationRecord(ctx, sessionID)
	if err != nil {
		return protocol.Annotation{}, err
	}
	defer release()
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return protocol.Annotation{}, err
	}
	created, err := s.annotations.Create(ctx, sessionID, record.CWD, input.Anchor, input.Body)
	if err != nil {
		return protocol.Annotation{}, err
	}
	return projectAnnotation(created, ""), nil
}

func (s runtimeSessionService) UpdateAnnotation(ctx context.Context, sessionID string, input protocol.UpdateAnnotationInput) (protocol.Annotation, error) {
	if err := input.Validate(); err != nil {
		return protocol.Annotation{}, fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	if s.annotations == nil {
		return protocol.Annotation{}, fmt.Errorf("annotation service is unavailable")
	}
	record, release, err := s.manager.BeginSessionOperationRecord(ctx, sessionID)
	if err != nil {
		return protocol.Annotation{}, err
	}
	defer release()
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return protocol.Annotation{}, err
	}
	updated, err := s.annotations.Update(ctx, sessionID, record.CWD, input.AnnotationID, input.Body)
	if err != nil {
		return protocol.Annotation{}, err
	}
	return projectAnnotation(updated, ""), nil
}

func (s runtimeSessionService) DeleteAnnotation(ctx context.Context, sessionID string, input protocol.DeleteAnnotationInput) error {
	if err := input.Validate(); err != nil {
		return fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	if s.annotations == nil {
		return fmt.Errorf("annotation service is unavailable")
	}
	record, release, err := s.manager.BeginSessionOperationRecord(ctx, sessionID)
	if err != nil {
		return err
	}
	defer release()
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return err
	}
	if err := s.annotations.Delete(ctx, sessionID, input.AnnotationID); err != nil {
		return err
	}
	return nil
}

func projectAnnotation(record kitannotation.Record, stale protocol.AnnotationStaleReason) protocol.Annotation {
	deferred := stale == protocol.AnnotationValidationDeferred
	if deferred {
		stale = ""
	}
	return protocol.Annotation{
		ID: record.ID, SessionID: record.SessionID,
		Anchor:     record.Anchor,
		DiffTarget: record.DiffTarget,
		Body:       record.Body,
		Preview:    protocol.AnnotationPreview{StartLine: record.Preview.StartLine, EndLine: record.Preview.EndLine, Text: record.Preview.Text, Truncated: record.Preview.Truncated},
		Stale:      stale != "", StaleReason: stale, ValidationDeferred: deferred,
	}
}

func projectAnnotationSummary(record kitannotation.Record, stale protocol.AnnotationStaleReason) protocol.AnnotationSummary {
	annotation := projectAnnotation(record, stale)
	return protocol.AnnotationSummary{
		ID: annotation.ID, Anchor: annotation.Anchor, DiffTarget: annotation.DiffTarget,
		BodyPreview: truncateAnnotationSummary(annotation.Body),
		Preview:     truncateAnnotationSummary(annotation.Preview.Text),
		Stale:       annotation.Stale, StaleReason: annotation.StaleReason, ValidationDeferred: annotation.ValidationDeferred,
	}
}

func truncateAnnotationSummary(value string) string {
	if len(value) <= protocol.MaxAnnotationSummaryTextBytes {
		return value
	}
	value = value[:protocol.MaxAnnotationSummaryTextBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type annotationCursor struct {
	SessionID string `json:"sessionId"`
	After     uint64 `json:"after"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (s runtimeSessionService) encodeAnnotationCursor(sessionID string, id uint64) string {
	payload, _ := json.Marshal(annotationCursor{SessionID: sessionID, After: id, ExpiresAt: time.Now().Add(5 * time.Minute).Unix()})
	signature := hmac.New(sha256.New, s.annotationCursorKey)
	_, _ = signature.Write(payload)
	return "annotation_" + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func (s runtimeSessionService) decodeAnnotationCursor(sessionID, cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	encoded, encodedSignature, found := strings.Cut(strings.TrimPrefix(cursor, "annotation_"), ".")
	if !strings.HasPrefix(cursor, "annotation_") || !found || len(s.annotationCursorKey) == 0 {
		return 0, fmt.Errorf("annotation cursor is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, fmt.Errorf("annotation cursor is invalid")
	}
	provided, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return 0, fmt.Errorf("annotation cursor is invalid")
	}
	signature := hmac.New(sha256.New, s.annotationCursorKey)
	_, _ = signature.Write(payload)
	if !hmac.Equal(provided, signature.Sum(nil)) {
		return 0, fmt.Errorf("annotation cursor is invalid")
	}
	var value annotationCursor
	if json.Unmarshal(payload, &value) != nil || value.SessionID != sessionID || value.After == 0 || value.ExpiresAt < time.Now().Unix() {
		return 0, fmt.Errorf("annotation cursor is unavailable")
	}
	return value.After, nil
}

func (s runtimeSessionService) FileIndex(ctx context.Context, sessionID string, refresh bool) (protocol.SessionFileIndex, error) {
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.SessionFileIndex{}, err
	}
	var indexed fileindex.Result
	if s.fileIndexes != nil {
		indexed, err = s.fileIndexes.load(ctx, sessionID, record.CWD, refresh)
	} else {
		indexed, err = fileindex.ScanResult(ctx, record.CWD, fileindex.Options{})
	}
	if err != nil {
		return protocol.SessionFileIndex{}, err
	}
	result := protocol.SessionFileIndex{SessionID: sessionID, CWD: record.CWD, Entries: make([]protocol.FileIndexEntry, 0, len(indexed.Entries)), Truncated: indexed.Truncated}
	for _, entry := range indexed.Entries {
		result.Entries = append(result.Entries, protocol.FileIndexEntry{Path: entry.Path, IsDir: entry.IsDir})
	}
	return result, nil
}

func (s runtimeSessionService) VCS(ctx context.Context, sessionID string) (protocol.SessionVCSStatus, error) {
	update, err := s.manager.VCS(ctx, sessionID)
	if err != nil {
		return protocol.SessionVCSStatus{}, err
	}
	result := projectVCSStatus(sessionID, update)
	return result, result.Validate()
}

func (s runtimeSessionService) Snapshot(ctx context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	if s.annotations != nil {
		release, err := s.manager.BeginSessionOperation(ctx, sessionID)
		if err != nil {
			return protocol.SessionSnapshot{}, err
		}
		defer release()
	}
	workspaces, workspaceErr := s.workspaceService()
	if workspaceErr != nil {
		return protocol.SessionSnapshot{}, workspaceErr
	}
	snapshot, err := s.manager.Snapshot(ctx, sessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	previousCursor := ""
	if snapshot.HasMoreMessages {
		previousCursor = strconv.FormatUint(snapshot.PreviousMessageCursor, 10)
	}
	if err := s.prepareAnnotationSession(ctx, snapshot.Session); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	projectedWorkspace := workspaces.Ref(snapshot.Session.ID, snapshot.Session.CWD)
	workspaceRef := &projectedWorkspace
	annotations := []protocol.AnnotationSummary(nil)
	if s.annotations != nil {
		var after uint64
		for {
			records, stale, listErr := s.annotations.List(ctx, sessionID, snapshot.Session.CWD, after, protocol.MaxAnnotationPageSize)
			if listErr != nil {
				return protocol.SessionSnapshot{}, listErr
			}
			for _, record := range records {
				annotations = append(annotations, projectAnnotationSummary(record, stale[record.ID]))
				after = record.ID
			}
			if len(records) < protocol.MaxAnnotationPageSize {
				break
			}
		}
	}
	result := protocol.SessionSnapshot{
		Session: s.projectSession(snapshot.Session), Workspace: workspaceRef, ActiveTurnID: snapshot.ActiveRunID,
		ActiveBashExecutionID: snapshot.ActiveBashExecutionID,
		EventStreamID:         snapshot.EventStreamID, EventCursor: snapshot.EventCursor,
		EventReplayFrom: snapshot.EventReplayFrom, EventReplayAvailable: snapshot.EventReplayAvailable,
		ContextTokens: snapshot.ContextTokens, ContextWindow: snapshot.ContextWindow,
		Usage:                 projectSessionUsage(snapshot.Usage),
		Messages:              make([]protocol.TranscriptMessage, 0, len(snapshot.Messages)),
		PreviousMessageCursor: previousCursor, HasMoreMessages: snapshot.HasMoreMessages,
		PendingBoundaries: make([]protocol.PendingBoundary, 0, len(snapshot.Boundaries)),
		PromptCommands:    make([]protocol.PromptCommand, 0, len(snapshot.PromptCommands)),
		FollowUps: protocol.FollowUpQueue{
			Count: snapshot.FollowUps.Count, Previews: append([]string(nil), snapshot.FollowUps.Previews...), AnnotationIDs: append([]uint64(nil), snapshot.FollowUps.AnnotationIDs...),
		},
		Warnings:              append([]string(nil), snapshot.Warnings...),
		MCPWarnings:           append([]string(nil), snapshot.MCPWarnings...),
		MCPServers:            make([]protocol.MCPServerStatus, 0, len(snapshot.MCPServers)),
		SubagentDefinitions:   make([]protocol.SubagentDefinition, 0, len(snapshot.SubagentDefinitions)),
		SubagentDiagnostics:   make([]protocol.SubagentDiagnostic, 0, len(snapshot.SubagentDiagnostics)),
		SubagentConversations: make([]protocol.SubagentConversation, 0, len(snapshot.SubagentConversations)),
		SubagentMailbox:       make([]protocol.SubagentMailboxItem, 0, len(snapshot.SubagentMailbox)),
		PendingInteractions:   make([]protocol.InteractionRequest, 0, len(snapshot.PendingInteractions)),
		Annotations:           annotations,
	}
	for _, server := range snapshot.MCPServers {
		result.MCPServers = append(result.MCPServers, protocol.MCPServerStatus{
			Name: server.Name, State: server.State, Transport: server.Transport, ToolCount: server.ToolCount,
			OAuthSaved: server.OAuthSaved, Description: server.Description, Source: server.Source,
			ConfigPath: server.ConfigPath, LastError: server.LastError,
		})
	}
	if snapshot.Scratchpad != nil {
		projectedScratchpad := projectScratchpad(*snapshot.Scratchpad)
		result.Scratchpad = &projectedScratchpad
	}
	if snapshot.ProviderRetry != nil {
		result.ProviderRetry = &protocol.ProviderRetry{
			Count: snapshot.ProviderRetry.Count, RetryAt: snapshot.ProviderRetry.RetryAt.Format(time.RFC3339Nano),
		}
	}
	if snapshot.ActiveCompaction != nil {
		result.ActiveCompaction = &protocol.ActiveCompaction{ID: snapshot.ActiveCompaction.ID, TurnID: snapshot.ActiveCompaction.RunID}
	}
	for _, interaction := range snapshot.PendingInteractions {
		result.PendingInteractions = append(result.PendingInteractions, projectInteractionRequest(interaction))
	}
	for _, item := range snapshot.SubagentMailbox {
		result.SubagentMailbox = append(result.SubagentMailbox, protocol.SubagentMailboxItem{
			ID: item.ID, Kind: string(item.Kind), ConversationID: item.ConversationID,
			AgentName: item.AgentName, State: item.State, Summary: item.Summary, Error: item.Error,
			CreatedAt: item.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	for _, definition := range snapshot.SubagentDefinitions {
		result.SubagentDefinitions = append(result.SubagentDefinitions, protocol.SubagentDefinition{
			Name: definition.Name, Description: definition.Description, Model: definition.Model,
			Source: protocol.SubagentSource{Kind: string(definition.Source.Kind), Path: definition.Source.Path, PluginID: definition.Source.PluginID},
		})
	}
	for _, diagnostic := range snapshot.SubagentDiagnostics {
		result.SubagentDiagnostics = append(result.SubagentDiagnostics, protocol.SubagentDiagnostic{
			Severity: diagnostic.Severity, Code: diagnostic.Code, Message: diagnostic.Message,
			Source: protocol.SubagentSource{Kind: string(diagnostic.Source.Kind), Path: diagnostic.Source.Path, PluginID: diagnostic.Source.PluginID},
		})
	}
	for _, conversation := range snapshot.SubagentConversations {
		projected := protocol.SubagentConversation{
			ID: conversation.ID, AgentName: conversation.AgentName, Model: conversation.Model,
			ThinkingLevel: conversation.ThinkingLevel, State: conversation.State, Generation: conversation.Generation,
			ActiveTaskID: conversation.ActiveTaskID, QueuedTasks: conversation.QueuedTasks,
			LastCompletedTaskID: conversation.LastCompletedTaskID, LastResultSummary: conversation.LastResultSummary,
			UpdatedAt: conversation.UpdatedAt.Format(time.RFC3339Nano),
			Tasks:     make([]protocol.SubagentTask, 0, len(conversation.Tasks)),
		}
		for _, task := range conversation.Tasks {
			projectedTask := protocol.SubagentTask{
				ID: task.ID, Sequence: task.Sequence, State: task.State,
				CancellationGeneration: task.CancellationGeneration,
				QueuedAt:               task.QueuedAt.Format(time.RFC3339Nano),
				ResultSummary:          task.ResultSummary, Error: task.Error,
			}
			if task.StartedAt != nil {
				projectedTask.StartedAt = task.StartedAt.Format(time.RFC3339Nano)
			}
			if task.FinishedAt != nil {
				projectedTask.FinishedAt = task.FinishedAt.Format(time.RFC3339Nano)
			}
			projected.Tasks = append(projected.Tasks, projectedTask)
		}
		result.SubagentConversations = append(result.SubagentConversations, projected)
	}
	if source := snapshot.PluginFooter; source != nil {
		footer := &protocol.PluginFooter{LocationHidden: source.LocationHidden}
		for _, item := range source.Items {
			target := protocol.PluginFooterItem{ID: item.ID, PluginID: item.PluginID, Instance: item.Instance}
			for _, segment := range item.Content {
				target.Content = append(target.Content, protocol.PluginFooterSegment{Text: segment.Text, Style: protocol.PluginFooterStyle(segment.Style)})
			}
			footer.Items = append(footer.Items, target)
		}
		result.PluginFooter = footer
	}
	for _, command := range snapshot.PluginCommands {
		result.PluginCommands = append(result.PluginCommands, protocol.PluginCommand{ID: command.ID, LocalID: command.LocalID, PluginID: command.PluginID, Instance: command.Instance, Description: command.Description, ArgName: command.ArgName, Category: command.Category})
	}
	for _, command := range snapshot.PromptCommands {
		result.PromptCommands = append(result.PromptCommands, protocol.PromptCommand{
			Name: command.Name, Description: command.Description, ArgumentHint: command.ArgumentHint,
			Source: command.Source, Location: command.Location,
		})
	}
	result.Messages = append(result.Messages, projectTranscriptMessages(snapshot.Messages)...)
	for _, boundary := range snapshot.Boundaries {
		result.PendingBoundaries = append(result.PendingBoundaries, protocol.PendingBoundary{
			ID: boundary.ID, Kind: boundary.Kind, Source: boundary.Source,
			Content:    projectTranscriptContent(boundary.Content),
			Details:    append(json.RawMessage(nil), boundary.Details...),
			AcceptedAt: boundary.AcceptedAt.Format(time.RFC3339Nano),
		})
	}
	return result, nil
}

func (s runtimeSessionService) BashHistory(ctx context.Context, sessionID string, before uint64, limit int) (protocol.BashHistoryPage, error) {
	page, err := s.manager.BashHistory(ctx, sessionID, before, limit)
	if err != nil {
		return protocol.BashHistoryPage{}, err
	}
	result := protocol.BashHistoryPage{
		SessionID: sessionID, Entries: make([]protocol.BashHistoryEntry, 0, len(page.Entries)),
		HasMore: page.HasMore,
	}
	for _, execution := range page.Entries {
		entry := protocol.BashHistoryEntry{
			ID: execution.ID, Sequence: execution.Sequence, Command: execution.Command,
			Status: string(execution.Status), ExcludeFromContext: execution.ExcludeFromContext,
			StartedAt: execution.StartedAt.UTC().Format(time.RFC3339Nano),
		}
		if execution.CompletedAt != nil {
			entry.CompletedAt = execution.CompletedAt.UTC().Format(time.RFC3339Nano)
		}
		result.Entries = append(result.Entries, entry)
	}
	if page.HasMore {
		result.NextCursor = strconv.FormatUint(page.Cursor, 10)
	}
	return result, nil
}

func (s runtimeSessionService) MessagePage(ctx context.Context, sessionID string, query protocol.MessagePageQuery) (protocol.MessagePage, error) {
	page, err := s.manager.MessagePage(ctx, sessionID, kitsession.MessagePageQuery{
		Before: query.Before, Limit: query.Limit, Roles: append([]string(nil), query.Roles...),
	})
	if err != nil {
		return protocol.MessagePage{}, err
	}
	result := protocol.MessagePage{
		SessionID: sessionID, Messages: projectTranscriptMessages(page.Messages), HasMore: page.HasMore,
	}
	if page.HasMore {
		result.NextCursor = strconv.FormatUint(page.NextCursor, 10)
	}
	return result, nil
}

func (s runtimeSessionService) TranscriptPage(ctx context.Context, sessionID, before string) (protocol.TranscriptPage, error) {
	cursor, err := strconv.ParseUint(before, 10, 64)
	if err != nil || cursor == 0 {
		return protocol.TranscriptPage{}, fmt.Errorf("%w: transcript cursor is invalid", errInvalidSessionRequest)
	}
	page, err := s.manager.TranscriptPage(ctx, sessionID, cursor)
	if err != nil {
		return protocol.TranscriptPage{}, err
	}
	previousCursor := ""
	if page.HasMoreMessages {
		previousCursor = strconv.FormatUint(page.PreviousMessageCursor, 10)
	}
	return protocol.TranscriptPage{
		SessionID: sessionID, Messages: projectTranscriptMessages(page.Messages),
		PreviousMessageCursor: previousCursor, HasMoreMessages: page.HasMoreMessages,
	}, nil
}

func projectTranscriptMessages(messages []kitsession.TranscriptMessage) []protocol.TranscriptMessage {
	result := make([]protocol.TranscriptMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, protocol.TranscriptMessage{
			ID: message.ID, TurnID: message.TurnID, Sequence: message.Sequence,
			Role: message.Role, Content: projectTranscriptContent(message.Content),
			StopReason: message.StopReason, ErrorMessage: message.ErrorMessage,
			ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			BoundaryID: message.BoundaryID, BoundaryKind: message.BoundaryKind,
			BoundarySource: message.BoundarySource,
			Details:        append(json.RawMessage(nil), message.Details...),
			IsError:        message.IsError, CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return result
}

func (s runtimeSessionService) SubagentEvents(ctx context.Context, sessionID, conversationID, streamID string, after int64) (protocol.SubagentLiveEventPage, error) {
	conversation, err := s.subagents.Conversation(ctx, subagent.ConversationID(conversationID))
	if err != nil || conversation.OwnerSessionID != sessionID {
		if err == nil {
			err = subagent.ErrNotFound
		}
		return protocol.SubagentLiveEventPage{}, err
	}
	page, err := s.subagents.LiveEvents(ctx, conversation.ID, streamID, after)
	if err != nil {
		return protocol.SubagentLiveEventPage{}, err
	}
	result := protocol.SubagentLiveEventPage{
		StreamID: page.StreamID, FirstSequence: page.FirstSequence,
		LastSequence: page.LastSequence, ResyncRequired: page.ResyncRequired,
		Events: make([]protocol.SubagentLiveEvent, 0, len(page.Events)),
	}
	for _, event := range page.Events {
		result.Events = append(result.Events, protocol.SubagentLiveEvent{
			Sequence: event.Sequence, Kind: event.Kind, TurnID: event.TurnID,
			MessageID: event.MessageID, ContentIndex: event.ContentIndex,
			Delta: event.Delta, Text: event.Text, ToolCallID: event.ToolCallID,
			ToolName: event.ToolName, IsError: event.IsError,
		})
	}
	return result, nil
}

func (s runtimeSessionService) SubagentTranscript(ctx context.Context, sessionID, conversationID, before string) (protocol.SubagentTranscript, error) {
	var cursor uint64
	if before != "" {
		parsed, err := strconv.ParseUint(before, 10, 64)
		if err != nil || parsed == 0 {
			return protocol.SubagentTranscript{}, fmt.Errorf("%w: transcript cursor is invalid", errInvalidSessionRequest)
		}
		cursor = parsed
	}
	conversation, err := s.subagents.Conversation(ctx, subagent.ConversationID(conversationID))
	if err != nil || conversation.OwnerSessionID != sessionID {
		if err == nil {
			err = subagent.ErrNotFound
		}
		return protocol.SubagentTranscript{}, err
	}
	transcript, err := s.subagents.Transcript(ctx, conversation.ID, cursor)
	if err != nil {
		return protocol.SubagentTranscript{}, err
	}
	result := protocol.SubagentTranscript{ConversationID: conversationID, HasMoreMessages: transcript.HasMoreMessages, Messages: make([]protocol.TranscriptMessage, 0, len(transcript.Messages))}
	if transcript.HasMoreMessages {
		result.PreviousMessageCursor = strconv.FormatUint(transcript.PreviousMessageCursor, 10)
	}
	for _, message := range transcript.Messages {
		projected := protocol.TranscriptMessage{
			ID: message.ID, TurnID: message.TurnID, Sequence: message.Sequence, Role: message.Role,
			StopReason: message.StopReason, ErrorMessage: message.ErrorMessage,
			ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			BoundaryID: message.BoundaryID, BoundaryKind: message.BoundaryKind, BoundarySource: message.BoundarySource,
			Details: append(json.RawMessage(nil), message.Details...), IsError: message.IsError,
			CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		}
		for _, block := range message.Content {
			projected.Content = append(projected.Content, transcriptContentFromFields(protocol.TranscriptContentKind(block.Kind), block.Text, block.ToolCallID, block.ToolName, block.Arguments, block.ArgumentsTruncated, block.Filename, block.MediaType, ""))
		}
		result.Messages = append(result.Messages, projected)
	}
	return result, nil
}

func (s runtimeSessionService) Subagent(ctx context.Context, sessionID string, input protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	if s.subagents == nil || s.subagentTools == nil {
		return protocol.SubagentOperationResult{}, subagent.ErrClosed
	}
	if err := input.Validate(); err != nil {
		return protocol.SubagentOperationResult{}, fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	if _, err := s.manager.Get(ctx, sessionID); err != nil {
		return protocol.SubagentOperationResult{}, err
	}
	result := protocol.SubagentOperationResult{}
	switch input.Action {
	case protocol.SubagentListAgents:
		loaded, err := s.manager.SubagentDefinitions(ctx, sessionID)
		if err != nil {
			return result, err
		}
		for _, definition := range loaded.Catalog.Definitions() {
			result.Definitions = append(result.Definitions, protocol.SubagentDefinition{
				Name: definition.Name, Description: definition.Description, Model: definition.Model,
				Source: protocol.SubagentSource{Kind: string(definition.Source.Kind), Path: definition.Source.Path, PluginID: definition.Source.PluginID},
			})
		}
		for _, diagnostic := range loaded.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, protocol.SubagentDiagnostic{
				Severity: string(diagnostic.Severity), Code: diagnostic.Code, Message: diagnostic.Message,
				Source: protocol.SubagentSource{Kind: string(diagnostic.Source.Kind), Path: diagnostic.Source.Path, PluginID: diagnostic.Source.PluginID},
			})
		}
		conversations, err := s.subagents.ListConversations(ctx, sessionID)
		if err != nil {
			return result, err
		}
		for _, conversation := range conversations {
			projected, err := s.projectSubagentConversation(ctx, conversation)
			if err != nil {
				return result, err
			}
			result.Conversations = append(result.Conversations, projected)
		}
	case protocol.SubagentStart:
		loaded, err := s.manager.SubagentDefinitions(ctx, sessionID)
		if err != nil {
			return result, err
		}
		conversation, task, warning, err := s.subagentTools.Start(ctx, sessionID, loaded.Catalog, input.Agent, input.Message)
		if err != nil {
			return result, err
		}
		projected, err := s.projectSubagentConversation(ctx, conversation)
		if err != nil {
			return result, err
		}
		projectedTask := projectSubagentTask(task)
		result.Conversation, result.Task, result.Warning = &projected, &projectedTask, warning
	case protocol.SubagentMessage:
		conversation, task, err := s.subagentTools.Message(ctx, sessionID, input.Agent, subagent.ConversationID(input.ConversationID), input.Message)
		if err != nil {
			return result, err
		}
		projected, err := s.projectSubagentConversation(ctx, conversation)
		if err != nil {
			return result, err
		}
		projectedTask := projectSubagentTask(task)
		result.Conversation, result.Task = &projected, &projectedTask
	case protocol.SubagentInspect:
		inspected, err := s.subagentTools.Inspect(ctx, sessionID, input.Agent, subagent.ConversationID(input.ConversationID), subagent.TaskID(input.TaskID))
		if err != nil {
			return result, err
		}
		if inspected.Conversation != nil {
			projected, err := s.projectSubagentConversation(ctx, *inspected.Conversation)
			if err != nil {
				return result, err
			}
			result.Conversation = &projected
		}
		if inspected.Task != nil {
			projected := projectSubagentTask(*inspected.Task)
			result.Task = &projected
		}
		for _, task := range inspected.Tasks {
			result.Tasks = append(result.Tasks, projectSubagentTask(task))
		}
	case protocol.SubagentWait:
		task, timedOut, err := s.subagentTools.Wait(ctx, sessionID, subagent.TaskID(input.TaskID), time.Duration(input.TimeoutMS)*time.Millisecond)
		if err != nil {
			return result, err
		}
		projected := projectSubagentTask(task)
		result.Task, result.TimedOut = &projected, timedOut
	case protocol.SubagentCancel:
		task, err := s.subagents.Task(ctx, subagent.TaskID(input.TaskID))
		if err != nil || task.OwnerSessionID != sessionID {
			if err == nil {
				err = subagent.ErrNotFound
			}
			return result, err
		}
		if task.CancellationGeneration != input.Generation {
			return result, subagent.ErrConflict
		}
		task, err = s.subagents.Cancel(ctx, task.ID, input.Generation, "canceled by client")
		if err != nil {
			return result, err
		}
		projected := projectSubagentTask(task)
		result.Task = &projected
	case protocol.SubagentDismiss:
		inspected, err := s.subagentTools.Inspect(ctx, sessionID, input.Agent, subagent.ConversationID(input.ConversationID), "")
		if err != nil || inspected.Conversation == nil {
			return result, err
		}
		if inspected.Conversation.Generation != input.Generation {
			return result, subagent.ErrConflict
		}
		if _, err := s.subagents.Dismiss(ctx, inspected.Conversation.ID, input.Generation, "dismissed by client"); err != nil {
			return result, err
		}
		result.Dismissed = true
	}
	return result, nil
}

func (s runtimeSessionService) projectSubagentConversation(ctx context.Context, conversation subagent.Conversation) (protocol.SubagentConversation, error) {
	projected := protocol.SubagentConversation{
		ID: string(conversation.ID), AgentName: conversation.Agent.Name, Model: conversation.Model,
		ThinkingLevel: conversation.ThinkingLevel, State: string(conversation.State), Generation: conversation.Generation,
		ActiveTaskID: string(conversation.ActiveTaskID), QueuedTasks: conversation.QueuedTasks,
		LastCompletedTaskID: string(conversation.LastCompletedTaskID), LastResultSummary: conversation.LastResultSummary,
		UpdatedAt: conversation.UpdatedAt.Format(time.RFC3339Nano),
	}
	tasks, err := s.subagents.ListTasks(ctx, conversation.ID)
	if err != nil {
		return protocol.SubagentConversation{}, err
	}
	if len(tasks) > 20 {
		tasks = tasks[len(tasks)-20:]
	}
	for _, task := range tasks {
		projected.Tasks = append(projected.Tasks, projectSubagentTask(task))
	}
	return projected, nil
}

func projectSubagentTask(task subagent.Task) protocol.SubagentTask {
	projected := protocol.SubagentTask{
		ID: string(task.ID), Sequence: task.Sequence, State: string(task.State),
		CancellationGeneration: task.CancellationGeneration,
		QueuedAt:               task.QueuedAt.Format(time.RFC3339Nano), ResultSummary: task.ResultSummary, Error: task.Error,
	}
	if task.StartedAt != nil {
		projected.StartedAt = task.StartedAt.Format(time.RFC3339Nano)
	}
	if task.FinishedAt != nil {
		projected.FinishedAt = task.FinishedAt.Format(time.RFC3339Nano)
	}
	return projected
}

func projectSessionUsage(usage kitsession.SessionUsage) protocol.SessionUsage {
	return protocol.SessionUsage{
		Input: usage.Input, Output: usage.Output,
		CacheRead: usage.CacheRead, CacheWrite: usage.CacheWrite,
		Reasoning: usage.Reasoning, TotalTokens: usage.TotalTokens,
		Cost: protocol.SessionUsageCost{
			Input: usage.Cost.Input, Output: usage.Cost.Output,
			CacheRead: usage.Cost.CacheRead, CacheWrite: usage.Cost.CacheWrite,
			Total: usage.Cost.Total,
		},
	}
}

func projectSessionUsagePointer(usage *kitsession.SessionUsage) *protocol.SessionUsage {
	if usage == nil {
		return nil
	}
	projected := projectSessionUsage(*usage)
	return &projected
}

func transcriptContentFromFields(kind protocol.TranscriptContentKind, text, callID, toolName, arguments string, argumentsTruncated bool, filename, mediaType, attachmentID string) protocol.TranscriptContent {
	switch kind {
	case protocol.TranscriptContentText:
		return protocol.NewTranscriptContent(protocol.TextContent{Text: text})
	case protocol.TranscriptContentThinking:
		return protocol.NewTranscriptContent(protocol.ThinkingContent{Text: text})
	case protocol.TranscriptContentToolCall:
		return protocol.NewTranscriptContent(protocol.ToolCallContent{ToolCallID: callID, ToolName: toolName, Arguments: arguments, ArgumentsTruncated: argumentsTruncated})
	case protocol.TranscriptContentImage:
		return protocol.NewTranscriptContent(protocol.ImageContent{Filename: filename, MediaType: mediaType, AttachmentID: attachmentID})
	case protocol.TranscriptContentFile:
		return protocol.NewTranscriptContent(protocol.FileContent{Filename: filename, MediaType: mediaType, AttachmentID: attachmentID})
	case protocol.TranscriptContentAnnotations:
		return protocol.NewTranscriptContent(protocol.AnnotationsContent{})
	}
	return protocol.TranscriptContent{}
}

func projectTranscriptContent(content []kitsession.TranscriptContent) []protocol.TranscriptContent {
	result := make([]protocol.TranscriptContent, 0, len(content))
	for _, block := range content {
		projected := transcriptContentFromFields(protocol.TranscriptContentKind(block.Kind), block.Text, block.ToolCallID, block.ToolName, block.Arguments, block.ArgumentsTruncated, block.Filename, block.MediaType, block.AttachmentID)
		annotationsContent, annotationsOK := projected.Payload.(protocol.AnnotationsContent)
		for _, annotation := range block.Annotations {
			if !annotationsOK {
				break
			}
			anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorKind(annotation.Kind)}
			if anchor.Kind == protocol.AnnotationAnchorWorkspaceFile {
				anchor.WorkspaceFile = &protocol.WorkspaceFileAnnotationAnchor{
					WorkspaceID: annotation.WorkspaceID, Path: annotation.Path, FileRevision: annotation.FileRevision,
					StartLine: annotation.StartLine, EndLine: annotation.EndLine,
				}
			} else if anchor.Kind == protocol.AnnotationAnchorWorkingTreeDiff {
				anchor.WorkingTreeDiff = &protocol.WorkingTreeDiffAnnotationAnchor{
					TargetID: annotation.TargetID, TargetRevision: annotation.TargetRevision, Path: annotation.Path,
					FileRevision: annotation.FileRevision, Side: annotation.Side, StartLine: annotation.StartLine, EndLine: annotation.EndLine,
				}
			}
			var diffTarget *protocol.PinnedDiffTarget
			if annotation.TargetKind != "" {
				diffTarget = &protocol.PinnedDiffTarget{
					WorkspaceID: annotation.TargetWorkspaceID, Kind: annotation.TargetKind,
					Base: protocol.DiffEndpoint{Kind: annotation.TargetBaseKind, OID: annotation.TargetBaseOID},
					Head: protocol.DiffEndpoint{Kind: annotation.TargetHeadKind, OID: annotation.TargetHeadOID},
				}
			}
			annotationsContent.Annotations = append(annotationsContent.Annotations, protocol.SubmittedAnnotation{
				OriginalAnnotationID: annotation.ID, Anchor: anchor, DiffTarget: diffTarget, Body: annotation.Body,
				Preview: protocol.AnnotationPreview{StartLine: annotation.StartLine, EndLine: annotation.EndLine, Text: annotation.Preview, Truncated: annotation.Truncated},
			})
			projected.Payload = annotationsContent
		}
		result = append(result, projected)
	}
	return result
}

func (s runtimeSessionService) Events(ctx context.Context, sessionID, streamID string, after int64) (protocol.SessionEventBatch, error) {
	if _, err := s.workspaceService(); err != nil {
		return protocol.SessionEventBatch{}, err
	}
	page, err := s.manager.Events(ctx, sessionID, streamID, after)
	if err != nil {
		return protocol.SessionEventBatch{}, err
	}
	return s.projectSessionEventPage(page), nil
}

func (s runtimeSessionService) WaitEvents(ctx context.Context, sessionID, streamID string, after int64) (protocol.SessionEventBatch, error) {
	if _, err := s.workspaceService(); err != nil {
		return protocol.SessionEventBatch{}, err
	}
	page, err := s.manager.WaitEvents(ctx, sessionID, streamID, after)
	if err != nil {
		return protocol.SessionEventBatch{}, err
	}
	return s.projectSessionEventPage(page), nil
}

func projectInteractionRequest(request kitsession.InteractionRequest) protocol.InteractionRequest {
	result := protocol.InteractionRequest{ID: request.ID, SessionID: request.SessionID, TurnID: request.RunID, ToolCallID: request.ToolCallID, Kind: protocol.InteractionKind(request.Kind), Title: request.Title, Detail: request.Detail, CreatedAt: request.CreatedAt.Format(time.RFC3339Nano), Options: make([]protocol.InteractionOption, 0, len(request.Options)), Questions: make([]protocol.InteractionQuestion, 0, len(request.Questions))}
	if request.Plugin != nil {
		result.Plugin = &protocol.PluginInteractionOwner{PluginID: request.Plugin.PluginID, Instance: request.Plugin.Instance}
	}
	result.ConfirmLabel, result.CancelLabel = request.ConfirmLabel, request.CancelLabel
	result.Placeholder, result.InitialValue = request.Placeholder, request.InitialValue
	if request.DefaultValue != nil {
		value := *request.DefaultValue
		result.DefaultValue = &value
	}
	if request.Filterable != nil {
		value := *request.Filterable
		result.Filterable = &value
	}
	for _, option := range request.Options {
		result.Options = append(result.Options, protocol.InteractionOption{ID: option.ID, Label: option.Label, Detail: option.Detail})
	}
	for _, question := range request.Questions {
		projected := protocol.InteractionQuestion{ID: question.ID, Prompt: question.Prompt, Detail: question.Detail, Kind: protocol.InteractionQuestionKind(question.Kind), Required: question.Required, Options: make([]protocol.InteractionOption, 0, len(question.Options))}
		for _, option := range question.Options {
			projected.Options = append(projected.Options, protocol.InteractionOption{ID: option.ID, Label: option.Label, Detail: option.Detail})
		}
		result.Questions = append(result.Questions, projected)
	}
	return result
}

func projectSessionEventPage(page kitsession.EventPage) protocol.SessionEventBatch {
	return (runtimeSessionService{}).projectSessionEventPage(page)
}

func (s runtimeSessionService) projectSessionEventPage(page kitsession.EventPage) protocol.SessionEventBatch {
	batch := protocol.SessionEventBatch{
		StreamID: page.StreamID, FirstSequence: page.FirstSequence, LastSequence: page.LastSequence,
		ResyncRequired: page.ResyncRequired, UsageBaseline: projectSessionUsagePointer(page.UsageBaseline),
		Events: make([]protocol.SessionEvent, 0, len(page.Events)),
	}
	for _, event := range page.Events {
		projected := protocol.SessionEvent{
			StreamID: event.StreamID, Sequence: event.Sequence, SessionID: event.SessionID,
			TurnID:  event.TurnID,
			Payload: s.projectSessionEventPayload(event),
		}
		batch.Events = append(batch.Events, projected)
	}
	for len(batch.Events) > 1 {
		encoded, err := json.Marshal(batch)
		if err != nil || len(encoded) <= maxSessionEventPageBytes {
			break
		}
		batch.Events = batch.Events[:len(batch.Events)-1]
	}
	return batch
}

func (s runtimeSessionService) projectSessionEventPayload(event kitsession.Event) protocol.SessionEventPayload {
	content := projectTranscriptContent(event.Content)
	providerRetry := func() *protocol.ProviderRetry {
		if event.ProviderRetry == nil {
			return nil
		}
		retry := &protocol.ProviderRetry{Count: event.ProviderRetry.Count}
		if !event.ProviderRetry.RetryAt.IsZero() {
			retry.RetryAt = event.ProviderRetry.RetryAt.Format(time.RFC3339Nano)
		}
		return retry
	}
	annotation := func() *protocol.Annotation {
		if event.Annotation == nil {
			return nil
		}
		value := projectAnnotation(*event.Annotation, "")
		return &value
	}
	scratchpad := func() *protocol.Scratchpad {
		if event.Scratchpad == nil {
			return nil
		}
		value := projectScratchpad(*event.Scratchpad)
		return &value
	}
	interaction := func() *protocol.InteractionRequest {
		if event.Interaction == nil {
			return nil
		}
		value := projectInteractionRequest(*event.Interaction)
		return &value
	}
	switch event.Kind {
	case kitsession.EventRunStarted:
		return protocol.TurnStartedEvent{Status: protocol.TurnStatus(event.Status)}
	case kitsession.EventUserMessage:
		return protocol.UserMessageAddedEvent{Text: event.Text}
	case kitsession.EventAssistantStarted:
		return protocol.AssistantStartedEvent{MessageID: event.MessageID, Text: event.Text, Thinking: event.Thinking}
	case kitsession.EventAssistantTextDelta:
		return protocol.AssistantTextDeltaEvent{MessageID: event.MessageID, ContentIndex: event.ContentIndex, Delta: event.Delta}
	case kitsession.EventThinkingDelta:
		return protocol.ThinkingDeltaEvent{MessageID: event.MessageID, ContentIndex: event.ContentIndex, Delta: event.Delta}
	case kitsession.EventAssistantCompleted:
		return protocol.AssistantCompletedEvent{MessageID: event.MessageID, Text: event.Text, Thinking: event.Thinking}
	case kitsession.EventToolPlanned:
		return protocol.ToolPlannedEvent{MessageID: event.MessageID, ContentIndex: event.ContentIndex, ToolCallID: event.ToolCallID, ToolName: event.ToolName, Arguments: event.Arguments, ArgumentsTruncated: event.ArgumentsTruncated}
	case kitsession.EventToolStarted:
		return protocol.ToolStartedEvent{ToolCallID: event.ToolCallID, ToolName: event.ToolName, Arguments: event.Arguments, ArgumentsTruncated: event.ArgumentsTruncated}
	case kitsession.EventToolUpdated:
		return protocol.ToolOutputDeltaEvent{ToolCallID: event.ToolCallID, ToolName: event.ToolName, Content: content, IsError: event.IsError}
	case kitsession.EventToolCompleted:
		return protocol.ToolCompletedEvent{ToolCallID: event.ToolCallID, ToolName: event.ToolName, Content: content, ContentTruncated: event.ContentTruncated, Details: append(json.RawMessage(nil), event.Details...), DetailsOmitted: event.DetailsOmitted, IsError: event.IsError}
	case kitsession.EventCompactionStarted:
		return protocol.CompactionStartedEvent{CompactionID: event.CompactionID}
	case kitsession.EventCompactionCompleted:
		return protocol.CompactionCompletedEvent{CompactionID: event.CompactionID}
	case kitsession.EventCompactionFailed:
		return protocol.CompactionCompletedEvent{CompactionID: event.CompactionID, ErrorMessage: event.ErrorMessage}
	case kitsession.EventProviderRetryScheduled:
		return protocol.ProviderRetryScheduledEvent{ProviderRetry: providerRetry()}
	case kitsession.EventProviderRetryStarted:
		return protocol.ProviderRetryStartedEvent{ProviderRetry: providerRetry()}
	case kitsession.EventContextUpdated:
		return protocol.ContextChangedEvent{ContextTokens: event.ContextTokens, ContextWindow: event.ContextWindow}
	case kitsession.EventUsageUpdated:
		return protocol.UsageChangedEvent{Usage: projectSessionUsagePointer(event.Usage)}
	case kitsession.EventRunFinished:
		return protocol.TurnCompletedEvent{Status: protocol.TurnStatus(event.Status), ErrorKind: projectProviderErrorKind(event.ErrorKind), ErrorMessage: event.ErrorMessage}
	case kitsession.EventSessionRenamed:
		return protocol.SessionNameChangedEvent{SessionName: event.SessionName}
	case kitsession.EventSessionCWDChanged:
		var workspace *protocol.WorkspaceRef
		if s.workspaces != nil {
			value := s.workspaces.Ref(event.SessionID, event.CWD)
			workspace = &value
		}
		return protocol.SessionCWDChangedEvent{Workspace: workspace}
	case kitsession.EventSubagentChanged:
		return protocol.SubagentChangedEvent{SubagentConversationID: event.SubagentConversationID, SubagentTaskID: event.SubagentTaskID}
	case kitsession.EventPeerQueryChanged:
		return protocol.PeerQueryChangedEvent{PeerRequestID: event.PeerRequestID}
	case kitsession.EventInteractionRequested:
		return protocol.InteractionRequestedEvent{Interaction: interaction()}
	case kitsession.EventInteractionResolved:
		return protocol.InteractionResolvedEvent{InteractionID: event.InteractionID, InteractionResolution: event.InteractionResolution}
	case kitsession.EventAnnotationCreated:
		return protocol.AnnotationCreatedEvent{AnnotationID: event.AnnotationID, Annotation: annotation()}
	case kitsession.EventAnnotationUpdated:
		return protocol.AnnotationUpdatedEvent{AnnotationID: event.AnnotationID, Annotation: annotation()}
	case kitsession.EventAnnotationDeleted:
		return protocol.AnnotationDeletedEvent{AnnotationID: event.AnnotationID}
	case kitsession.EventAnnotationSubmitted:
		return protocol.AnnotationSubmittedEvent{AnnotationIDs: append([]uint64(nil), event.AnnotationIDs...), AcceptedMessageID: event.AcceptedMessageID}
	case kitsession.EventScratchpadChanged:
		return protocol.ScratchpadChangedEvent{Scratchpad: scratchpad()}
	default:
		return nil
	}
}

func (s runtimeSessionService) Configure(ctx context.Context, sessionID string, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	if s.availableProviders != nil {
		provider, _, _ := strings.Cut(input.Model, "/")
		available := false
		for _, candidate := range s.availableProviders(ctx) {
			available = available || candidate == provider
		}
		if !available {
			return protocol.ConfigureSessionResult{}, fmt.Errorf("%w: model provider %q is not authenticated", kitsession.ErrInvalidInput, provider)
		}
	}
	var thinking *string
	if input.ThinkingLevel != nil {
		value := string(*input.ThinkingLevel)
		thinking = &value
	}
	result, err := s.manager.ConfigureSession(ctx, sessionID, kitsession.ConfigureSessionInput{
		ExpectedRevision: input.ExpectedRevision, Model: input.Model, ThinkingLevel: thinking,
	})
	if err != nil {
		return protocol.ConfigureSessionResult{}, err
	}
	return protocol.ConfigureSessionResult{
		Session: s.projectSession(result.Session), EventStreamID: result.EventStreamID,
		Compacted: result.Compacted, CheckpointID: result.CheckpointID,
		Warnings: append([]string(nil), result.Warnings...),
	}, nil
}

func (s runtimeSessionService) Scratchpad(ctx context.Context, sessionID string) (protocol.Scratchpad, error) {
	if s.manager == nil {
		return protocol.Scratchpad{}, kitscratchpad.ErrUnavailable
	}
	record, err := s.manager.Scratchpad(ctx, sessionID)
	if err != nil {
		return protocol.Scratchpad{}, normalizeScratchpadError(err)
	}
	return projectScratchpad(record), nil
}

func (s runtimeSessionService) UpdateScratchpad(ctx context.Context, sessionID string, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
	if s.manager == nil {
		return protocol.Scratchpad{}, kitscratchpad.ErrUnavailable
	}
	if err := validateScratchpadInput(input); err != nil {
		return protocol.Scratchpad{}, err
	}
	record, err := s.manager.UpdateScratchpad(ctx, sessionID, int64(input.ExpectedRevision), input.Content)
	if err != nil {
		return protocol.Scratchpad{}, normalizeScratchpadError(err)
	}
	return projectScratchpad(record), nil
}

func validateScratchpadInput(input protocol.UpdateScratchpadInput) error {
	if err := input.Validate(); err != nil {
		if errors.Is(err, kitscratchpad.ErrInvalidContent) || errors.Is(err, kitscratchpad.ErrContentTooLarge) {
			return err
		}
		return fmt.Errorf("%w: %v", errInvalidSessionRequest, err)
	}
	return nil
}

func normalizeScratchpadError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, kitsession.ErrNotFound), errors.Is(err, kitsession.ErrInvalidInput),
		errors.Is(err, kitsession.ErrClosed), errors.Is(err, kitsession.ErrDeleteBusy):
		return err
	case errors.Is(err, kitscratchpad.ErrInvalidContent), errors.Is(err, kitscratchpad.ErrContentTooLarge),
		errors.Is(err, kitscratchpad.ErrConflict),
		errors.Is(err, kitscratchpad.ErrRevisionExhausted), errors.Is(err, kitscratchpad.ErrMigrationRequired),
		errors.Is(err, kitscratchpad.ErrUnsupported):
		return err
	default:
		return kitscratchpad.ErrUnavailable
	}
}

func projectScratchpad(record kitscratchpad.Record) protocol.Scratchpad {
	return protocol.Scratchpad{
		OwnerSessionID: record.OwnerSessionID,
		Content:        record.Content,
		Revision:       protocol.ScratchpadRevision(record.Revision),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s runtimeSessionService) Compact(ctx context.Context, sessionID string, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	result, err := s.manager.CompactSession(ctx, sessionID, input.OperationID)
	if err != nil {
		return protocol.CompactSessionResult{}, err
	}
	return protocol.CompactSessionResult{
		OperationID: result.OperationID, Compacted: result.Compacted,
		CheckpointID: result.CheckpointID, EventStreamID: result.EventStreamID,
	}, nil
}

func (s runtimeSessionService) Reload(ctx context.Context, sessionID string) (protocol.ReloadSessionResult, error) {
	result, err := s.manager.ReloadSession(ctx, sessionID)
	if err != nil {
		return protocol.ReloadSessionResult{}, err
	}
	projected := protocol.ReloadSessionResult{
		SessionID: sessionID, EventStreamID: result.EventStreamID,
		Sources:     make([]protocol.PromptSource, 0, len(result.Sources)),
		Diagnostics: make([]protocol.PromptDiagnostic, 0, len(result.Diagnostics)),
		Warnings:    append([]string(nil), result.Warnings...),
	}
	for _, source := range result.Sources {
		projected.Sources = append(projected.Sources, projectPromptSource(source))
	}
	for _, diagnostic := range result.Diagnostics {
		projected.Diagnostics = append(projected.Diagnostics, protocol.PromptDiagnostic{
			Severity: string(diagnostic.Severity), Code: diagnostic.Code, Message: diagnostic.Message,
			Source: projectPromptSource(diagnostic.Source),
		})
	}
	return projected, nil
}

func projectPromptSource(source systemprompt.Source) protocol.PromptSource {
	return protocol.PromptSource{
		SectionID: source.SectionID, ID: source.ID, Kind: promptSectionKind(source.Kind), Path: source.Path,
	}
}

func promptSectionKind(kind systemprompt.SectionKind) protocol.PromptSectionKind {
	switch kind {
	case systemprompt.SectionCore:
		return protocol.PromptSectionCore
	case systemprompt.SectionFeature:
		return protocol.PromptSectionFeature
	case systemprompt.SectionSkillCatalog:
		return protocol.PromptSectionSkillCatalog
	case systemprompt.SectionPlugin:
		return protocol.PromptSectionPlugin
	case systemprompt.SectionContext:
		return protocol.PromptSectionContext
	default:
		return ""
	}
}

func (s runtimeSessionService) StartPrompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.TurnReservation, error) {
	reservation, err := s.manager.StartPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.TurnReservation{}, err
	}
	return protocol.TurnReservation{
		SessionID: reservation.SessionID, TurnID: reservation.TurnID,
	}, nil
}

func (s runtimeSessionService) SubmitPrompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptSubmission, error) {
	result, err := s.manager.SubmitPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.PromptSubmission{}, err
	}
	output := protocol.PromptSubmission{Queued: result.Queued, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews, AnnotationIDs: result.Queue.AnnotationIDs}}
	if !result.Queued {
		output.Reservation = &protocol.TurnReservation{SessionID: result.Reservation.SessionID, TurnID: result.Reservation.TurnID}
	}
	return output, nil
}

func (s runtimeSessionService) RestoreFollowUps(ctx context.Context, sessionID string) (protocol.RestoreFollowUpsResult, error) {
	result, err := s.manager.RestoreFollowUps(ctx, sessionID)
	messages := make([]protocol.PromptInput, 0, len(result.Messages))
	for _, message := range result.Messages {
		messages = append(messages, protocol.PromptInput{Text: message.Text, AttachmentIDs: append([]string(nil), message.AttachmentIDs...), AnnotationIDs: append([]uint64(nil), message.AnnotationIDs...)})
	}
	return protocol.RestoreFollowUpsResult{Messages: messages, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews, AnnotationIDs: result.Queue.AnnotationIDs}}, err
}

func (s runtimeSessionService) PromoteFollowUps(ctx context.Context, sessionID string) (protocol.PromoteFollowUpsResult, error) {
	result, err := s.manager.PromoteFollowUps(ctx, sessionID)
	return protocol.PromoteFollowUpsResult{Promoted: result.Promoted, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews, AnnotationIDs: result.Queue.AnnotationIDs}}, err
}

func (s runtimeSessionService) ExecutePluginCommand(ctx context.Context, sessionID string, input protocol.PluginCommandInput) error {
	return s.manager.ExecutePluginCommand(ctx, sessionID, input.Instance, input.ID, input.Args)
}

func (s runtimeSessionService) SubmitPromptCommand(ctx context.Context, sessionID string, input protocol.PromptCommandInput) (protocol.PromptSubmission, error) {
	result, err := s.manager.SubmitPromptCommand(ctx, sessionID, input.Name, input.Args)
	if err != nil {
		return protocol.PromptSubmission{}, err
	}
	output := protocol.PromptSubmission{Queued: result.Queued, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews, AnnotationIDs: result.Queue.AnnotationIDs}}
	if result.Reservation.TurnID != "" {
		output.Reservation = &protocol.TurnReservation{SessionID: result.Reservation.SessionID, TurnID: result.Reservation.TurnID}
	}
	return output, nil
}

func (s runtimeSessionService) StartPromptCommand(ctx context.Context, sessionID string, input protocol.PromptCommandInput) (protocol.TurnReservation, error) {
	reservation, err := s.manager.StartPromptCommand(ctx, sessionID, input.Name, input.Args)
	if err != nil {
		return protocol.TurnReservation{}, err
	}
	return protocol.TurnReservation{
		SessionID: reservation.SessionID, TurnID: reservation.TurnID,
	}, nil
}

func (s runtimeSessionService) Turn(ctx context.Context, sessionID, turnID string) (protocol.TurnInfo, error) {
	record, err := s.manager.GetRun(ctx, sessionID, turnID)
	if err != nil {
		return protocol.TurnInfo{}, err
	}
	return protocol.TurnInfo{
		SessionID: record.SessionID, TurnID: record.TurnID,
		Status: protocol.TurnStatus(record.Status), ErrorMessage: record.Error,
	}, nil
}

func (s runtimeSessionService) Prompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptOutcome, error) {
	result, err := s.manager.RunPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.PromptOutcome{}, err
	}
	return protocol.PromptOutcome{
		SessionID: result.SessionID, TurnID: result.TurnID,
		Text: result.Text, StopReason: result.StopReason,
		Status: protocol.TurnStatus(result.Status), ErrorKind: projectProviderErrorKind(result.ErrorKind),
		ErrorMessage: result.ErrorMessage,
	}, nil
}

func projectProviderErrorKind(kind kitsession.ProviderErrorKind) protocol.ProviderErrorKind {
	switch kind {
	case kitsession.ProviderErrorAuthentication:
		return protocol.ProviderErrorAuthentication
	case kitsession.ProviderErrorEntitlement:
		return protocol.ProviderErrorEntitlement
	case kitsession.ProviderErrorUsageLimit:
		return protocol.ProviderErrorUsageLimit
	case kitsession.ProviderErrorRateLimit:
		return protocol.ProviderErrorRateLimit
	case kitsession.ProviderErrorTransport:
		return protocol.ProviderErrorTransport
	case kitsession.ProviderErrorProtocol:
		return protocol.ProviderErrorProtocol
	default:
		return ""
	}
}

func (s runtimeSessionService) Abort(ctx context.Context, sessionID, turnID string) error {
	return s.manager.Abort(ctx, sessionID, turnID)
}

func (s runtimeSessionService) RespondInteraction(ctx context.Context, sessionID string, response protocol.InteractionResponse) error {
	answers := make(map[string]kitsession.InteractionAnswer, len(response.Answers))
	for id, answer := range response.Answers {
		answers[id] = kitsession.InteractionAnswer{Text: answer.Text, Boolean: answer.Boolean, OptionIDs: append([]string(nil), answer.OptionIDs...), Skipped: answer.Skipped}
	}
	return s.manager.RespondInteraction(ctx, sessionID, kitsession.InteractionResponse{RequestID: response.RequestID, Cancelled: response.Cancelled, Confirmed: response.Confirmed, Value: response.Value, SelectedOptionID: response.SelectedOptionID, Answers: answers})
}

func (s runtimeSessionService) StartBash(ctx context.Context, sessionID string, input protocol.BashExecutionInput) (protocol.BashExecution, error) {
	execution, err := s.manager.StartBash(ctx, sessionID, input.ExecutionID, input.Command, input.ExcludeFromContext)
	if err != nil {
		return protocol.BashExecution{}, err
	}
	return projectBashExecution(execution), nil
}

func (s runtimeSessionService) Bash(ctx context.Context, sessionID, executionID string) (protocol.BashExecution, error) {
	execution, err := s.manager.GetBash(ctx, sessionID, executionID)
	if err != nil {
		return protocol.BashExecution{}, err
	}
	return projectBashExecution(execution), nil
}

func (s runtimeSessionService) AbortBash(ctx context.Context, sessionID, executionID string) error {
	return s.manager.AbortBash(ctx, sessionID, executionID)
}

func projectBashExecution(execution kitsession.BashExecution) protocol.BashExecution {
	completedAt := ""
	if execution.CompletedAt != nil {
		completedAt = execution.CompletedAt.Format(time.RFC3339Nano)
	}
	return protocol.BashExecution{
		ID: execution.ID, SessionID: execution.SessionID, Sequence: execution.Sequence,
		Command: execution.Command, Status: protocol.BashExecutionStatus(execution.Status),
		Output: execution.Output, ExitCode: execution.ExitCode,
		ExcludeFromContext: execution.ExcludeFromContext, Truncated: execution.Truncated,
		TimedOut: execution.TimedOut, ErrorMessage: execution.ErrorMessage,
		StartedAt: execution.StartedAt.Format(time.RFC3339Nano), CompletedAt: completedAt,
	}
}

type sessionEventStreamSource struct {
	service   sessionService
	sessionID string
	streamID  string
	after     int64
	initial   *protocol.SessionEventBatch
	terminal  bool
}

func (source *sessionEventStreamSource) Next(ctx context.Context) (httpapi.StreamRecord[protocol.SessionEventBatch], error) {
	if source.terminal {
		return httpapi.StreamRecord[protocol.SessionEventBatch]{}, io.EOF
	}
	var batch protocol.SessionEventBatch
	if source.initial != nil {
		batch, source.initial = *source.initial, nil
	} else {
		var err error
		batch, err = source.service.WaitEvents(ctx, source.sessionID, source.streamID, source.after)
		if err != nil {
			return httpapi.StreamRecord[protocol.SessionEventBatch]{}, err
		}
	}
	if err := batch.Validate(); err != nil {
		return httpapi.StreamRecord[protocol.SessionEventBatch]{}, err
	}
	for _, event := range batch.Events {
		if event.Sequence > source.after {
			source.after = event.Sequence
		}
	}
	if batch.StreamID != "" {
		source.streamID = batch.StreamID
	}
	name := httpapi.SessionEventsRecord
	id := source.streamID + ":" + strconv.FormatInt(source.after, 10)
	if batch.ResyncRequired {
		name, id, source.terminal = httpapi.SessionResyncRecord, "", true
	}
	return httpapi.StreamRecord[protocol.SessionEventBatch]{Name: name, ID: id, Payload: batch}, nil
}

func (*sessionEventStreamSource) Close() {}

func registerSessionRoutes(mux *http.ServeMux, service sessionService) {
	httpOptions := httpapi.ServeOptions{MaxRequestBytes: maxSessionRequestBytes, WriteError: func(writer http.ResponseWriter, err error) {
		var requestErr *httpapi.RequestError
		if errors.As(err, &requestErr) {
			err = fmt.Errorf("%w: %v", errInvalidSessionRequest, requestErr)
		}
		writeSessionError(writer, err)
	}}
	httpapi.Handle(mux, httpOptions, httpapi.RefreshModels, func(ctx context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (protocol.ModelCatalog, error) {
		catalog, err := service.RefreshModels(ctx)
		if err != nil {
			return protocol.ModelCatalog{}, lifecycleAPIError(err)
		}
		if err := catalog.Validate(); err != nil {
			return protocol.ModelCatalog{}, internalAPIError()
		}
		return catalog, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ListModels, func(ctx context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (protocol.ModelCatalog, error) {
		catalog, err := service.Models(ctx)
		if err != nil {
			return protocol.ModelCatalog{}, lifecycleAPIError(err)
		}
		if err := catalog.Validate(); err != nil {
			return protocol.ModelCatalog{}, internalAPIError()
		}
		return catalog, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ListSessions, func(ctx context.Context, params httpapi.ListSessionsPath, _ httpapi.NoBody) (protocol.SessionList, error) {
		records, err := service.List(ctx, params.CWD)
		if err != nil {
			return protocol.SessionList{}, lifecycleAPIError(err)
		}
		if records == nil {
			records = []protocol.SessionInfo{}
		}
		result := protocol.SessionList{Sessions: records}
		if err := result.Validate(); err != nil {
			return protocol.SessionList{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ForkSession, func(ctx context.Context, params httpapi.SessionPath, input protocol.ForkSessionInput) (protocol.SessionInfo, error) {
		if err := input.Validate(); err != nil {
			return protocol.SessionInfo{}, invalidAPIError()
		}
		record, err := service.Fork(ctx, params.SessionID, input)
		if err != nil {
			return protocol.SessionInfo{}, lifecycleAPIError(err)
		}
		if err := record.Validate(); err != nil {
			return protocol.SessionInfo{}, internalAPIError()
		}
		return record, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.RenameSession, func(ctx context.Context, params httpapi.SessionPath, input protocol.RenameSessionInput) (protocol.SessionInfo, error) {
		if err := input.Validate(); err != nil {
			return protocol.SessionInfo{}, invalidAPIError()
		}
		record, err := service.Rename(ctx, params.SessionID, input)
		if err != nil {
			return protocol.SessionInfo{}, lifecycleAPIError(err)
		}
		if err := record.Validate(); err != nil {
			return protocol.SessionInfo{}, internalAPIError()
		}
		return record, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.DeleteSession, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		if err := service.Delete(ctx, params.SessionID); err != nil {
			return httpapi.NoBody{}, lifecycleAPIError(err)
		}
		return httpapi.NoBody{}, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.DisposeTemporarySession, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		if err := service.DisposeTemporary(ctx, params.SessionID); err != nil {
			return httpapi.NoBody{}, lifecycleAPIError(err)
		}
		return httpapi.NoBody{}, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.GetSession, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (protocol.SessionSnapshot, error) {
		snapshot, err := service.Snapshot(ctx, params.SessionID)
		if err != nil {
			return protocol.SessionSnapshot{}, lifecycleAPIError(err)
		}
		if err := snapshot.Validate(); err != nil {
			return protocol.SessionSnapshot{}, internalAPIError()
		}
		return snapshot, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.GetBashHistory, func(ctx context.Context, params httpapi.BashHistoryPath, _ httpapi.NoBody) (protocol.BashHistoryPage, error) {
		if params.Limit < 0 || params.Limit > protocol.MaxBashHistoryPageSize {
			return protocol.BashHistoryPage{}, invalidAPIError()
		}
		if params.Limit == 0 {
			params.Limit = protocol.DefaultBashHistoryPageSize
		}
		result, err := service.BashHistory(ctx, params.SessionID, params.Before, params.Limit)
		if err != nil {
			return protocol.BashHistoryPage{}, lifecycleAPIError(err)
		}
		return result, nil
	})
	httpapi.HandleRaw(mux, httpapi.GetMessagePage, func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		limit := 50
		if raw := query.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 100 {
				writeSessionError(writer, fmt.Errorf("%w: message limit must be between 1 and 100", errInvalidSessionRequest))
				return
			}
			limit = parsed
		}
		var before uint64
		if raw := query["before"]; len(raw) > 1 || (len(raw) == 1 && (raw[0] == "" || len(raw[0]) > 256 || strings.TrimSpace(raw[0]) != raw[0])) {
			writeSessionError(writer, fmt.Errorf("%w: message cursor is invalid", errInvalidSessionRequest))
			return
		} else if len(raw) == 1 {
			parsed, err := strconv.ParseUint(raw[0], 10, 64)
			if err != nil || parsed == 0 {
				writeSessionError(writer, fmt.Errorf("%w: message cursor is invalid", errInvalidSessionRequest))
				return
			}
			before = parsed
		}
		roles := append([]string(nil), query["role"]...)
		result, err := service.MessagePage(request.Context(), request.PathValue("sessionID"), protocol.MessagePageQuery{
			Before: before, Limit: limit, Roles: roles,
		})
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid message page: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.GetTranscriptPage, func(writer http.ResponseWriter, request *http.Request) {
		before := request.URL.Query().Get("before")
		if before == "" || len(before) > 256 || strings.TrimSpace(before) != before {
			writeSessionError(writer, fmt.Errorf("%w: before cursor is required", errInvalidSessionRequest))
			return
		}
		result, err := service.TranscriptPage(request.Context(), request.PathValue("sessionID"), before)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid transcript page: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.GetWorkspace, func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.Workspace(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid workspace reference: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.ListWorkspaceDirectory, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ListDirectoryInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		result, err := service.ListDirectory(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid directory page: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.ReadWorkspaceFile, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ReadWorkspaceFileInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		result, err := service.ReadWorkspaceFile(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid workspace file: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.Handle(mux, httpOptions, httpapi.ListDiffTargets, func(ctx context.Context, params httpapi.SessionPath, input protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
		if err := input.Validate(); err != nil {
			return protocol.DiffTargetCatalog{}, invalidAPIError()
		}
		result, err := service.ListDiffTargets(ctx, params.SessionID, input)
		if err != nil {
			return protocol.DiffTargetCatalog{}, diffAPIError(err)
		}
		if err := validateDiffTargetCatalogResponse(params.SessionID, input, result); err != nil {
			return protocol.DiffTargetCatalog{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ObserveDiff, func(ctx context.Context, params httpapi.SessionPath, input protocol.ObserveDiffInput) (protocol.DiffPage, error) {
		if err := input.Validate(); err != nil {
			return protocol.DiffPage{}, invalidAPIError()
		}
		result, err := service.ObserveDiff(ctx, params.SessionID, input)
		if err != nil {
			return protocol.DiffPage{}, diffAPIError(err)
		}
		if err := validateObserveDiffResponse(params.SessionID, input, result); err != nil {
			return protocol.DiffPage{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ObserveWorkingTree, func(ctx context.Context, params httpapi.SessionPath, input protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
		if err := input.Validate(); err != nil {
			return protocol.WorkingTreePage{}, invalidAPIError()
		}
		result, err := service.ObserveWorkingTree(ctx, params.SessionID, input)
		if err != nil {
			return protocol.WorkingTreePage{}, diffAPIError(err)
		}
		if err := result.Validate(); err != nil || result.Observation.SessionID != params.SessionID {
			return protocol.WorkingTreePage{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ReadFileDiff, func(ctx context.Context, params httpapi.SessionPath, input protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
		if err := input.Validate(); err != nil {
			return protocol.FileDiffPage{}, invalidAPIError()
		}
		result, err := service.ReadFileDiff(ctx, params.SessionID, input)
		if err != nil {
			return protocol.FileDiffPage{}, diffAPIError(err)
		}
		if err := validateFileDiffResponse(params.SessionID, input, result); err != nil {
			return protocol.FileDiffPage{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ListAnnotations, func(ctx context.Context, params httpapi.AnnotationListParams, _ httpapi.NoBody) (protocol.AnnotationPage, error) {
		input := protocol.ListAnnotationsInput{Cursor: params.Cursor, PageSize: params.PageSize}
		if err := input.Validate(); err != nil {
			return protocol.AnnotationPage{}, invalidAPIError()
		}
		result, err := service.ListAnnotations(ctx, params.SessionID, input)
		if err != nil {
			return protocol.AnnotationPage{}, annotationAPIError(err)
		}
		if err := result.Validate(); err != nil || result.SessionID != params.SessionID {
			return protocol.AnnotationPage{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.CreateAnnotation, func(ctx context.Context, params httpapi.SessionPath, input protocol.CreateAnnotationInput) (protocol.Annotation, error) {
		if err := input.Validate(); err != nil {
			return protocol.Annotation{}, invalidAPIError()
		}
		result, err := service.CreateAnnotation(ctx, params.SessionID, input)
		if err != nil {
			return protocol.Annotation{}, annotationAPIError(err)
		}
		if err := result.Validate(); err != nil || result.SessionID != params.SessionID {
			return protocol.Annotation{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.UpdateAnnotation, func(ctx context.Context, params httpapi.SessionPath, input protocol.UpdateAnnotationInput) (protocol.Annotation, error) {
		if err := input.Validate(); err != nil {
			return protocol.Annotation{}, invalidAPIError()
		}
		result, err := service.UpdateAnnotation(ctx, params.SessionID, input)
		if err != nil {
			return protocol.Annotation{}, annotationAPIError(err)
		}
		if err := result.Validate(); err != nil || result.SessionID != params.SessionID || result.ID != input.AnnotationID {
			return protocol.Annotation{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.DeleteAnnotation, func(ctx context.Context, params httpapi.SessionPath, input protocol.DeleteAnnotationInput) (httpapi.NoBody, error) {
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, invalidAPIError()
		}
		if err := service.DeleteAnnotation(ctx, params.SessionID, input); err != nil {
			return httpapi.NoBody{}, annotationAPIError(err)
		}
		return httpapi.NoBody{}, nil
	})
	httpapi.HandleRaw(mux, httpapi.GetSessionFileIndex, func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.FileIndex(request.Context(), request.PathValue("sessionID"), request.URL.Query().Get("refresh") == "true")
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session file index: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})

	httpapi.HandleRaw(mux, httpapi.GetSessionEventPage, func(writer http.ResponseWriter, request *http.Request) {
		after := int64(0)
		if raw := request.URL.Query().Get("after"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				writeSessionError(writer, fmt.Errorf("%w: event cursor must be a non-negative integer", errInvalidSessionRequest))
				return
			}
			after = parsed
		}
		batch, err := service.Events(request.Context(), request.PathValue("sessionID"), request.URL.Query().Get("stream"), after)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := batch.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session event page: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, batch)
	})
	httpapi.HandleStream(mux, httpOptions, httpapi.StreamSessionEvents, func(ctx context.Context, params httpapi.EventStreamPath) (httpapi.StreamSource[protocol.SessionEventBatch], error) {
		streamID, after, err := sessionEventCursorValues(params.StreamID, strconv.FormatInt(params.After, 10), params.LastEventID)
		if err != nil {
			return nil, err
		}
		batch, err := service.Events(ctx, params.SessionID, streamID, after)
		if err != nil {
			return nil, err
		}
		if err := batch.Validate(); err != nil {
			return nil, fmt.Errorf("invalid session event batch: %w", err)
		}
		return &sessionEventStreamSource{service: service, sessionID: params.SessionID, streamID: streamID, after: after, initial: &batch}, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.CreateSession, func(ctx context.Context, _ httpapi.NoBody, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
		if err := input.Validate(); err != nil {
			return protocol.SessionInfo{}, invalidAPIError()
		}
		record, err := service.Create(ctx, input)
		if err != nil {
			return protocol.SessionInfo{}, lifecycleAPIError(err)
		}
		if err := record.Validate(); err != nil {
			return protocol.SessionInfo{}, internalAPIError()
		}
		return record, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ChangeSessionCWD, func(ctx context.Context, params httpapi.SessionPath, input protocol.ChangeCWDInput) (protocol.ChangeWorkspaceCWDResult, error) {
		if err := input.Validate(); err != nil {
			return protocol.ChangeWorkspaceCWDResult{}, invalidAPIError()
		}
		result, err := service.ChangeCWD(ctx, params.SessionID, input)
		if err != nil {
			return protocol.ChangeWorkspaceCWDResult{}, lifecycleAPIError(err)
		}
		if err := result.Validate(); err != nil {
			return protocol.ChangeWorkspaceCWDResult{}, internalAPIError()
		}
		return result, nil
	})
	registerVCSRoutes(mux, httpOptions, service)
	registerPluginRoutes(mux, httpOptions, service)
	httpapi.Handle(mux, httpOptions, httpapi.GetScratchpad, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (protocol.Scratchpad, error) {
		record, err := service.Scratchpad(ctx, params.SessionID)
		if err != nil {
			return protocol.Scratchpad{}, scratchpadAPIError(err)
		}
		if err := record.Validate(); err != nil {
			return protocol.Scratchpad{}, httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
		}
		return record, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.UpdateScratchpad, func(ctx context.Context, params httpapi.SessionPath, input protocol.UpdateScratchpadInput) (protocol.Scratchpad, error) {
		if err := validateScratchpadInput(input); err != nil {
			return protocol.Scratchpad{}, scratchpadAPIError(err)
		}
		record, err := service.UpdateScratchpad(ctx, params.SessionID, input)
		if err != nil {
			return protocol.Scratchpad{}, scratchpadAPIError(err)
		}
		if err := record.ValidateApplied(input); err != nil {
			return protocol.Scratchpad{}, httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
		}
		return record, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ConfigureSession, func(ctx context.Context, params httpapi.SessionPath, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
		if err := input.Validate(); err != nil {
			return protocol.ConfigureSessionResult{}, invalidAPIError()
		}
		result, err := service.Configure(ctx, params.SessionID, input)
		if err != nil {
			return protocol.ConfigureSessionResult{}, lifecycleAPIError(err)
		}
		if err := result.ValidateApplied(input); err != nil {
			return protocol.ConfigureSessionResult{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.CompactSession, func(ctx context.Context, params httpapi.SessionPath, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
		if err := input.Validate(); err != nil {
			return protocol.CompactSessionResult{}, invalidAPIError()
		}
		result, err := service.Compact(ctx, params.SessionID, input)
		if err != nil {
			return protocol.CompactSessionResult{}, lifecycleAPIError(err)
		}
		if err := result.ValidateApplied(input); err != nil {
			return protocol.CompactSessionResult{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.ReloadSession, func(ctx context.Context, params httpapi.SessionPath, _ httpapi.NoBody) (protocol.ReloadSessionResult, error) {
		result, err := service.Reload(ctx, params.SessionID)
		if err != nil {
			return protocol.ReloadSessionResult{}, lifecycleAPIError(err)
		}
		if err := result.Validate(); err != nil {
			return protocol.ReloadSessionResult{}, internalAPIError()
		}
		return result, nil
	})
	httpapi.HandleRaw(mux, httpapi.GetSubagentEvents, func(writer http.ResponseWriter, request *http.Request) {
		after := int64(0)
		if raw := request.URL.Query().Get("after"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				writeSessionError(writer, subagentAPIError(fmt.Errorf("%w: child event cursor must be non-negative", errInvalidSessionRequest)))
				return
			}
			after = parsed
		}
		result, err := service.SubagentEvents(request.Context(), request.PathValue("sessionID"), request.PathValue("conversationID"), request.URL.Query().Get("stream"), after)
		if err != nil {
			writeSessionError(writer, subagentAPIError(err))
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, subagentAPIError(fmt.Errorf("invalid subagent events: %w", err)))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.GetSubagentTranscript, func(writer http.ResponseWriter, request *http.Request) {
		before := request.URL.Query().Get("before")
		if before != "" && (len(before) > 256 || strings.TrimSpace(before) != before) {
			writeSessionError(writer, subagentAPIError(fmt.Errorf("%w: transcript cursor is invalid", errInvalidSessionRequest)))
			return
		}
		result, err := service.SubagentTranscript(request.Context(), request.PathValue("sessionID"), request.PathValue("conversationID"), before)
		if err != nil {
			writeSessionError(writer, subagentAPIError(err))
			return
		}
		if err := result.ValidateBefore(before); err != nil {
			writeSessionError(writer, subagentAPIError(fmt.Errorf("invalid subagent transcript: %w", err)))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.OperateSubagent, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.SubagentOperationInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, subagentAPIError(err))
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, subagentAPIError(fmt.Errorf("%w: %v", errInvalidSessionRequest, err)))
			return
		}
		result, err := service.Subagent(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, subagentAPIError(err))
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, subagentAPIError(fmt.Errorf("invalid subagent operation result: %w", err)))
			return
		}
		status := http.StatusOK
		if input.Action == protocol.SubagentStart || input.Action == protocol.SubagentMessage || input.Action == protocol.SubagentCancel {
			status = http.StatusAccepted
		}
		writeJSON(writer, status, result)
	})
	httpapi.HandleRaw(mux, httpapi.SubmitPrompt, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.SubmitPrompt(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid prompt submission result: %w", err))
			return
		}
		writeJSON(writer, http.StatusAccepted, result)
	})
	httpapi.HandleRaw(mux, httpapi.RestoreTurnFollowUps, func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.RestoreFollowUps(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid follow-up restore result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.PromoteTurnFollowUps, func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.PromoteFollowUps(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid follow-up promotion result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.HandleRaw(mux, httpapi.StartPrompt, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.StartPrompt(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if result.TurnID == "" {
			writeSessionError(writer, errors.New("session prompt returned no reservation"))
			return
		}
		writeJSON(writer, http.StatusAccepted, result)
	})
	httpapi.Handle(mux, httpOptions, httpapi.SubmitPromptCommand, func(ctx context.Context, params httpapi.SessionPath, input protocol.PromptCommandInput) (protocol.PromptSubmission, error) {
		if err := input.Validate(); err != nil {
			return protocol.PromptSubmission{}, httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, err.Error(), nil)
		}
		result, err := service.SubmitPromptCommand(ctx, params.SessionID, input)
		if err != nil {
			return protocol.PromptSubmission{}, turnAPIError(err)
		}
		return result, nil
	})
	httpapi.HandleRaw(mux, httpapi.StartPromptCommand, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptCommandInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.StartPromptCommand(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if result.TurnID == "" {
			writeSessionError(writer, errors.New("session prompt command returned no reservation"))
			return
		}
		writeJSON(writer, http.StatusAccepted, result)
	})
	httpapi.HandleRaw(mux, httpapi.Prompt, func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.Prompt(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if result.TurnID == "" {
			writeSessionError(writer, errors.New("session turn returned no result"))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	httpapi.Handle(mux, httpOptions, httpapi.StartBash, func(ctx context.Context, params httpapi.SessionPath, input protocol.BashExecutionInput) (protocol.BashExecution, error) {
		if err := input.Validate(); err != nil {
			return protocol.BashExecution{}, invalidAPIError()
		}
		result, err := service.StartBash(ctx, params.SessionID, input)
		if err != nil {
			return protocol.BashExecution{}, lifecycleAPIError(err)
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.GetBash, func(ctx context.Context, params httpapi.BashExecutionPath, _ httpapi.NoBody) (protocol.BashExecution, error) {
		result, err := service.Bash(ctx, params.SessionID, params.ExecutionID)
		if err != nil {
			return protocol.BashExecution{}, lifecycleAPIError(err)
		}
		return result, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.AbortBash, func(ctx context.Context, params httpapi.BashExecutionPath, _ httpapi.NoBody) (httpapi.BashAbortResult, error) {
		if err := service.AbortBash(ctx, params.SessionID, params.ExecutionID); err != nil {
			return httpapi.BashAbortResult{}, lifecycleAPIError(err)
		}
		return httpapi.BashAbortResult{Aborting: true}, nil
	})
	httpapi.HandleRaw(mux, httpapi.GetTurn, func(writer http.ResponseWriter, request *http.Request) {
		turn, err := service.Turn(
			request.Context(), request.PathValue("sessionID"), request.PathValue("turnID"),
		)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, turn)
	})
	httpapi.Handle(mux, httpOptions, httpapi.RespondInteraction, func(ctx context.Context, params httpapi.InteractionPath, input protocol.InteractionResponse) (protocol.InteractionResponseResult, error) {
		if input.RequestID != params.InteractionID || input.Validate() != nil {
			return protocol.InteractionResponseResult{}, httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
		}
		if err := service.RespondInteraction(ctx, params.SessionID, input); err != nil {
			return protocol.InteractionResponseResult{}, turnAPIError(err)
		}
		return protocol.InteractionResponseResult{Settled: true}, nil
	})
	httpapi.Handle(mux, httpOptions, httpapi.AbortTurn, func(ctx context.Context, params httpapi.TurnPath, _ httpapi.NoBody) (protocol.TurnAbortResult, error) {
		if err := service.Abort(ctx, params.SessionID, params.TurnID); err != nil {
			return protocol.TurnAbortResult{}, turnAPIError(err)
		}
		return protocol.TurnAbortResult{Aborting: true}, nil
	})
}

// projectSession projects record with the inputs its model accepts in user
// messages.
func (s runtimeSessionService) projectSession(record kitsession.SessionRecord) protocol.SessionInfo {
	session := projectSessionRecord(record)
	if s.manager != nil {
		for _, input := range s.manager.ModelInputs(session.Model) {
			session.Inputs = append(session.Inputs, protocol.ModelInputKind(input))
		}
	}
	return session
}

func projectSessionRecord(record kitsession.SessionRecord) protocol.SessionInfo {
	name := record.Name
	if !protocol.ValidSessionName(name) {
		name = ""
	}
	parentName := record.ParentSessionName
	if !protocol.ValidSessionName(parentName) {
		parentName = ""
	}
	return protocol.SessionInfo{
		ID: record.ID, CWD: record.CWD, Name: name, ParentSessionID: record.ParentSessionID,
		ParentSessionName: parentName,
		Temporary:         !record.Persistent,
		Model:             record.ModelProvider + "/" + record.ModelID,
		ThinkingLevel:     record.ThinkingLevel, ConfigurationRevision: record.ConfigurationRevision,
		CreatedAt: record.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: record.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func sessionEventCursor(request *http.Request) (string, int64, error) {
	return sessionEventCursorValues(request.URL.Query().Get("stream"), request.URL.Query().Get("after"), request.Header.Get("Last-Event-ID"))
}

func sessionEventCursorValues(streamID, rawAfter, lastEventID string) (string, int64, error) {
	if eventID := lastEventID; eventID != "" {
		separator := strings.LastIndexByte(eventID, ':')
		if separator <= 0 {
			return "", 0, fmt.Errorf("%w: invalid last event id", errInvalidSessionRequest)
		}
		streamID = eventID[:separator]
		rawAfter = eventID[separator+1:]
	}
	if lastEventID != "" && rawAfter == "" {
		return "", 0, fmt.Errorf("%w: event cursor must include a sequence", errInvalidSessionRequest)
	}
	after := int64(0)
	if rawAfter != "" {
		parsed, err := strconv.ParseInt(rawAfter, 10, 64)
		if err != nil || parsed < 0 {
			return "", 0, fmt.Errorf("%w: event cursor must be a non-negative integer", errInvalidSessionRequest)
		}
		after = parsed
	}
	if after > 0 && streamID == "" {
		return "", 0, fmt.Errorf("%w: event stream identity is required with a cursor", errInvalidSessionRequest)
	}
	if streamID != "" && !identifier.Valid(streamID, "stream_") {
		return "", 0, fmt.Errorf("%w: invalid event stream identity", errInvalidSessionRequest)
	}
	return streamID, after, nil
}

func decodeSessionJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxSessionRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", errInvalidSessionRequest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", errInvalidSessionRequest)
		}
		return fmt.Errorf("%w: invalid JSON: %v", errInvalidSessionRequest, err)
	}
	return nil
}

func invalidAPIError() error {
	return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
}

func internalAPIError() error {
	return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
}

// lifecycleAPIError projects the declared ADR-0034 failures for the initial
// session lifecycle catalog. Domain-specific distinctions remain private until
// their operation declares a stable code.
func lifecycleAPIError(err error) error {
	var apiErr *httpapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var workspaceErr *kitworkspace.Error
	workspaceUnavailable := errors.As(err, &workspaceErr) && workspaceErr.Code == kitworkspace.Unavailable
	status, code, message := http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error"
	switch {
	case errors.Is(err, kitsession.ErrNotFound):
		status, code, message = http.StatusNotFound, httpapi.ErrorNotFound, "session not found"
	case errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrReloadBusy), errors.Is(err, kitsession.ErrConfigureBusy), errors.Is(err, kitsession.ErrDeleteBusy), errors.As(err, new(*kitsession.ConfigurationConflictError)):
		status, code, message = http.StatusConflict, httpapi.ErrorConflict, "operation conflicts with session state"
	case errors.Is(err, kitsession.ErrCompactionFailed):
		status, code, message = http.StatusUnprocessableEntity, httpapi.ErrorUnprocessable, "context compaction failed"
	case errors.Is(err, kitsession.ErrClosed):
		status, code, message = http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "session is unavailable"
	case workspaceUnavailable:
		status, code, message = http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "workspace service is unavailable"
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, kitsession.ErrTemporary), errors.Is(err, kitsession.ErrNotTemporary), errors.Is(err, errInvalidSessionRequest):
		status, code, message = http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request"
	}
	apiErr = httpapi.NewAPIError(status, code, message, nil)
	apiErr.Cause = err
	return apiErr
}

// turnAPIError projects the generic failures declared by catalogued turn
// operations into ADR-0034 envelopes. Domain-specific session errors remain
// private until their operation declares a dedicated code.
func turnAPIError(err error) error {
	var apiErr *httpapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	status, code, message := http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error"
	switch {
	case errors.Is(err, kitsession.ErrNotFound), errors.Is(err, kitsession.ErrInteractionNotFound):
		status, code, message = http.StatusNotFound, httpapi.ErrorNotFound, "not found"
	case errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrRunNotAbortable), errors.Is(err, kitsession.ErrInteractionSettled):
		status, code, message = http.StatusConflict, httpapi.ErrorConflict, "operation conflicts with session state"
	case errors.Is(err, kitsession.ErrInteractionCapacity):
		status, code, message = http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, "interaction capacity exceeded"
	case errors.Is(err, kitsession.ErrClosed):
		status, code, message = http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "session is unavailable"
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, errInvalidSessionRequest):
		status, code, message = http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request"
	}
	return httpapi.NewAPIError(status, code, message, nil)
}

// diffAPIError projects the declared diff failures into ADR-0034 envelopes.
func diffAPIError(err error) error {
	var diffErr *protocol.DiffError
	if errors.As(err, &diffErr) {
		if diffErr.Validate() != nil {
			return internalAPIError()
		}
		status := map[protocol.DiffErrorCode]int{
			protocol.DiffErrorInvalidPath:           http.StatusBadRequest,
			protocol.DiffErrorNotRepository:         http.StatusNotFound,
			protocol.DiffErrorUnsupportedRepository: http.StatusUnprocessableEntity,
			protocol.DiffErrorStaleWorkspace:        http.StatusConflict,
			protocol.DiffErrorStaleTarget:           http.StatusConflict,
			protocol.DiffErrorStaleFile:             http.StatusConflict,
			protocol.DiffErrorStaleCursor:           http.StatusConflict,
			protocol.DiffErrorNotFound:              http.StatusNotFound,
			protocol.DiffErrorPermissionDenied:      http.StatusForbidden,
			protocol.DiffErrorLimit:                 http.StatusRequestEntityTooLarge,
			protocol.DiffErrorCapacity:              http.StatusTooManyRequests,
			protocol.DiffErrorRepositoryUnavailable: http.StatusServiceUnavailable,
			protocol.DiffErrorUnavailable:           http.StatusServiceUnavailable,
		}[diffErr.Code]
		code := httpapi.ErrorCode(diffErr.Code)
		if diffErr.Code == protocol.DiffErrorUnavailable {
			code = httpapi.ErrorUnavailable
		}
		var details any
		switch diffErr.Code {
		case protocol.DiffErrorUnsupportedRepository, protocol.DiffErrorLimit, protocol.DiffErrorCapacity:
			details = diffErr.Details
		}
		return httpapi.NewAPIError(status, code, diffErr.Message, details)
	}
	if errors.Is(err, kitsession.ErrNotFound) {
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	}
	if errors.Is(err, kitsession.ErrInvalidInput) || errors.Is(err, errInvalidSessionRequest) {
		return invalidAPIError()
	}
	return internalAPIError()
}

func scratchpadAPIError(err error) error {
	var conflict *kitscratchpad.ConflictError
	if errors.As(err, &conflict) {
		current := projectScratchpad(conflict.Current)
		if current.Validate() != nil {
			return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
		}
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorCode(protocol.ScratchpadRevisionConflict), "scratchpad revision conflict", protocol.ScratchpadErrorDetails{Scratchpad: &current})
	}
	for _, mapped := range []struct {
		target  error
		code    httpapi.ErrorCode
		status  int
		message string
	}{
		{kitscratchpad.ErrContentTooLarge, httpapi.ErrorCode(protocol.ScratchpadTooLarge), http.StatusRequestEntityTooLarge, "scratchpad content is too large"},
		{kitscratchpad.ErrInvalidContent, httpapi.ErrorCode(protocol.ScratchpadInvalidContent), http.StatusBadRequest, "scratchpad content is invalid"},
		{kitscratchpad.ErrRevisionExhausted, httpapi.ErrorCode(protocol.ScratchpadRevisionExhausted), http.StatusConflict, "scratchpad revision is exhausted"},
		{kitscratchpad.ErrMigrationRequired, httpapi.ErrorCode(protocol.ScratchpadMigrationRequired), http.StatusConflict, "scratchpad migration is required"},
		{kitscratchpad.ErrUnsupported, httpapi.ErrorCode(protocol.ScratchpadUnsupported), http.StatusConflict, "scratchpad is unsupported for this session"},
		{kitscratchpad.ErrUnavailable, httpapi.ErrorCode(protocol.ScratchpadUnavailable), http.StatusServiceUnavailable, "scratchpad is unavailable"},
	} {
		if errors.Is(err, mapped.target) {
			return httpapi.NewAPIError(mapped.status, mapped.code, mapped.message, nil)
		}
	}
	if errors.Is(err, kitsession.ErrNotFound) {
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	}
	if errors.Is(err, kitsession.ErrInvalidInput) || errors.Is(err, errInvalidSessionRequest) {
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request", nil)
	}
	return httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error", nil)
}

// subagentAPIError projects child-session failures into the codes declared
// by the subagent operation catalog. The cause stays available for logs.
func subagentAPIError(err error) error {
	var apiErr *httpapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	status, code, message := http.StatusInternalServerError, httpapi.ErrorInternal, "internal server error"
	switch {
	case errors.Is(err, kitsession.ErrNotFound), errors.Is(err, subagent.ErrNotFound):
		status, code, message = http.StatusNotFound, httpapi.ErrorNotFound, "subagent not found"
	case errors.Is(err, subagent.ErrQueueFull):
		status, code, message = http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, "subagent queue is full"
	case errors.Is(err, subagent.ErrTranscriptCursorUnavailable):
		status, code, message = http.StatusConflict, httpapi.ErrorTranscriptCursorUnavailable, "transcript cursor is unavailable"
	case errors.Is(err, subagent.ErrConflict), errors.Is(err, subagent.ErrNotCancelable), errors.Is(err, subagent.ErrDismissed), errors.Is(err, kitsession.ErrBusy):
		status, code, message = http.StatusConflict, httpapi.ErrorConflict, "operation conflicts with subagent state"
	case errors.Is(err, kitsession.ErrClosed), errors.Is(err, subagent.ErrClosed):
		status, code, message = http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "subagent is unavailable"
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, subagent.ErrInvalidInput), errors.Is(err, subagent.ErrTemporaryUnavailable), errors.Is(err, errInvalidSessionRequest):
		status, code, message = http.StatusBadRequest, httpapi.ErrorInvalidRequest, "invalid request"
	}
	apiErr = httpapi.NewAPIError(status, code, message, nil)
	apiErr.Cause = err
	return apiErr
}

func writeSessionError(writer http.ResponseWriter, err error) {
	var apiError *httpapi.APIError
	if errors.As(err, &apiError) {
		if apiError.StatusCode >= http.StatusInternalServerError || apiError.Cause != nil && apiError.StatusCode == http.StatusUnprocessableEntity {
			logged := err
			if apiError.Cause != nil {
				logged = apiError.Cause
			}
			reportRequestError(writer, apiError.StatusCode, logged)
		}
		httpapi.WriteError(writer, apiError)
		return
	}
	if errors.Is(err, kitsession.ErrCompactionFailed) {
		// The cause can reflect provider text, so it is logged but not returned.
		reportRequestError(writer, http.StatusUnprocessableEntity, err)
		httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUnprocessableEntity, httpapi.ErrorUnprocessable, kitsession.ErrCompactionFailed.Error(), nil))
		return
	}
	var evidenceErr *kitannotation.EvidenceError
	if errors.As(err, &evidenceErr) {
		status := http.StatusServiceUnavailable
		switch evidenceErr.Kind {
		case kitannotation.EvidenceStaleWorkspace, kitannotation.EvidenceStaleTarget, kitannotation.EvidenceStaleFile:
			status = http.StatusConflict
		case kitannotation.EvidencePermission:
			status = http.StatusForbidden
		case kitannotation.EvidenceInvalid:
			status = http.StatusUnprocessableEntity
		case kitannotation.EvidenceLimit:
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(writer, status, map[string]any{"error": map[string]any{"code": evidenceErr.Kind, "message": evidenceErr.Error()}})
		return
	}
	var diffErr *kitworkingdiff.Error
	if errors.As(err, &diffErr) {
		if diffErr.Validate() != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		status := map[protocol.DiffErrorCode]int{
			kitworkingdiff.InvalidPath: http.StatusBadRequest, kitworkingdiff.NotRepository: http.StatusNotFound,
			kitworkingdiff.UnsupportedRepository: http.StatusUnprocessableEntity, kitworkingdiff.StaleWorkspace: http.StatusConflict,
			kitworkingdiff.StaleTarget: http.StatusConflict, kitworkingdiff.StaleFile: http.StatusConflict, kitworkingdiff.StaleCursor: http.StatusConflict,
			kitworkingdiff.NotFound: http.StatusNotFound, kitworkingdiff.PermissionDenied: http.StatusForbidden,
			kitworkingdiff.LimitExceeded: http.StatusRequestEntityTooLarge, kitworkingdiff.CapacityExceeded: http.StatusTooManyRequests,
			kitworkingdiff.RepositoryUnavailable: http.StatusServiceUnavailable, kitworkingdiff.Unavailable: http.StatusServiceUnavailable,
		}[diffErr.Code]
		writeJSON(writer, status, map[string]any{"error": map[string]any{"code": diffErr.Code, "message": diffErr.Message, "details": diffErr.Details}})
		return
	}
	var workspaceErr *kitworkspace.Error
	if errors.As(err, &workspaceErr) {
		if validationErr := workspaceErr.Validate(); validationErr != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		status := map[protocol.WorkspaceErrorCode]int{
			kitworkspace.InvalidPath: http.StatusBadRequest, kitworkspace.NotFound: http.StatusNotFound,
			kitworkspace.NotDirectory: http.StatusBadRequest, kitworkspace.NotFile: http.StatusBadRequest,
			kitworkspace.PermissionDenied: http.StatusForbidden, kitworkspace.OutsideWorkspace: http.StatusForbidden,
			kitworkspace.SymlinkTraversal: http.StatusBadRequest, kitworkspace.BinaryFile: http.StatusUnsupportedMediaType,
			kitworkspace.StaleWorkspace: http.StatusConflict, kitworkspace.StaleFile: http.StatusConflict,
			kitworkspace.StaleCursor: http.StatusConflict, kitworkspace.LimitExceeded: http.StatusRequestEntityTooLarge,
			kitworkspace.CapacityExceeded: http.StatusTooManyRequests, kitworkspace.Unavailable: http.StatusServiceUnavailable,
		}[workspaceErr.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeJSON(writer, status, map[string]any{"error": map[string]any{"code": workspaceErr.Code, "message": workspaceErr.Message, "details": workspaceErr.Details}})
		return
	}
	status := http.StatusInternalServerError
	message := "internal server error"
	switch {
	case errors.Is(err, kitsession.ErrPluginCommandUnavailable):
		writeJSON(writer, http.StatusConflict, map[string]any{"error": map[string]string{"code": protocol.PluginCommandUnavailable, "message": kitsession.ErrPluginCommandUnavailable.Error()}})
		return
	case errors.Is(err, kitsession.ErrPluginCommandFailed):
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]any{"error": map[string]string{"code": protocol.PluginCommandFailed, "message": kitsession.ErrPluginCommandFailed.Error()}})
		return
	case errors.Is(err, kitsession.ErrNotFound), errors.Is(err, kitsession.ErrInteractionNotFound), errors.Is(err, subagent.ErrNotFound), errors.Is(err, kitannotation.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, kitsession.ErrTranscriptCursorUnavailable), errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrReloadBusy), errors.Is(err, kitsession.ErrConfigureBusy), errors.Is(err, kitsession.ErrConfigurationConflict), errors.Is(err, kitsession.ErrDeleteBusy), errors.Is(err, kitsession.ErrRunNotAbortable), errors.Is(err, kitsession.ErrBashBusy), errors.Is(err, kitsession.ErrBashNotAbortable), errors.Is(err, kitsession.ErrInteractionSettled), errors.Is(err, subagent.ErrConflict), errors.Is(err, subagent.ErrNotCancelable), errors.Is(err, subagent.ErrDismissed), errors.Is(err, kitannotation.ErrStale):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, kitsession.ErrPluginNotificationCapacity), errors.Is(err, kitsession.ErrInteractionCapacity), errors.Is(err, subagent.ErrQueueFull), errors.Is(err, kitannotation.ErrCapacity):
		status = http.StatusTooManyRequests
		message = err.Error()
	case errors.Is(err, kitsession.ErrClosed), errors.Is(err, subagent.ErrClosed):
		status = http.StatusServiceUnavailable
		message = err.Error()
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, kitsession.ErrNotTemporary), errors.Is(err, kitsession.ErrTemporary), errors.Is(err, subagent.ErrInvalidInput), errors.Is(err, subagent.ErrTemporaryUnavailable), errors.Is(err, errInvalidSessionRequest):
		status = http.StatusBadRequest
		message = err.Error()
	}
	if status >= http.StatusInternalServerError {
		reportRequestError(writer, status, err)
	}
	writeJSON(writer, status, map[string]string{"error": message})
}

// annotationAPIError projects annotation operation failures into ADR-0034 envelopes.
func annotationAPIError(err error) error {
	var evidence *kitannotation.EvidenceError
	if errors.As(err, &evidence) {
		status := map[protocol.AnnotationEvidenceErrorCode]int{
			protocol.AnnotationEvidenceErrorInvalid: http.StatusBadRequest, protocol.AnnotationEvidenceErrorPermission: http.StatusForbidden,
			protocol.AnnotationEvidenceErrorStaleWorkspace: http.StatusConflict, protocol.AnnotationEvidenceErrorStaleTarget: http.StatusConflict, protocol.AnnotationEvidenceErrorStaleFile: http.StatusConflict,
			protocol.AnnotationEvidenceErrorLimit: http.StatusRequestEntityTooLarge, protocol.AnnotationEvidenceErrorUnavailable: http.StatusServiceUnavailable,
		}[evidence.Kind]
		return httpapi.NewAPIError(status, httpapi.ErrorCode(evidence.Kind), evidence.Error(), nil)
	}
	if errors.Is(err, kitsession.ErrNotFound) {
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "session not found", nil)
	}
	if errors.Is(err, kitsession.ErrInvalidInput) || errors.Is(err, errInvalidSessionRequest) {
		return invalidAPIError()
	}
	return internalAPIError()
}
