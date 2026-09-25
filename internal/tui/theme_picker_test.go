package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type fakeThemeService struct {
	names       []string
	definitions map[string]kittheme.Definition
	loadErrors  map[string]error
	discoverErr error
	saveErr     error
	saved       []string
}

func (s *fakeThemeService) Discover() ([]string, error) { return s.names, s.discoverErr }
func (s *fakeThemeService) Load(name string) (kittheme.Definition, []kittheme.Diagnostic, error) {
	return s.definitions[name], nil, s.loadErrors[name]
}
func (s *fakeThemeService) Save(name string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, name)
	return nil
}

func TestThemePickerSurfacePresentsSystemThemesAndErrors(t *testing.T) {
	t.Parallel()

	application := uitest.New(themePickerSurface{Snapshot: themePickerSnapshot{
		Open: true, Names: []string{kittheme.SystemName, "nord"}, Selection: 1,
		Error: "could not load theme", Diagnostics: []kittheme.Diagnostic{{Section: "tokens"}},
	}})
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	for _, expected := range []string{"Theme", "System (terminal colors)", "nord", "could not load theme", "1 theme value(s) were ignored", "enter use", "esc cancel"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("render does not contain %q:\n%s", expected, text)
		}
	}
	column, row := findPaintedCellSequence(t, application, 80, 24, "nord")
	if got, want := application.Cell(column, row).Style.Background, ui.DefaultTheme().Selection; got != want {
		t.Fatalf("selected theme background = %v, want picker selection %v", got, want)
	}
}

func TestThemePickerSurfaceRevealsKeyboardSelectionAndProgress(t *testing.T) {
	t.Parallel()

	names := make([]string, 15)
	for index := range names {
		names[index] = fmt.Sprintf("theme-%02d", index)
	}
	application := uitest.New(themePickerSurface{Snapshot: themePickerSnapshot{
		Open: true, Names: names, Selection: 14, Loading: true,
	}})
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	for _, expected := range []string{"theme-14", "Loading preview…", "Loading… · esc cancel"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("render does not contain %q:\n%s", expected, text)
		}
	}
}

func findPaintedCellSequence(t *testing.T, app *uitest.App, width, height int, value string) (int, int) {
	t.Helper()
	cellWidth := len([]rune(value))
	for row := 0; row < height; row++ {
		for column := 0; column+cellWidth <= width; column++ {
			var candidate strings.Builder
			for offset := range cellWidth {
				candidate.WriteString(app.Cell(column+offset, row).Grapheme)
			}
			if candidate.String() == value {
				return column, row
			}
		}
	}
	t.Fatalf("painted text %q not found", value)
	return 0, 0
}

func TestThemePickerDiscoveryCompletionClearsLoading(t *testing.T) {
	t.Parallel()

	picker := themePickerController{Loading: true}
	picker.OpenNames([]string{"nord"}, kittheme.SystemName, kittheme.Definition{})
	if picker.Loading {
		t.Fatal("picker remained loading after discovery completed")
	}
}

func TestThemePickerPreviewsCommitsAndRestores(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	custom := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{names: []string{"custom"}, definitions: map[string]kittheme.Definition{"custom": custom}}
	var applied []kittheme.Definition
	apply := func(definition kittheme.Definition) { applied = append(applied, definition) }

	var picker themePickerController
	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(picker.Names, []string{kittheme.SystemName, "custom"}) || picker.Selection != 0 {
		t.Fatalf("opened picker = %+v", picker)
	}
	picker.Move(service, 1, apply)
	if !reflect.DeepEqual(applied, []kittheme.Definition{custom}) {
		t.Fatalf("preview applications = %#v", applied)
	}
	picker.Cancel(apply)
	if picker.Open || !reflect.DeepEqual(applied, []kittheme.Definition{custom, original}) {
		t.Fatalf("cancel state = %+v, applications = %#v", picker, applied)
	}

	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	picker.Move(service, 1, apply)
	name, definition, err := picker.Commit(service, apply)
	if err != nil || name != "custom" || !reflect.DeepEqual(definition, custom) || picker.Open || !reflect.DeepEqual(service.saved, []string{"custom"}) {
		t.Fatalf("commit name=%q err=%v picker=%+v saved=%v", name, err, picker, service.saved)
	}
}

func TestThemePickerKeepsLastValidPreviewOnLoadAndSaveFailures(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	service := &fakeThemeService{
		names:       []string{"broken"},
		definitions: map[string]kittheme.Definition{},
		loadErrors:  map[string]error{"broken": errors.New("bad file")},
	}
	applications := 0
	var picker themePickerController
	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	picker.Move(service, 1, func(kittheme.Definition) { applications++ })
	if picker.Err == nil || applications != 0 || picker.PreviewDefinition.Tokens[kittheme.TokenBackground].R != 1 {
		t.Fatalf("failed preview = %+v, applications=%d", picker, applications)
	}
	if _, _, err := picker.Commit(service, nil); err == nil || !picker.Open {
		t.Fatalf("invalid commit err=%v open=%v", err, picker.Open)
	}

	picker.Select(service, 0, nil)
	var applied kittheme.Definition
	service.saveErr = errors.New("read-only")
	if _, _, err := picker.Commit(service, func(definition kittheme.Definition) { applied = definition }); err == nil || !picker.Open || !reflect.DeepEqual(applied, original) {
		t.Fatalf("save failure err=%v open=%v", err, picker.Open)
	}
}

func TestThemePickerDiscoveryFailureDoesNotOpen(t *testing.T) {
	t.Parallel()

	service := &fakeThemeService{discoverErr: errors.New("permission denied")}
	var picker themePickerController
	if err := picker.OpenPicker(service, kittheme.SystemName, kittheme.Definition{}); err == nil || picker.Open {
		t.Fatalf("OpenPicker() err=%v picker=%+v", err, picker)
	}
}
