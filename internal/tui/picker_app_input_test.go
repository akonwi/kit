package tui

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// pickerAppHarness renders every app-owned palette picker from appState and
// routes input through the application input owner.
type pickerAppHarness struct{ state *pickerAppHarnessState }

func (w pickerAppHarness) CreateState() ui.State { return w.state }

type pickerAppHarnessState struct {
	appState
	scroll ui.ScrollController
}

func (*pickerAppHarnessState) InitState() {}
func (*pickerAppHarnessState) Dispose()   {}
func (s *pickerAppHarnessState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *pickerAppHarnessState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: s.phase, Session: s.session, Scroll: &s.scroll, CurrentWorkspaceID: s.workspaceID,
			PaletteOpen: s.palette.Open, PaletteQuery: s.palette.Query, PaletteSelection: s.palette.Selection,
			ConfigurationPicker: s.configurationPicker.Snapshot(), ThemePicker: s.themePicker.Snapshot(),
			SessionExplorer: s.sessionExplorer.Snapshot(), WorkspaceFilePicker: s.workspaceFilePicker, IndexedFiles: s.indexedFiles,
			Workspace: s.workspace.Snapshot(), WorkspacePickerOpen: s.workspacePickerOpen,
			WorkspacePickerQuery: s.workspacePickerQuery, WorkspacePickerSelection: s.workspacePickerSelection,
			AuthReturnReady: s.authReturnReady, AuthQuery: s.authPicker.Query, AuthSelection: s.authPicker.Selection,
		},
		Callbacks: shellCallbacks{InputOwner: s.inputOwner, Dismiss: s.dismiss},
	}
}

