package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	paletteMaxNameWidth = 32

	paletteCommandCD        paletteCommandID = "cd"
	paletteCommandCompact   paletteCommandID = "compact"
	paletteCommandLogin     paletteCommandID = "login"
	paletteCommandModel     paletteCommandID = "model"
	paletteCommandName      paletteCommandID = "name"
	paletteCommandNew       paletteCommandID = "new"
	paletteCommandQuit      paletteCommandID = "quit"
	paletteCommandReload    paletteCommandID = "reload"
	paletteCommandDebug     paletteCommandID = "debug"
	paletteCommandDiff      paletteCommandID = "diff"
	paletteCommandFork      paletteCommandID = "fork"
	paletteCommandFiles     paletteCommandID = "files"
	paletteCommandSessions  paletteCommandID = "sessions"
	paletteCommandSubagents paletteCommandID = "subagents"
	paletteCommandTabs      paletteCommandID = "tabs"
	paletteCommandTheme     paletteCommandID = "theme"
	paletteCommandThinking  paletteCommandID = "thinking"
)

type paletteCommandID string

type paletteCommand struct {
	ArgumentHint string
	ID           paletteCommandID
	Name         string
	Description  string
	Aliases      []string
}

type paletteSnapshot struct {
	Query         string
	Selection     paletteCommandID
	Running       bool
	Contributions []paletteCommand
}

type paletteCallbacks struct {
	QueryChanged ui.TextChangedCallback
	RunQuery     ui.TextChangedCallback
	RunCommand   func(ui.EventContext, paletteCommandID)
}

type commandPaletteSurface struct {
	Snapshot  paletteSnapshot
	Callbacks paletteCallbacks
}

func (commandPaletteSurface) CreateState() ui.State {
	return &commandPaletteSurfaceState{selection: -1}
}

type commandPaletteSurfaceState struct {
	ui.StateBase
	scroll      ui.ScrollController
	list        ui.SliverListController
	selection   int
	query       string
	count       int
	viewport    int
	needsReveal bool
}

func (s *commandPaletteSurfaceState) TickFrame(_ time.Time) bool {
	viewport := s.scroll.Metrics().ViewportHeight
	if viewport != s.viewport {
		s.viewport = viewport
		s.needsReveal = true
	}
	if !s.needsReveal || viewport <= 0 || !s.list.Attached() {
		return false
	}
	s.needsReveal = false
	return s.list.RevealIndex(s.selection)
}

func (s *commandPaletteSurfaceState) Build(ctx ui.BuildContext) ui.Widget {
	return s.build(ctx, s.Widget().(commandPaletteSurface))
}

func (s *commandPaletteSurfaceState) build(ctx ui.BuildContext, w commandPaletteSurface) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	commands := filteredPaletteCommands(w.Snapshot.Running, w.Snapshot.Query, w.Snapshot.Contributions)
	selection, hasSelection := paletteSelectionIndex(w.Snapshot.Selection, commands)
	if !hasSelection {
		selection = 0
	}
	if selection != s.selection || w.Snapshot.Query != s.query || len(commands) != s.count {
		s.selection, s.query, s.count = selection, w.Snapshot.Query, len(commands)
		s.needsReveal = true
	}
	catalog := paletteCommands(w.Snapshot.Contributions)
	nameWidth := paletteNameWidth(catalog)
	for _, command := range catalog {
		if paletteCommandDisabledReason(command.ID, w.Snapshot.Running) != "" {
			nameWidth = min(nameWidth, 16)
			break
		}
	}

	var sliver ui.Widget
	if len(commands) == 0 {
		sliver = ui.SliverToBox{Child: ui.Text{Value: "No results", Style: ui.Style{Foreground: theme.MutedForeground}}}
	} else {
		sliver = ui.SliverListBuilder{Controller: &s.list, Count: len(commands), ItemExtent: 1,
			Builder: func(_ ui.BuildContext, index int) ui.Widget {
				command := commands[index]
				return paletteOptionRow{
					Command: command, NameWidth: nameWidth, DisabledReason: paletteCommandDisabledReason(command.ID, w.Snapshot.Running),
					Selected: hasSelection && index == selection,
					OnPressed: func(event ui.EventContext) {
						if w.Callbacks.RunCommand != nil {
							w.Callbacks.RunCommand(event, command.ID)
						}
					},
				}
			},
		}
	}

	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	queryCursor := len(w.Snapshot.Query)
	query := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}},
		ui.SizedBox{Width: 1},
		textInput(fieldTheme, textInputConfig{
			Value: w.Snapshot.Query, Placeholder: "Search commands…",
			CursorOffset: &queryCursor,
			OnChanged:    w.Callbacks.QueryChanged, OnSubmitted: w.Callbacks.RunQuery,
			AutoFocus: true,
		}),
	}}
	body := ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			query,
			ui.SizedBox{Height: 1},
			ui.Expanded(ui.Scrollbar{Child: ui.CustomScrollView{Controller: &s.scroll, Slivers: []ui.Widget{sliver}}}),
		},
	})
	footer := ui.Text{
		Value:    "↑↓ move · enter run · esc close",
		Style:    ui.Style{Foreground: theme.MutedForeground},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	}
	content := pickerDialogContent(theme, body, footer)
	return pickerDialogPositioner{
		Percent: 80, MinWidth: 48, MaxWidth: 96, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}

