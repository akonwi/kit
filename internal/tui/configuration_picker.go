package tui

import (
	"fmt"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type configurationPickerMode uint8

const (
	configurationPickerClosed configurationPickerMode = iota
	configurationPickerModel
	configurationPickerThinking
)

type configurationPickerController struct {
	Mode            configurationPickerMode
	Loading         bool
	Pending         bool
	Error           string
	Query           string
	Selection       string
	CurrentModel    string
	CurrentThinking string
	Models          []protocol.ModelCapability
	generation      uint64
}

type configurationPickerSnapshot struct {
	Mode            configurationPickerMode
	Loading         bool
	Pending         bool
	Error           string
	Query           string
	Selection       string
	CurrentModel    string
	CurrentThinking string
	Models          []protocol.ModelCapability
}

func (controller *configurationPickerController) Begin(mode configurationPickerMode, currentModel, currentThinking string) uint64 {
	controller.generation++
	controller.Mode = mode
	controller.Loading = true
	controller.Pending = false
	controller.Error = ""
	controller.Query = ""
	controller.Selection = ""
	controller.CurrentModel = currentModel
	controller.CurrentThinking = currentThinking
	controller.Models = nil
	return controller.generation
}

func (controller *configurationPickerController) Resolve(generation uint64, catalog protocol.ModelCatalog, err error) bool {
	if controller.Mode == configurationPickerClosed || generation != controller.generation {
		return false
	}
	controller.Loading = false
	if err != nil {
		controller.Error = err.Error()
		return true
	}
	controller.Models = append([]protocol.ModelCapability(nil), catalog.Models...)
	if controller.Mode == configurationPickerModel {
		controller.Selection = controller.CurrentModel
		if modelCapabilityIndex(controller.Models, controller.Selection) < 0 && len(controller.Models) > 0 {
			controller.Selection = controller.Models[0].ID
		}
	} else {
		levels := controller.thinkingLevels()
		controller.Selection = controller.CurrentThinking
		if stringIndex(levels, controller.Selection) < 0 && len(levels) > 0 {
			controller.Selection = levels[0]
		}
	}
	return true
}

func (controller *configurationPickerController) Close() {
	if controller.Pending {
		return
	}
	controller.generation++
	*controller = configurationPickerController{generation: controller.generation}
}

func (controller *configurationPickerController) Snapshot() configurationPickerSnapshot {
	return configurationPickerSnapshot{
		Mode: controller.Mode, Loading: controller.Loading, Pending: controller.Pending,
		Error: controller.Error, Query: controller.Query, Selection: controller.Selection,
		CurrentModel: controller.CurrentModel, CurrentThinking: controller.CurrentThinking,
		Models: append([]protocol.ModelCapability(nil), controller.Models...),
	}
}

func (controller *configurationPickerController) SetQuery(value string) {
	if controller.Mode != configurationPickerModel || controller.Pending {
		return
	}
	controller.Query = value
	models := controller.filteredModels()
	if len(models) > 0 {
		controller.Selection = models[0].ID
	} else {
		controller.Selection = ""
	}
	controller.Error = ""
}

func (controller *configurationPickerController) Select(value string) {
	if controller.Pending {
		return
	}
	if controller.Mode == configurationPickerModel {
		if modelCapabilityIndex(controller.filteredModels(), value) >= 0 {
			controller.Selection = value
		}
	} else if stringIndex(controller.thinkingLevels(), value) >= 0 {
		controller.Selection = value
	}
	controller.Error = ""
}

func (controller *configurationPickerController) Move(delta int) {
	if controller.Pending || delta == 0 {
		return
	}
	values := controller.values()
	if len(values) == 0 {
		return
	}
	index := stringIndex(values, controller.Selection)
	if index < 0 {
		index = 0
	} else {
		index = (index + delta) % len(values)
		if index < 0 {
			index += len(values)
		}
	}
	controller.Selection = values[index]
	controller.Error = ""
}

func (controller *configurationPickerController) BeginApply() (uint64, string, bool) {
	if controller.Mode == configurationPickerClosed || controller.Loading || controller.Pending || controller.Selection == "" {
		return 0, "", false
	}
	controller.generation++
	controller.Pending = true
	controller.Error = ""
	return controller.generation, controller.Selection, true
}

func (controller *configurationPickerController) ResolveApply(generation uint64, err error) bool {
	if controller.Mode == configurationPickerClosed || !controller.Pending || generation != controller.generation {
		return false
	}
	controller.Pending = false
	if err != nil {
		controller.Error = err.Error()
		return true
	}
	controller.Close()
	return true
}

func (controller *configurationPickerController) HandleKey(key ui.Key) (bool, bool) {
	if controller.Mode == configurationPickerClosed || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return false, false
	}
	switch {
	case key.MatchString("Escape"):
		controller.Close()
		return false, true
	case key.MatchString("Up"):
		controller.Move(-1)
		return false, true
	case key.MatchString("Down"):
		controller.Move(1)
		return false, true
	case key.MatchString("Enter"):
		return true, true
	default:
		return false, false
	}
}

