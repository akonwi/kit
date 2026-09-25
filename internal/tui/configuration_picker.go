package tui

import (
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// ModelOverrideService persists model-specific context-window settings.
type ModelOverrideService interface {
	SetContextWindow(selector string, contextWindow int) error
}

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
	EditingContext  bool
	EditModel       string
	EditValue       string
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
	EditingContext  bool
	EditModel       string
	EditValue       string
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

func (controller *configurationPickerController) BeginRefresh() uint64 {
	controller.generation++
	controller.Loading = true
	controller.Error = ""
	controller.EditingContext = false
	controller.EditModel = ""
	controller.EditValue = ""
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
		models := controller.filteredModels()
		if modelCapabilityIndex(models, controller.Selection) < 0 {
			controller.Selection = controller.CurrentModel
			if modelCapabilityIndex(models, controller.Selection) < 0 {
				controller.Selection = ""
				if len(models) > 0 {
					controller.Selection = models[0].ID
				}
			}
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
		Models:         append([]protocol.ModelCapability(nil), controller.Models...),
		EditingContext: controller.EditingContext, EditModel: controller.EditModel, EditValue: controller.EditValue,
	}
}

func (controller *configurationPickerController) SetQuery(value string) {
	if controller.Mode != configurationPickerModel || controller.Loading || controller.Pending {
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
	if controller.Loading || controller.Pending {
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
	if controller.Loading || controller.Pending || delta == 0 {
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

func (controller *configurationPickerController) BeginContextEdit() bool {
	if controller.Mode != configurationPickerModel || controller.Loading || controller.Pending || controller.Selection == "" {
		return false
	}
	index := modelCapabilityIndex(controller.Models, controller.Selection)
	if index < 0 {
		return false
	}
	controller.EditingContext = true
	controller.EditModel = controller.Selection
	controller.EditValue = fmt.Sprint(controller.Models[index].ContextWindow)
	controller.Error = ""
	return true
}

func (controller *configurationPickerController) HandleKey(key ui.Key) (bool, bool) {
	if controller.Mode == configurationPickerClosed || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return false, false
	}
	if controller.EditingContext && !key.MatchString("Escape") && !key.MatchString("Enter") {
		return false, false
	}
	switch {
	case key.MatchString("Escape"):
		if controller.EditingContext {
			controller.EditingContext = false
			controller.EditModel = ""
			controller.EditValue = ""
		} else {
			controller.Close()
		}
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
	if controller.Mode != configurationPickerModel || controller.Loading || controller.Pending || key.EventType == ui.EventRelease {
		return false
	}
	if controller.EditingContext {
		value := controller.EditValue
		if key.EventType == vaxis.EventPaste {
			value += palettePasteText(key)
		} else if key.MatchString("Backspace") {
			runes := []rune(value)
			if len(runes) > 0 {
				value = string(runes[:len(runes)-1])
			}
		} else if key.Text != "" && key.Text[0] >= '0' && key.Text[0] <= '9' {
			value += key.Text
		}
		controller.EditValue = value
		controller.Error = ""
		return true
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
	return filterModels(controller.Query, controller.Models)
}

func (controller *configurationPickerController) thinkingLevels() []string {
	index := modelCapabilityIndex(controller.Models, controller.CurrentModel)
	if index < 0 || !controller.Models[index].Available {
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

func (configurationPickerSurface) CreateState() ui.State {
	return &configurationPickerSurfaceState{selection: -1}
}

type configurationPickerSurfaceState struct {
	ui.StateBase
	scroll      ui.ScrollController
	list        ui.SliverListController
	selection   int
	query       string
	count       int
	viewport    int
	needsReveal bool
}

func (s *configurationPickerSurfaceState) TickFrame(time.Time) bool {
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

func (s *configurationPickerSurfaceState) optionsList(rows []ui.Widget, selection int, query string) ui.Widget {
	if s.selection != selection || s.query != query || s.count != len(rows) {
		s.selection, s.query, s.count = selection, query, len(rows)
		s.needsReveal = true
	}
	return ui.Scrollbar{Child: ui.CustomScrollView{Controller: &s.scroll, Slivers: []ui.Widget{
		ui.SliverListBuilder{Controller: &s.list, Count: len(rows), ItemExtent: 1,
			Builder: func(_ ui.BuildContext, index int) ui.Widget { return rows[index] }},
	}}}
}

func (s *configurationPickerSurfaceState) Build(ctx ui.BuildContext) ui.Widget {
	surface := s.Widget().(configurationPickerSurface)
	if surface.Snapshot.Loading || surface.Snapshot.EditingContext {
		s.needsReveal = true
	}
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
	case surface.Snapshot.EditingContext:
		cursor := len(surface.Snapshot.EditValue)
		body = ui.Padding(ui.Insets{Top: 1, Left: 2, Right: 2}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Text{Value: surface.Snapshot.EditModel, Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.SizedBox{Height: 1},
			textInput(theme, textInputConfig{Value: surface.Snapshot.EditValue, Placeholder: "Blank clears the override", CursorOffset: &cursor, OnChanged: surface.QueryChanged, AutoFocus: true}),
			ui.Text{Value: surface.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true},
		}})
	case surface.Snapshot.Mode == configurationPickerModel:
		body = surface.modelBody(theme, s)
	default:
		body = surface.thinkingBody(theme, s)
	}
	footerText := "↑↓ move · enter apply · ctrl+o overrides · esc close"
	if surface.Snapshot.EditingContext {
		footerText = "enter save · blank clears · esc back"
	}
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
	percent, minWidth, maxWidth := 80, 56, 104
	if surface.Snapshot.Mode == configurationPickerThinking {
		percent, minWidth, maxWidth = 60, 40, 56
	}
	return pickerDialogPositioner{
		Percent: percent, MinWidth: minWidth, MaxWidth: maxWidth, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}

func (surface configurationPickerSurface) modelBody(theme ui.Theme, state *configurationPickerSurfaceState) ui.Widget {
	models := surface.filteredModels()
	queryCursor := len(surface.Snapshot.Query)
	rows := make([]ui.Widget, 0, len(models))
	for _, model := range models {
		model := model
		status := formatContextWindow(model.ContextWindow) + " context"
		rows = append(rows, configurationOptionRow{
			Label: model.Name, Details: model.ID, Meta: status,
			Current:  model.ID == surface.Snapshot.CurrentModel,
			Selected: model.ID == surface.Snapshot.Selection,
			OnPressed: func(event ui.EventContext) {
				if surface.Select != nil {
					surface.Select(event, model.ID)
				}
				if surface.Apply != nil {
					surface.Apply(event)
				}
			},
		})
	}
	if len(rows) == 0 {
		rows = []ui.Widget{ui.Text{Value: "No models", Style: ui.Style{Foreground: theme.MutedForeground}}}
	}
	children := []ui.Widget{
		pickerSearchInput(theme, textInputConfig{
			Value: surface.Snapshot.Query, Placeholder: "Search models…", CursorOffset: &queryCursor,
			OnChanged: surface.QueryChanged, AutoFocus: true,
		}),
		ui.SizedBox{Height: 1},
	}
	if surface.Snapshot.Error != "" {
		children = append(children, ui.Text{Value: surface.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
	}
	children = append(children, ui.Expanded(state.optionsList(rows, max(0, modelCapabilityIndex(models, surface.Snapshot.Selection)), surface.Snapshot.Query)))
	return ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	})
}

func (surface configurationPickerSurface) filteredModels() []protocol.ModelCapability {
	return filterModels(surface.Snapshot.Query, surface.Snapshot.Models)
}

func filterModels(query string, models []protocol.ModelCapability) []protocol.ModelCapability {
	authenticated := make([]protocol.ModelCapability, 0, len(models))
	for _, model := range models {
		if model.Available {
			authenticated = append(authenticated, model)
		}
	}
	return ui.DefaultFuzzySelectFilter(query, authenticated, func(model protocol.ModelCapability) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: model.Name, Description: model.ID, Aliases: []string{model.Provider}}
	})
}

func (surface configurationPickerSurface) thinkingBody(theme ui.Theme, state *configurationPickerSurfaceState) ui.Widget {
	index := modelCapabilityIndex(surface.Snapshot.Models, surface.Snapshot.CurrentModel)
	var levels []protocol.ThinkingLevel
	if index >= 0 {
		levels = surface.Snapshot.Models[index].ThinkingLevels
	}
	rows := make([]ui.Widget, 0, len(levels))
	for _, level := range levels {
		level := string(level)
		rows = append(rows, configurationOptionRow{
			Label:    level,
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
	selection := 0
	for index, level := range levels {
		if string(level) == surface.Snapshot.Selection {
			selection = index
			break
		}
	}
	if surface.Snapshot.Error != "" {
		selection += 2
	}
	return ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, state.optionsList(rows, selection, ""))
}

type configurationOptionRow struct {
	Label     string
	Details   string
	Meta      string
	Current   bool
	Selected  bool
	OnPressed ui.VoidCallback
}

func (row configurationOptionRow) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	marker := "  "
	if row.Current {
		marker = glyphCheck + " "
	}
	presentation := resolvePickerRowPresentation(ctx, theme)
	foreground := presentation.ItemText
	detailsForeground := theme.MutedForeground
	if row.Selected {
		foreground = presentation.FocusedText
		detailsForeground = presentation.FocusedText
	}
	// Keep text backgrounds transparent so ListTile paints one continuous
	// hover/selection surface through the label, metadata and padding.
	label := ui.Text{Value: marker + row.Label, Style: ui.Style{Foreground: foreground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	var content ui.Widget = label
	if row.Details != "" {
		children := []ui.Widget{
			ui.SizedBox{Width: 24, Child: label},
			ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{Value: row.Details, Style: ui.Style{Foreground: detailsForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		}
		if row.Meta != "" {
			children = append(children,
				ui.SizedBox{Width: 1},
				ui.SizedBox{Width: 12, Child: ui.Text{Value: row.Meta, Style: ui.Style{Foreground: detailsForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
			)
		}
		content = ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
	}
	return ui.Provider[ui.Theme]{Value: presentation.Theme, Child: ui.ListTile{
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
