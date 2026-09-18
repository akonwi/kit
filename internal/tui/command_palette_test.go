package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestPaletteControllerRoutesComposerInputAndPreservesSelectionIdentity(t *testing.T) {
	t.Parallel()

	var palette paletteController
	if _, intercepted := palette.HandleComposerChange("", "/", false); !intercepted || !palette.Open || palette.Query != "" {
		t.Fatalf("slash transition = %+v", palette)
	}
	if !palette.HandleEditorKey(false, ui.Key{Text: "q", Keycode: 'q'}) || palette.Query != "q" {
		t.Fatalf("coalesced slash input = %+v", palette)
	}
	command, ok := palette.Selected(false, palette.Query)
	if !ok || command.ID != paletteCommandQuit {
		t.Fatalf("selected coalesced command = %#v, %v", command, ok)
	}

	palette.Close()
	palette.OpenFor(false)
	if !palette.HandleEditorKey(false, ui.Key{Text: "q", Keycode: 'q'}) || palette.Query != "q" {
		t.Fatalf("coalesced Ctrl+P input = %+v", palette)
	}

	palette.Close()
	palette.OpenFor(true)
	palette.Move(true, 1)
	if palette.Selection != paletteCommandDiff {
		t.Fatalf("running selection = %q, want diff row", palette.Selection)
	}
	command, ok = palette.Selected(false, "")
	if !ok || command.ID != paletteCommandDiff {
		t.Fatalf("stable diff selection = %#v, %v", command, ok)
	}

	palette.Close()
	palette.OpenFor(true)
	if command, ok := palette.Selected(false, ""); !ok || command.ID != paletteCommandDebug {
		t.Fatalf("initial enabled selection = %#v, %v", command, ok)
	}
}