func (controller *configurationPickerController) HandleEditorKey(key ui.Key) bool {
	if controller.Mode != configurationPickerModel || controller.Pending || key.EventType == ui.EventRelease {
		return false
	}
	query := controller.Query
	if key.EventType == vaxis.EventPaste {
		query += palettePasteText(key)
		controller.SetQuery(query)
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
	controller.SetQuery(query)
	return true
}

func (controller *configurationPickerController) filteredModels() []protocol.ModelCapability {
	return ui.DefaultFuzzySelectFilter(controller.Query, controller.Models, func(model protocol.ModelCapability) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: model.Name, Description: model.ID, Aliases: []string{model.Provider}}
	})
}

func (controller *configurationPickerController) thinkingLevels() []string {
	index := modelCapabilityIndex(controller.Models, controller.CurrentModel)
	if index < 0 {
		return nil
	}
	levels := make([]string, 0, len(controller.Models[index].ThinkingLevels))
	for _, level := range controller.Models[index].ThinkingLevels {
		levels = append(levels, string(level))
	}
	return levels
}

func (controller *configurationPickerController) values() []string {
	if controller.Mode == configurationPickerModel {
		models := controller.filteredModels()
		values := make([]string, 0, len(models))
		for _, model := range models {
			values = append(values, model.ID)
		}
		return values
	}
	return controller.thinkingLevels()
}

func modelCapabilityIndex(models []protocol.ModelCapability, id string) int {
	for index, model := range models {
		if model.ID == id {
			return index
		}
	}
	return -1
}

func stringIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

type configurationPickerSurface struct {
	Snapshot     configurationPickerSnapshot
	QueryChanged ui.TextChangedCallback
	Select       func(ui.EventContext, string)
	Apply        ui.VoidCallback
}

func (surface configurationPickerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	title := "Select model"
	if surface.Snapshot.Mode == configurationPickerThinking {
		title = "Thinking level"
	}
	var body ui.Widget
	switch {
	case surface.Snapshot.Loading:
		body = ui.Center(ui.Text{Value: "Loading…", Style: ui.Style{Foreground: theme.MutedForeground}})
	case surface.Snapshot.Error != "" && len(surface.Snapshot.Models) == 0:
		body = ui.Center(ui.Text{Value: surface.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true})
	case surface.Snapshot.Mode == configurationPickerModel:
		body = surface.modelBody(theme)
	default:
		body = surface.thinkingBody(theme)
	}
	footerText := "↑↓ move · enter apply · esc close"
	if surface.Snapshot.Pending {
		footerText = "Applying configuration…"
	}
	dialogBody := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Padding(ui.Insets{Top: 1, Left: 2, Right: 2}, ui.Text{Value: title, Style: ui.Style{Attribute: ui.AttrBold}}),
		ui.Expanded(body),
	}}
	content := pickerDialogContent(theme, dialogBody, ui.Text{
		Value: footerText, Style: ui.Style{Foreground: theme.MutedForeground},
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	})
	return pickerDialogPositioner{
		Percent: 80, MinWidth: 56, MaxWidth: 104, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}

