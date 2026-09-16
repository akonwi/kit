package daemon

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

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/fileindex"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
	kitvcs "github.com/akonwi/kit/internal/vcs"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

type annotationWorkspaceReader struct{ service *kitworkspace.Service }

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

const maxSessionRequestBytes = 1 << 20

var errInvalidSessionRequest = errors.New("invalid session request")

type sessionService interface {
	Create(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	Fork(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error)
	ChangeCWD(context.Context, string, protocol.ChangeCWDInput) (protocol.ChangeWorkspaceCWDResult, error)
	Rename(context.Context, string, protocol.RenameSessionInput) (protocol.SessionInfo, error)
	Delete(context.Context, string) error
	DisposeTemporary(context.Context, string) error
	List(context.Context, string) ([]protocol.SessionInfo, error)
	Models(context.Context) (protocol.ModelCatalog, error)
	Snapshot(context.Context, string) (protocol.SessionSnapshot, error)
	TranscriptPage(context.Context, string, string) (protocol.TranscriptPage, error)
	VCS(context.Context, string) (protocol.SessionVCSStatus, error)
	FileIndex(context.Context, string, bool) (protocol.SessionFileIndex, error)
	Workspace(context.Context, string) (protocol.WorkspaceRef, error)
	ListDirectory(context.Context, string, protocol.ListDirectoryInput) (protocol.DirectoryPage, error)
	ReadWorkspaceFile(context.Context, string, protocol.ReadWorkspaceFileInput) (protocol.WorkspaceFileRead, error)
	ListAnnotations(context.Context, string, protocol.ListAnnotationsInput) (protocol.AnnotationPage, error)
	CreateAnnotation(context.Context, string, protocol.CreateAnnotationInput) (protocol.Annotation, error)
	UpdateAnnotation(context.Context, string, protocol.UpdateAnnotationInput) (protocol.Annotation, error)
	DeleteAnnotation(context.Context, string, protocol.DeleteAnnotationInput) error
	Events(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	WaitEvents(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	Reload(context.Context, string) (protocol.ReloadSessionResult, error)
	Configure(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	StartPrompt(context.Context, string, protocol.PromptInput) (protocol.RunReservation, error)
	SubmitPrompt(context.Context, string, protocol.PromptInput) (protocol.PromptSubmission, error)
	RestoreFollowUps(context.Context, string) (protocol.RestoreFollowUpsResult, error)
	PromoteFollowUps(context.Context, string) (protocol.PromoteFollowUpsResult, error)
	StartPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.RunReservation, error)
	Run(context.Context, string, string) (protocol.RunInfo, error)
	RunPrompt(context.Context, string, protocol.PromptInput) (protocol.PromptOutcome, error)
	Abort(context.Context, string, string) error
	RespondInteraction(context.Context, string, protocol.InteractionResponse) error
	StartBash(context.Context, string, protocol.BashExecutionInput) (protocol.BashExecution, error)
	Bash(context.Context, string, string) (protocol.BashExecution, error)
	AbortBash(context.Context, string, string) error
	Subagent(context.Context, string, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error)
	SubagentTranscript(context.Context, string, string) (protocol.SubagentTranscript, error)
	SubagentEvents(context.Context, string, string, string, int64) (protocol.SubagentLiveEventPage, error)
}

type runtimeSessionService struct {
	manager             *kitsession.Manager
	availableProviders  func(context.Context) []string
	modelContextWindow  func(string) int
	probeVCS            func(context.Context, string) (*kitvcs.Status, error)
	fileIndexes         *sessionFileIndexCache
	workspaces          *kitworkspace.Service
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
	return projectSession(record), nil
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
	return projectSession(result.Session), nil
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
	sessionInfo := projectSession(result.Session)
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
	return projectSession(record), nil
}

func (s runtimeSessionService) Delete(ctx context.Context, sessionID string) error {
	if err := s.manager.Delete(ctx, sessionID); err != nil {
		return err
	}
	if s.workspaces != nil {
		s.workspaces.RemoveSession(sessionID)
	}
	if s.annotations != nil {
		s.annotations.ForgetSession(sessionID)
	}
	if s.attachments != nil {
		return s.attachments.RemoveSession(ctx, sessionID)
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
		result = append(result, projectSession(record))
	}
	return result, nil
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
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.AnnotationPage{}, err
	}
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
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.Annotation{}, err
	}
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return protocol.Annotation{}, err
	}
	anchor := input.Anchor.WorkspaceFile
	created, err := s.annotations.Create(ctx, sessionID, record.CWD, kitannotation.WorkspaceFileAnchor{
		WorkspaceID: anchor.WorkspaceID, Path: anchor.Path, FileRevision: anchor.FileRevision,
		StartLine: anchor.StartLine, EndLine: anchor.EndLine,
	}, input.Body)
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
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.Annotation{}, err
	}
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
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := s.prepareAnnotationSession(ctx, record); err != nil {
		return err
	}
	if err := s.annotations.Delete(ctx, sessionID, input.AnnotationID); err != nil {
		return err
	}
	return nil
}

