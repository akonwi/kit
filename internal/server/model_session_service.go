package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/droids"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/sessiontool"
)

type modelSessionManager interface {
	Get(context.Context, string) (kitsession.SessionRecord, error)
	List(context.Context, string) ([]kitsession.SessionRecord, error)
	Create(context.Context, kitsession.CreateInput) (kitsession.SessionRecord, error)
	StartPrompt(context.Context, string, string) (kitsession.RunReservation, error)
	Delete(context.Context, string) error
	DeletionBlocker(context.Context, string) error
	ConfirmToolCall(context.Context, string, droids.ToolContext, kitsession.ToolConfirmation) (kitsession.ToolConfirmationResult, error)
}

type modelSessionService struct {
	manager  modelSessionManager
	defaults sessionDefaults
	// deleteSession permanently deletes a session that has no children,
	// along with the server's per-session state. Without it, deletion is
	// unavailable.
	deleteSession func(context.Context, string) error
}

func (s modelSessionService) CreateModelSession(ctx context.Context, input sessiontool.CreateInput) (sessiontool.Session, error) {
	if s.manager == nil || s.defaults.providers == nil {
		return sessiontool.Session{}, errors.New("session creation service is unavailable")
	}
	owner, err := s.manager.Get(ctx, input.OwnerSessionID)
	if err != nil {
		return sessiontool.Session{}, err
	}
	if !owner.Persistent {
		return sessiontool.Session{}, errors.New("temporary sessions cannot create top-level sessions")
	}
	if input.Name == "" {
		return sessiontool.Session{}, errors.New("session name is required")
	}
	model, err := s.defaults.model(ctx)
	if err != nil {
		return sessiontool.Session{}, err
	}
	record, err := s.manager.Create(ctx, kitsession.CreateInput{
		CWD: input.CWD, Name: input.Name, Model: model, ParentSessionID: owner.ID,
	})
	if err != nil {
		return sessiontool.Session{}, err
	}
	result := sessiontool.Session{
		ID: record.ID, CWD: record.CWD, Name: record.Name,
		Model: record.ModelProvider + "/" + record.ModelID, ThinkingLevel: record.ThinkingLevel,
	}
	if strings.TrimSpace(input.Prompt) != "" {
		reservation, promptErr := s.manager.StartPrompt(ctx, record.ID, input.Prompt)
		if promptErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			cleanupErr := s.manager.Delete(cleanupCtx, record.ID)
			cancel()
			if cleanupErr != nil {
				return sessiontool.Session{}, errors.Join(promptErr, fmt.Errorf("delete session after prompt admission failure: %w", cleanupErr))
			}
			return sessiontool.Session{}, promptErr
		}
		result.RunID = reservation.RunID
	}
	return result, nil
}

// ModelSessions lists the stored persistent sessions with their parent links.
func (s modelSessionService) ModelSessions(ctx context.Context) ([]sessiontool.SessionSummary, error) {
	if s.manager == nil {
		return nil, errors.New("session deletion service is unavailable")
	}
	records, err := s.manager.List(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]sessiontool.SessionSummary, 0, len(records))
	for _, record := range records {
		if !record.Persistent {
			continue
		}
		result = append(result, sessiontool.SessionSummary{
			ID: record.ID, Name: record.Name, CWD: record.CWD, ParentSessionID: record.ParentSessionID,
			Persistent: true, LastActivity: record.UpdatedAt,
		})
	}
	return result, nil
}

// DeletionBlocker reports why the session can't be deleted now.
func (s modelSessionService) DeletionBlocker(ctx context.Context, sessionID string) error {
	if s.manager == nil {
		return errors.New("session deletion service is unavailable")
	}
	return s.manager.DeletionBlocker(ctx, sessionID)
}

// ConfirmToolCall asks the owner session's user through its confirm
// interaction.
func (s modelSessionService) ConfirmToolCall(ctx context.Context, owner string, call droids.ToolContext, confirmation sessiontool.Confirmation) (sessiontool.Approval, error) {
	if s.manager == nil {
		return sessiontool.Approval{}, errors.New("session deletion service is unavailable")
	}
	result, err := s.manager.ConfirmToolCall(ctx, owner, call, kitsession.ToolConfirmation{
		Title: confirmation.Title, Detail: confirmation.Detail,
		ConfirmLabel: confirmation.ConfirmLabel, CancelLabel: confirmation.CancelLabel,
	})
	if err != nil {
		return sessiontool.Approval{}, err
	}
	return sessiontool.Approval{Confirmed: result.Confirmed, Resolution: result.Resolution}, nil
}

// DeleteModelSession permanently deletes one session that has no children
// through the session service's delete path.
func (s modelSessionService) DeleteModelSession(ctx context.Context, sessionID string) error {
	if s.deleteSession == nil {
		return errors.New("session deletion service is unavailable")
	}
	return s.deleteSession(ctx, sessionID)
}

var (
	_ sessiontool.Service         = modelSessionService{}
	_ sessiontool.DeletionService = modelSessionService{}
)
