package cli

import "github.com/akonwi/kit/internal/settings"

type interactiveDiffPreferenceService struct {
	settings *settings.Store
}

func (s interactiveDiffPreferenceService) SetWrapLines(enabled bool) error {
	_, err := s.settings.UpdateDiffWrapLines(enabled)
	return err
}

func (s interactiveDiffPreferenceService) SetLayout(layout string) error {
	_, err := s.settings.UpdateDiffLayout(layout)
	return err
}

type interactiveWorkspacePaneService struct {
	settings *settings.Store
}

func (s interactiveWorkspacePaneService) SetWorkspacePanes(panes string) error {
	_, err := s.settings.UpdateWorkspacePanes(panes)
	return err
}