func TestCommandPaletteModelFiltersAliasesArgumentsAndWindows(t *testing.T) {
	t.Parallel()

	commands := filteredPaletteCommands(false, "directory")
	if len(commands) != 1 || commands[0].ID != paletteCommandCD {
		t.Fatalf("directory matches = %#v, want cd", commands)
	}
	commands = filteredPaletteCommands(false, "provider")
	if len(commands) != 1 || commands[0].ID != paletteCommandLogin {
		t.Fatalf("provider matches = %#v, want login", commands)
	}
	commands = filteredPaletteCommands(false, "threads")
	if len(commands) == 0 || commands[0].ID != paletteCommandSessions {
		t.Fatalf("threads matches = %#v, want sessions first", commands)
	}
	commands = filteredPaletteCommands(false, "refresh")
	if len(commands) != 1 || commands[0].ID != paletteCommandReload {
		t.Fatalf("refresh matches = %#v, want reload", commands)
	}
	commands = filteredPaletteCommands(true, "debug")
	if len(commands) != 1 || commands[0].ID != paletteCommandDebug {
		t.Fatalf("debug matches = %#v, want debug", commands)
	}
	commands = filteredPaletteCommands(false, "shrink")
	if len(commands) != 1 || commands[0].ID != paletteCommandCompact {
		t.Fatalf("shrink matches = %#v, want compact", commands)
	}
	commands = filteredPaletteCommands(false, "engine")
	if len(commands) != 1 || commands[0].ID != paletteCommandModel {
		t.Fatalf("engine matches = %#v, want model", commands)
	}
	commands = filteredPaletteCommands(false, "rename")
	if len(commands) != 1 || commands[0].ID != paletteCommandName {
		t.Fatalf("rename matches = %#v, want name", commands)
	}
	commands = filteredPaletteCommands(false, "new")
	if len(commands) == 0 || commands[0].ID != paletteCommandNew {
		t.Fatalf("new matches = %#v, want new first", commands)
	}
	commands = filteredPaletteCommands(false, "panes")
	if len(commands) != 1 || commands[0].ID != paletteCommandTabs {
		t.Fatalf("panes matches = %#v, want tabs", commands)
	}
	commands = filteredPaletteCommands(false, "files")
	if len(commands) == 0 || commands[0].ID != paletteCommandFiles {
		t.Fatalf("files matches = %#v, want files first", commands)
	}
	commands = filteredPaletteCommands(false, "working tree")
	if len(commands) == 0 || commands[0].ID != paletteCommandDiff {
		t.Fatalf("working tree matches = %#v, want diff first", commands)
	}
	commands = filteredPaletteCommands(false, "appearance")
	if len(commands) != 1 || commands[0].ID != paletteCommandTheme {
		t.Fatalf("appearance matches = %#v, want theme", commands)
	}
	commands = filteredPaletteCommands(false, "effort")
	if len(commands) != 1 || commands[0].ID != paletteCommandThinking {
		t.Fatalf("effort matches = %#v, want thinking", commands)
	}
	if !paletteCommandAvailable(paletteCommandLogin, true) {
		t.Fatal("login was unavailable during an active run")
	}
	if !paletteCommandAvailable(paletteCommandSessions, true) {
		t.Fatal("sessions was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandTabs, true) {
		t.Fatal("tabs was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandFiles, true) {
		t.Fatal("files was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandDiff, true) {
		t.Fatal("diff was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandNew, true) {
		t.Fatal("new was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandDebug, true) {
		t.Fatal("debug command was unavailable during active work")
	}
	if !paletteCommandAvailable(paletteCommandName, true) {
		t.Fatal("name command was unavailable during active work")
	}
	for _, command := range []paletteCommandID{paletteCommandModel, paletteCommandReload, paletteCommandThinking} {
		if !paletteCommandAvailable(command, true) {
			t.Fatalf("live command %q was unavailable during active work", command)
		}
	}
	for _, command := range []paletteCommandID{paletteCommandCD, paletteCommandCompact} {
		if paletteCommandAvailable(command, true) || !paletteCommandAvailable(command, false) {
			t.Fatalf("configuration command %q availability does not follow idle state", command)
		}
	}
	toast, disabled := paletteCommandDisabledToast(paletteCommandCD, true)
	if !disabled || toast.Title != "Command unavailable" || toast.Subtitle != "Available when the session is idle." || toast.Variant != toastWarning {
		t.Fatalf("disabled cd toast = %+v, %v", toast, disabled)
	}
	if _, disabled := paletteCommandDisabledToast(paletteCommandDebug, true); disabled {
		t.Fatal("debug produced disabled-command feedback while available")
	}
	compactToast, disabled := paletteCommandDisabledToast(paletteCommandCompact, true)
	if !disabled || compactToast.Title != "Compaction failed" || compactToast.Subtitle != "Cannot compact while the agent is running." || compactToast.Variant != toastError {
		t.Fatalf("disabled compact toast = %+v, disabled=%v", compactToast, disabled)
	}

	var pasted paletteController
	pasted.OpenFor(false)
	for _, key := range []ui.Key{
		{Text: "q", Keycode: 'q', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste},
		{Keycode: 'j', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyTab, EventType: vaxis.EventPaste},
	} {
		if !pasted.HandleEditorKey(false, key) {
			t.Fatalf("paste key was not consumed: %#v", key)
		}
	}
	if pasted.Query != "q   " {
		t.Fatalf("separate paste events = %q", pasted.Query)
	}

	if width := paletteNameWidth([]paletteCommand{{Name: "界界"}}); width != 4 {
		t.Fatalf("wide command name width = %d, want 4", width)
	}

}

func TestPromptCommandsContributeToIdlePaletteWithArguments(t *testing.T) {
	t.Parallel()
	contributions := promptPaletteCommands([]protocol.PromptCommand{{
		Name: "review", Description: "Review recent changes", ArgumentHint: "<scope>", Source: "project", Location: "/repo/.agents/prompts/review.md",
	}})
	var palette paletteController
	palette.SetContributions(contributions, false)
	palette.OpenFor(false)
	palette.SetQuery(false, `review "auth module" carefully`)
	command, ok := palette.Selected(false, palette.Query)
	if !ok || command.Name != "review" {
		t.Fatalf("selected prompt command = %#v, %v", command, ok)
	}
	name, ok := promptPaletteCommandName(command.ID)
	_, args := splitPaletteQuery(palette.Query)
	if !ok || name != "review" || args != `"auth module" carefully` {
		t.Fatalf("prompt execution = name:%q args:%q found:%v", name, args, ok)
	}
	if paletteCommandAvailable(command.ID, true, contributions) {
		t.Fatal("prompt command remained available during active work")
	}
	if commands := paletteCommands([]paletteCommand{{ID: "prompt:quit", Name: "quit"}}); len(commands) != 17 {
		t.Fatalf("prompt command shadowed a built-in: %#v", commands)
	}
	state := &paletteHarnessState{}
	state.palette.SetContributions(contributions, false)
	state.palette.OpenFor(false)
	state.palette.SetQuery(false, "review")
	application := uitest.New(paletteHarness{State: state})
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	if !strings.Contains(text, "review <scope>") || !strings.Contains(text, "Review recent changes") {
		t.Fatalf("prompt command palette =\n%s", text)
	}
	rows := paintedRows(application, 80, 24)
	reviewRow := findPaintedRow(rows, "Review recent changes")
	if reviewRow < 0 {
		t.Fatalf("review result row missing:\n%s", strings.Join(rows, "\n"))
	}
	application.Click(40, reviewRow)
	application.Pump(80, 24)
	if state.executed != paletteCommandID("prompt:review") || state.palette.Open {
		t.Fatalf("prompt command mouse execution = %q, open=%v", state.executed, state.palette.Open)
	}
	state.SetState(func() {
		state.palette.OpenFor(false)
		state.palette.SetQuery(false, "review")
	})
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc, Text: "\x1b"})
	application.Pump(80, 24)
	application.Key("x")
	application.Pump(80, 24)
	if state.palette.Open || len(state.palette.Contributions) != 1 || state.composer != "x" {
		t.Fatalf("Escape with prompt contributions left palette=%+v composer=%q", state.palette, state.composer)
	}
}

func TestCWDChangeToastSuggestsExplicitContextReload(t *testing.T) {
	t.Parallel()
	toast := cwdChangeToast("/repo/packages/api")
	if toast.Title != "Working directory changed" || toast.Subtitle != "Now /repo/packages/api · run /reload to refresh agent context" || toast.Variant != toastWarning || toast.Persistent {
		t.Fatalf("cwd toast = %#v", toast)
	}
}

func TestReloadToastReportsCompletionWarningsAndFailure(t *testing.T) {
	t.Parallel()
	if toast := reloadToast(protocol.ReloadSessionResult{}, nil, nil); toast.Title != "Session context reloaded" || toast.Variant != toastInfo || toast.Subtitle != "" {
		t.Fatalf("success toast = %#v", toast)
	}
	warnings := protocol.ReloadSessionResult{Diagnostics: []protocol.PromptDiagnostic{
		{Severity: "warning", Message: "Could not read root guidance"}, {Severity: "warning", Message: "Local guidance was oversized"},
	}}
	if toast := reloadToast(warnings, nil, nil); toast.Title != "Session context reloaded" || toast.Variant != toastWarning || toast.Subtitle != "Could not read root guidance (+1 more)" {
		t.Fatalf("warning toast = %#v", toast)
	}
	if toast := reloadToast(protocol.ReloadSessionResult{}, errors.New("busy"), nil); toast.Title != "Session reload failed" || toast.Variant != toastError || toast.Subtitle != "busy" {
		t.Fatalf("failure toast = %#v", toast)
	}
	if toast := reloadToast(protocol.ReloadSessionResult{}, nil, errors.New("offline")); toast.Title != "Session context reloaded" || toast.Variant != toastWarning || toast.Subtitle != "Session refresh failed: offline" {
		t.Fatalf("snapshot failure toast = %#v", toast)
	}
}

func TestCommandPalettePresentationFilteringAndExecution(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	backgroundStyle := application.Cell(1, 3).Style
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)

	rows := paintedRows(application, width, height)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Search commands…", "cd", "Change working directory", "login", "Connect another provider", "new", "Start a new session", "quit", "Exit Kit",
		"diff", "Review working-tree changes", "files", "Open a workspace file", "↑↓ move · enter run · esc close",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("palette missing %q:\n%s", expected, text)
		}
	}
	if application.Cell(1, 3).Style != backgroundStyle {
		t.Fatalf("palette recolored the background: got %+v, want %+v", application.Cell(1, 3).Style, backgroundStyle)
	}
	left, right, top := paletteBorder(rows)
	bottom := dialogBottom(rows)
	if got := bottom - top + 1; got != pickerModalMinHeight {
		t.Fatalf("palette height = %d, want minimum %d", got, pickerModalMinHeight)
	}
	assertPickerFooter(t, rows, "↑↓ move · enter run · esc close")
	if got := right - left + 1; got != 64 {
		t.Fatalf("palette width = %d, want 64", got)
	}
	promptColumn, _ := findTextCell(t, rows, ">")
	cdColumn, cdRow := findTextCell(t, rows, "cd")
	quitColumn, quitRow := findTextCell(t, rows, "quit")
	if cdColumn != promptColumn {
		t.Fatalf("command column = %d, want filter prompt column %d", cdColumn, promptColumn)
	}
	if application.Cell(cdColumn, cdRow).Style.Background == application.Cell(quitColumn, quitRow).Style.Background {
		t.Fatal("selected cd row is not visually distinct")
	}
	if application.Cell(cdColumn, cdRow).Style.Foreground == application.Cell(quitColumn, quitRow).Style.Foreground {
		t.Fatal("selected cd text is not inverted")
	}

	for _, character := range "provider" {
		application.Key(string(character))
		application.Pump(width, height)
	}
	rows = paintedRows(application, width, height)
	text = strings.Join(rows, "\n")
	if !strings.Contains(text, "login") || strings.Contains(text, "Exit Kit") {
		t.Fatalf("filtered palette =\n%s", text)
	}
	_, _, filteredTop := paletteBorder(rows)
	filteredBottom := dialogBottom(rows)
	if filteredTop != top || filteredBottom != bottom {
		t.Fatalf("palette bounds moved from rows %d–%d to %d–%d while filtering", top, bottom, filteredTop, filteredBottom)
	}
	application.Enter()
	application.Pump(width, height)
	if state.executed != paletteCommandLogin || state.palette.Open {
		t.Fatalf("execution = %q, open=%v", state.executed, state.palette.Open)
	}
}

