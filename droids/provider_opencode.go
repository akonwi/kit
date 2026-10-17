package droids

import "net/http"

const defaultOpenCodeBaseURL = "https://opencode.ai/zen/v1"

// OpenCode configures the OpenCode Zen provider.
type OpenCode struct {
	APIKey       string
	APIKeySource APIKeySource
	BaseURL      string
	HTTPClient   *http.Client
	// ErrorReporter receives raw provider transport and stream failures.
	ErrorReporter ProviderErrorReporter
}

func (c OpenCode) build() (providerEntry, error) {
	return buildOpenCodeProvider("opencode", "opencode", "OpenCode Zen", defaultOpenCodeBaseURL,
		c.APIKey, c.APIKeySource, c.BaseURL, c.HTTPClient, c.ErrorReporter, openCodeGoImagePolicy)
}
