package cli

import (
	"github.com/akonwi/kit/internal/settings"
	kittheme "github.com/akonwi/kit/internal/theme"
)

// ThemeLoad projects ThemeService.Load's three Go return values into the
// single value-plus-error shape supported by Ard's direct Go interop.
type ThemeLoad struct {
	Definition  kittheme.Definition
	Diagnostics []kittheme.Diagnostic
}

// LoadTheme loads one user theme through the CLI service boundary.
func LoadTheme(service ThemeService, name string) (ThemeLoad, error) {
	definition, diagnostics, err := service.Load(name)
	return ThemeLoad{Definition: definition, Diagnostics: diagnostics}, err
}

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