func TestCommandPaletteShowsStableDisabledCommandsAndQuietEmptyState(t *testing.T) {
	t.Parallel()

	const width, height = 60, 18
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, PaletteQuery: "no-such-command",
		Running: true, Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	text := strings.Join(paintedRows(application, width, height), "\n")
	if !strings.Contains(text, "No results") || !strings.Contains(text, "esc close") {
		t.Fatalf("empty palette state =\n%s", text)
	}

	activated := paletteCommandID("")
	application = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, Running: true, Scroll: &ui.ScrollController{},
	}, Callbacks: shellCallbacks{RunPaletteCommand: func(_ ui.EventContext, command paletteCommandID) { activated = command }}})
	application.Pump(width, height)
	text = strings.Join(paintedRows(application, width, height), "\n")
	if !strings.Contains(text, "debug") || !strings.Contains(text, "Show session diagnostics") {
		t.Fatalf("running palette omitted enabled debug command:\n%s", text)
	}
	runningRows := paintedRows(application, width, height)
	for name, description := range map[string]string{
		"model": "Change session model", "new": "Start a new session", "files": "Open a workspace file", "diff": "Review working-tree changes",
	} {
		column, row := findTextCell(t, runningRows, name)
		if !strings.Contains(runningRows[row], description) || application.Cell(column, row).Style.Foreground != ui.DefaultTheme().Foreground {
			t.Fatalf("enabled %s row = %q style=%+v", name, runningRows[row], application.Cell(column, row).Style)
		}
	}
	if !strings.Contains(text, "compact") || !strings.Contains(text, glyphCircleSlash+" idle only") {
		t.Fatalf("running palette did not retain visibly disabled idle commands:\n%s", text)
	}
	modelColumn, modelRow := findTextCell(t, paintedRows(application, width, height), "model")
	application.Click(modelColumn, modelRow)
	application.Pump(width, height)
	if activated != paletteCommandModel {
		t.Fatalf("model pointer activation ran %q", activated)
	}
}

