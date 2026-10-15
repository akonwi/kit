package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/settings"
)

// settingsAccess serves the settings that govern the server's behavior.
// Client preferences in the same file, such as the theme, and the server's
// access settings are not among them.
type settingsAccess struct {
	store *settings.Store
	// models checks that the models settings name are in the catalog.
	models   droids.Providers
	defaults sessionDefaults
}

func (a settingsAccess) read(ctx context.Context, logger *slog.Logger) (protocol.ServerSettings, error) {
	current, _, err := a.store.Load()
	if err != nil {
		return protocol.ServerSettings{}, settingsError(err, logger)
	}
	return a.project(ctx, current), nil
}

// project reports current settings. Values a client could not accept, which
// the server also ignores or treats leniently, are omitted.
func (a settingsAccess) project(ctx context.Context, current settings.Settings) protocol.ServerSettings {
	result := protocol.ServerSettings{
		DefaultModel: acceptedSelector(current.DefaultModel), CompactionModel: acceptedSelector(current.CompactionModel),
		ModelOverrides:       []protocol.ModelOverride{},
		PromptCacheRetention: protocol.PromptCacheShort, ReadClaudeConfigs: current.ReadClaudeConfigs,
	}
	if current.PromptCacheRetention == settings.PromptCacheLong {
		result.PromptCacheRetention = protocol.PromptCacheLong
	}
	for selector, override := range current.ModelOverrides {
		if acceptedSelector(selector) != "" && override.ContextWindow <= protocol.MaxContextWindow {
			result.ModelOverrides = append(result.ModelOverrides, protocol.ModelOverride{Model: selector, ContextWindow: override.ContextWindow})
		}
	}
	slices.SortFunc(result.ModelOverrides, func(a, b protocol.ModelOverride) int { return strings.Compare(a.Model, b.Model) })
	result.ModelOverrides = result.ModelOverrides[:min(len(result.ModelOverrides), protocol.MaxModelOverrides)]
	model, level := a.defaults.newSession(ctx)
	result.NewSessionModel, result.NewSessionThinkingLevel = model, protocol.ThinkingLevel(level)
	return result
}

// acceptedSelector returns selector when the contract accepts it.
func acceptedSelector(selector string) string {
	if selector == "" || (protocol.ServerSettings{DefaultModel: selector, PromptCacheRetention: protocol.PromptCacheShort}).Validate() != nil {
		return ""
	}
	return selector
}

// patch validates input against the model catalog and the current settings,
// and converts it. Models are stored by their canonical provider/model id.
// Removing an override does not require its model to still exist; one named
// by an alias is removed under both names.
func (a settingsAccess) patch(input protocol.UpdateServerSettingsInput, current settings.Settings) (settings.Patch, error) {
	invalid := func(format string, arguments ...any) error {
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, fmt.Sprintf(format, arguments...), nil)
	}
	canonical := func(selector string) (string, error) {
		model, ok := a.models.Model(selector)
		if !ok {
			return "", invalid("model %q is not in the server's catalog", selector)
		}
		return model.Provider + "/" + model.ID, nil
	}
	optionalModel := func(selector *string) (*string, error) {
		if selector == nil || *selector == "" {
			return selector, nil
		}
		value, err := canonical(*selector)
		return &value, err
	}
	var patch settings.Patch
	var err error
	if patch.DefaultModel, err = optionalModel(input.DefaultModel); err != nil {
		return settings.Patch{}, err
	}
	if patch.CompactionModel, err = optionalModel(input.CompactionModel); err != nil {
		return settings.Patch{}, err
	}
	if retention := input.PromptCacheRetention; retention != nil {
		value := settings.PromptCacheShort
		if *retention == protocol.PromptCacheLong {
			value = settings.PromptCacheLong
		}
		patch.PromptCacheRetention = &value
	}
	patch.ReadClaudeConfigs = input.ReadClaudeConfigs
	if len(input.ModelOverrides) > 0 {
		patch.ContextWindows = make(map[string]int, len(input.ModelOverrides))
		named := make(map[string]bool, len(input.ModelOverrides))
		for _, override := range input.ModelOverrides {
			selectors := []string{override.Model}
			if model, ok := a.models.Model(override.Model); ok {
				selectors = []string{model.Provider + "/" + model.ID}
				if override.ContextWindow == 0 && selectors[0] != override.Model {
					selectors = append(selectors, override.Model)
				}
			} else if override.ContextWindow > 0 {
				return settings.Patch{}, invalid("model %q is not in the server's catalog", override.Model)
			}
			if named[selectors[0]] {
				return settings.Patch{}, invalid("model overrides name %q more than once", selectors[0])
			}
			named[selectors[0]] = true
			for _, selector := range selectors {
				patch.ContextWindows[selector] = override.ContextWindow
			}
		}
		count := len(current.ModelOverrides)
		for selector, contextWindow := range patch.ContextWindows {
			_, exists := current.ModelOverrides[selector]
			switch {
			case contextWindow > 0 && !exists:
				count++
			case contextWindow == 0 && exists:
				count--
			}
		}
		if count > protocol.MaxModelOverrides {
			return settings.Patch{}, invalid("a server keeps at most %d model overrides", protocol.MaxModelOverrides)
		}
	}
	return patch, nil
}

// settingsError reports a settings file the user must fix as a conflict. The
// details, which name the file's path, stay in the server's log.
func settingsError(err error, logger *slog.Logger) error {
	if errors.Is(err, settings.ErrMalformed) {
		if logger != nil {
			logger.Warn("settings file needs fixing", "error", err)
		}
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, "the server's settings.json needs fixing; its log says why", nil)
	}
	return err
}

func registerSettingsRoutes(mux *http.ServeMux, a settingsAccess, logger *slog.Logger) {
	// A full set of overrides with the longest selectors fits.
	options := httpapi.ServeOptions{MaxRequestBytes: 256 << 10, WriteError: func(writer http.ResponseWriter, err error) {
		var apiError *httpapi.APIError
		if !errors.As(err, &apiError) {
			if logger != nil {
				logger.Error("settings request failed", "error", err)
			}
			apiError = httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal error", nil)
		}
		httpapi.WriteError(writer, apiError)
	}}
	httpapi.Handle(mux, options, httpapi.GetServerSettings, func(ctx context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (protocol.ServerSettings, error) {
		return a.read(ctx, logger)
	})
	httpapi.Handle(mux, options, httpapi.UpdateServerSettings, func(ctx context.Context, _ httpapi.ServerPath, input protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error) {
		if err := input.Validate(); err != nil {
			return protocol.ServerSettings{}, httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, err.Error(), nil)
		}
		current, _, err := a.store.Load()
		if err != nil {
			return protocol.ServerSettings{}, settingsError(err, logger)
		}
		patch, err := a.patch(input, current)
		if err != nil {
			return protocol.ServerSettings{}, err
		}
		updated, err := a.store.Update(patch)
		if err != nil {
			return protocol.ServerSettings{}, settingsError(err, logger)
		}
		return a.project(ctx, updated), nil
	})
}
