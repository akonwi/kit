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
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestPickerRowsPaintHoverAcrossContentAndPadding(t *testing.T) {
	theme := ui.DefaultThemeSet().Dark
	tests := []struct {
		name    string
		widget  ui.Widget
		targets []string
		samples []string
	}{
		{
			name: "model",
			widget: configurationOptionRow{
				Label: "Picker Model", Details: "provider/picker-model", Meta: "128k context", OnPressed: func(ui.EventContext) {},
			},
			targets: []string{"Picker Model", "provider/picker-model", "128k context"},
			samples: []string{"Picker Model", "provider/picker-model", "128k context"},
		},
		{
			name: "command",
			widget: paletteOptionRow{
				Command:   paletteCommand{ID: "picker-test", Name: "Picker command", ArgumentHint: "[target]", Description: "Run the selected picker command"},
				NameWidth: 28, OnPressed: func(ui.EventContext) {},
			},
			targets: []string{"Picker command", "[target]", "Run the selected picker command"},
			samples: []string{"Picker command", "[target]", "Run the selected picker command"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const width = 72
			application := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: test.widget})
			application.Pump(width, 3)
			rows := paintedRows(application, width, 3)
			row := findPaintedRow(rows, test.targets[0])
			if row < 0 {
				t.Fatalf("picker row was not painted:\n%s", strings.Join(rows, "\n"))
			}
			targetColumns := make([]int, 0, len(test.targets)+1)
			for _, text := range test.targets {
				column, targetRow := findTextCell(t, rows, text)
				if targetRow != row {
					t.Fatalf("%q row = %d, want %d", text, targetRow, row)
				}
				targetColumns = append(targetColumns, column)
			}
			// The final cell is ListTile padding and remains part of the row hit surface.
			targetColumns = append(targetColumns, width-1)
			for _, target := range targetColumns {
				application.Send(vaxis.Mouse{Col: target, Row: row, EventType: vaxis.EventMotion})
				application.Pump(width, 3)
				hoveredRows := paintedRows(application, width, 3)
				for _, text := range test.samples {
					column, sampleRow := findTextCell(t, hoveredRows, text)
					if got := application.Cell(column, sampleRow).Style.Background; got != theme.SurfaceHovered {
						t.Fatalf("hover at column %d left %q background %v (target %v, padding %v), want %v", target, text, got, application.Cell(target, row).Style.Background, application.Cell(width-1, row).Style.Background, theme.SurfaceHovered)
					}
				}
				if got := application.Cell(width-1, row).Style.Background; got != theme.SurfaceHovered {
					t.Fatalf("hover at column %d left padding background %v, want %v", target, got, theme.SurfaceHovered)
				}
			}
		})
	}
}

func TestPickerRowsUseFocusedSelectionColorsAcrossContentAndPadding(t *testing.T) {
	theme := ui.DefaultThemeSet().Light
	semantic := semanticFallback(theme)
	wantBackground := semantic.Token(kittheme.TokenPickerFocusedBackground)
	wantForeground := semantic.Token(kittheme.TokenPickerFocusedText)
	tests := []struct {
		name   string
		widget ui.Widget
		texts  []string
	}{
		{
			name: "model",
			widget: configurationOptionRow{
				Label: "Selected Model", Details: "provider/selected", Meta: "256k context", Selected: true,
			},
			texts: []string{"Selected Model", "provider/selected", "256k context"},
		},
		{
			name: "command",
			widget: paletteOptionRow{
				Command:   paletteCommand{ID: "selected-test", Name: "Selected command", ArgumentHint: "[target]", Description: "Selected command description"},
				NameWidth: 28, Selected: true,
			},
			texts: []string{"Selected command", "[target]", "Selected command description"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const width = 72
			application := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: test.widget})
			application.Pump(width, 3)
			rows := paintedRows(application, width, 3)
			row := findPaintedRow(rows, test.texts[0])
			for _, text := range test.texts {
				column, textRow := findTextCell(t, rows, text)
				cell := application.Cell(column, textRow)
				if cell.Style.Background != wantBackground || cell.Style.Foreground != wantForeground {
					t.Fatalf("selected %q style = %+v, want foreground %v on background %v", text, cell.Style, wantForeground, wantBackground)
				}
			}
			if got := application.Cell(width-1, row).Style.Background; got != wantBackground {
				t.Fatalf("selected padding background = %v, want %v", got, wantBackground)
			}
		})
	}
}

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

func TestModelPickerShowsInputMarkerScrollbarAndRevealsKeyboardSelection(t *testing.T) {
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
	searchColumn, markerRow := findTextCell(t, initial, "Search models…")
	markerCells := []rune(initial[markerRow])
	if searchColumn < 2 || markerCells[searchColumn-2] != '>' || markerCells[searchColumn-1] != ' ' {
		t.Fatalf("model search marker geometry does not match the command palette:\n%s", strings.Join(initial, "\n"))
	}
	firstRow := findPaintedRow(initial, "Model 00")
	footerRow := findPaintedRow(initial, "↑↓ move · enter apply · ctrl+o overrides · esc close")
	if firstRow < 0 || footerRow < 0 || findPaintedRow(initial, "Model 29") >= 0 {
		t.Fatalf("model picker did not start with a clipped catalog:\n%s", strings.Join(initial, "\n"))
	}
	_, dialogRight, _ := paletteBorder(initial)
	scrollbarColumn := dialogRight - 2 // one body-padding cell remains before the dialog border
	thumbFound, trackFound := false, false
	var thumbStyle, trackStyle ui.Style
	for row := firstRow; row < footerRow; row++ {
		cell := backend.painter.Cell(scrollbarColumn, row)
		if strings.ContainsAny(cell.Character.Grapheme, "▁▂▃▄▅▆▇█") {
			if thumbFound && cell.Style != thumbStyle {
				t.Fatalf("scrollbar thumb style changed within column %d: first=%+v row %d=%+v", scrollbarColumn, thumbStyle, row, cell.Style)
			}
			thumbFound, thumbStyle = true, cell.Style
		} else {
			trackFound, trackStyle = true, cell.Style
		}
	}
	if !thumbFound {
		t.Fatalf("model picker did not paint a scrollbar thumb at column %d beside its clipped catalog:\n%s", scrollbarColumn, strings.Join(initial, "\n"))
	}
	if !trackFound || thumbStyle == trackStyle || thumbStyle.Foreground == 0 {
		t.Fatalf("scrollbar thumb style %+v is not distinct from track style %+v", thumbStyle, trackStyle)
	}

	for range len(models) - 1 {
		runner.HandleEvent(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress}, now)
		pump()
	}
	for range 6 {
		pump()
	}
	rows := toastPainterRows(backend.painter)
	lastRow := findPaintedRow(rows, "Model 29")
	if lastRow < firstRow || lastRow >= footerRow {
		t.Fatalf("keyboard selection %q was not revealed inside the fixed picker body:\n%s", state.configurationPicker.Selection, strings.Join(rows, "\n"))
	}
	if findPaintedRow(rows[firstRow:footerRow], "test/model-00") >= 0 {
		t.Fatalf("selection changed without scrolling the model viewport:\n%s", strings.Join(rows, "\n"))
	}
	column, row := findTextCell(t, rows, "Model 29")
	want := semanticFallback(ui.DefaultTheme()).Token(kittheme.TokenPickerFocusedBackground)
	if got := backend.painter.Cell(column, row).Style.Background; got != want {
		t.Fatalf("revealed selection background = %v, want %v", got, want)
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
