package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type themePickerTestHarness struct {
	Theme   ui.Theme
	Surface themePickerSurface
}

func (w themePickerTestHarness) Build(ui.BuildContext) ui.Widget {
	return ui.Provider[ui.Theme]{Value: w.Theme, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: w.Theme.Foreground, Background: w.Theme.Background}}, w.Surface,
	)}
}

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

func TestThemePickerSurfaceUsesCanonicalPickerPresentation(t *testing.T) {
	t.Parallel()

	pickerTheme := ui.DefaultThemeSet().Dark
	application := uitest.New(themePickerTestHarness{Theme: pickerTheme, Surface: themePickerSurface{Snapshot: themePickerSnapshot{
		Open: true, Names: []string{kittheme.SystemName, "nord"}, Selection: "nord", CommittedName: kittheme.SystemName,
	}}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	searchColumn, searchRow := assertPickerSearchField(t, rows, "Search themes…")
	assertPickerTitleSpacing(t, rows, "Theme", searchRow)
	assertDialogRow(t, rows, "Terminal colors", "│ system  Terminal colors                                      │")
	assertDialogRow(t, rows, "nord", "│▌nord                                                         │")
	assertPickerFooter(t, rows, "↑↓ preview · enter use · esc cancel")

	systemColumn, systemRow := findTextCell(t, rows, kittheme.SystemName)
	if systemColumn != searchColumn {
		t.Fatalf("system column = %d, want search column %d:\n%s", systemColumn, searchColumn, strings.Join(rows, "\n"))
	}
	if got, want := application.Cell(systemColumn, systemRow).Style.Foreground, pickerTheme.PrimaryText; got != want {
		t.Fatalf("current theme label = %v, want accent %v", got, want)
	}
	nordColumn, nordRow := findTextCell(t, rows, "nord")
	if got := application.Cell(nordColumn-1, nordRow).Grapheme; got != glyphLeftBar {
		t.Fatalf("highlight gutter = %q, want %q", got, glyphLeftBar)
	}
	if got, want := application.Cell(nordColumn, nordRow).Style.Background, blendPickerColor(pickerTheme.Selection, pickerTheme.Background, pickerHighlightPercent, pickerTheme.SurfaceHovered); got != want {
		t.Fatalf("selected theme background = %v, want tinted %v", got, want)
	}
}

func TestThemePickerSurfaceRevealsKeyboardSelection(t *testing.T) {
	t.Parallel()

	names := make([]string, 15)
	for index := range names {
		names[index] = fmt.Sprintf("theme-%02d", index)
	}
	application := uitest.New(themePickerSurface{Snapshot: themePickerSnapshot{
		Open: true, Names: names, Selection: "theme-14", PreviewLoading: true,
	}})
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	assertDialogRow(t, rows, "theme-14", "│▌theme-14                                                     │")
	assertDialogRow(t, rows, "Loading preview…", "│ Loading preview…                                  esc cancel │")
	if got := dialogRowText(rows, findPaintedRow(rows, glyphEllipsis)); got != glyphEllipsis {
		t.Fatalf("overflow row = %q, want %q", got, glyphEllipsis)
	}
}

func TestThemePickerSurfacePlacesStateInMessageAndStatusSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot themePickerSnapshot
		text     string
		row      string
	}{
		{name: "catalog loading", snapshot: themePickerSnapshot{Open: true, Loading: true}, text: "Loading…", row: "│ Loading…                                                     │"},
		{name: "saving", snapshot: themePickerSnapshot{Open: true, Pending: true, Names: []string{kittheme.SystemName}, Selection: kittheme.SystemName}, text: "Saving…", row: "│ Saving… · ctrl+c force quit                                  │"},
		{name: "diagnostics", snapshot: themePickerSnapshot{Open: true, Names: []string{kittheme.SystemName}, Selection: kittheme.SystemName, Diagnostics: []kittheme.Diagnostic{{Section: "tokens"}}}, text: "1 theme value(s) were ignored", row: "│ 1 theme value(s) were ignored                     esc cancel │"},
		{name: "preview error", snapshot: themePickerSnapshot{Open: true, Names: []string{kittheme.SystemName}, Selection: kittheme.SystemName, Error: "could not load theme"}, text: "could not load theme", row: "│ could not load theme                              esc cancel │"},
		{name: "catalog error", snapshot: themePickerSnapshot{Open: true, Error: "discover themes: denied"}, text: "discover themes: denied", row: "│ discover themes: denied                                      │"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(themePickerSurface{Snapshot: test.snapshot})
			application.Pump(80, 24)
			rows := paintedRows(application, 80, 24)
			if test.name == "catalog loading" {
				row := findPaintedRow(rows, test.text)
				if got := strings.TrimLeft(dialogRowText(rows, row), "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ "); got != test.text {
					t.Fatalf("loading message = %q, want %q", got, test.text)
				}
				assertPickerFooter(t, rows, "Loading… · esc cancel")
				return
			}
			assertDialogRow(t, rows, test.text, test.row)
		})
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

func TestThemePickerKeysFilterPreviewCancelAndCommit(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	custom := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{names: []string{"custom", "nord"}, definitions: map[string]kittheme.Definition{"custom": custom}}
	var applied []kittheme.Definition
	apply := func(definition kittheme.Definition) { applied = append(applied, definition) }

	var picker themePickerController
	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	result, preview := picker.HandleKey(ui.Key{Keycode: 'c', Text: "c", EventType: vaxis.EventPress})
	if !result.Handled || !result.QueryChanged || !preview || picker.Query != "c" || picker.Selection != "custom" {
		t.Fatalf("typing result=%+v preview=%t picker=%+v", result, preview, picker)
	}
	picker.preview(service, apply)
	if !reflect.DeepEqual(applied, []kittheme.Definition{custom}) {
		t.Fatalf("filtered preview applications = %#v", applied)
	}
	result, _ = picker.HandleKey(ui.Key{Keycode: vaxis.KeyEsc, EventType: vaxis.EventPress})
	if !result.Dismiss {
		t.Fatalf("escape result = %+v, want dismiss", result)
	}
	picker.Cancel(apply)
	if picker.Open || !reflect.DeepEqual(applied, []kittheme.Definition{custom, original}) {
		t.Fatalf("cancel state = %+v, applications = %#v", picker, applied)
	}

	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	picker.Move(service, 1, apply)
	result, _ = picker.HandleKey(ui.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	if !result.Activate {
		t.Fatalf("enter result = %+v, want activate", result)
	}
	name, definition, err := picker.Commit(service, apply)
	if err != nil || name != "custom" || !reflect.DeepEqual(definition, custom) || picker.Open || !reflect.DeepEqual(service.saved, []string{"custom"}) {
		t.Fatalf("commit name=%q err=%v picker=%+v saved=%v", name, err, picker, service.saved)
	}
}

func TestThemePickerClickPreviewsAndCommitsTheme(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	nord := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{names: []string{"nord"}, definitions: map[string]kittheme.Definition{"nord": nord}}
	var picker themePickerController
	if err := picker.OpenPicker(service, kittheme.SystemName, original); err != nil {
		t.Fatal(err)
	}
	var applied kittheme.Definition
	application := uitest.New(themePickerSurface{
		Snapshot: picker.Snapshot(),
		Callbacks: themePickerCallbacks{Select: func(_ ui.EventContext, name string) {
			picker.Select(service, name, func(definition kittheme.Definition) { applied = definition })
			_, _, _ = picker.Commit(service, nil)
		}},
	})
	application.Pump(80, 24)
	application.Pump(80, 24)
	column, row := findTextCell(t, paintedRows(application, 80, 24), "nord")
	application.Click(column+20, row)
	application.Pump(80, 24)
	if picker.Open || !reflect.DeepEqual(applied, nord) || !reflect.DeepEqual(service.saved, []string{"nord"}) {
		t.Fatalf("click result picker=%+v applied=%+v saved=%v", picker, applied, service.saved)
	}
}

func TestThemePickerPendingSaveConsumesKeysWithoutMutation(t *testing.T) {
	t.Parallel()

	picker := themePickerController{
		Open: true, Pending: true, Names: []string{kittheme.SystemName, "nord"},
		Query: "nor", Selection: "nord", Diagnostics: []kittheme.Diagnostic{{Section: "tokens"}},
	}
	result, preview := picker.HandleKey(ui.Key{Keycode: vaxis.KeyBackspace, EventType: vaxis.EventPress})
	if !result.Handled || preview || picker.Query != "nor" || picker.Selection != "nord" || len(picker.Diagnostics) != 1 {
		t.Fatalf("pending key result=%+v preview=%t picker=%+v", result, preview, picker)
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
	if !reflect.DeepEqual(picker.Names, []string{kittheme.SystemName, "custom"}) || picker.Selection != kittheme.SystemName {
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

	picker.Select(service, kittheme.SystemName, nil)
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
