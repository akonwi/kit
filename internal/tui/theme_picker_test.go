package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
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
	mu              sync.Mutex
	names           []string
	definitions     map[string]kittheme.Definition
	diagnostics     map[string][]kittheme.Diagnostic
	loadErrors      map[string]error
	discoverErr     error
	saveErr         error
	saved           []string
	discoverStarted chan struct{}
	discoverRelease <-chan struct{}
	loadStarted     chan string
	loadRelease     map[string]<-chan struct{}
	saveStarted     chan struct{}
	saveRelease     <-chan struct{}
}

func (s *fakeThemeService) Discover() ([]string, error) {
	if s.discoverStarted != nil {
		select {
		case s.discoverStarted <- struct{}{}:
		default:
		}
	}
	if s.discoverRelease != nil {
		<-s.discoverRelease
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.names...), s.discoverErr
}
func (s *fakeThemeService) Load(name string) (kittheme.Definition, []kittheme.Diagnostic, error) {
	if s.loadStarted != nil {
		s.loadStarted <- name
	}
	if release := s.loadRelease[name]; release != nil {
		<-release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.definitions[name], append([]kittheme.Diagnostic(nil), s.diagnostics[name]...), s.loadErrors[name]
}
func (s *fakeThemeService) Save(name string) error {
	if s.saveStarted != nil {
		s.saveStarted <- struct{}{}
	}
	if s.saveRelease != nil {
		<-s.saveRelease
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, name)
	return nil
}

func (s *fakeThemeService) savedNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.saved...)
}

type themePickerAppHarness struct {
	state   *themePickerAppState
	service ThemeService
}

func (w themePickerAppHarness) CreateState() ui.State      { return w.state }
func (w themePickerAppHarness) themeService() ThemeService { return w.service }

type themePickerAppState struct {
	appState
	scroll  ui.ScrollController
	applied []kittheme.Definition
}

func (*themePickerAppState) InitState() {}
func (*themePickerAppState) Dispose()   {}
func (s *themePickerAppState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *themePickerAppState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Session: protocol.SessionInfo{ID: "theme", Name: "Theme", Model: "test/model"},
			Scroll: &s.scroll, ThemePicker: s.themePicker.Snapshot(),
		},
		Callbacks: shellCallbacks{
			InputOwner:  s.inputOwner,
			SelectTheme: func(_ ui.EventContext, name string) { s.activateTheme(name) },
			Dismiss:     func(ui.EventContext) { s.cancelThemePicker() },
		},
	}
}

type themePickerBackend struct {
	events     chan ui.Event
	dispatches chan func()
	painter    *ui.Painter
}

func (b *themePickerBackend) Events() <-chan ui.Event { return b.events }
func (*themePickerBackend) Size() ui.Size             { return ui.Size{Width: 80, Height: 24} }
func (b *themePickerBackend) Render(painter *ui.Painter) error {
	b.painter = painter
	return nil
}
func (b *themePickerBackend) Dispatch(callback func())  { b.dispatches <- callback }
func (*themePickerBackend) SetMouseShape(ui.MouseShape) {}
func (*themePickerBackend) Close() error                { return nil }

type themePickerTestApp struct {
	runner  *ui.Runner
	backend *themePickerBackend
	now     time.Time
}

func (a *themePickerTestApp) pump(t *testing.T) {
	t.Helper()
	a.now = a.now.Add(time.Second / 30)
	if err := a.runner.HandleFrame(a.now); err != nil {
		t.Fatal(err)
	}
}
func (a *themePickerTestApp) send(event ui.Event) { a.runner.HandleEvent(event, a.now) }
func (a *themePickerTestApp) key(text string) {
	for _, character := range text {
		a.send(vaxis.Key{Keycode: character, Text: string(character), EventType: vaxis.EventPress})
	}
}
func (a *themePickerTestApp) click(column, row int) {
	a.send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
}
func (a *themePickerTestApp) rows() []string { return toastPainterRows(a.backend.painter) }

