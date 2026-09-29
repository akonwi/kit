package tui

import "go.rockorager.dev/vaxis/ui"

// authAPIKeyPrompt renders the API-key login step as a palette picker prompt.
// The key is obscured and display-only: edits arrive through
// handleAPIKeyPromptKey on the input-owner route, so keys and pastes that
// arrive before the prompt's first frame are kept.
type authAPIKeyPrompt struct {
	ProviderID string
	APIKey     string
	Error      string
	Pending    bool
}

func (w authAPIKeyPrompt) Build(ui.BuildContext) ui.Widget {
	provider, _ := authProviderByID(w.ProviderID)
	cursor := len((ui.LayoutContext{}).Characters(w.APIKey))
	prompt := palettePickerPrompt{
		Title: "Connect " + provider.Name,
		Input: textInputConfig{
			Value: w.APIKey, Placeholder: "Paste API key…", ObscureText: true,
			CursorOffset: &cursor, AutoFocus: true, ReadOnly: true,
		},
		Error:  w.Error,
		Footer: "enter save " + glyphMiddleDot + " esc back",
	}
	if w.Pending {
		// A save cannot be canceled once it starts, so no dismiss hint is shown.
		prompt.TitleMeta, prompt.TitleMetaTone = "saving…", pickerToneLoading
		prompt.Footer = ""
	}
	return prompt
}

// handleAPIKeyPromptKey edits the API key with the canonical picker key model:
// typing, Backspace, Ctrl+Backspace, and paste edit the key, Enter saves it,
// and Escape returns to the provider picker. While a save is pending every key
// is consumed without effect.
func (s *appState) handleAPIKeyPromptKey(ctx ui.EventContext, key ui.Key) ui.EventResult {
	if s.authPending {
		return ui.EventHandled
	}
	model := pickerKeyModel{Query: s.authAPIKey}
	result := model.HandleKey(key, nil)
	switch {
	case !result.Handled:
		return ui.EventIgnored
	case result.Dismiss:
		s.dismiss(ctx)
	case result.Activate:
		s.submitAPIKey(ctx, s.authAPIKey)
	case result.QueryChanged:
		s.SetState(func() { s.authAPIKey = model.Query })
	}
	return ui.EventHandled
}
