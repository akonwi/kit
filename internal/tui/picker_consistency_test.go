package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type modelPickerConsistencyHarness struct{ state *modelPickerConsistencyState }

func (w modelPickerConsistencyHarness) CreateState() ui.State { return w.state }

type modelPickerConsistencyState struct {
	appState
	scroll ui.ScrollController
}

func (*modelPickerConsistencyState) InitState() {}
func (*modelPickerConsistencyState) Dispose()   {}
func (s *modelPickerConsistencyState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}

func (s *modelPickerConsistencyState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{Snapshot: shellSnapshot{
		Phase: s.phase, Session: s.session, Scroll: &s.scroll, ConfigurationPicker: s.configurationPicker.Snapshot(),
	}, Callbacks: shellCallbacks{InputOwner: s.inputOwner, FocusWorkspaceComposer: func(ui.EventContext) {
		s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) })
	}}}
}

func TestModelPickerSharesSearchFieldOverflowRowsAndRevealsKeyboardSelection(t *testing.T) {
	models := make([]protocol.ModelCapability, 30)
	for index := range models {
		models[index] = protocol.ModelCapability{
			ID: fmt.Sprintf("test/model-%02d", index), Name: fmt.Sprintf("Model %02d", index),
			Provider: "test", Available: true, ContextWindow: 128_000,
		}
	}
	state := &modelPickerConsistencyState{appState: appState{
		phase: phaseReady, session: protocol.SessionInfo{ID: "picker", Name: "Picker", Model: models[0].ID},
		configurationPicker: configurationPickerController{
			Mode: configurationPickerModel, Models: models, CurrentModel: models[0].ID, Selection: models[0].ID,
		},
	}}
	const width, height = 80, 24
	pickerTheme := ui.DefaultThemeSet().Dark
	backend := &toastAnimationBackend{events: make(chan ui.Event), size: ui.Size{Width: width, Height: height}}
	application := ui.NewApp(ui.Provider[ui.Theme]{Value: pickerTheme, Child: modelPickerConsistencyHarness{state: state}})
	runner := ui.NewRunner(application, backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	pump := func() {
		t.Helper()
		application.RequestFrame()
		now = now.Add(time.Second / 30)
		if err := runner.HandleFrame(now); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		pump()
	}
	initial := toastPainterRows(backend.painter)
	searchColumn, searchRow := assertPickerSearchField(t, initial, "Search models…")
	assertPickerTitleSpacing(t, initial, "Select model", searchRow)
	if titleColumn, _ := findTextCell(t, initial, "Select model"); titleColumn != searchColumn {
		t.Fatalf("model search column = %d, want title column %d:\n%s", searchColumn, titleColumn, strings.Join(initial, "\n"))
	}
	firstRow := searchRow + findPaintedRow(initial[searchRow:], "Model 00")
	if firstRow != searchRow+2 {
		t.Fatalf("first model row = %d, want %d directly below the search divider:\n%s", firstRow, searchRow+2, strings.Join(initial, "\n"))
	}
	footerRow := findPaintedRow(initial, "↑↓ move · enter apply · ctrl+o overrides · esc close")
	if firstRow < 0 || footerRow < 0 || findPaintedRow(initial, "Model 29") >= 0 {
		t.Fatalf("model picker did not start with a clipped catalog:\n%s", strings.Join(initial, "\n"))
	}
	// No scrollbar: the last list row is a ⋯ marking the hidden models below.
	if got := dialogRowText(initial, footerRow-2); got != glyphEllipsis {
		t.Fatalf("last list row = %q, want %q overflow row:\n%s", initial[footerRow-2], glyphEllipsis, strings.Join(initial, "\n"))
	}

	for range len(models) - 1 {
		runner.HandleEvent(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress}, now)
		pump()
	}
	for range 6 {
		pump()
	}
	rows := toastPainterRows(backend.painter)
	// The last model fills the bottom row and a ⋯ now marks the models above.
	if lastRow := findPaintedRow(rows, "Model 29"); lastRow != footerRow-2 {
		t.Fatalf("keyboard selection %q at row %d, want last list row %d:\n%s", state.configurationPicker.Selection, lastRow, footerRow-2, strings.Join(rows, "\n"))
	}
	if got := dialogRowText(rows, firstRow); got != glyphEllipsis {
		t.Fatalf("first list row = %q, want %q overflow row:\n%s", rows[firstRow], glyphEllipsis, strings.Join(rows, "\n"))
	}
	column, row := findTextCell(t, rows, "Model 29")
	focus := semanticFallback(pickerTheme).Token(kittheme.TokenPickerFocusedBackground)
	bar := backend.painter.Cell(column-1, row)
	if bar.Character.Grapheme != glyphLeftBar || bar.Style.Foreground != focus {
		t.Fatalf("revealed selection gutter = %q %+v, want %q in %v", bar.Character.Grapheme, bar.Style, glyphLeftBar, focus)
	}
	if got, want := backend.painter.Cell(column, row).Style.Background, blendPickerColor(focus, pickerTheme.Background, pickerHighlightPercent, pickerTheme.SurfaceHovered); got != want {
		t.Fatalf("revealed selection fill = %v, want tinted %v", got, want)
	}

	// Resizing schedules another reveal against the new viewport without moving selection.
	backend.size = ui.Size{Width: width, Height: 18}
	for range 6 {
		pump()
	}
	resized := toastPainterRows(backend.painter)
	resizedFooter := findPaintedRow(resized, "↑↓ move · enter apply · ctrl+o overrides · esc close")
	if selectedRow := findPaintedRow(resized, "Model 29"); selectedRow < 0 || selectedRow >= resizedFooter {
		t.Fatalf("resized picker did not retain the keyboard selection in view:\n%s", strings.Join(resized, "\n"))
	}

	// Filtering resets selection and scrolls the sole matching result into view.
	for _, character := range "Model 05" {
		runner.HandleEvent(vaxis.Key{Keycode: character, Text: string(character), EventType: vaxis.EventPress}, now)
		pump()
	}
	for range 6 {
		pump()
	}
	filtered := toastPainterRows(backend.painter)
	if state.configurationPicker.Selection != models[5].ID || findPaintedRow(filtered, "test/model-05") < 0 || findPaintedRow(filtered, "test/model-29") >= 0 {
		t.Fatalf("filtered picker did not reset and reveal its selected result:\n%s", strings.Join(filtered, "\n"))
	}
}

// dialogRowText returns the trimmed text between the dialog borders on row.
func dialogRowText(rows []string, row int) string {
	left, right, _ := paletteBorder(rows)
	cells := []rune(rows[row])
	if right >= len(cells) {
		return ""
	}
	return strings.TrimSpace(string(cells[left+1 : right]))
}