func mountThemePickerApp(t *testing.T, service ThemeService, picker themePickerController, name string, definition kittheme.Definition) (*themePickerTestApp, *themePickerAppState) {
	t.Helper()
	state := &themePickerAppState{appState: appState{phase: phaseReady, themePicker: picker, themeName: name, themeDefinition: definition}}
	state.applyTheme = func(applied kittheme.Definition) { state.applied = append(state.applied, applied) }
	backend := &themePickerBackend{events: make(chan ui.Event), dispatches: make(chan func(), 8)}
	application := ui.NewApp(themePickerAppHarness{state: state, service: service})
	runner := ui.NewRunner(application, backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	testApp := &themePickerTestApp{runner: runner, backend: backend, now: now}
	testApp.pump(t)
	return testApp, state
}

func dispatchThemePicker(t *testing.T, application *themePickerTestApp) {
	t.Helper()
	select {
	case callback := <-application.backend.dispatches:
		callback()
		application.pump(t)
	case <-time.After(2 * time.Second):
		t.Fatal("theme picker dispatch was not received")
	}
}

func pumpThemePickerUntil(t *testing.T, application *themePickerTestApp, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for !condition() {
		select {
		case callback := <-application.backend.dispatches:
			callback()
			application.pump(t)
		case <-deadline.C:
			t.Fatal("theme picker condition was not reached")
		}
	}
	application.pump(t)
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

func TestThemePickerAppKeysPreviewCancelAndCommit(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	custom := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{definitions: map[string]kittheme.Definition{"custom": custom}}
	var picker themePickerController
	picker.OpenNames([]string{"custom"}, kittheme.SystemName, original)
	application, state := mountThemePickerApp(t, service, picker, kittheme.SystemName, original)

	application.send(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress})
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.PreviewLoading && len(state.applied) == 1 })
	if state.themePicker.Selection != "custom" || !reflect.DeepEqual(state.applied, []kittheme.Definition{custom}) {
		t.Fatalf("move preview picker=%+v applied=%#v", state.themePicker, state.applied)
	}
	application.send(vaxis.Key{Keycode: vaxis.KeyEsc, Text: "\x1b", EventType: vaxis.EventPress})
	application.pump(t)
	if state.themePicker.Open || !reflect.DeepEqual(state.applied, []kittheme.Definition{custom, original}) {
		t.Fatalf("cancel picker=%+v applied=%#v", state.themePicker, state.applied)
	}

	state.SetState(func() { state.themePicker.OpenNames([]string{"custom"}, kittheme.SystemName, original) })
	application.pump(t)
	application.send(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress})
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.PreviewLoading })
	application.send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.Open })
	if !reflect.DeepEqual(service.savedNames(), []string{"custom"}) || state.themeName != "custom" || !reflect.DeepEqual(state.themeDefinition, custom) {
		t.Fatalf("commit picker=%+v saved=%v name=%q definition=%+v", state.themePicker, service.savedNames(), state.themeName, state.themeDefinition)
	}
}

func TestThemePickerAppClickPreviewsAndCommitsTheme(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	nord := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{definitions: map[string]kittheme.Definition{"nord": nord}}
	var picker themePickerController
	picker.OpenNames([]string{"nord"}, kittheme.SystemName, original)
	application, state := mountThemePickerApp(t, service, picker, kittheme.SystemName, original)
	application.pump(t)
	column, row := findTextCell(t, application.rows(), "nord")
	application.click(column+20, row)
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.Open })
	if !reflect.DeepEqual(state.applied, []kittheme.Definition{nord}) || !reflect.DeepEqual(service.savedNames(), []string{"nord"}) {
		t.Fatalf("click picker=%+v applied=%+v saved=%v", state.themePicker, state.applied, service.savedNames())
	}
}

func TestThemePickerAppClickIntentFollowsSelectionAcrossStalePreview(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	alpha := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	beta := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 3, A: 255}}}
	alphaRelease := make(chan struct{})
	betaRelease := make(chan struct{})
	started := make(chan string, 2)
	service := &fakeThemeService{
		definitions: map[string]kittheme.Definition{"alpha": alpha, "beta": beta}, loadStarted: started,
		loadRelease: map[string]<-chan struct{}{"alpha": alphaRelease, "beta": betaRelease},
	}
	var picker themePickerController
	picker.OpenNames([]string{"alpha", "beta"}, kittheme.SystemName, original)
	application, state := mountThemePickerApp(t, service, picker, kittheme.SystemName, original)
	application.send(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress})
	if name := <-started; name != "alpha" {
		t.Fatalf("first preview = %q, want alpha", name)
	}
	application.pump(t)
	column, row := findTextCell(t, application.rows(), "beta")
	application.click(column+20, row)
	if state.themePicker.Selection != "beta" || state.themePicker.commitOnPreview != "beta" {
		t.Fatalf("clicked selection picker=%+v", state.themePicker)
	}
	close(alphaRelease)
	dispatchThemePicker(t, application)
	if name := <-started; name != "beta" {
		t.Fatalf("replacement preview = %q, want beta", name)
	}
	if len(state.applied) != 0 || len(service.savedNames()) != 0 {
		t.Fatalf("stale alpha was applied=%v saved=%v", state.applied, service.savedNames())
	}
	close(betaRelease)
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.Open })
	if !reflect.DeepEqual(state.applied, []kittheme.Definition{beta}) || !reflect.DeepEqual(service.savedNames(), []string{"beta"}) {
		t.Fatalf("replacement result applied=%v saved=%v", state.applied, service.savedNames())
	}
}

func TestThemePickerAppPendingSaveIgnoresPointerActivation(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	nord := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	service := &fakeThemeService{
		definitions: map[string]kittheme.Definition{"nord": nord}, saveErr: errors.New("read-only"),
		saveStarted: started, saveRelease: release,
	}
	var picker themePickerController
	picker.OpenNames([]string{"dracula", "nord"}, kittheme.SystemName, original)
	application, state := mountThemePickerApp(t, service, picker, kittheme.SystemName, original)
	state.selectTheme("nord")
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.PreviewLoading })
	application.send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("theme save did not start")
	}
	application.pump(t)
	column, row := findTextCell(t, application.rows(), "dracula")
	application.click(column+20, row)
	if state.themePicker.Selection != "nord" || state.themePicker.commitOnPreview != "" {
		t.Fatalf("pending click mutated picker=%+v", state.themePicker)
	}
	close(release)
	pumpThemePickerUntil(t, application, func() bool { return state.themePicker.Err != nil })
	if state.themePicker.Selection != "nord" {
		t.Fatalf("failed save selection = %q, want nord", state.themePicker.Selection)
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
	if picker.requestCommit(kittheme.SystemName) || picker.Selection != "nord" || picker.commitOnPreview != "" {
		t.Fatalf("pending click mutated picker=%+v", picker)
	}
}