func projectAnnotation(record kitannotation.Record, stale protocol.AnnotationStaleReason) protocol.Annotation {
	return protocol.Annotation{
		ID: record.ID, SessionID: record.SessionID,
		Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
			WorkspaceID: record.Anchor.WorkspaceID, Path: record.Anchor.Path, FileRevision: record.Anchor.FileRevision,
			StartLine: record.Anchor.StartLine, EndLine: record.Anchor.EndLine,
		}},
		Body:    record.Body,
		Preview: protocol.AnnotationPreview{StartLine: record.Preview.StartLine, EndLine: record.Preview.EndLine, Text: record.Preview.Text, Truncated: record.Preview.Truncated},
		Stale:   stale != "", StaleReason: stale,
	}
}

func projectAnnotationSummary(record kitannotation.Record, stale protocol.AnnotationStaleReason) protocol.AnnotationSummary {
	annotation := projectAnnotation(record, stale)
	return protocol.AnnotationSummary{
		ID: annotation.ID, Anchor: annotation.Anchor,
		BodyPreview: truncateAnnotationSummary(annotation.Body),
		Preview:     truncateAnnotationSummary(annotation.Preview.Text),
		Stale:       annotation.Stale, StaleReason: annotation.StaleReason,
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
	record, err := s.manager.Get(ctx, sessionID)
	if err != nil {
		return protocol.SessionVCSStatus{}, err
	}
	result := protocol.SessionVCSStatus{SessionID: sessionID, CWD: record.CWD}
	probe := s.probeVCS
	if probe == nil {
		probe = kitvcs.Probe
	}
	status, err := probe(ctx, record.CWD)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.SessionVCSStatus{}, ctx.Err()
		}
		return result, nil
	}
	if status != nil {
		result.Status = &protocol.VCSStatus{
			Root: status.Root, Dirty: status.Dirty,
			Head: protocol.VCSHead{Kind: protocol.VCSHeadKind(status.Head.Kind), Name: status.Head.Name, OID: status.Head.OID},
		}
		if err := result.Validate(); err != nil {
			result.Status = nil
		}
	}
	return result, nil
}

