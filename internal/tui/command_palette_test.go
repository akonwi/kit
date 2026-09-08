package tui

import (
	"errors"
	"strings"
	"testing"

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
	if palette.Selection != paletteCommandQuit {
		t.Fatalf("running selection = %q, want quit", palette.Selection)
	}
	command, ok = palette.Selected(false, "")
	if !ok || command.ID != paletteCommandQuit {
		t.Fatalf("stable quit selection = %#v, %v", command, ok)
	}

	palette.Close()
	palette.OpenFor(true)
	if _, ok := palette.Selected(false, ""); ok {
		t.Fatal("disappearing abort selection retargeted another command")
	}
}

func TestCommandPaletteModelFiltersAliasesArgumentsAndWindows(t *testing.T) {
	t.Parallel()

	commands := filteredPaletteCommands(false, "provider")
	if len(commands) != 1 || commands[0].ID != paletteCommandLogin {
		t.Fatalf("provider matches = %#v, want login", commands)
	}
	commands = filteredPaletteCommands(false, "threads")
	if len(commands) != 1 || commands[0].ID != paletteCommandSessions {
		t.Fatalf("threads matches = %#v, want sessions", commands)
	}
	commands = filteredPaletteCommands(false, "context")
	if len(commands) != 1 || commands[0].ID != paletteCommandReload {
		t.Fatalf("context matches = %#v, want reload", commands)
	}
	commands = filteredPaletteCommands(true, "stop because it is stuck")
	if len(commands) != 1 || commands[0].ID != paletteCommandAbort {
		t.Fatalf("stop matches = %#v, want abort", commands)
	}
	if paletteCommandAvailable(paletteCommandLogin, true) {
		t.Fatal("login remained available during an active run")
	}
	if paletteCommandAvailable(paletteCommandSessions, true) {
		t.Fatal("sessions remained available during active work")
	}
	if paletteCommandAvailable(paletteCommandReload, true) || !paletteCommandAvailable(paletteCommandReload, false) {
		t.Fatal("reload command availability does not follow idle state")
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

	many := make([]paletteCommand, 20)
	for index := range many {
		many[index] = paletteCommand{ID: paletteCommandID(string(rune('a' + index))), Name: string(rune('a' + index))}
	}
	if width := paletteNameWidth([]paletteCommand{{Name: "界界"}}); width != 4 {
		t.Fatalf("wide command name width = %d, want 4", width)
	}

	window, offset := paletteCommandWindow(many, 12, 6)
	if len(window) != 6 || offset != 9 || window[3].Name != many[12].Name {
		t.Fatalf("window length=%d offset=%d commands=%#v", len(window), offset, window)
	}
}

func TestPromptCommandsContributeToIdlePaletteWithArguments(t *testing.T) {
	t.Parallel()
	contributions := promptPaletteCommands([]protocol.PromptCommand{{
		Name: "review", Description: "Review recent changes", Source: "project", Location: "/repo/.agents/prompts/review.md",
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
	if commands := availablePaletteCommands(false, []paletteCommand{{ID: "prompt:quit", Name: "quit"}}); len(commands) != 4 {
		t.Fatalf("prompt command shadowed a built-in: %#v", commands)
	}
	state := &paletteHarnessState{}
	state.palette.SetContributions(contributions, false)
	state.palette.OpenFor(false)
	application := uitest.New(paletteHarness{State: state})
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	if !strings.Contains(text, "review") || !strings.Contains(text, "Review recent changes") {
		t.Fatalf("prompt command palette =\n%s", text)
	}
	rows := paintedRows(application, 80, 24)
	_, reviewRow := findTextCell(t, rows, "review")
	application.Click(40, reviewRow)
	application.Pump(80, 24)
	if state.executed != paletteCommandID("prompt:review") || state.palette.Open {
		t.Fatalf("prompt command mouse execution = %q, open=%v", state.executed, state.palette.Open)
	}
}

func TestReloadToastReportsSuccessWarningsAndFailure(t *testing.T) {
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
	info := protocol.ReloadSessionResult{Diagnostics: []protocol.PromptDiagnostic{{Severity: "info", Message: "Duplicate guidance omitted"}}}
	if toast := reloadToast(info, nil, nil); toast.Variant != toastInfo || toast.Subtitle != "Duplicate guidance omitted" {
		t.Fatalf("informational toast = %#v", toast)
	}
	if toast := reloadToast(protocol.ReloadSessionResult{}, nil, errors.New("offline")); toast.Title != "Session context reloaded" || toast.Variant != toastWarning || toast.Subtitle != "Transcript refresh failed: offline" {
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
		"Search commands…", "login", "Connect another provider", "quit", "Exit Kit",
		"reload", "Reload session context", "sessions", "Browse sessions", "↑↓ move · enter run · esc close",
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
	loginColumn, loginRow := findTextCell(t, rows, "login")
	quitColumn, quitRow := findTextCell(t, rows, "quit")
	if loginColumn != promptColumn {
		t.Fatalf("command column = %d, want filter prompt column %d", loginColumn, promptColumn)
	}
	if application.Cell(loginColumn, loginRow).Style.Background == application.Cell(quitColumn, quitRow).Style.Background {
		t.Fatal("selected login row is not visually distinct")
	}
	if application.Cell(loginColumn, loginRow).Style.Foreground == application.Cell(quitColumn, quitRow).Style.Foreground {
		t.Fatal("selected login text is not inverted")
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

func TestCommandPaletteShowsConditionalAbortAndQuietEmptyState(t *testing.T) {
	t.Parallel()

	const width, height = 60, 16
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, PaletteQuery: "no-such-command",
		Running: true, Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	text := strings.Join(paintedRows(application, width, height), "\n")
	if !strings.Contains(text, "No results") || !strings.Contains(text, "esc close") {
		t.Fatalf("empty palette state =\n%s", text)
	}

	application = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, Running: true, Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	text = strings.Join(paintedRows(application, width, height), "\n")
	if !strings.Contains(text, "abort") || !strings.Contains(text, "Stop the active run") {
		t.Fatalf("running palette omitted abort command:\n%s", text)
	}
	if strings.Contains(text, "Reload session context") {
		t.Fatalf("running palette exposed reload command:\n%s", text)
	}
}

func TestCommandPaletteDoesNotPaintReplacementForMissingSelection(t *testing.T) {
	t.Parallel()

	const width, height = 60, 14
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, PaletteSelection: paletteCommandAbort,
		Scroll: &ui.ScrollController{},
	}})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	loginColumn, loginRow := findTextCell(t, rows, "login")
	quitColumn, quitRow := findTextCell(t, rows, "quit")
	if application.Cell(loginColumn, loginRow).Style.Background != application.Cell(quitColumn, quitRow).Style.Background {
		t.Fatal("missing abort selection visibly retargeted another command")
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
	for _, expected := range []string{"Search commands…", "login", "quit", "enter run", "esc close"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("short palette missing %q:\n%s", expected, text)
		}
	}
	_, _, top := paletteBorder(rows)
	if top != 0 || !strings.Contains(rows[height-1], "└") {
		t.Fatalf("short palette bounds top=%d bottom=%q", top, rows[height-1])
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
	if state.palette.Selection != paletteCommandQuit {
		t.Fatalf("wrapped Up selection = %q, want quit", state.palette.Selection)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	if state.palette.Selection != paletteCommandAbort {
		t.Fatalf("wrapped Down selection = %q, want abort", state.palette.Selection)
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
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
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
	if state.executed != paletteCommandQuit {
		t.Fatalf("pre-frame navigation executed %q, want quit", state.executed)
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
	palette   paletteController
	running   bool
	composer  string
	executed  paletteCommandID
	submitted []string
	scroll    ui.ScrollController
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
		s.execute(command.ID)
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
				if ok {
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
