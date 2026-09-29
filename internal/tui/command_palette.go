package tui

import (
	"sort"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	paletteCommandCD            paletteCommandID = "cd"
	paletteCommandCompact       paletteCommandID = "compact"
	paletteCommandLogin         paletteCommandID = "login"
	paletteCommandModel         paletteCommandID = "model"
	paletteCommandModelsRefresh paletteCommandID = "refresh-models"
	paletteCommandName          paletteCommandID = "name"
	paletteCommandNew           paletteCommandID = "new"
	paletteCommandQuit          paletteCommandID = "quit"
	paletteCommandReload        paletteCommandID = "reload"
	paletteCommandDebug         paletteCommandID = "debug"
	paletteCommandMCP           paletteCommandID = "mcp"
	paletteCommandDiff          paletteCommandID = "diff"
	paletteCommandFork          paletteCommandID = "fork"
	paletteCommandFiles         paletteCommandID = "files"
	paletteCommandSessions      paletteCommandID = "sessions"
	paletteCommandScratchpad    paletteCommandID = "scratchpad"
	paletteCommandSubagents     paletteCommandID = "subagents"
	paletteCommandTabs          paletteCommandID = "tabs"
	paletteCommandTheme         paletteCommandID = "theme"
	paletteCommandThinking      paletteCommandID = "thinking"
)

type paletteCommandID string

type paletteCommand struct {
	DisabledReason string
	Plugin         *protocol.PluginCommand
	ArgumentHint   string
	// Source names where a non-built-in command comes from: user or project
	// for prompt commands, and the owning plugin for plugin commands.
	Source      string
	ID          paletteCommandID
	Name        string
	Description string
	Aliases     []string
}

type paletteSnapshot struct {
	Query         string
	Selection     paletteCommandID
	Running       bool
	Contributions []paletteCommand
}

type paletteCallbacks struct {
	RunCommand func(ui.EventContext, paletteCommandID)
}

type commandPaletteSurface struct {
	Snapshot  paletteSnapshot
	Callbacks paletteCallbacks
}

// Build maps palette commands onto the canonical picker: name as label,
// argument hint, description, and the source of non-built-in commands as
// trailing metadata. A selection hidden by the query falls back to the first
// enabled match.
func (w commandPaletteSurface) Build(ui.BuildContext) ui.Widget {
	catalog := paletteCatalog(w.Snapshot.Running, w.Snapshot.Contributions)
	model := pickerKeyModel{Query: w.Snapshot.Query, Selection: string(w.Snapshot.Selection), Filter: filterPaletteItems}
	items := model.Items(catalog)
	if pickerItemIndex(items, model.Selection) < 0 {
		model.Selection = firstEnabledPickerKey(items)
	}
	return palettePicker{
		Query: model.Query, Search: &pickerSearch{Placeholder: "Search commands…"},
		Catalog: catalog, Filter: model.Filter, Selection: model.Selection,
		Footer: "↑↓ move · enter run · esc close",
		OnActivate: func(ctx ui.EventContext, key string) {
			if w.Callbacks.RunCommand != nil {
				w.Callbacks.RunCommand(ctx, paletteCommandID(key))
			}
		},
	}
}

// filterPaletteItems is the command palette's filter hook. The query is a
// command followed by its arguments, so only the command word is matched with
// the shared filter; "review auth module" keeps showing review.
func filterPaletteItems(query string, catalog []pickerItem) []pickerItem {
	return filterPickerItems(paletteFilterQuery(query), catalog)
}

// paletteCatalog maps every palette command onto a picker item.
func paletteCatalog(running bool, contributions []paletteCommand) []pickerItem {
	commands := paletteCommands(contributions)
	items := make([]pickerItem, 0, len(commands))
	for _, command := range commands {
		items = append(items, palettePickerItem(command, running, contributions))
	}
	return items
}