func (s runtimeSessionService) Snapshot(ctx context.Context, sessionID string) (protocol.SessionSnapshot, error) {
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
		Session: projectSession(snapshot.Session), Workspace: workspaceRef, ActiveRunID: snapshot.ActiveRunID,
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
			Count: snapshot.FollowUps.Count, Previews: append([]string(nil), snapshot.FollowUps.Previews...),
		},
		Warnings:              append([]string(nil), snapshot.Warnings...),
		SubagentDefinitions:   make([]protocol.SubagentDefinition, 0, len(snapshot.SubagentDefinitions)),
		SubagentDiagnostics:   make([]protocol.SubagentDiagnostic, 0, len(snapshot.SubagentDiagnostics)),
		SubagentConversations: make([]protocol.SubagentConversation, 0, len(snapshot.SubagentConversations)),
		SubagentMailbox:       make([]protocol.SubagentMailboxItem, 0, len(snapshot.SubagentMailbox)),
		PendingInteractions:   make([]protocol.InteractionRequest, 0, len(snapshot.PendingInteractions)),
		Annotations:           annotations,
	}
	if snapshot.ProviderRetry != nil {
		result.ProviderRetry = &protocol.ProviderRetry{
			Count: snapshot.ProviderRetry.Count, RetryAt: snapshot.ProviderRetry.RetryAt.Format(time.RFC3339Nano),
		}
	}
	if snapshot.ActiveCompaction != nil {
		result.ActiveCompaction = &protocol.ActiveCompaction{ID: snapshot.ActiveCompaction.ID, RunID: snapshot.ActiveCompaction.RunID}
	}
	for _, interaction := range snapshot.PendingInteractions {
		result.PendingInteractions = append(result.PendingInteractions, projectInteractionRequest(interaction))
	}
	for _, item := range snapshot.SubagentMailbox {
		result.SubagentMailbox = append(result.SubagentMailbox, protocol.SubagentMailboxItem{
			ID: item.ID, ConversationID: item.ConversationID, TaskID: item.TaskID,
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
	for _, command := range snapshot.PromptCommands {
		result.PromptCommands = append(result.PromptCommands, protocol.PromptCommand{
			Name: command.Name, Description: command.Description,
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

func (s runtimeSessionService) SubagentTranscript(ctx context.Context, sessionID, conversationID string) (protocol.SubagentTranscript, error) {
	conversation, err := s.subagents.Conversation(ctx, subagent.ConversationID(conversationID))
	if err != nil || conversation.OwnerSessionID != sessionID {
		if err == nil {
			err = subagent.ErrNotFound
		}
		return protocol.SubagentTranscript{}, err
	}
	transcript, err := s.subagents.Transcript(ctx, conversation.ID)
	if err != nil {
		return protocol.SubagentTranscript{}, err
	}
	result := protocol.SubagentTranscript{ConversationID: conversationID, Messages: make([]protocol.TranscriptMessage, 0, len(transcript.Messages))}
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
			projected.Content = append(projected.Content, protocol.TranscriptContent{
				Kind: protocol.TranscriptContentKind(block.Kind), Text: block.Text,
				ToolCallID: block.ToolCallID, ToolName: block.ToolName,
				Arguments: block.Arguments, ArgumentsTruncated: block.ArgumentsTruncated,
				Filename: block.Filename, MediaType: block.MediaType,
			})
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

func projectTranscriptContent(content []kitsession.TranscriptContent) []protocol.TranscriptContent {
	result := make([]protocol.TranscriptContent, 0, len(content))
	for _, block := range content {
		projected := protocol.TranscriptContent{
			Kind: protocol.TranscriptContentKind(block.Kind), Text: block.Text,
			ToolCallID: block.ToolCallID, ToolName: block.ToolName,
			Arguments: block.Arguments, ArgumentsTruncated: block.ArgumentsTruncated,
			Filename: block.Filename, MediaType: block.MediaType, AttachmentID: block.AttachmentID,
		}
		for _, annotation := range block.Annotations {
			projected.Annotations = append(projected.Annotations, protocol.SubmittedAnnotation{
				OriginalAnnotationID: annotation.ID,
				Anchor: protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorKind(annotation.Kind), WorkspaceFile: &protocol.WorkspaceFileAnnotationAnchor{
					WorkspaceID: annotation.WorkspaceID, Path: annotation.Path, FileRevision: annotation.FileRevision,
					StartLine: annotation.StartLine, EndLine: annotation.EndLine,
				}},
				Body:    annotation.Body,
				Preview: protocol.AnnotationPreview{StartLine: annotation.StartLine, EndLine: annotation.EndLine, Text: annotation.Preview, Truncated: annotation.Truncated},
			})
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
	result := protocol.InteractionRequest{ID: request.ID, SessionID: request.SessionID, RunID: request.RunID, ToolCallID: request.ToolCallID, Kind: protocol.InteractionKind(request.Kind), Title: request.Title, Detail: request.Detail, CreatedAt: request.CreatedAt.Format(time.RFC3339Nano), Options: make([]protocol.InteractionOption, 0, len(request.Options)), Questions: make([]protocol.InteractionQuestion, 0, len(request.Questions))}
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
			StreamID: event.StreamID, Sequence: event.Sequence,
			SessionID: event.SessionID, TurnID: event.TurnID, RunID: event.RunID,
			MessageID: event.MessageID, Kind: protocol.SessionEventKind(event.Kind), ContentIndex: event.ContentIndex,
			Delta: event.Delta, Text: event.Text, Thinking: event.Thinking,
			ToolCallID: event.ToolCallID, ToolName: event.ToolName,
			Arguments: event.Arguments, ArgumentsTruncated: event.ArgumentsTruncated,
			Content: projectTranscriptContent(event.Content), ContentTruncated: event.ContentTruncated,
			Details: append(json.RawMessage(nil), event.Details...), DetailsOmitted: event.DetailsOmitted,
			IsError: event.IsError, Status: protocol.RunStatus(event.Status),
			ErrorKind: projectProviderErrorKind(event.ErrorKind), ErrorMessage: event.ErrorMessage,
			CompactionID:  event.CompactionID,
			ContextTokens: event.ContextTokens, ContextWindow: event.ContextWindow,
			Usage:                  projectSessionUsagePointer(event.Usage),
			SessionName:            event.SessionName,
			SubagentConversationID: event.SubagentConversationID, SubagentTaskID: event.SubagentTaskID,
			PeerRequestID: event.PeerRequestID,
			InteractionID: event.InteractionID, InteractionResolution: event.InteractionResolution,
			AnnotationID: event.AnnotationID, AnnotationIDs: append([]uint64(nil), event.AnnotationIDs...), AcceptedMessageID: event.AcceptedMessageID,
		}
		if event.Annotation != nil {
			annotation := projectAnnotation(*event.Annotation, "")
			projected.Annotation = &annotation
		}
		if event.Kind == kitsession.EventSessionCWDChanged && s.workspaces != nil {
			workspaceRef := s.workspaces.Ref(event.SessionID, event.CWD)
			projected.Workspace = &workspaceRef
		}
		if event.ProviderRetry != nil {
			projected.ProviderRetry = &protocol.ProviderRetry{Count: event.ProviderRetry.Count}
			if !event.ProviderRetry.RetryAt.IsZero() {
				projected.ProviderRetry.RetryAt = event.ProviderRetry.RetryAt.Format(time.RFC3339Nano)
			}
		}
		if event.Interaction != nil {
			interaction := projectInteractionRequest(*event.Interaction)
			projected.Interaction = &interaction
		}
		batch.Events = append(batch.Events, projected)
	}
	return batch
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
		Session: projectSession(result.Session), EventStreamID: result.EventStreamID,
		Compacted: result.Compacted, CheckpointID: result.CheckpointID,
		Warnings: append([]string(nil), result.Warnings...),
	}, nil
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

func (s runtimeSessionService) StartPrompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.RunReservation, error) {
	reservation, err := s.manager.StartPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.RunReservation{}, err
	}
	return protocol.RunReservation{
		SessionID: reservation.SessionID, TurnID: reservation.TurnID, RunID: reservation.RunID,
	}, nil
}

func (s runtimeSessionService) SubmitPrompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptSubmission, error) {
	result, err := s.manager.SubmitPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.PromptSubmission{}, err
	}
	output := protocol.PromptSubmission{Queued: result.Queued, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews}}
	if !result.Queued {
		output.Reservation = &protocol.RunReservation{SessionID: result.Reservation.SessionID, TurnID: result.Reservation.TurnID, RunID: result.Reservation.RunID}
	}
	return output, nil
}

func (s runtimeSessionService) RestoreFollowUps(ctx context.Context, sessionID string) (protocol.RestoreFollowUpsResult, error) {
	result, err := s.manager.RestoreFollowUps(ctx, sessionID)
	messages := make([]protocol.PromptInput, 0, len(result.Messages))
	for _, message := range result.Messages {
		messages = append(messages, protocol.PromptInput{Text: message.Text, AttachmentIDs: append([]string(nil), message.AttachmentIDs...), AnnotationIDs: append([]uint64(nil), message.AnnotationIDs...)})
	}
	return protocol.RestoreFollowUpsResult{Messages: messages, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews}}, err
}