func (surface configurationPickerSurface) modelBody(theme ui.Theme) ui.Widget {
	models := surface.filteredModels()
	queryCursor := len(surface.Snapshot.Query)
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	rows := make([]ui.Widget, 0, len(models))
	for _, model := range models {
		model := model
		status := formatContextWindow(model.ContextWindow) + " context"
		if !model.Available {
			status += " · sign in required"
		}
		rows = append(rows, configurationOptionRow{
			Label: model.Name, Details: model.ID + " · " + status,
			Current:  model.ID == surface.Snapshot.CurrentModel,
			Selected: model.ID == surface.Snapshot.Selection,
			Disabled: !model.Available,
			OnPressed: func(event ui.EventContext) {
				if surface.Select != nil {
					surface.Select(event, model.ID)
				}
				if model.Available && surface.Apply != nil {
					surface.Apply(event)
				}
			},
		})
	}
	if len(rows) == 0 {
		rows = []ui.Widget{ui.Text{Value: "No models", Style: ui.Style{Foreground: theme.MutedForeground}}}
	}
	children := []ui.Widget{
		ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			textInput(fieldTheme, textInputConfig{
				Value: surface.Snapshot.Query, Placeholder: "Search models…", CursorOffset: &queryCursor,
				OnChanged: surface.QueryChanged, AutoFocus: true,
			}),
		}},
		ui.SizedBox{Height: 1},
	}
	if surface.Snapshot.Error != "" {
		children = append(children, ui.Text{Value: surface.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
	}
	children = append(children, ui.Expanded(ui.ScrollView{Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}}))
	return ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	})
}

func (surface configurationPickerSurface) filteredModels() []protocol.ModelCapability {
	return ui.DefaultFuzzySelectFilter(surface.Snapshot.Query, surface.Snapshot.Models, func(model protocol.ModelCapability) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: model.Name, Description: model.ID, Aliases: []string{model.Provider}}
	})
}

func (surface configurationPickerSurface) thinkingBody(theme ui.Theme) ui.Widget {
	index := modelCapabilityIndex(surface.Snapshot.Models, surface.Snapshot.CurrentModel)
	var levels []protocol.ThinkingLevel
	if index >= 0 {
		levels = surface.Snapshot.Models[index].ThinkingLevels
	}
	rows := make([]ui.Widget, 0, len(levels))
	for _, level := range levels {
		level := string(level)
		rows = append(rows, configurationOptionRow{
			Label: level, Details: "Reasoning effort",
			Current:  level == surface.Snapshot.CurrentThinking,
			Selected: level == surface.Snapshot.Selection,
			OnPressed: func(event ui.EventContext) {
				if surface.Select != nil {
					surface.Select(event, level)
				}
				if surface.Apply != nil {
					surface.Apply(event)
				}
			},
		})
	}
	if len(rows) == 0 {
		rows = []ui.Widget{ui.Text{Value: "No thinking levels", Style: ui.Style{Foreground: theme.MutedForeground}}}
	}
	if surface.Snapshot.Error != "" {
		rows = append([]ui.Widget{ui.Text{Value: surface.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}}, ui.SizedBox{Height: 1}}, rows...)
	}
	return ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.ScrollView{Child: ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows,
	}})
}

type configurationOptionRow struct {
	Label     string
	Details   string
	Current   bool
	Selected  bool
	Disabled  bool
	OnPressed ui.VoidCallback
}

func (row configurationOptionRow) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	marker := "  "
	if row.Current {
		marker = glyphCheck + " "
	}
	foreground := theme.Foreground
	if row.Disabled {
		foreground = theme.DisabledForeground
	}
	content := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Width: 24, Child: ui.Text{Value: marker + row.Label, Style: ui.Style{Foreground: foreground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
		ui.SizedBox{Width: 1},
		ui.Expanded(ui.Text{Value: row.Details, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
	}}
	rowTheme := theme
	rowTheme.Foreground = theme.Background
	rowTheme.Primary = theme.Foreground
	rowTheme.PrimaryHovered = theme.Foreground
	return ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
		Title: content, Selected: row.Selected, OnPressed: row.OnPressed,
		Padding: ui.Insets{Right: 1}, MinHeight: 1,
	}}
}

func formatContextWindow(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%dk", tokens/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}