func palettePickerItem(command paletteCommand, running bool, contributions []paletteCommand) pickerItem {
	return pickerItem{
		Key: string(command.ID), Label: command.Name, Hint: command.ArgumentHint,
		Description: command.Description, Meta: command.Source,
		DisabledReason: paletteCommandDisabledReason(command.ID, running, contributions),
		SearchText:     command.Description, SearchAliases: command.Aliases,
	}
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
	p.SetQuery(running, "")
}

func (p *paletteController) Close() {
	contributions := p.Contributions
	*p = paletteController{Contributions: contributions}
}

func (p *paletteController) SetContributions(commands []paletteCommand, running bool) {
	p.Contributions = append([]paletteCommand(nil), commands...)
	if p.Open && !paletteCommandExists(p.Selection, p.Contributions) && !strings.HasPrefix(string(p.Selection), "plugin:") {
		p.SetQuery(running, p.Query)
	}
}

// keys returns the palette's canonical picker key model and catalog.
func (p *paletteController) keys(running bool) (pickerKeyModel, []pickerItem) {
	model := pickerKeyModel{Query: p.Query, Selection: string(p.Selection), Filter: filterPaletteItems}
	return model, paletteCatalog(running, p.Contributions)
}

func (p *paletteController) apply(model pickerKeyModel) {
	p.Query, p.Selection = model.Query, paletteCommandID(model.Selection)
}

// SetQuery replaces the query and highlights the first enabled match.
func (p *paletteController) SetQuery(running bool, query string) {
	model, catalog := p.keys(running)
	model.SetQuery(query, catalog)
	p.apply(model)
}

