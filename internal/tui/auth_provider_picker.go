package tui

import "go.rockorager.dev/vaxis/ui"

// authProviderPickerItems is the login provider catalog. Options are keyed by
// option ID because two options can connect the same provider. The method is
// shown as the description and matched as search text, so a query such as
// "browser" or "api key" finds options by how they sign in.
func authProviderPickerItems() []pickerItem {
	items := make([]pickerItem, 0, len(authProviderOptions))
	for _, provider := range authProviderOptions {
		items = append(items, pickerItem{
			Key: provider.ID, Label: provider.Name, Description: provider.Method, SearchText: provider.Method,
		})
	}
	return items
}

// newAuthProviderPicker returns the login provider key model with an empty
// query and the first option highlighted.
func newAuthProviderPicker() pickerKeyModel {
	var model pickerKeyModel
	model.SetQuery("", authProviderPickerItems())
	return model
}

// authProviderPickerSurface renders the login provider picker ("Connect a
// provider") as a palette picker. The login error and pending state use the
// footer status slot so the list keeps only providers.
type authProviderPickerSurface struct {
	Query     string
	Selection string
	Error     string
	Pending   bool
	Select    providerSelectedCallback
}

func (w authProviderPickerSurface) Build(ui.BuildContext) ui.Widget {
	result := palettePicker{
		Title: "Connect a provider", Query: w.Query, Search: &pickerSearch{Placeholder: "Search providers…"},
		Catalog: authProviderPickerItems(), Selection: w.Selection,
		Footer: "↑↓ move · enter select · esc close",
		OnActivate: func(ctx ui.EventContext, key string) {
			if w.Select != nil {
				w.Select(ctx, key)
			}
		},
	}
	switch {
	case w.Pending:
		result.Status, result.StatusTone = "Connecting…", pickerToneLoading
	case w.Error != "":
		result.Status, result.StatusTone = w.Error, pickerToneDanger
	}
	return result
}

// handleAuthPickerKey routes login provider picker keys through the canonical
// picker key model from the input owner, so keys typed before the picker's
// first frame apply exactly like later ones. While a login is pending the
// picker only dismisses.
func (s *appState) handleAuthPickerKey(ctx ui.EventContext, key ui.Key) ui.EventResult {
	catalog := authProviderPickerItems()
	model := s.authPicker
	result := model.HandleKey(key, catalog)
	if !result.Handled {
		return ui.EventIgnored
	}
	if result.Dismiss {
		s.dismiss(ctx)
		return ui.EventHandled
	}
	if s.authPending {
		return ui.EventHandled
	}
	s.SetState(func() { s.authPicker = model })
	if result.Activate {
		if item, ok := pickerItemByKey(model.Items(catalog), model.Selection); ok {
			s.activateAuthProvider(ctx, item.Key)
		}
	}
	return ui.EventHandled
}

// activateAuthProvider starts login with a provider option chosen by Enter or
// a click, keeping it highlighted if login returns to the picker.
func (s *appState) activateAuthProvider(ctx ui.EventContext, providerID string) {
	if s.phase != phaseAuthSelect || s.authPending {
		return
	}
	s.SetState(func() { s.authPicker.Selection = providerID })
	s.selectProvider(ctx, providerID)
}
