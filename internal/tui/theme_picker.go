package tui

import (
	"errors"
	"fmt"
	"sort"

	kittheme "github.com/akonwi/kit/internal/theme"
)

// ThemeService is the narrow discovery, loading, and persistence boundary used
// by the interactive theme picker.
type ThemeService interface {
	Discover() ([]string, error)
	Load(string) (kittheme.Definition, []kittheme.Diagnostic, error)
	Save(string) error
}

type themePickerSnapshot struct {
	Open        bool
	Loading     bool
	Pending     bool
	Names       []string
	Selection   int
	Diagnostics []kittheme.Diagnostic
	Error       string
}

type themePickerController struct {
	Open                bool
	Loading             bool
	Pending             bool
	Names               []string
	Selection           int
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
	return themePickerSnapshot{Open: p.Open, Loading: p.Loading, Pending: p.Pending, Names: append([]string(nil), p.Names...), Selection: p.Selection,
		Diagnostics: append([]kittheme.Diagnostic(nil), p.Diagnostics...), Error: errorText}
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
	p.Names = append([]string{kittheme.SystemName}, names...)
	p.Selection = 0
	for index, name := range p.Names {
		if name == currentName {
			p.Selection = index
			break
		}
	}
	p.CommittedName = currentName
	p.CommittedDefinition = current
	p.PreviewDefinition = current
	p.Diagnostics = nil
	p.Err = nil
	p.previewValid = true
}

func (p *themePickerController) Move(service ThemeService, delta int, apply func(kittheme.Definition)) {
	if !p.Open || len(p.Names) == 0 {
		return
	}
	p.Selection = (p.Selection + delta) % len(p.Names)
	if p.Selection < 0 {
		p.Selection += len(p.Names)
	}
	p.preview(service, apply)
}

func (p *themePickerController) Select(service ThemeService, index int, apply func(kittheme.Definition)) {
	if !p.Open || index < 0 || index >= len(p.Names) {
		return
	}
	p.Selection = index
	p.preview(service, apply)
}

func (p *themePickerController) preview(service ThemeService, apply func(kittheme.Definition)) {
	name := p.Names[p.Selection]
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
	if !p.Open || len(p.Names) == 0 {
		return "", kittheme.Definition{}, errors.New("theme picker is not open")
	}
	if !p.previewValid {
		return "", kittheme.Definition{}, errors.New("selected theme could not be previewed")
	}
	name := p.Names[p.Selection]
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