func (p *paletteController) Move(running bool, delta int) {
	if !p.Open {
		return
	}
	model, catalog := p.keys(running)
	model.Move(catalog, delta)
	p.apply(model)
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

// HandleKey applies one key through the canonical picker key model against
// current controller state, so keys do not depend on whether a newly opened
// palette has painted yet. It returns the command to run on activation.
func (p *paletteController) HandleKey(running bool, key ui.Key) (paletteCommand, bool, bool) {
	if !p.Open {
		return paletteCommand{}, false, false
	}
	model, catalog := p.keys(running)
	result := model.HandleKey(key, catalog)
	p.apply(model)
	switch {
	case result.Dismiss:
		p.Close()
	case result.Activate:
		command, ok := p.Selected(running, p.Query)
		if !ok && strings.HasPrefix(string(p.Selection), "plugin:") {
			return paletteCommand{ID: p.Selection}, true, true
		}
		return command, ok, true
	}
	return paletteCommand{}, false, result.Handled
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

func paletteCommands(contributions ...[]paletteCommand) []paletteCommand {
	commands := []paletteCommand{
		{ID: paletteCommandCD, Name: "cd", Description: "Change working directory", Aliases: []string{"cwd", "directory", "folder"}},
		{ID: paletteCommandCompact, Name: "compact", Description: "Compact session context", Aliases: []string{"summarize", "shrink"}},
		{ID: paletteCommandLogin, Name: "login", Description: "Connect another provider", Aliases: []string{"auth", "connect", "provider"}},
		{ID: paletteCommandModel, Name: "model", Description: "Change session model", Aliases: []string{"engine"}},
		{ID: paletteCommandModelsRefresh, Name: "refresh-models", Description: "Update the provider catalog of models", Aliases: []string{"catalog", "models"}},
		{ID: paletteCommandName, Name: "name", Description: "Rename session", Aliases: []string{"rename", "title"}},
		{ID: paletteCommandNew, Name: "new", Description: "Start a new session"},
		{ID: paletteCommandQuit, Name: "quit", Description: "Exit Kit", Aliases: []string{"close", "exit"}},
		{ID: paletteCommandReload, Name: "reload", Description: "Reload session context", Aliases: []string{"agents", "context", "refresh"}},
		{ID: paletteCommandDebug, Name: "debug", Description: "Show session diagnostics", Aliases: []string{"details", "usage"}},
		{ID: paletteCommandMCP, Name: "mcp", Description: "Show configured MCP servers", Aliases: []string{"tools", "connections", "status"}},
		{ID: paletteCommandDiff, Name: "diff", Description: "Review working-tree changes", Aliases: []string{"changes", "working tree"}},
		{ID: paletteCommandFork, Name: "fork", Description: "Fork the current session into a linked child session", Aliases: []string{"branch"}},
		{ID: paletteCommandFiles, Name: "files", Description: "Open a workspace file", Aliases: []string{"file", "open", "browse"}},
		{ID: paletteCommandSessions, Name: "sessions", Description: "Browse sessions", Aliases: []string{"list", "resume", "switch", "threads"}},
		{ID: paletteCommandSubagents, Name: "subagents", Description: "Inspect delegated work", Aliases: []string{"agents", "delegates", "children"}},
		{ID: paletteCommandTabs, Name: "tabs", Description: "Open a workspace tab", Aliases: []string{"panes", "workspace"}},
		{ID: paletteCommandTheme, Name: "theme", Description: "Choose UI colors", Aliases: []string{"appearance", "colors"}},
		{ID: paletteCommandThinking, Name: "thinking", Description: "Change reasoning effort", Aliases: []string{"reasoning", "effort"}},
	}
	seen := map[string]bool{"cd": true, "compact": true, "debug": true, "diff": true, "files": true, "fork": true, "login": true, "model": true, "refresh-models": true, "name": true, "new": true, "quit": true, "reload": true, "sessions": true, "subagents": true, "tabs": true, "theme": true, "thinking": true}
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
	return paletteCommandExists(commandID, contributions...) && paletteCommandDisabledReason(commandID, running, contributions...) == ""
}

func paletteCommandDisabledReason(commandID paletteCommandID, running bool, contributions ...[]paletteCommand) string {
	if len(contributions) > 0 {
		for _, command := range contributions[0] {
			if command.ID == commandID && command.DisabledReason != "" {
				return command.DisabledReason
			}
		}
	}
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

// filteredPaletteCommands returns the commands the palette shows for query,
// in display order.
func filteredPaletteCommands(running bool, query string, contributions ...[]paletteCommand) []paletteCommand {
	var extra []paletteCommand
	if len(contributions) > 0 {
		extra = contributions[0]
	}
	commands := paletteCommands(extra)
	byID := make(map[string]paletteCommand, len(commands))
	for _, command := range commands {
		byID[string(command.ID)] = command
	}
	items := filterPaletteItems(query, paletteCatalog(running, extra))
	result := make([]paletteCommand, 0, len(items))
	for _, item := range items {
		result = append(result, byID[item.Key])
	}
	return result
}

func scratchpadPaletteCommand() paletteCommand {
	return paletteCommand{ID: paletteCommandScratchpad, Name: "scratchpad", Description: "Open shared working notes", Aliases: []string{"notes", "memory"}}
}

func promptPaletteCommands(commands []protocol.PromptCommand) []paletteCommand {
	result := make([]paletteCommand, 0, len(commands))
	for _, command := range commands {
		result = append(result, paletteCommand{
			ID: paletteCommandID("prompt:" + command.Name), Name: command.Name, Source: string(command.Source),
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

func pluginPaletteCommands(commands []protocol.PluginCommand) []paletteCommand {
	result := make([]paletteCommand, 0, len(commands))
	for _, command := range commands {
		hint := ""
		if command.ArgName != "" {
			hint = "<" + command.ArgName + ">"
		}
		result = append(result, paletteCommand{ID: paletteCommandID("plugin:" + command.Instance + ":" + command.ID), Name: command.ID, Source: command.PluginID, Description: command.Description, ArgumentHint: hint, Aliases: []string{command.LocalID, command.PluginID, command.Category}, Plugin: &command})
	}
	return result
}

// Plugin arguments are literal: remove the command and one separator only.
func pluginPaletteArgs(query string) string {
	query = strings.TrimLeft(query, " \t")
	if index := strings.IndexAny(query, " \t"); index >= 0 {
		return query[index+1:]
	}
	return ""
}
