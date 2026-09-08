package tui

import (
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	paletteMaxVisible = 10

	paletteCommandAbort    paletteCommandID = "abort"
	paletteCommandLogin    paletteCommandID = "login"
	paletteCommandQuit     paletteCommandID = "quit"
	paletteCommandReload   paletteCommandID = "reload"
	paletteCommandSessions paletteCommandID = "sessions"
)

type paletteCommandID string

type paletteCommand struct {
	ID          paletteCommandID
	Name        string
	Description string
	Aliases     []string
}

type paletteSnapshot struct {
	Query     string
	Selection paletteCommandID
	Running   bool
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

func (w commandPaletteSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	commands := filteredPaletteCommands(w.Snapshot.Running, w.Snapshot.Query)
	selection, hasSelection := paletteSelectionIndex(w.Snapshot.Selection, commands)
	if !hasSelection {
		selection = 0
	}
	nameWidth := paletteNameWidth(availablePaletteCommands(w.Snapshot.Running))

	results := []ui.Widget(nil)
	if len(commands) == 0 {
		results = []ui.Widget{ui.Text{
			Value: "No results", Style: ui.Style{Foreground: theme.MutedForeground},
		}}
	} else {
		visible, _ := paletteCommandWindow(commands, selection, paletteMaxVisible)
		results = make([]ui.Widget, 0, len(visible))
		for _, command := range visible {
			command := command
			results = append(results, paletteOptionRow{
				Command: command, NameWidth: nameWidth,
				Selected: hasSelection && command.ID == commands[selection].ID,
				OnPressed: func(event ui.EventContext) {
					if w.Callbacks.RunCommand != nil {
						w.Callbacks.RunCommand(event, command.ID)
					}
				},
			})
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
			ui.Expanded(ui.ScrollView{Child: ui.Flex{
				Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: results,
			}}),
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
	Command   paletteCommand
	NameWidth int
	Selected  bool
	OnPressed ui.VoidCallback
}

func (w paletteOptionRow) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	rowTheme := theme
	rowTheme.Foreground = theme.Background
	rowTheme.Primary = theme.Foreground
	rowTheme.PrimaryHovered = theme.Foreground
	primary := theme.Foreground
	if w.Selected {
		primary = theme.Background
	}
	content := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: w.NameWidth, Child: ui.Text{
			Value: w.Command.Name, Style: ui.Style{Foreground: primary},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}},
		ui.SizedBox{Width: 1},
		ui.Expanded(ui.Text{
			Value: w.Command.Description, Style: ui.Style{Foreground: theme.MutedForeground},
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}),
	}}
	return ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
		Title: content, Selected: w.Selected, OnPressed: w.OnPressed,
		Padding: ui.Insets{Right: 1}, MinHeight: 1,
	}}
}

type paletteController struct {
	Open      bool
	Query     string
	Selection paletteCommandID
}

func (p *paletteController) OpenFor(running bool) {
	if p.Open {
		return
	}
	p.Open = true
	p.Query = ""
	p.Selection = firstPaletteCommandID(filteredPaletteCommands(running, ""))
}

func (p *paletteController) Close() {
	*p = paletteController{}
}

func (p *paletteController) SetQuery(running bool, query string) {
	p.Query = query
	p.Selection = firstPaletteCommandID(filteredPaletteCommands(running, query))
}

func (p *paletteController) Move(running bool, delta int) {
	commands := filteredPaletteCommands(running, p.Query)
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
	commands := filteredPaletteCommands(running, query)
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

func availablePaletteCommands(running bool) []paletteCommand {
	if running {
		return []paletteCommand{
			{ID: paletteCommandAbort, Name: "abort", Description: "Stop the active run", Aliases: []string{"cancel", "stop"}},
			{ID: paletteCommandQuit, Name: "quit", Description: "Exit Kit", Aliases: []string{"close", "exit"}},
		}
	}
	return []paletteCommand{
		{ID: paletteCommandLogin, Name: "login", Description: "Connect another provider", Aliases: []string{"auth", "connect", "provider"}},
		{ID: paletteCommandQuit, Name: "quit", Description: "Exit Kit", Aliases: []string{"close", "exit"}},
		{ID: paletteCommandReload, Name: "reload", Description: "Reload session context", Aliases: []string{"agents", "context", "refresh"}},
		{ID: paletteCommandSessions, Name: "sessions", Description: "Browse sessions", Aliases: []string{"list", "resume", "switch", "threads"}},
	}
}

func paletteCommandAvailable(commandID paletteCommandID, running bool) bool {
	for _, command := range availablePaletteCommands(running) {
		if command.ID == commandID {
			return true
		}
	}
	return false
}

func filteredPaletteCommands(running bool, query string) []paletteCommand {
	commands := availablePaletteCommands(running)
	return ui.DefaultFuzzySelectFilter(paletteFilterQuery(query), commands, func(command paletteCommand) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{
			Title: command.Name, Description: command.Description, Aliases: command.Aliases,
		}
	})
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
		width = max(width, len([]rune(command.Name)))
	}
	return width
}

func paletteCommandWindow(commands []paletteCommand, selection, maximum int) ([]paletteCommand, int) {
	if maximum <= 0 || len(commands) <= maximum {
		return commands, 0
	}
	offset := selection - maximum/2
	offset = max(0, min(offset, len(commands)-maximum))
	return commands[offset : offset+maximum], offset
}