func TestCommandPalettePaintsDisabledSelectionWithoutRetargeting(t *testing.T) {
	t.Parallel()

	const width, height = 60, 14
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, PaletteSelection: paletteCommandCompact, Running: true,
		Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	compactColumn, compactRow := findTextCell(t, rows, "compact")
	debugColumn, debugRow := findTextCell(t, rows, "debug")
	compactStyle := application.Cell(compactColumn, compactRow).Style
	if compactStyle.Background == application.Cell(debugColumn, debugRow).Style.Background {
		t.Fatal("disabled compact selection had no subdued selection background")
	}
	if want := ui.DefaultTheme().DisabledForeground; compactStyle.Foreground != want {
		t.Fatalf("disabled compact foreground = %v, want %v", compactStyle.Foreground, want)
	}
	if !strings.Contains(strings.Join(rows, "\n"), glyphCircleSlash+" idle only") {
		t.Fatalf("disabled compact reason missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestCommandPaletteFitsShortViewport(t *testing.T) {
	t.Parallel()

	const width, height = 40, 8
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"Search commands…", "cd", "compact", "enter run", "esc close"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("short palette missing %q:\n%s", expected, text)
		}
	}
	_, _, top := paletteBorder(rows)
	if top != 0 || !strings.Contains(rows[height-1], "└") {
		t.Fatalf("short palette bounds top=%d bottom=%q", top, rows[height-1])
	}
}

func TestDisabledCommandReasonSurvivesNarrowLongContribution(t *testing.T) {
	t.Parallel()

	const width, height = 40, 8
	name := "extraordinarily-long-project-review-command"
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, PaletteQuery: name, Running: true,
		PaletteCommands: []paletteCommand{{ID: paletteCommandID("prompt:" + name), Name: name, Description: "Review the current project thoroughly"}},
		Scroll:          &ui.ScrollController{},
	}})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "extraordinarily…") || !strings.Contains(text, glyphCircleSlash+" idle only") {
		t.Fatalf("narrow disabled contribution lost identity or reason:\n%s", text)
	}
}