func (s runtimeSessionService) PromoteFollowUps(ctx context.Context, sessionID string) (protocol.PromoteFollowUpsResult, error) {
	result, err := s.manager.PromoteFollowUps(ctx, sessionID)
	return protocol.PromoteFollowUpsResult{Promoted: result.Promoted, Queue: protocol.FollowUpQueue{Count: result.Queue.Count, Previews: result.Queue.Previews}}, err
}

func (s runtimeSessionService) StartPromptCommand(ctx context.Context, sessionID string, input protocol.PromptCommandInput) (protocol.RunReservation, error) {
	reservation, err := s.manager.StartPromptCommand(ctx, sessionID, input.Name, input.Args)
	if err != nil {
		return protocol.RunReservation{}, err
	}
	return protocol.RunReservation{
		SessionID: reservation.SessionID, TurnID: reservation.TurnID, RunID: reservation.RunID,
	}, nil
}

func (s runtimeSessionService) Run(ctx context.Context, sessionID, runID string) (protocol.RunInfo, error) {
	record, err := s.manager.GetRun(ctx, sessionID, runID)
	if err != nil {
		return protocol.RunInfo{}, err
	}
	return protocol.RunInfo{
		SessionID: record.SessionID, TurnID: record.TurnID, RunID: record.ID,
		Status: protocol.RunStatus(record.Status), ErrorMessage: record.Error,
	}, nil
}

