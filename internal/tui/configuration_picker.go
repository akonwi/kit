package tui

import (
	"fmt"

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
		models := controller.keyModel().Items(controller.pickerCatalog())
		if pickerItemIndex(models, controller.Selection) < 0 {
			controller.Selection = controller.CurrentModel
			if pickerItemIndex(models, controller.Selection) < 0 {
				controller.Selection = ""
				if len(models) > 0 {
					controller.Selection = models[0].Key
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

// keyModel returns the canonical picker key model for the current query and
// highlighted item.
func (controller *configurationPickerController) keyModel() pickerKeyModel {
	return pickerKeyModel{Query: controller.Query, Selection: controller.Selection}
}

func (controller *configurationPickerController) applyKeyModel(model pickerKeyModel) {
	controller.Query, controller.Selection = model.Query, model.Selection
}

// pickerCatalog returns every authenticated model or reasoning level.
func (controller *configurationPickerController) pickerCatalog() []pickerItem {
	if controller.Mode == configurationPickerModel {
		return modelPickerItems(controller.Models, controller.CurrentModel)
	}
	return thinkingPickerItems(controller.thinkingLevels(), controller.CurrentThinking)
}

func (controller *configurationPickerController) Select(value string) {
	if controller.Loading || controller.Pending {
		return
	}
	if pickerItemIndex(controller.keyModel().Items(controller.pickerCatalog()), value) >= 0 {
		controller.Selection = value
	}
	controller.Error = ""
}

func (controller *configurationPickerController) Move(delta int) {
	if controller.Loading || controller.Pending || delta == 0 {
		return
	}
	model := controller.keyModel()
	model.Move(controller.pickerCatalog(), delta)
	controller.applyKeyModel(model)
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

// HandleKey applies one key. Pickers use the canonical picker key model; the
// context-window editor accepts digits, Backspace, paste, and Escape. It
// reports whether the highlighted value should be applied.
func (controller *configurationPickerController) HandleKey(key ui.Key) (bool, bool) {
	if controller.Mode == configurationPickerClosed || key.EventType == ui.EventRelease {
		return false, false
	}
	if controller.EditingContext {
		return false, controller.handleContextEditKey(key)
	}
	if controller.Loading || controller.Pending {
		if key.EventType != vaxis.EventPaste && key.MatchString("Escape") {
			controller.Close()
		}
		return false, true
	}
	model := controller.keyModel()
	result := model.HandleKey(key, controller.pickerCatalog())
	controller.applyKeyModel(model)
	if result.QueryChanged {
		controller.Error = ""
	}
	if result.Dismiss {
		controller.Close()
	}
	return result.Activate, result.Handled
}

func (controller *configurationPickerController) handleContextEditKey(key ui.Key) bool {
	if key.EventType != vaxis.EventPaste && key.MatchString("Escape") {
		controller.EditingContext = false
		controller.EditModel = ""
		controller.EditValue = ""
		return true
	}
	value := controller.EditValue
	switch {
	case key.EventType == vaxis.EventPaste:
		value += palettePasteText(key)
	case key.MatchString("Backspace"):
		runes := []rune(value)
		if len(runes) > 0 {
			value = string(runes[:len(runes)-1])
		}
	case key.Text != "" && key.Text[0] >= '0' && key.Text[0] <= '9':
		value += key.Text
	case key.Modifiers&^(vaxis.ModShift|vaxis.ModCapsLock|vaxis.ModNumLock) != 0:
		return false
	}
	controller.EditValue = value
	controller.Error = ""
	return true
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

func modelCapabilityIndex(models []protocol.ModelCapability, id string) int {
	for index, model := range models {
		if model.ID == id {
			return index
		}
	}
	return -1
}

type configurationPickerSurface struct {
	Snapshot configurationPickerSnapshot
	// ContextChanged edits the context-window override prompt.
	ContextChanged ui.TextChangedCallback
	Select         func(ui.EventContext, string)
	Apply          ui.VoidCallback
}

// Build maps the model and thinking pickers onto the canonical picker, and the
// context-window override onto the canonical prompt.
func (surface configurationPickerSurface) Build(ui.BuildContext) ui.Widget {
	snapshot := surface.Snapshot
	footer := "↑↓ move · enter apply · ctrl+o overrides · esc close"
	if snapshot.Mode == configurationPickerThinking {
		footer = "↑↓ move · enter apply · esc close"
	}
	if snapshot.Pending {
		footer = "Applying configuration…"
	}
	if snapshot.EditingContext {
		cursor := len(snapshot.EditValue)
		return palettePickerPrompt{
			Title: "Context window", TitleMeta: snapshot.EditModel,
			Input: textInputConfig{Value: snapshot.EditValue, Placeholder: "Blank clears the override", CursorOffset: &cursor, OnChanged: surface.ContextChanged, AutoFocus: true},
			Error: snapshot.Error, Footer: "enter save · blank clears · esc back",
		}
	}
	result := palettePicker{
		Title: "Select model", Footer: footer, Selection: snapshot.Selection,
		Query: snapshot.Query, Search: &pickerSearch{Placeholder: "Search models…"},
		OnActivate: func(ctx ui.EventContext, key string) {
			if surface.Select != nil {
				surface.Select(ctx, key)
			}
			if surface.Apply != nil {
				surface.Apply(ctx)
			}
		},
	}
	if snapshot.Mode == configurationPickerThinking {
		result.Title = "Thinking level"
		result.Search.Placeholder = "Search effort levels…"
		result.Catalog = thinkingPickerItems(surface.thinkingLevels(), snapshot.CurrentThinking)
	} else {
		result.Catalog = modelPickerItems(snapshot.Models, snapshot.CurrentModel)
	}
	switch {
	case snapshot.Loading:
		result.Message, result.MessageTone = "Loading…", pickerToneLoading
	case snapshot.Error != "" && len(snapshot.Models) == 0:
		result.Message, result.MessageTone = snapshot.Error, pickerToneDanger
	case snapshot.Error != "":
		result.Status, result.StatusTone = snapshot.Error, pickerToneDanger
	case len(result.Catalog) == 0 && snapshot.Mode == configurationPickerThinking:
		result.Message = "No thinking levels"
	case len(result.Catalog) == 0:
		result.Message = "No models"
	}
	return result
}

// modelPickerItems maps the authenticated models onto picker items; the
// model ID and provider are also matched by the filter.
func modelPickerItems(models []protocol.ModelCapability, current string) []pickerItem {
	items := make([]pickerItem, 0, len(models))
	for _, model := range models {
		if !model.Available {
			continue
		}
		items = append(items, pickerItem{
			Key: model.ID, Label: model.Name, Description: model.ID,
			Meta: formatContextWindow(model.ContextWindow) + " context", Current: model.ID == current,
			SearchText: model.ID, SearchAliases: []string{model.Provider},
		})
	}
	return items
}

func thinkingPickerItems(levels []string, current string) []pickerItem {
	items := make([]pickerItem, 0, len(levels))
	for _, level := range levels {
		items = append(items, pickerItem{Key: level, Label: level, Current: level == current})
	}
	return items
}

func (surface configurationPickerSurface) thinkingLevels() []string {
	index := modelCapabilityIndex(surface.Snapshot.Models, surface.Snapshot.CurrentModel)
	if index < 0 {
		return nil
	}
	levels := make([]string, 0, len(surface.Snapshot.Models[index].ThinkingLevels))
	for _, level := range surface.Snapshot.Models[index].ThinkingLevels {
		levels = append(levels, string(level))
	}
	return levels
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

func stringIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
