package cli

import "github.com/akonwi/kit/internal/tui"

// RunVaxisTUI runs the existing vaxis/ui terminal client. It lets the Ard
// entry point ship a complete client until the Cooper client replaces it.
func RunVaxisTUI(options TUIOptions) error {
	return tui.Run(tui.Options{
		Context:               options.Context,
		Server:                options.Server,
		CWD:                   options.CWD,
		Location:              options.Location,
		ResolveLocation:       options.ResolveLocation,
		DefaultModel:          options.DefaultModel,
		DefaultThinking:       options.DefaultThinking,
		ResumeModelFilter:     options.ResumeModelFilter,
		ResumeThinkingFilter:  options.ResumeThinkingFilter,
		AvailableProviders:    options.AvailableProviders,
		Authenticated:         options.Authenticated,
		SessionID:             options.SessionID,
		NewSessionID:          options.NewSessionID,
		NewSessionName:        options.NewSessionName,
		TemporarySession:      options.TemporarySession,
		Login:                 options.Login,
		BrowserLogin:          options.BrowserLogin,
		APIKeyLogin:           options.APIKeyLogin,
		ThemeName:             options.ThemeName,
		ThemeDefinition:       options.ThemeDefinition,
		ThemeService:          options.ThemeService,
		ModelOverrideService:  options.ModelOverrideService,
		DiffWrapLines:         options.DiffWrapLines,
		DiffPreferenceService: options.DiffPreferenceService,
	})
}

// PickVaxisSession runs the existing vaxis/ui saved-session picker.
func PickVaxisSession(options SessionPickerOptions) (string, error) {
	return tui.RunSessionPicker(tui.SessionPickerOptions{
		Context: options.Context,
		Server:  options.Server,
	})
}