func (s runtimeSessionService) RunPrompt(ctx context.Context, sessionID string, input protocol.PromptInput) (protocol.PromptOutcome, error) {
	result, err := s.manager.RunPromptInput(ctx, sessionID, kitsession.PromptInput{Text: input.Text, AttachmentIDs: input.AttachmentIDs, AnnotationIDs: input.AnnotationIDs})
	if err != nil {
		return protocol.PromptOutcome{}, err
	}
	return protocol.PromptOutcome{
		SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID,
		Text: result.Text, StopReason: result.StopReason,
		Status: protocol.RunStatus(result.Status), ErrorKind: projectProviderErrorKind(result.ErrorKind),
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

func (s runtimeSessionService) Abort(ctx context.Context, sessionID, runID string) error {
	return s.manager.Abort(ctx, sessionID, runID)
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

func registerSessionRoutes(mux *http.ServeMux, service sessionService) {
	mux.HandleFunc("GET /v1/models", func(writer http.ResponseWriter, request *http.Request) {
		catalog, err := service.Models(request.Context())
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := catalog.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid model catalog: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, catalog)
	})
	mux.HandleFunc("GET /v1/sessions", func(writer http.ResponseWriter, request *http.Request) {
		records, err := service.List(request.Context(), request.URL.Query().Get("cwd"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"sessions": records})
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/forks", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ForkSessionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		record, err := service.Fork(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusCreated, record)
	})
	mux.HandleFunc("PATCH /v1/sessions/{sessionID}", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.RenameSessionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		record, err := service.Rename(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, record)
	})
	mux.HandleFunc("DELETE /v1/sessions/{sessionID}", func(writer http.ResponseWriter, request *http.Request) {
		if err := service.Delete(request.Context(), request.PathValue("sessionID")); err != nil {
			writeSessionError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/dispose", func(writer http.ResponseWriter, request *http.Request) {
		if err := service.DisposeTemporary(request.Context(), request.PathValue("sessionID")); err != nil {
			writeSessionError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}", func(writer http.ResponseWriter, request *http.Request) {
		snapshot, err := service.Snapshot(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, snapshot)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/messages", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("GET /v1/sessions/{sessionID}/workspace", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/workspace/directories", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/workspace/files/read", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("GET /v1/sessions/{sessionID}/annotations", func(writer http.ResponseWriter, request *http.Request) {
		input := protocol.ListAnnotationsInput{Cursor: request.URL.Query().Get("cursor")}
		if raw := request.URL.Query().Get("pageSize"); raw != "" {
			pageSize, err := strconv.Atoi(raw)
			if err != nil {
				writeSessionError(writer, fmt.Errorf("%w: annotation page size is invalid", errInvalidSessionRequest))
				return
			}
			input.PageSize = pageSize
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.ListAnnotations(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid annotation page: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/annotations", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.CreateAnnotationInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.CreateAnnotation(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid annotation: %w", err))
			return
		}
		writeJSON(writer, http.StatusCreated, result)
	})
	mux.HandleFunc("PATCH /v1/sessions/{sessionID}/annotations", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.UpdateAnnotationInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.UpdateAnnotation(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("DELETE /v1/sessions/{sessionID}/annotations", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.DeleteAnnotationInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		if err := service.DeleteAnnotation(request.Context(), request.PathValue("sessionID"), input); err != nil {
			writeSessionError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/files", func(writer http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/sessions/{sessionID}/vcs", func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.VCS(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session VCS status: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/events", func(writer http.ResponseWriter, request *http.Request) {
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
		writeJSON(writer, http.StatusOK, batch)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/events/stream", func(writer http.ResponseWriter, request *http.Request) {
		streamID, after, err := sessionEventCursor(request)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		flusher, ok := writer.(http.Flusher)
		if !ok {
			writeSessionError(writer, errors.New("streaming responses are unsupported"))
			return
		}
		batch, err := service.Events(request.Context(), request.PathValue("sessionID"), streamID, after)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := batch.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session event batch: %w", err))
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.Header().Set("X-Accel-Buffering", "no")
		writer.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(writer, ": connected\n\n"); err != nil {
			return
		}
		flusher.Flush()
		initial := true
		for {
			if !initial {
				waitContext, cancel := context.WithTimeout(request.Context(), 15*time.Second)
				var waitErr error
				batch, waitErr = service.WaitEvents(waitContext, request.PathValue("sessionID"), streamID, after)
				cancel()
				if waitErr != nil {
					if errors.Is(waitErr, context.DeadlineExceeded) && request.Context().Err() == nil {
						if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
							return
						}
						flusher.Flush()
						continue
					}
					return
				}
				if err := batch.Validate(); err != nil {
					return
				}
			}
			initial = false
			encoded, err := json.Marshal(batch)
			if err != nil {
				return
			}
			for _, event := range batch.Events {
				if event.Sequence > after {
					after = event.Sequence
				}
			}
			if batch.StreamID != "" {
				streamID = batch.StreamID
			}
			if batch.ResyncRequired {
				if _, err := fmt.Fprintf(writer, "event: session.resync\ndata: %s\n\n", encoded); err != nil {
					return
				}
				flusher.Flush()
				return
			}
			if _, err := fmt.Fprintf(writer, "event: session.events\nid: %s:%d\ndata: %s\n\n", streamID, after, encoded); err != nil {
				return
			}
			flusher.Flush()
		}
	})
	mux.HandleFunc("POST /v1/sessions", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.CreateSessionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		record, err := service.Create(request.Context(), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusCreated, record)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/cwd", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ChangeCWDInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.ChangeCWD(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session cwd result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/configure", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ConfigureSessionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.Configure(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.ValidateApplied(input); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session configuration result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/compact", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.CompactSessionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.Compact(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.ValidateApplied(input); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session compaction result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/reload", func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.Reload(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid session reload result: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/subagents/{conversationID}/events", func(writer http.ResponseWriter, request *http.Request) {
		after := int64(0)
		if raw := request.URL.Query().Get("after"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				writeSessionError(writer, fmt.Errorf("%w: child event cursor must be non-negative", errInvalidSessionRequest))
				return
			}
			after = parsed
		}
		result, err := service.SubagentEvents(request.Context(), request.PathValue("sessionID"), request.PathValue("conversationID"), request.URL.Query().Get("stream"), after)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid subagent events: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/subagents/{conversationID}/transcript", func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.SubagentTranscript(request.Context(), request.PathValue("sessionID"), request.PathValue("conversationID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid subagent transcript: %w", err))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/subagents", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.SubagentOperationInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.Subagent(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := result.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("invalid subagent operation result: %w", err))
			return
		}
		status := http.StatusOK
		if input.Action == protocol.SubagentStart || input.Action == protocol.SubagentMessage || input.Action == protocol.SubagentCancel {
			status = http.StatusAccepted
		}
		writeJSON(writer, status, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/submissions", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/follow-ups/restore", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/follow-ups/promote", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/prompts", func(writer http.ResponseWriter, request *http.Request) {
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
		if result.RunID == "" {
			writeSessionError(writer, errors.New("session prompt returned no reservation"))
			return
		}
		writeJSON(writer, http.StatusAccepted, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/prompt-commands", func(writer http.ResponseWriter, request *http.Request) {
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
		if result.RunID == "" {
			writeSessionError(writer, errors.New("session prompt command returned no reservation"))
			return
		}
		writeJSON(writer, http.StatusAccepted, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/prompt", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		result, err := service.RunPrompt(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		if result.RunID == "" {
			writeSessionError(writer, errors.New("session run returned no result"))
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/bash-executions", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.BashExecutionInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		execution, err := service.StartBash(request.Context(), request.PathValue("sessionID"), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, execution)
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/bash-executions/{executionID}", func(writer http.ResponseWriter, request *http.Request) {
		execution, err := service.Bash(request.Context(), request.PathValue("sessionID"), request.PathValue("executionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, execution)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/bash-executions/{executionID}/abort", func(writer http.ResponseWriter, request *http.Request) {
		if err := service.AbortBash(request.Context(), request.PathValue("sessionID"), request.PathValue("executionID")); err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, map[string]bool{"aborting": true})
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}/runs/{runID}", func(writer http.ResponseWriter, request *http.Request) {
		run, err := service.Run(
			request.Context(), request.PathValue("sessionID"), request.PathValue("runID"),
		)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, run)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/interactions/{interactionID}/response", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.InteractionResponse
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		if input.RequestID != request.PathValue("interactionID") {
			writeSessionError(writer, fmt.Errorf("%w: interaction identity mismatch", errInvalidSessionRequest))
			return
		}
		if err := input.Validate(); err != nil {
			writeSessionError(writer, fmt.Errorf("%w: %v", errInvalidSessionRequest, err))
			return
		}
		if err := service.RespondInteraction(request.Context(), request.PathValue("sessionID"), input); err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]bool{"settled": true})
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/runs/{runID}/abort", func(writer http.ResponseWriter, request *http.Request) {
		if err := service.Abort(
			request.Context(), request.PathValue("sessionID"), request.PathValue("runID"),
		); err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, map[string]bool{"aborting": true})
	})
}

func projectSession(record kitsession.SessionRecord) protocol.SessionInfo {
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
		Model:             record.ModelProvider + "/" + record.ModelID,
		ThinkingLevel:     record.ThinkingLevel, ConfigurationRevision: record.ConfigurationRevision,
		CreatedAt: record.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: record.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func sessionEventCursor(request *http.Request) (string, int64, error) {
	streamID := request.URL.Query().Get("stream")
	rawAfter := request.URL.Query().Get("after")
	lastEventID := request.Header.Get("Last-Event-ID")
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

func writeSessionError(writer http.ResponseWriter, err error) {
	var evidenceErr *kitannotation.EvidenceError
	if errors.As(err, &evidenceErr) {
		status := http.StatusServiceUnavailable
		switch evidenceErr.Kind {
		case kitannotation.EvidenceStaleWorkspace, kitannotation.EvidenceStaleFile:
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
	case errors.Is(err, kitsession.ErrNotFound), errors.Is(err, kitsession.ErrInteractionNotFound), errors.Is(err, subagent.ErrNotFound), errors.Is(err, kitannotation.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, kitsession.ErrTranscriptCursorUnavailable), errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrReloadBusy), errors.Is(err, kitsession.ErrConfigureBusy), errors.Is(err, kitsession.ErrConfigurationConflict), errors.Is(err, kitsession.ErrDeleteBusy), errors.Is(err, kitsession.ErrRunNotAbortable), errors.Is(err, kitsession.ErrBashBusy), errors.Is(err, kitsession.ErrBashNotAbortable), errors.Is(err, kitsession.ErrInteractionSettled), errors.Is(err, subagent.ErrConflict), errors.Is(err, subagent.ErrNotCancelable), errors.Is(err, subagent.ErrDismissed), errors.Is(err, kitannotation.ErrStale):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, kitsession.ErrInteractionCapacity), errors.Is(err, subagent.ErrQueueFull), errors.Is(err, kitannotation.ErrCapacity):
		status = http.StatusTooManyRequests
		message = err.Error()
	case errors.Is(err, kitsession.ErrClosed), errors.Is(err, subagent.ErrClosed):
		status = http.StatusServiceUnavailable
		message = err.Error()
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, kitsession.ErrNotTemporary), errors.Is(err, kitsession.ErrTemporary), errors.Is(err, subagent.ErrInvalidInput), errors.Is(err, subagent.ErrTemporaryUnavailable), errors.Is(err, errInvalidSessionRequest):
		status = http.StatusBadRequest
		message = err.Error()
	}
	writeJSON(writer, status, map[string]string{"error": message})
}
