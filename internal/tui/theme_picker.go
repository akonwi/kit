package tui

import (
	"errors"
	"fmt"
	"sort"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

// ThemeService is the narrow discovery, loading, and persistence boundary used
// by the interactive theme picker.
type ThemeService interface {
	Discover() ([]string, error)
	Load(string) (kittheme.Definition, []kittheme.Diagnostic, error)
	Save(string) error
}

type themePickerSnapshot struct {
	Open           bool
	Loading        bool
	PreviewLoading bool
	Pending        bool
	Names          []string
	Query          string
	Selection      string
	CommittedName  string
	Diagnostics    []kittheme.Diagnostic
	Error          string
}

type themePickerController struct {
	Open                bool
	Loading             bool
	PreviewLoading      bool
	Pending             bool
	Names               []string
	Query               string
	Selection           string
	CommittedName       string
	CommittedDefinition kittheme.Definition
	PreviewDefinition   kittheme.Definition
	Diagnostics         []kittheme.Diagnostic
	Err                 error
	previewValid        bool
}

func (p *themePickerController) Snapshot() themePickerSnapshot {
	errorText := ""
	if p.Err != nil {
		errorText = p.Err.Error()
	}
	return themePickerSnapshot{
		Open: p.Open, Loading: p.Loading, PreviewLoading: p.PreviewLoading, Pending: p.Pending,
		Names: append([]string(nil), p.Names...), Query: p.Query, Selection: p.Selection,
		CommittedName: p.CommittedName, Diagnostics: append([]kittheme.Diagnostic(nil), p.Diagnostics...), Error: errorText,
	}
}

func (p *themePickerController) OpenPicker(service ThemeService, currentName string, current kittheme.Definition) error {
	if service == nil {
		return errors.New("theme picker is unavailable")
	}
	names, err := service.Discover()
	if err != nil {
		return fmt.Errorf("discover themes: %w", err)
	}
	p.OpenNames(names, currentName, current)
	return nil
}

func (p *themePickerController) OpenNames(names []string, currentName string, current kittheme.Definition) {
	if currentName == "" {
		currentName = kittheme.SystemName
	}
	if currentName != kittheme.SystemName {
		found := false
		for _, name := range names {
			found = found || name == currentName
		}
		if !found {
			names = append(names, currentName)
			sort.Strings(names)
		}
	}
	p.Open = true
	p.Loading = false
	p.PreviewLoading = false
	p.Names = append([]string{kittheme.SystemName}, names...)
	p.Query = ""
	p.Selection = currentName
	p.CommittedName = currentName
	p.CommittedDefinition = current
	p.PreviewDefinition = current
	p.Diagnostics = nil
	p.Err = nil
	p.previewValid = true
}

func (p *themePickerController) keyModel() pickerKeyModel {
	return pickerKeyModel{Query: p.Query, Selection: p.Selection}
}

func (p *themePickerController) applyKeyModel(model pickerKeyModel) {
	p.Query, p.Selection = model.Query, model.Selection
}

func (p *themePickerController) pickerItems(query string) []pickerItem {
	return themePickerItems(filterThemeNames(query, p.Names), p.CommittedName)
}

// SetQuery filters the catalog and highlights the first matching theme. It
// reports whether the highlighted theme changed and should be previewed.
func (p *themePickerController) SetQuery(query string) bool {
	if !p.Open || p.Loading || p.Pending {
		return false
	}
	previous := p.Selection
	model := p.keyModel()
	model.SetQuery(query, p.pickerItems(query))
	p.applyKeyModel(model)
	p.Err = nil
	p.Diagnostics = nil
	return p.Selection != previous && p.Selection != ""
}

// HandleKey routes theme-picker input through the canonical picker key model.
// It reports the model result and whether a newly highlighted theme needs a
// live preview.
func (p *themePickerController) HandleKey(key ui.Key) (pickerKeyResult, bool) {
	if !p.Open {
		return pickerKeyResult{}, false
	}
	previous := p.Selection
	model := p.keyModel()
	result := model.HandleKey(key, p.pickerItems)
	if p.Pending {
		return result, false
	}
	p.applyKeyModel(model)
	if result.QueryChanged {
		p.Err = nil
		p.Diagnostics = nil
	}
	return result, p.Selection != previous && p.Selection != ""
}

// Move is retained for controller callers and applies the same key-model
// selection behavior as keyboard navigation.
func (p *themePickerController) Move(service ThemeService, delta int, apply func(kittheme.Definition)) {
	if !p.Open {
		return
	}
	model := p.keyModel()
	model.Move(p.pickerItems(p.Query), delta)
	p.applyKeyModel(model)
	p.preview(service, apply)
}

func (p *themePickerController) Select(service ThemeService, name string, apply func(kittheme.Definition)) {
	if !p.Open || pickerItemIndex(p.pickerItems(p.Query), name) < 0 {
		return
	}
	p.Selection = name
	p.preview(service, apply)
}

func (p *themePickerController) preview(service ThemeService, apply func(kittheme.Definition)) {
	name := p.Selection
	if name == "" {
		return
	}
	definition := kittheme.Definition{}
	var diagnostics []kittheme.Diagnostic
	var err error
	if name != kittheme.SystemName {
		definition, diagnostics, err = service.Load(name)
	}
	p.Diagnostics = diagnostics
	p.Err = err
	p.previewValid = err == nil
	if err != nil {
		return
	}
	p.PreviewDefinition = definition
	if apply != nil {
		apply(definition)
	}
}

func (p *themePickerController) Commit(service ThemeService, apply func(kittheme.Definition)) (string, kittheme.Definition, error) {
	if !p.Open || p.Selection == "" {
		return "", kittheme.Definition{}, errors.New("theme picker is not open")
	}
	if !p.previewValid {
		return "", kittheme.Definition{}, errors.New("selected theme could not be previewed")
	}
	name := p.Selection
	if err := service.Save(name); err != nil {
		p.Err = err
		p.PreviewDefinition = p.CommittedDefinition
		p.previewValid = false
		if apply != nil {
			apply(p.CommittedDefinition)
		}
		return "", kittheme.Definition{}, fmt.Errorf("save theme %q: %w", name, err)
	}
	definition := p.PreviewDefinition
	p.Close()
	return name, definition, nil
}

func (p *themePickerController) Cancel(apply func(kittheme.Definition)) {
	if !p.Open {
		return
	}
	if apply != nil {
		apply(p.CommittedDefinition)
	}
	p.Close()
}

func (p *themePickerController) Close() {
	*p = themePickerController{}
}

func filterThemeNames(query string, names []string) []string {
	return ui.DefaultFuzzySelectFilter(query, names, func(name string) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: name}
	})
}

func themePickerItems(names []string, current string) []pickerItem {
	items := make([]pickerItem, 0, len(names))
	for _, name := range names {
		item := pickerItem{Key: name, Label: name, Current: name == current}
		if name == kittheme.SystemName {
			item.Description = "Terminal colors"
		}
		items = append(items, item)
	}
	return items
}
