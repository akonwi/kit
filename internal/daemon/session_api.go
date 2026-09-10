package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/systemprompt"
	kitvcs "github.com/akonwi/kit/internal/vcs"
)

const maxSessionRequestBytes = 1 << 20

var errInvalidSessionRequest = errors.New("invalid session request")

type sessionService interface {
	Create(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	ChangeCWD(context.Context, string, protocol.ChangeCWDInput) (protocol.SessionInfo, error)
	Rename(context.Context, string, protocol.RenameSessionInput) (protocol.SessionInfo, error)
	Delete(context.Context, string) error
	DisposeTemporary(context.Context, string) error
	List(context.Context, string) ([]protocol.SessionInfo, error)
	Models(context.Context) (protocol.ModelCatalog, error)
	Snapshot(context.Context, string) (protocol.SessionSnapshot, error)
	VCS(context.Context, string) (protocol.SessionVCSStatus, error)
	Events(context.Context, string, string, int64) (protocol.SessionEventBatch, error)
	Reload(context.Context, string) (protocol.ReloadSessionResult, error)
	Configure(context.Context, string, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	Compact(context.Context, string, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
	StartPrompt(context.Context, string, string) (protocol.RunReservation, error)
	StartPromptCommand(context.Context, string, protocol.PromptCommandInput) (protocol.RunReservation, error)
	Run(context.Context, string, string) (protocol.RunInfo, error)
	RunPrompt(context.Context, string, string) (protocol.PromptOutcome, error)
	Abort(context.Context, string, string) error
	StartBash(context.Context, string, protocol.BashExecutionInput) (protocol.BashExecution, error)
	Bash(context.Context, string, string) (protocol.BashExecution, error)
	AbortBash(context.Context, string, string) error
}

type runtimeSessionService struct {
	manager            *kitsession.Manager
	availableProviders func(context.Context) []string
	probeVCS           func(context.Context, string) (*kitvcs.Status, error)
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

func (s runtimeSessionService) ChangeCWD(
	ctx context.Context,
	sessionID string,
	input protocol.ChangeCWDInput,
) (protocol.SessionInfo, error) {
	result, err := s.manager.ChangeCWDWithID(ctx, sessionID, input.MutationID, input.Path)
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return projectSession(result.Session), nil
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
	return s.manager.Delete(ctx, sessionID)
}

func (s runtimeSessionService) DisposeTemporary(ctx context.Context, sessionID string) error {
	return s.manager.DisposeTemporary(ctx, sessionID)
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
	snapshot, err := s.manager.Snapshot(ctx, sessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	result := protocol.SessionSnapshot{
		Session: projectSession(snapshot.Session), ActiveRunID: snapshot.ActiveRunID,
		ActiveBashExecutionID: snapshot.ActiveBashExecutionID,
		EventStreamID:         snapshot.EventStreamID, EventCursor: snapshot.EventCursor,
		EventReplayFrom: snapshot.EventReplayFrom, EventReplayAvailable: snapshot.EventReplayAvailable,
		ContextTokens: snapshot.ContextTokens, ContextWindow: snapshot.ContextWindow,
		Usage:             projectSessionUsage(snapshot.Usage),
		Messages:          make([]protocol.TranscriptMessage, 0, len(snapshot.Messages)),
		PendingBoundaries: make([]protocol.PendingBoundary, 0, len(snapshot.Boundaries)),
		PromptCommands:    make([]protocol.PromptCommand, 0, len(snapshot.PromptCommands)),
		Warnings:          append([]string(nil), snapshot.Warnings...),
	}
	for _, command := range snapshot.PromptCommands {
		result.PromptCommands = append(result.PromptCommands, protocol.PromptCommand{
			Name: command.Name, Description: command.Description,
			Source: command.Source, Location: command.Location,
		})
	}
	for _, message := range snapshot.Messages {
		result.Messages = append(result.Messages, protocol.TranscriptMessage{
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
		result = append(result, protocol.TranscriptContent{
			Kind: protocol.TranscriptContentKind(block.Kind), Text: block.Text,
			ToolCallID: block.ToolCallID, ToolName: block.ToolName,
			Arguments: block.Arguments, ArgumentsTruncated: block.ArgumentsTruncated,
			Filename: block.Filename, MediaType: block.MediaType,
		})
	}
	return result
}

func (s runtimeSessionService) Events(ctx context.Context, sessionID, streamID string, after int64) (protocol.SessionEventBatch, error) {
	page, err := s.manager.Events(ctx, sessionID, streamID, after)
	if err != nil {
		return protocol.SessionEventBatch{}, err
	}
	batch := protocol.SessionEventBatch{
		StreamID: page.StreamID, FirstSequence: page.FirstSequence, LastSequence: page.LastSequence,
		ResyncRequired: page.ResyncRequired, UsageBaseline: projectSessionUsagePointer(page.UsageBaseline),
		Events: make([]protocol.SessionEvent, 0, len(page.Events)),
	}
	for _, event := range page.Events {
		batch.Events = append(batch.Events, protocol.SessionEvent{
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
			Usage: projectSessionUsagePointer(event.Usage),
		})
	}
	return batch, nil
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

func (s runtimeSessionService) StartPrompt(
	ctx context.Context,
	sessionID, text string,
) (protocol.RunReservation, error) {
	reservation, err := s.manager.StartPrompt(ctx, sessionID, text)
	if err != nil {
		return protocol.RunReservation{}, err
	}
	return protocol.RunReservation{
		SessionID: reservation.SessionID, TurnID: reservation.TurnID, RunID: reservation.RunID,
	}, nil
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

func (s runtimeSessionService) RunPrompt(
	ctx context.Context,
	sessionID, text string,
) (protocol.PromptOutcome, error) {
	result, err := s.manager.RunPrompt(ctx, sessionID, text)
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
		result, err := service.StartPrompt(
			request.Context(), request.PathValue("sessionID"), input.Text,
		)
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
		result, err := service.RunPrompt(
			request.Context(), request.PathValue("sessionID"), input.Text,
		)
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
	return protocol.SessionInfo{
		ID: record.ID, CWD: record.CWD, Name: name,
		Model:         record.ModelProvider + "/" + record.ModelID,
		ThinkingLevel: record.ThinkingLevel, ConfigurationRevision: record.ConfigurationRevision,
		CreatedAt: record.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: record.UpdatedAt.Format(time.RFC3339Nano),
	}
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
	status := http.StatusInternalServerError
	message := "internal server error"
	switch {
	case errors.Is(err, kitsession.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrReloadBusy), errors.Is(err, kitsession.ErrConfigureBusy), errors.Is(err, kitsession.ErrConfigurationConflict), errors.Is(err, kitsession.ErrDeleteBusy), errors.Is(err, kitsession.ErrRunNotAbortable), errors.Is(err, kitsession.ErrBashBusy), errors.Is(err, kitsession.ErrBashNotAbortable):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, kitsession.ErrClosed):
		status = http.StatusServiceUnavailable
		message = err.Error()
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, kitsession.ErrNotTemporary), errors.Is(err, kitsession.ErrTemporary), errors.Is(err, errInvalidSessionRequest):
		status = http.StatusBadRequest
		message = err.Error()
	}
	writeJSON(writer, status, map[string]string{"error": message})
}
