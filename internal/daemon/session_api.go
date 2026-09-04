package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	kitsession "github.com/akonwi/kit/internal/session"
)

const maxSessionRequestBytes = 1 << 20

var errInvalidSessionRequest = errors.New("invalid session request")

type sessionService interface {
	Create(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error)
	List(context.Context, string) ([]protocol.SessionInfo, error)
	Snapshot(context.Context, string) (protocol.SessionSnapshot, error)
	Events(context.Context, string, int64) (protocol.SessionEventBatch, error)
	ReserveRun(context.Context, string, string) (protocol.RunReservation, error)
	StartPrompt(context.Context, string, string, string) (protocol.RunReservation, error)
	Run(context.Context, string, string) (protocol.RunInfo, error)
	RunPrompt(context.Context, string, string, string) (protocol.PromptOutcome, error)
	Abort(context.Context, string, string) error
}

type runtimeSessionService struct {
	manager *kitsession.Manager
}

func (s runtimeSessionService) Create(
	ctx context.Context,
	input protocol.CreateSessionInput,
) (protocol.SessionInfo, error) {
	record, err := s.manager.Create(ctx, kitsession.CreateInput{
		CWD: input.CWD, Name: input.Name, Model: input.Model,
		ThinkingLevel: input.ThinkingLevel,
	})
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	return projectSession(record), nil
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

func (s runtimeSessionService) Snapshot(ctx context.Context, sessionID string) (protocol.SessionSnapshot, error) {
	snapshot, err := s.manager.Snapshot(ctx, sessionID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	result := protocol.SessionSnapshot{
		Session: projectSession(snapshot.Session), ActiveRunID: snapshot.ActiveRunID,
		ContextTokens: snapshot.ContextTokens, ContextWindow: snapshot.ContextWindow,
		Messages: make([]protocol.TranscriptMessage, 0, len(snapshot.Messages)),
	}
	for _, message := range snapshot.Messages {
		content := make([]protocol.TranscriptContent, 0, len(message.Content))
		for _, block := range message.Content {
			content = append(content, protocol.TranscriptContent{
				Kind: protocol.TranscriptContentKind(block.Kind), Text: block.Text,
				ToolCallID: block.ToolCallID, ToolName: block.ToolName,
				Arguments: block.Arguments, ArgumentsTruncated: block.ArgumentsTruncated,
				Filename: block.Filename, MediaType: block.MediaType,
			})
		}
		result.Messages = append(result.Messages, protocol.TranscriptMessage{
			ID: message.ID, TurnID: message.TurnID, Sequence: message.Sequence,
			Role: message.Role, Content: content, StopReason: message.StopReason,
			ErrorMessage: message.ErrorMessage, ToolCallID: message.ToolCallID,
			ToolName: message.ToolName, Details: append(json.RawMessage(nil), message.Details...),
			IsError: message.IsError, CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return result, nil
}

func (s runtimeSessionService) Events(ctx context.Context, sessionID string, after int64) (protocol.SessionEventBatch, error) {
	page, err := s.manager.Events(ctx, sessionID, after)
	if err != nil {
		return protocol.SessionEventBatch{}, err
	}
	batch := protocol.SessionEventBatch{
		StreamID: page.StreamID, FirstSequence: page.FirstSequence, LastSequence: page.LastSequence,
		ResyncRequired: page.ResyncRequired, Events: make([]protocol.SessionEvent, 0, len(page.Events)),
	}
	for _, event := range page.Events {
		batch.Events = append(batch.Events, protocol.SessionEvent{
			StreamID: event.StreamID, Sequence: event.Sequence,
			SessionID: event.SessionID, TurnID: event.TurnID, RunID: event.RunID,
			Kind: protocol.SessionEventKind(event.Kind), ContentIndex: event.ContentIndex,
			Delta: event.Delta, Text: event.Text, Thinking: event.Thinking,
			ToolCallID: event.ToolCallID, ToolName: event.ToolName,
			Arguments: event.Arguments, ArgumentsTruncated: event.ArgumentsTruncated,
			IsError: event.IsError, Status: protocol.RunStatus(event.Status),
			ErrorKind: projectProviderErrorKind(event.ErrorKind), ErrorMessage: event.ErrorMessage,
		})
	}
	return batch, nil
}

func (s runtimeSessionService) ReserveRun(
	ctx context.Context,
	sessionID, runID string,
) (protocol.RunReservation, error) {
	reservation, err := s.manager.ReservePrompt(ctx, sessionID, runID)
	if err != nil {
		return protocol.RunReservation{}, err
	}
	return protocol.RunReservation{
		SessionID: reservation.SessionID,
		TurnID:    reservation.TurnID,
		RunID:     reservation.RunID,
	}, nil
}

func (s runtimeSessionService) StartPrompt(
	ctx context.Context,
	sessionID, runID, text string,
) (protocol.RunReservation, error) {
	reservation, err := s.manager.StartPrompt(ctx, sessionID, runID, text)
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
	sessionID, runID, text string,
) (protocol.PromptOutcome, error) {
	result, err := s.manager.RunPrompt(ctx, sessionID, runID, text)
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

func registerSessionRoutes(mux *http.ServeMux, service sessionService) {
	mux.HandleFunc("GET /v1/sessions", func(writer http.ResponseWriter, request *http.Request) {
		records, err := service.List(request.Context(), request.URL.Query().Get("cwd"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"sessions": records})
	})
	mux.HandleFunc("GET /v1/sessions/{sessionID}", func(writer http.ResponseWriter, request *http.Request) {
		snapshot, err := service.Snapshot(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, snapshot)
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
		batch, err := service.Events(request.Context(), request.PathValue("sessionID"), after)
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
		record, err := service.Create(request.Context(), input)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusCreated, record)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/runs", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.ReserveRunInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		reservation, err := service.ReserveRun(
			request.Context(), request.PathValue("sessionID"), input.RunID,
		)
		if err != nil {
			writeSessionError(writer, err)
			return
		}
		writeJSON(writer, http.StatusCreated, reservation)
	})
	mux.HandleFunc("POST /v1/sessions/{sessionID}/prompts", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		result, err := service.StartPrompt(
			request.Context(), request.PathValue("sessionID"), input.RunID, input.Text,
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
	mux.HandleFunc("POST /v1/sessions/{sessionID}/prompt", func(writer http.ResponseWriter, request *http.Request) {
		var input protocol.PromptInput
		if err := decodeSessionJSON(writer, request, &input); err != nil {
			writeSessionError(writer, err)
			return
		}
		result, err := service.RunPrompt(
			request.Context(), request.PathValue("sessionID"), input.RunID, input.Text,
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
	return protocol.SessionInfo{
		ID: record.ID, CWD: record.CWD, Name: record.Name,
		Model:         record.ModelProvider + "/" + record.ModelID,
		ThinkingLevel: record.ThinkingLevel,
		CreatedAt:     record.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:     record.UpdatedAt.Format(time.RFC3339Nano),
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
	case errors.Is(err, kitsession.ErrBusy), errors.Is(err, kitsession.ErrRunNotAbortable):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, kitsession.ErrClosed):
		status = http.StatusServiceUnavailable
		message = err.Error()
	case errors.Is(err, kitsession.ErrInvalidInput), errors.Is(err, errInvalidSessionRequest):
		status = http.StatusBadRequest
		message = err.Error()
	}
	writeJSON(writer, status, map[string]string{"error": message})
}
