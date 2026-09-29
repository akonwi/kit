package tui

import (
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
	commitOnPreview     string
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

// OpenNames installs the discovered catalog without discarding a query typed
// while discovery was in flight. It reports whether the resulting selection
// needs to be previewed.
func (p *themePickerController) OpenNames(names []string, currentName string, current kittheme.Definition) bool {
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
	query := p.Query
	p.Open = true
	p.Loading = false
	p.PreviewLoading = false
	p.Names = append([]string{kittheme.SystemName}, names...)
	p.CommittedName = currentName
	p.CommittedDefinition = current
	p.PreviewDefinition = current
	p.Diagnostics = nil
	p.Err = nil
	p.previewValid = true
	p.commitOnPreview = ""
	selection := currentName
	if query != "" {
		selection = firstEnabledPickerKey(pickerKeyModel{Query: query}.Items(p.pickerCatalog()))
	}
	p.applyKeyState(query, selection)
	return selection != "" && selection != currentName
}

func (p *themePickerController) keyModel() pickerKeyModel {
	return pickerKeyModel{Query: p.Query, Selection: p.Selection}
}

func (p *themePickerController) applyKeyModel(model pickerKeyModel) {
	p.applyKeyState(model.Query, model.Selection)
}

func (p *themePickerController) applyKeyState(query, selection string) {
	if p.Query != query || p.Selection != selection {
		p.commitOnPreview = ""
	}
	p.Query, p.Selection = query, selection
}

func (p *themePickerController) selectForPreview(name string) {
	p.applyKeyState(p.Query, name)
}

// requestCommit marks a pointer-activated theme to be committed after its
// asynchronous preview succeeds. It reports whether the current preview can be
// committed immediately.
func (p *themePickerController) requestCommit(name string) bool {
	if !p.Open || p.Loading || p.Pending {
		return false
	}
	if name == p.Selection && p.previewValid && !p.PreviewLoading {
		p.commitOnPreview = ""
		return true
	}
	p.selectForPreview(name)
	p.commitOnPreview = name
	return false
}

func (p *themePickerController) completePreview(name string, succeeded bool) bool {
	if p.commitOnPreview != name || p.Selection != name {
		return false
	}
	p.commitOnPreview = ""
	return succeeded
}

func (p *themePickerController) pickerCatalog() []pickerItem {
	return themePickerItems(p.Names, p.CommittedName)
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
	result := model.HandleKey(key, p.pickerCatalog())
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

func (p *themePickerController) Close() {
	*p = themePickerController{}
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
