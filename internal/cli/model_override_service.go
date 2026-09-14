package cli

import "github.com/akonwi/kit/internal/settings"

type interactiveModelOverrideService struct {
	settings *settings.Store
}

func (s interactiveModelOverrideService) SetContextWindow(selector string, contextWindow int) error {
	_, err := s.settings.UpdateModelContextWindow(selector, contextWindow)
	return err
}