func TestAppDisabledCommandActivationKeepsPaletteOpenAndPresentsToast(t *testing.T) {
	t.Parallel()

	var presented toastInput
	state := &appState{
		runPending:        true,
		palette:           paletteController{Open: true, Selection: paletteCommandCD},
		showToastOverride: func(toast toastInput) { presented = toast },
	}
	state.runPaletteCommand(ui.EventContext{}, paletteCommandCD)
	if !state.palette.Open || presented.Title != "Command unavailable" || presented.Subtitle != "Available when the session is idle." || presented.Variant != toastWarning {
		t.Fatalf("disabled app activation = open:%v toast:%+v", state.palette.Open, presented)
	}
}

func TestTabsActionExplainsAgentOnlyWorkspace(t *testing.T) {
	t.Parallel()
	var presented toastInput
	state := &appState{showToastOverride: func(toast toastInput) { presented = toast }}
	state.openWorkspacePicker()
	if state.workspacePickerOpen || presented.Title != "No workspace tabs" || presented.Subtitle != "Open a secondary surface first." {
		t.Fatalf("Agent-only tabs action = picker:%t toast:%+v", state.workspacePickerOpen, presented)
	}
}

func TestCreateNewSessionReportsDuplicatePendingWork(t *testing.T) {
	t.Parallel()

	var presented toastInput
	state := &appState{
		phase:                phaseReady,
		bound:                fakeSession{id: "session_current"},
		sessionCreatePending: true,
		showToastOverride:    func(toast toastInput) { presented = toast },
	}
	state.createNewSession()
	if presented.Title != "New session unavailable" || presented.Subtitle != "Session creation is already in progress." || presented.Variant != toastWarning {
		t.Fatalf("pending new-session toast = %+v", presented)
	}
}

func TestDisabledCommandToastOverlaysPaletteWithoutReplacingFooter(t *testing.T) {
	t.Parallel()

	toast, _ := paletteCommandDisabledToast(paletteCommandCD, true)
	application := uitest.New(ui.Overlay{
		Child: commandPaletteSurface{Snapshot: paletteSnapshot{
			Query: "cd", Selection: paletteCommandCD, Running: true,
		}},
		Entries: []ui.OverlayEntry{{Child: toastStack{Toasts: []toastRecord{{ID: 1, toastInput: toast}}}}},
	})
	application.Pump(80, 24)
	text := application.Text()
	for _, expected := range []string{"Command unavailable", "Available when the session is idle.", "↑↓ move · enter run · esc close"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("toast-over-palette presentation missing %q:\n%s", expected, text)
		}
	}
}

func TestDisabledCommandKeyboardActivationKeepsPaletteOpenForToastFeedback(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{running: true}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	for _, character := range "compact" {
		application.Key(string(character))
	}
	application.Enter()
	application.Pump(width, height)
	if state.executed != "" || !state.palette.Open || state.disabledToasts != 1 {
		t.Fatalf("disabled activation = executed:%q open:%v toast requests:%d", state.executed, state.palette.Open, state.disabledToasts)
	}
}

func TestCommandPaletteResolvesRapidKeyboardInputFromControllerState(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{running: true}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)

	application.Send(vaxis.Key{Keycode: vaxis.KeyUp})
	if state.palette.Selection != paletteCommandCompact {
		t.Fatalf("previous selection = %q, want disabled compact", state.palette.Selection)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	if state.palette.Selection != paletteCommandDebug {
		t.Fatalf("wrapped Down selection = %q, want debug", state.palette.Selection)
	}

	application.Key("q")
	application.Enter()
	application.Pump(width, height)
	if state.executed != paletteCommandQuit {
		t.Fatalf("rapid query execution = %q, want quit", state.executed)
	}

	state.SetState(func() { state.executed = "" })
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	for range 8 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	}
	application.Enter()
	application.Pump(width, height)
	if state.executed != paletteCommandQuit {
		t.Fatalf("rapid selection execution = %q, want quit", state.executed)
	}
}

func TestCommandPaletteCapturesInputBeforeOverlayFrame(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Key("/")
	for _, character := range "quit" {
		application.Key(string(character))
	}
	application.Enter()
	application.Pump(width, height)
	if state.composer != "" || state.executed != paletteCommandQuit {
		t.Fatalf("coalesced slash input composer=%q executed=%q", state.composer, state.executed)
	}

	state = &paletteHarnessState{}
	application = uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Key("/")
	application.Key("q")
	application.Send(vaxis.Key{Keycode: vaxis.KeyBackspace})
	application.Pump(width, height)
	if !state.palette.Open || state.palette.Query != "" {
		t.Fatalf("coalesced slash backspace palette=%+v", state.palette)
	}

	state = &paletteHarnessState{composer: "draft"}
	application = uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Key("q")
	application.Enter()
	application.Pump(width, height)
	if state.composer != "draft" || state.executed != paletteCommandQuit {
		t.Fatalf("coalesced Ctrl+P input composer=%q executed=%q", state.composer, state.executed)
	}
}