func TestThemePickerAppInvalidPreviewBlocksCommitAndShowsDiagnostics(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	diagnostics := []kittheme.Diagnostic{{Section: "tokens"}}
	service := &fakeThemeService{
		definitions: map[string]kittheme.Definition{}, diagnostics: map[string][]kittheme.Diagnostic{"broken": diagnostics},
		loadErrors: map[string]error{"broken": errors.New("bad file")},
	}
	var picker themePickerController
	picker.OpenNames([]string{"broken"}, kittheme.SystemName, original)
	application, state := mountThemePickerApp(t, service, picker, kittheme.SystemName, original)
	application.send(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress})
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.PreviewLoading && state.themePicker.Err != nil })
	application.send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	application.pump(t)
	if !state.themePicker.Open || state.themePicker.previewValid || !reflect.DeepEqual(state.themePicker.Diagnostics, diagnostics) || len(service.savedNames()) != 0 || len(state.applied) != 0 {
		t.Fatalf("invalid preview picker=%+v applied=%v saved=%v", state.themePicker, state.applied, service.savedNames())
	}
}

func TestThemePickerAppSystemPreviewAndSaveFailureRollback(t *testing.T) {
	t.Parallel()

	custom := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	service := &fakeThemeService{definitions: map[string]kittheme.Definition{"custom": custom}, saveErr: errors.New("read-only")}
	var picker themePickerController
	picker.OpenNames([]string{"custom"}, "custom", custom)
	application, state := mountThemePickerApp(t, service, picker, "custom", custom)
	application.send(vaxis.Key{Keycode: vaxis.KeyUp, EventType: vaxis.EventPress})
	application.pump(t)
	if len(state.applied) != 1 || !reflect.DeepEqual(state.applied[0], kittheme.Definition{}) {
		t.Fatalf("system preview applications = %#v", state.applied)
	}
	application.send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	pumpThemePickerUntil(t, application, func() bool { return state.themePicker.Err != nil })
	if !state.themePicker.Open || len(state.applied) != 2 || !reflect.DeepEqual(state.applied[1], custom) || state.themeName != "custom" {
		t.Fatalf("save rollback picker=%+v applied=%#v name=%q", state.themePicker, state.applied, state.themeName)
	}
}

func TestThemePickerAppKeepsQueryTypedWhileCatalogLoads(t *testing.T) {
	t.Parallel()

	original := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 1, A: 255}}}
	nord := kittheme.Definition{Tokens: map[string]kittheme.Color{kittheme.TokenBackground: {R: 2, A: 255}}}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	service := &fakeThemeService{
		names: []string{"dracula", "nord"}, definitions: map[string]kittheme.Definition{"nord": nord},
		discoverStarted: started, discoverRelease: release,
	}
	application, state := mountThemePickerApp(t, service, themePickerController{}, kittheme.SystemName, original)
	state.openThemePicker()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("theme discovery did not start")
	}
	application.key("nord")
	application.pump(t)
	if state.themePicker.Query != "nord" || !state.themePicker.Loading {
		t.Fatalf("loading query picker=%+v", state.themePicker)
	}
	close(release)
	pumpThemePickerUntil(t, application, func() bool {
		return !state.themePicker.Loading && !state.themePicker.PreviewLoading && len(state.applied) == 1
	})
	rows := application.rows()
	_, searchRow := assertPickerSearchField(t, rows, "nord")
	if got := dialogRowText(rows, searchRow+2); got != glyphLeftBar+"nord" {
		t.Fatalf("filtered theme row = %q, want %q:\n%s", got, glyphLeftBar+"nord", strings.Join(rows, "\n"))
	}
	if state.themePicker.Selection != "nord" || !reflect.DeepEqual(state.applied, []kittheme.Definition{nord}) {
		t.Fatalf("loaded query picker=%+v applied=%#v", state.themePicker, state.applied)
	}
}

func TestThemePickerAppDiscoveryFailureStaysOpen(t *testing.T) {
	t.Parallel()

	service := &fakeThemeService{discoverErr: errors.New("permission denied")}
	application, state := mountThemePickerApp(t, service, themePickerController{}, kittheme.SystemName, kittheme.Definition{})
	state.openThemePicker()
	pumpThemePickerUntil(t, application, func() bool { return !state.themePicker.Loading && state.themePicker.Err != nil })
	if !state.themePicker.Open || state.themePicker.Err.Error() != "discover themes: permission denied" {
		t.Fatalf("discovery failure picker=%+v", state.themePicker)
	}
}
