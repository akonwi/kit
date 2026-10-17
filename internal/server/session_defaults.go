package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/httpapi"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/settings"
)

// fallbackModels are tried, in order, after the default model setting when a
// new session's request names no model.
var fallbackModels = []string{"openai-codex/gpt-5.6-sol", "openai/gpt-5.6-sol", "anthropic/claude-sonnet-4-6", "opencode-go/kimi-k2.7-code", "opencode/gpt-5.6-sol"}

// errNoModel reports that a new session has no model to start with.
func errNoModel() error {
	return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, "no model is available; log in to a provider", nil)
}

// sessionDefaults resolves the model of a new session whose request names
// none: the server's default model setting, then built-in preferences, among
// the providers with credentials. Settings that cannot be read are logged and
// treated as unset, as the server's other settings readers do.
type sessionDefaults struct {
	providers droids.Providers
	// availableProviders lists the providers with credentials; nil treats
	// every catalog provider as available.
	availableProviders func(context.Context) []string
	// settings reads the current settings; nil uses the defaults.
	settings func() (settings.Settings, error)
	logger   *slog.Logger
}

func (d sessionDefaults) current() settings.Settings {
	if d.settings == nil {
		return settings.Settings{}
	}
	current, err := d.settings()
	if err != nil {
		if d.logger != nil {
			d.logger.Warn("load settings for new-session defaults", "error", err)
		}
		return settings.Settings{}
	}
	return current
}

// model returns the model for a session whose request names none: the
// default model setting, the fallback models, another model of the first
// of those providers with credentials, then any model with credentials.
func (d sessionDefaults) model(ctx context.Context) (string, error) {
	if d.providers == nil {
		return "", errNoModel()
	}
	available := make(map[string]bool)
	if d.availableProviders == nil {
		for _, model := range d.providers.Models() {
			available[model.Provider] = true
		}
	} else {
		for _, provider := range d.availableProviders(ctx) {
			available[provider] = true
		}
	}
	preferred := fallbackModels
	if configured := d.current().DefaultModel; configured != "" {
		preferred = append([]string{configured}, fallbackModels...)
	}
	for _, selector := range preferred {
		if model, ok := d.providers.Model(selector); ok && available[model.Provider] {
			return model.Provider + "/" + model.ID, nil
		}
	}
	models := d.providers.Models()
	for _, selector := range preferred {
		provider, _, _ := strings.Cut(selector, "/")
		if !available[provider] {
			continue
		}
		for _, model := range models {
			if model.Provider == provider {
				return model.Provider + "/" + model.ID, nil
			}
		}
	}
	for _, model := range models {
		if available[model.Provider] {
			return model.Provider + "/" + model.ID, nil
		}
	}
	return "", errNoModel()
}

// newSession reports the model and thinking level a session whose request
// names neither would start with now: the chosen model at its default
// thinking level. Both are empty when no model is available.
func (d sessionDefaults) newSession(ctx context.Context) (string, string) {
	selector, err := d.model(ctx)
	if err != nil {
		return "", ""
	}
	model, _ := d.providers.Model(selector)
	effective, err := kitsession.ResolveCompatibleThinkingLevel(model, "")
	if err != nil {
		return "", ""
	}
	return selector, effective
}