func TestCommandPaletteHandlesNavigationAndDismissalBeforeOverlayFrame(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Enter()
	application.Pump(width, height)
	if state.executed != paletteCommandCompact {
		t.Fatalf("pre-frame navigation executed %q, want compact", state.executed)
	}

	state = &paletteHarnessState{}
	application = uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Key("/")
	application.Key("q")
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Key("x")
	application.Pump(width, height)
	if state.palette.Open || state.composer != "x" {
		t.Fatalf("pre-frame Escape palette=%+v composer=%q", state.palette, state.composer)
	}

	state = &paletteHarnessState{composer: "draft"}
	application = uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Key("q")
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Enter()
	application.Pump(width, height)
	if len(state.submitted) != 1 || state.submitted[0] != "draft" {
		t.Fatalf("pre-frame draft submission = %#v", state.submitted)
	}

	state = &paletteHarnessState{}
	application = uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Key("/")
	application.Pump(width, height)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Pump(width, height)
	application.Pump(width, height)
	application.Key("/")
	application.Pump(width, height)
	if !state.palette.Open || state.composer != "" {
		t.Fatalf("second slash after reconciliation palette=%+v composer=%q", state.palette, state.composer)
	}
}

func TestGlobalPaletteCapturesPreFrameQueryAfterTranscriptFocus(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Click(2, 0)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Key("q")
	application.Enter()
	application.Pump(width, height)
	if state.executed != paletteCommandQuit || state.composer != "" {
		t.Fatalf("global pre-frame query executed=%q composer=%q", state.executed, state.composer)
	}
}

func TestPastedComposerSlashDoesNotOpenPalette(t *testing.T) {
	t.Parallel()

	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(80, 24)
	application.Send(vaxis.Key{Text: "/", Keycode: '/', EventType: vaxis.EventPaste})
	application.Send(vaxis.Key{Text: "help", Keycode: 'h', EventType: vaxis.EventPaste})
	if state.palette.Open || state.composer != "/help" {
		t.Fatalf("pasted slash palette=%+v composer=%q", state.palette, state.composer)
	}
}

func TestNativeRootLeavesShortcutFilteringToShell(t *testing.T) {
	t.Parallel()

	shortcuts := nativeRootShortcuts()
	if _, exists := shortcuts["Tab"]; exists {
		t.Fatal("native root retained the outer Tab shortcut")
	}
	if len(shortcuts) != 0 {
		t.Fatalf("native root shortcuts = %#v, want shell-owned paste filtering", shortcuts)
	}
	state := &paletteHarnessState{}
	application := ui.NewApp(paletteHarness{State: state}, ui.WithShortcuts(shortcuts))
	application.Pump(ui.Size{Width: 80, Height: 24})
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Send(vaxis.Key{Keycode: vaxis.KeyTab, EventType: vaxis.EventPaste})
	if state.palette.Query != " " {
		t.Fatalf("pasted Tab query = %q", state.palette.Query)
	}
}

func TestPastedEscapeCannotDismissPalette(t *testing.T) {
	t.Parallel()

	state := &paletteHarnessState{}
	state.palette.OpenFor(false)
	application := ui.NewApp(paletteHarness{State: state}, ui.WithShortcuts(nativeRootShortcuts()))
	application.Pump(ui.Size{Width: 80, Height: 24})
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc, EventType: vaxis.EventPaste})
	if !state.palette.Open {
		t.Fatal("pasted Escape dismissed the palette")
	}
}

func TestCommandPalettePasteCannotActivateCommand(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{running: true}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste})
	application.Pump(width, height)
	if state.executed != "" || !state.palette.Open || state.palette.Query != " " {
		t.Fatalf("paste event executed=%q open=%v query=%q", state.executed, state.palette.Open, state.palette.Query)
	}
}

func TestCommandPaletteDismissRestoresComposerFocus(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	application.Pump(width, height)
	application.Key("x")
	application.Pump(width, height)
	if state.palette.Open || state.composer != "x" {
		t.Fatalf("after dismissal open=%v composer=%q", state.palette.Open, state.composer)
	}
}