type paletteOptionRow struct {
	Command        paletteCommand
	NameWidth      int
	Selected       bool
	DisabledReason string
	OnPressed      ui.VoidCallback
}

func (w paletteOptionRow) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	presentation := resolvePickerRowPresentation(ctx, theme)
	rowTheme := presentation.Theme
	background := theme.Background
	primary := presentation.ItemText
	if w.Selected {
		background = presentation.FocusedBg
		primary = presentation.FocusedText
	}
	if w.DisabledReason != "" {
		primary = theme.DisabledForeground
		rowTheme.Primary = theme.SurfaceHovered
		rowTheme.PrimaryHovered = theme.SurfaceHovered
		if w.Selected {
			background = theme.SurfaceHovered
		}
	}
	secondary := theme.MutedForeground
	if w.Selected {
		secondary = presentation.FocusedText
	}
	if w.DisabledReason != "" {
		secondary = theme.DisabledForeground
	}
	description := w.Command.Description
	if w.DisabledReason != "" {
		description = glyphCircleSlash + " " + w.DisabledReason + " · " + description
	}
	content := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: w.NameWidth, Child: ui.RichText{
			Spans:    paletteCommandNameSpans(w.Command, ui.Style{Foreground: primary, Background: background}, ui.Style{Foreground: secondary, Background: background}),
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}},
		ui.SizedBox{Width: 1},
		ui.Expanded(ui.Text{
			Value: description, Style: ui.Style{Foreground: secondary, Background: background},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}),
	}}
	return ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
		Title: content, Selected: w.Selected, Disabled: w.DisabledReason != "", OnPressed: w.OnPressed,
		Padding: ui.Insets{Right: 1}, MinHeight: 1,
	}}
}

type paletteController struct {
	Open          bool
	Query         string
	Selection     paletteCommandID
	Contributions []paletteCommand
}

func (p *paletteController) OpenFor(running bool) {
	if p.Open {
		return
	}
	p.Open = true
	p.Query = ""
	p.Selection = firstEnabledPaletteCommandID(filteredPaletteCommands(running, "", p.Contributions), running)
}

func (p *paletteController) Close() {
	contributions := p.Contributions
	*p = paletteController{Contributions: contributions}
}

func (p *paletteController) SetContributions(commands []paletteCommand, running bool) {
	p.Contributions = append([]paletteCommand(nil), commands...)
	if p.Open && !paletteCommandExists(p.Selection, p.Contributions) {
		p.Selection = firstEnabledPaletteCommandID(filteredPaletteCommands(running, p.Query, p.Contributions), running)
	}
}

func (p *paletteController) SetQuery(running bool, query string) {
	p.Query = query
	p.Selection = firstPaletteCommandID(filteredPaletteCommands(running, query, p.Contributions))
}

func (p *paletteController) Move(running bool, delta int) {
	commands := filteredPaletteCommands(running, p.Query, p.Contributions)
	if !p.Open || len(commands) == 0 {
		return
	}
	selection, ok := paletteSelectionIndex(p.Selection, commands)
	if !ok {
		p.Selection = commands[0].ID
		return
	}
	selection = (selection + delta) % len(commands)
	if selection < 0 {
		selection += len(commands)
	}
	p.Selection = commands[selection].ID
}

func (p *paletteController) Selected(running bool, query string) (paletteCommand, bool) {
	commands := filteredPaletteCommands(running, query, p.Contributions)
	if !p.Open || len(commands) == 0 {
		return paletteCommand{}, false
	}
	for _, command := range commands {
		if command.ID == p.Selection {
			return command, true
		}
	}
	return paletteCommand{}, false
}

