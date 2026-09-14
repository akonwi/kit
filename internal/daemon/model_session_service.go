package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/sessiontool"
)

type modelSessionManager interface {
	Get(context.Context, string) (kitsession.SessionRecord, error)
	Create(context.Context, kitsession.CreateInput) (kitsession.SessionRecord, error)
	StartPrompt(context.Context, string, string) (kitsession.RunReservation, error)
	Delete(context.Context, string) error
}

type modelSessionService struct {
	manager            modelSessionManager
	providers          droids.Providers
	availableProviders func(context.Context) []string
	configuredDefault  func() (string, error)
}

func (s modelSessionService) CreateModelSession(ctx context.Context, input sessiontool.CreateInput) (sessiontool.Session, error) {
	if s.manager == nil || s.providers == nil {
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
	model, err := s.defaultModel(ctx)
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

func (s modelSessionService) defaultModel(ctx context.Context) (string, error) {
	available := make(map[string]bool)
	if s.availableProviders == nil {
		for _, model := range s.providers.Models() {
			available[model.Provider] = true
		}
	} else {
		for _, provider := range s.availableProviders(ctx) {
			available[provider] = true
		}
	}
	models := s.providers.Models()
	preferredModels := []string{}
	if s.configuredDefault != nil {
		configured, err := s.configuredDefault()
		if err != nil {
			return "", err
		}
		if configured != "" {
			preferredModels = append(preferredModels, configured)
		}
	}
	preferredModels = append(preferredModels, "openai-codex/gpt-5.6-sol", "anthropic/claude-sonnet-4-6")
	for _, preferred := range preferredModels {
		model, ok := s.providers.Model(preferred)
		if ok && available[model.Provider] {
			return model.Provider + "/" + model.ID, nil
		}
	}
	for _, model := range models {
		if available[model.Provider] {
			return model.Provider + "/" + model.ID, nil
		}
	}
	return "", fmt.Errorf("no available model provider")
}

var _ sessiontool.Service = modelSessionService{}