func TestCommandPaletteSupportsFullRowMouseAndBlocksBackgroundFocus(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	state := &paletteHarnessState{}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	_, right, _ := paletteBorder(rows)
	_, quitRow := findTextCell(t, rows, "quit")
	application.Click(right-3, quitRow)
	application.Pump(width, height)
	if state.executed != paletteCommandQuit || state.palette.Open {
		t.Fatalf("mouse execution = %q, open=%v", state.executed, state.palette.Open)
	}

	state.SetState(func() { state.executed = "" })
	application.Pump(width, height)
	application.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
	application.Pump(width, height)
	application.Click(2, 0)
	application.Key("x")
	application.Pump(width, height)
	if state.composer != "" || state.palette.Query != "x" {
		t.Fatalf("modal pointer behavior composer=%q query=%q", state.composer, state.palette.Query)
	}
}

func TestPaletteLaunchedAuthKeepsConversationVisible(t *testing.T) {
	t.Parallel()

	const width, height = 120, 20
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseAuthSelect, AuthReturnReady: true,
		Session:  protocol.SessionInfo{Name: "Attached session", Model: "openai/gpt-5.3-codex"},
		Messages: []transcriptMessage{{Role: "assistant", Text: "History"}},
		Scroll:   &ui.ScrollController{},
	}})
	application.Pump(width, height)
	text := strings.Join(paintedRows(application, width, height), "\n")
	for _, expected := range []string{"Attached session", "History", "Connect a provider"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("palette-launched auth missing %q:\n%s", expected, text)
		}
	}
}

func paletteBorder(rows []string) (int, int, int) {
	for rowIndex, row := range rows {
		left := strings.Index(row, "┌")
		right := strings.LastIndex(row, "┐")
		if left >= 0 && right >= left {
			return len([]rune(row[:left])), len([]rune(row[:right])), rowIndex
		}
	}
	return -1, -1, -1
}

func assertPickerFooter(t *testing.T, rows []string, hint string) {
	t.Helper()
	bottom := dialogBottom(rows)
	if bottom < 2 || !strings.Contains(rows[bottom-2], "├") || !strings.Contains(rows[bottom-2], "┤") ||
		!strings.Contains(rows[bottom-1], hint) {
		t.Fatalf("picker footer is not fixed below its divider:\n%s", strings.Join(rows, "\n"))
	}
}

func dialogBottom(rows []string) int {
	for rowIndex := len(rows) - 1; rowIndex >= 0; rowIndex-- {
		if strings.Contains(rows[rowIndex], "└") && strings.Contains(rows[rowIndex], "┘") {
			return rowIndex
		}
	}
	return -1
}

func findTextCell(t *testing.T, rows []string, value string) (int, int) {
	t.Helper()
	for row, line := range rows {
		if index := strings.Index(line, value); index >= 0 {
			return len([]rune(line[:index])), row
		}
	}
	t.Fatalf("text %q not found in:\n%s", value, strings.Join(rows, "\n"))
	return 0, 0
}

type paletteHarness struct{ State *paletteHarnessState }

func (w paletteHarness) CreateState() ui.State { return w.State }

type paletteHarnessState struct {
	ui.StateBase
	palette        paletteController
	running        bool
	composer       string
	executed       paletteCommandID
	disabledToasts int
	submitted      []string
	scroll         ui.ScrollController
}

func (s *paletteHarnessState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	key, ok := event.(ui.Key)
	if !ok || !s.palette.Open {
		return ui.EventIgnored
	}
	var command paletteCommand
	var run, handled bool
	s.SetState(func() {
		command, run, handled = s.palette.HandleKey(s.running, key)
		if !handled {
			handled = s.palette.HandleEditorKey(s.running, key)
		}
	})
	if run {
		if paletteCommandAvailable(command.ID, s.running, s.palette.Contributions) {
			s.execute(command.ID)
		} else {
			s.disabledToasts++
		}
	}
	if handled {
		return ui.EventHandled
	}
	return ui.EventIgnored
}

