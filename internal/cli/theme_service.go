package cli

import (
	"github.com/akonwi/kit/internal/settings"
	kittheme "github.com/akonwi/kit/internal/theme"
)

type interactiveThemeService struct {
	directory string
	settings  *settings.Store
}

func (s interactiveThemeService) Discover() ([]string, error) {
	return kittheme.Discover(s.directory)
}

func (s interactiveThemeService) Load(name string) (kittheme.Definition, []kittheme.Diagnostic, error) {
	return kittheme.Load(s.directory, name)
}

func (s interactiveThemeService) Save(name string) error {
	_, err := s.settings.UpdateTheme(name)
	return err
}