// HandleKey applies navigation against current controller state so events do
// not depend on whether a newly opened palette has painted yet.
func (p *paletteController) HandleKey(running bool, key ui.Key) (paletteCommand, bool, bool) {
	if !p.Open || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return paletteCommand{}, false, false
	}
	switch {
	case key.MatchString("Escape"):
		p.Close()
		return paletteCommand{}, false, true
	case key.MatchString("Up"):
		p.Move(running, -1)
		return paletteCommand{}, false, true
	case key.MatchString("Down"):
		p.Move(running, 1)
		return paletteCommand{}, false, true
	case key.MatchString("Enter"):
		command, ok := p.Selected(running, p.Query)
		return command, ok, true
	default:
		return paletteCommand{}, false, false
	}
}

// HandleEditorKey routes text that arrives before the palette field is painted
// without allowing the still-focused composer editor to mutate.
func (p *paletteController) HandleEditorKey(running bool, key ui.Key) bool {
	if !p.Open {
		return false
	}
	if key.EventType == ui.EventRelease {
		return false
	}
	query := p.Query
	if key.EventType == vaxis.EventPaste {
		query += palettePasteText(key)
		p.SetQuery(running, query)
		return true
	}
	modifiers := key.Modifiers &^ (vaxis.ModShift | vaxis.ModCapsLock | vaxis.ModNumLock)
	if modifiers != 0 {
		return false
	}
	switch {
	case key.MatchString("Backspace"):
		runes := []rune(query)
		if len(runes) > 0 {
			query = string(runes[:len(runes)-1])
		}
	case key.Text != "":
		query += key.Text
	default:
		return true
	}
	p.SetQuery(running, query)
	return true
}

// HandleComposerChange is a fallback for renderers that deliver the slash to
// the editor instead of the focused composer shortcut.
func (p *paletteController) HandleComposerChange(previous, next string, running bool) (string, bool) {
	if p.Open {
		return previous, true
	}
	if previous == "" && next == "/" {
		p.OpenFor(running)
		return previous, true
	}
	return next, false
}

func palettePasteText(key ui.Key) string {
	text := key.Text
	if text == "" && key.Modifiers&vaxis.ModCtrl != 0 {
		switch key.Keycode {
		case 'i', 'j', 'm':
			text = " "
		}
	}
	if text == "" {
		switch key.Keycode {
		case '\r', '\n', '\t':
			text = " "
		default:
			if key.Keycode >= 0x20 && key.Keycode != 0x7f {
				text = string(key.Keycode)
			}
		}
	}
	return strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character == '\t' {
			return ' '
		}
		return character
	}, text)
}

func firstPaletteCommandID(commands []paletteCommand) paletteCommandID {
	if len(commands) == 0 {
		return ""
	}
	return commands[0].ID
}

func firstEnabledPaletteCommandID(commands []paletteCommand, running bool) paletteCommandID {
	for _, command := range commands {
		if paletteCommandDisabledReason(command.ID, running) == "" {
			return command.ID
		}
	}
	return firstPaletteCommandID(commands)
}