// TestPalettePickersEditQueriesThroughTheAppInputPath opens each palette
// picker and, before it paints, types a query with a stray character removed
// by Backspace and a word removed by Ctrl+Backspace, then pastes the rest.
// Every edit reaches the picker's key model through the input owner, and the
// picker paints the query and the rows it filters.
func TestPalettePickersEditQueriesThroughTheAppInputPath(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		open  func(*pickerAppHarnessState)
		typed string
		paste string
		query func(*pickerAppHarnessState) string
		rows  []string
	}{
		{
			name:  "command palette",
			open:  func(s *pickerAppHarnessState) { s.palette.OpenFor(false) },
			typed: "th", paste: "eme",
			query: func(s *pickerAppHarnessState) string { return s.palette.Query },
			rows: []string{
				glyphLeftBar + "theme           Choose UI colors",
				"refresh-models  Update the provider catalog of models",
			},
		},
		{
			name: "model",
			open: func(s *pickerAppHarnessState) {
				generation := s.configurationPicker.Begin(configurationPickerModel, "openai/gpt", "")
				s.configurationPicker.Resolve(generation, protocol.ModelCatalog{Models: []protocol.ModelCapability{
					{ID: "anthropic/claude", Name: "Claude", Provider: "anthropic", Available: true, ContextWindow: 200_000},
					{ID: "openai/gpt", Name: "GPT", Provider: "openai", Available: true, ContextWindow: 128_000},
				}}, nil)
			},
			typed: "an", paste: "thropic",
			query: func(s *pickerAppHarnessState) string { return s.configurationPicker.Query },
			rows:  []string{glyphLeftBar + "Claude  anthropic/claude                        200k context"},
		},
		{
			name: "theme",
			open: func(s *pickerAppHarnessState) {
				s.themePicker = themePickerController{Open: true, Names: []string{kittheme.SystemName, "dracula", "nord"}, CommittedName: kittheme.SystemName, Selection: kittheme.SystemName}
			},
			// The theme owner does not accept pasted text, so the whole query is typed.
			typed: "nord",
			query: func(s *pickerAppHarnessState) string { return s.themePicker.Query },
			rows:  []string{glyphLeftBar + "nord"},
		},
		{
			name: "file",
			open: func(s *pickerAppHarnessState) {
				s.workspaceFilePicker = readyFilePickerController()
				s.indexedFiles = indexedPickerSource(
					protocol.FileIndexEntry{Path: "docs/", IsDir: true},
					protocol.FileIndexEntry{Path: "internal/tui/app.go"},
				)
				s.workspaceFilePicker.ensureSelection(s.indexedFiles)
			},
			typed: "tu", paste: "i/app",
			query: func(s *pickerAppHarnessState) string { return s.workspaceFilePicker.Query },
			rows:  []string{glyphLeftBar + "internal/tui/app.go"},
		},
		{
			name: "workspace tab",
			open: func(s *pickerAppHarnessState) {
				s.workspaceID = "workspace"
				if _, _, err := s.workspace.Open(fileWorkspacePane("workspace", "notes.md")); err != nil {
					t.Fatal(err)
				}
				s.workspacePickerOpen = true
			},
			typed: "no", paste: "tes",
			query: func(s *pickerAppHarnessState) string { return s.workspacePickerQuery },
			rows:  []string{glyphLeftBar + "notes.md                                                file"},
		},
		{
			name: "session",
			open: func(s *pickerAppHarnessState) {
				s.sessionExplorer.Resolve(s.sessionExplorer.Begin("session_other"), explorerFixture(), nil)
			},
			typed: "pro", paste: "vid",
			query: func(s *pickerAppHarnessState) string { return s.sessionExplorer.Query },
			rows: []string{
				// The collapsed match is shown under its ancestors, open; row text
				// is trimmed, so only the highlighted row keeps its indent.
				"Authentication       /repo/authentication         2026-06-01",
				"OAuth              /repo/oauth                  2026-06-02",
				glyphLeftBar + "    Providers        /repo/providers              2026-06-05",
			},
		},
		{
			name: "login provider",
			open: func(s *pickerAppHarnessState) {
				s.phase, s.authReturnReady, s.authPicker = phaseAuthSelect, true, newAuthProviderPicker()
			},
			typed: "brow", paste: "ser",
			query: func(s *pickerAppHarnessState) string { return s.authPicker.Query },
			rows:  []string{glyphLeftBar + "Claude        Pro or Max plan · browser"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &pickerAppHarnessState{appState: appState{phase: phaseReady, session: protocol.SessionInfo{ID: "session_other", Name: "Pickers", Model: "openai/gpt"}}}
			application := uitest.New(pickerAppHarness{state: state})
			application.Pump(80, 24)
			state.SetState(func() { test.open(state) })

			// Nothing below paints until every key has been applied.
			application.Key(test.typed + "x")
			application.Send(ui.Key{Keycode: vaxis.KeyBackspace})
			application.Key(" stray")
			application.Send(ui.Key{Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModCtrl})
			application.Send(ui.Key{Keycode: vaxis.KeyBackspace})
			if test.paste != "" {
				application.Send(vaxis.PasteStartEvent{})
				application.Send(ui.Key{Text: test.paste, Keycode: rune(test.paste[0]), EventType: vaxis.EventPaste})
				application.Send(vaxis.PasteEndEvent{})
			}
			want := test.typed + test.paste
			if got := test.query(state); got != want {
				t.Fatalf("query before paint = %q, want %q", got, want)
			}

			application.Pump(80, 24)
			application.Pump(80, 24)
			rows := paintedRows(application, 80, 24)
			searchRow := -1
			for row := range rows {
				if dialogRowText(rows, row) == want {
					searchRow = row
					break
				}
			}
			if searchRow < 0 {
				t.Fatalf("no search field showing %q:\n%s", want, strings.Join(rows, "\n"))
			}
			for index, expected := range test.rows {
				if got := dialogRowText(rows, searchRow+2+index); got != expected {
					t.Fatalf("row %d = %q, want %q:\n%s", index, got, expected, strings.Join(rows, "\n"))
				}
			}
		})
	}
}