func (s *paletteHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Composer: s.composer, PaletteOpen: s.palette.Open,
			PaletteQuery: s.palette.Query, PaletteSelection: s.palette.Selection,
			PaletteCommands: s.palette.Contributions, Running: s.running, Scroll: &s.scroll,
			Session: protocol.SessionInfo{Name: "Palette test", Model: "openai/gpt-5.3-codex"},
		},
		Callbacks: shellCallbacks{
			ComposerPasted: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() {
					composer, intercepted := s.palette.HandleComposerChange(s.composer, value, s.running)
					if !intercepted {
						s.composer = composer
					}
				})
			},
			OpenPalette: func(ui.EventContext) {
				s.SetState(func() { s.palette.OpenFor(s.running) })
			},
			PaletteQueryChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.palette.SetQuery(s.running, value) })
			},
			MovePaletteSelection: func(_ ui.EventContext, delta int) {
				s.SetState(func() { s.palette.Move(s.running, delta) })
			},
			RunPaletteQuery: func(_ ui.EventContext, query string) {
				command, ok := s.palette.Selected(s.running, query)
				if ok && paletteCommandAvailable(command.ID, s.running, s.palette.Contributions) {
					s.execute(command.ID)
				}
			},
			RunPaletteCommand: func(_ ui.EventContext, command paletteCommandID) {
				if paletteCommandAvailable(command, s.running, s.palette.Contributions) {
					s.execute(command)
				}
			},
			Submit: func(_ ui.EventContext, value string) {
				if s.palette.Open {
					command, ok := s.palette.Selected(s.running, s.palette.Query)
					if ok {
						s.execute(command.ID)
					}
					return
				}
				s.SetState(func() { s.submitted = append(s.submitted, value) })
			},
			Dismiss: func(ui.EventContext) {
				s.SetState(func() { s.palette.Close() })
			},
		},
	}
}

func (s *paletteHarnessState) execute(command paletteCommandID) {
	s.SetState(func() {
		s.executed = command
		s.palette.Close()
	})
}

func TestPaletteScrollListFillsBodyAndRevealsSelection(t *testing.T) {
	state := &paletteScrollHarnessState{}
	state.snapshot.Selection = filteredPaletteCommands(false, "")[0].ID
	app := uitest.New(paletteScrollHarness{state})
	const width, height = 100, 30
	app.Pump(width, height)
	state.TickFrame(time.Now())
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	commands := filteredPaletteCommands(false, "")
	first := findPaintedRow(rows, commands[0].Description)
	footer := findPaintedRow(rows, "↑↓ move · enter run · esc close")
	if first < 0 || footer-first-1 <= 11 {
		t.Fatalf("expected more than eleven available command rows:\n%s", strings.Join(rows, "\n"))
	}
	for row := first; row < footer-1; row++ {
		index := row - first
		if index >= len(commands) || !strings.Contains(rows[row], commands[index].Name) {
			t.Fatalf("command row %d = %q; expected command index %d", row, rows[row], index)
		}
	}
	// Wheel scrolling moves the viewport without changing keyboard selection.
	app.Send(vaxis.Mouse{Col: 20, Row: first + 1, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
	for range 6 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	rows = paintedRows(app, width, height)
	if got := findPaintedRow(rows, commands[1].Description); got != first {
		t.Fatalf("wheel-scrolled row = %d, want %d:\n%s", got, first, strings.Join(rows, "\n"))
	}
	// Keyboard navigation reveals the last command without moving the footer.
	state.SetState(func() { state.snapshot.Selection = commands[len(commands)-1].ID })
	for range 6 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if row := findPaintedRow(rows, commands[len(commands)-1].Description); row != footer-2 {
		t.Fatalf("last command row = %d, want %d:\n%s", row, footer-2, strings.Join(rows, "\n"))
	}
	// A smaller viewport still reveals the selected row above the fixed footer.
	for range 6 {
		app.Pump(width, 18)
		state.TickFrame(time.Now())
	}
	app.Pump(width, 18)
	smallRows := paintedRows(app, width, 18)
	smallFooter := findPaintedRow(smallRows, "↑↓ move · enter run · esc close")
	if got := findPaintedRow(smallRows, commands[len(commands)-1].Description); got != smallFooter-2 {
		t.Fatalf("resized selection row = %d, want %d", got, smallFooter-2)
	}
	for range 6 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	// Filtering resets the scroll position and exposes the selected result.
	state.SetState(func() { state.snapshot.Query = "compact"; state.snapshot.Selection = paletteCommandCompact })
	for range 6 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if got := findPaintedRow(rows, "Compact session context"); got != first {
		t.Fatalf("filtered row = %d, want %d:\n%s", got, first, strings.Join(rows, "\n"))
	}
	if got := findPaintedRow(rows, "↑↓ move · enter run · esc close"); got != footer {
		t.Fatalf("footer row = %d, want %d", got, footer)
	}
}

type paletteScrollHarness struct{ state *paletteScrollHarnessState }

func (w paletteScrollHarness) CreateState() ui.State { return w.state }

type paletteScrollHarnessState struct {
	commandPaletteSurfaceState
	snapshot paletteSnapshot
}

func (s *paletteScrollHarnessState) Build(ctx ui.BuildContext) ui.Widget {
	return s.commandPaletteSurfaceState.build(ctx, commandPaletteSurface{Snapshot: s.snapshot})
}