func paletteCommands(contributions ...[]paletteCommand) []paletteCommand {
	commands := []paletteCommand{
		{ID: paletteCommandCD, Name: "cd", Description: "Change working directory", Aliases: []string{"cwd", "directory", "folder"}},
		{ID: paletteCommandCompact, Name: "compact", Description: "Compact session context", Aliases: []string{"summarize", "shrink"}},
		{ID: paletteCommandLogin, Name: "login", Description: "Connect another provider", Aliases: []string{"auth", "connect", "provider"}},
		{ID: paletteCommandModel, Name: "model", Description: "Change session model", Aliases: []string{"engine"}},
		{ID: paletteCommandName, Name: "name", Description: "Rename session", Aliases: []string{"rename", "title"}},
		{ID: paletteCommandNew, Name: "new", Description: "Start a new session"},
		{ID: paletteCommandQuit, Name: "quit", Description: "Exit Kit", Aliases: []string{"close", "exit"}},
		{ID: paletteCommandReload, Name: "reload", Description: "Reload session context", Aliases: []string{"agents", "context", "refresh"}},
		{ID: paletteCommandDebug, Name: "debug", Description: "Show session diagnostics", Aliases: []string{"details", "usage"}},
		{ID: paletteCommandDiff, Name: "diff", Description: "Review working-tree changes", Aliases: []string{"changes", "working tree"}},
		{ID: paletteCommandFork, Name: "fork", Description: "Fork the current session into a linked child session", Aliases: []string{"branch"}},
		{ID: paletteCommandFiles, Name: "files", Description: "Open a workspace file", Aliases: []string{"file", "open", "browse"}},
		{ID: paletteCommandSessions, Name: "sessions", Description: "Browse sessions", Aliases: []string{"list", "resume", "switch", "threads"}},
		{ID: paletteCommandSubagents, Name: "subagents", Description: "Inspect delegated work", Aliases: []string{"agents", "delegates", "children"}},
		{ID: paletteCommandTabs, Name: "tabs", Description: "Open a workspace tab", Aliases: []string{"panes", "workspace"}},
		{ID: paletteCommandTheme, Name: "theme", Description: "Choose UI colors", Aliases: []string{"appearance", "colors"}},
		{ID: paletteCommandThinking, Name: "thinking", Description: "Change reasoning effort", Aliases: []string{"reasoning", "effort"}},
	}
	seen := map[string]bool{"cd": true, "compact": true, "debug": true, "diff": true, "files": true, "fork": true, "login": true, "model": true, "name": true, "new": true, "quit": true, "reload": true, "sessions": true, "subagents": true, "tabs": true, "theme": true, "thinking": true}
	if len(contributions) > 0 {
		for _, command := range contributions[0] {
			if !seen[command.Name] {
				seen[command.Name] = true
				commands = append(commands, command)
			}
		}
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func paletteCommandExists(commandID paletteCommandID, contributions ...[]paletteCommand) bool {
	for _, command := range paletteCommands(contributions...) {
		if command.ID == commandID {
			return true
		}
	}
	return false
}

func paletteCommandAvailable(commandID paletteCommandID, running bool, contributions ...[]paletteCommand) bool {
	return paletteCommandExists(commandID, contributions...) && paletteCommandDisabledReason(commandID, running) == ""
}

func paletteCommandDisabledReason(commandID paletteCommandID, running bool) string {
	if !running {
		return ""
	}
	if _, prompt := promptPaletteCommandName(commandID); prompt {
		return "idle only"
	}
	switch commandID {
	case paletteCommandCD, paletteCommandCompact, paletteCommandFork:
		return "idle only"
	default:
		return ""
	}
}

func paletteCommandDisabledToast(commandID paletteCommandID, running bool) (toastInput, bool) {
	reason := paletteCommandDisabledReason(commandID, running)
	if reason == "" {
		return toastInput{}, false
	}
	if commandID == paletteCommandCompact {
		return toastInput{Title: "Compaction failed", Subtitle: "Cannot compact while the agent is running.", Variant: toastError}, true
	}
	return toastInput{Title: "Command unavailable", Subtitle: "Available when the session is idle.", Variant: toastWarning}, true
}

func filteredPaletteCommands(_ bool, query string, contributions ...[]paletteCommand) []paletteCommand {
	commands := paletteCommands(contributions...)
	return ui.DefaultFuzzySelectFilter(paletteFilterQuery(query), commands, func(command paletteCommand) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{
			Title: command.Name, Description: command.Description, Aliases: command.Aliases,
		}
	})
}

func promptPaletteCommands(commands []protocol.PromptCommand) []paletteCommand {
	result := make([]paletteCommand, 0, len(commands))
	for _, command := range commands {
		result = append(result, paletteCommand{
			ID: paletteCommandID("prompt:" + command.Name), Name: command.Name,
			Description: command.Description, ArgumentHint: command.ArgumentHint, Aliases: []string{command.Source, command.Location},
		})
	}
	return result
}

func promptPaletteCommandName(id paletteCommandID) (string, bool) {
	name, ok := strings.CutPrefix(string(id), "prompt:")
	return name, ok && name != ""
}

func paletteFilterQuery(value string) string {
	command, _ := splitPaletteQuery(value)
	return command
}

func splitPaletteQuery(value string) (string, string) {
	value = strings.TrimLeft(value, " \t")
	separator := strings.IndexAny(value, " \t")
	if separator < 0 {
		return value, ""
	}
	return value[:separator], strings.TrimSpace(value[separator+1:])
}

func paletteSelectionIndex(selection paletteCommandID, commands []paletteCommand) (int, bool) {
	for index, command := range commands {
		if command.ID == selection {
			return index, true
		}
	}
	return 0, false
}

func paletteNameWidth(commands []paletteCommand) int {
	width := 0
	for _, command := range commands {
		commandWidth := 0
		for _, character := range vaxis.Characters(paletteCommandLabel(command)) {
			commandWidth += character.Width
		}
		width = max(width, commandWidth)
	}
	return min(width, paletteMaxNameWidth)
}

func paletteCommandLabel(command paletteCommand) string {
	if command.ArgumentHint == "" {
		return command.Name
	}
	return command.Name + " " + command.ArgumentHint
}

func paletteCommandNameSpans(command paletteCommand, primary, secondary ui.Style) []ui.TextSpan {
	spans := []ui.TextSpan{{Text: command.Name, Style: primary}}
	if command.ArgumentHint != "" {
		spans = append(spans, ui.TextSpan{Text: " " + command.ArgumentHint, Style: secondary})
	}
	return spans
}
