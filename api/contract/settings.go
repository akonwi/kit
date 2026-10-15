package contract

import (
	"fmt"
	"strings"
)

// PromptCacheRetention is how long providers are asked to keep prompt cache
// entries. Providers map it to their own lifetimes.
type PromptCacheRetention string

// Prompt cache retention values.
const (
	PromptCacheShort PromptCacheRetention = "short"
	PromptCacheLong  PromptCacheRetention = "long"
)

// EnumValues lists the prompt cache retention values.
func (PromptCacheRetention) EnumValues() []string { return []string{"short", "long"} }

// Limits on server settings.
const (
	// MaxModelOverrides bounds the model overrides in settings or one update.
	MaxModelOverrides = 256
	// MaxContextWindow bounds a context-window override, in tokens.
	MaxContextWindow = 100_000_000
)

// ModelOverride overrides request defaults for one exact provider/model.
type ModelOverride struct {
	Model string `json:"model"`
	// ContextWindow is the model's context limit in tokens. In an update,
	// zero removes the override.
	ContextWindow int `json:"contextWindow"`
}

// ServerSettings are the settings that govern a server's behavior. Client
// preferences such as themes are not among them.
type ServerSettings struct {
	// DefaultModel is the model for new sessions whose request names none;
	// empty lets the server choose.
	DefaultModel string `json:"defaultModel,omitempty"`
	// CompactionModel summarizes context during compaction; empty uses the
	// session's own model.
	CompactionModel string `json:"compactionModel,omitempty"`
	// ModelOverrides are ordered by model.
	ModelOverrides       []ModelOverride      `json:"modelOverrides"`
	PromptCacheRetention PromptCacheRetention `json:"promptCacheRetention"`
	// ReadClaudeConfigs enables discovering Claude Code configuration, such
	// as a project's .claude/commands.
	ReadClaudeConfigs bool `json:"readClaudeConfigs"`
	// NewSessionModel and NewSessionThinkingLevel are what a new session
	// whose request names neither would start with now: the server's choice
	// of model at that model's default thinking level. Both are empty when
	// no model is available.
	NewSessionModel         string        `json:"newSessionModel,omitempty"`
	NewSessionThinkingLevel ThinkingLevel `json:"newSessionThinkingLevel,omitempty"`
}

// UpdateServerSettingsInput changes server settings. Absent fields are left
// unchanged; an empty string clears a model, and a model override with a zero
// context window is removed.
type UpdateServerSettingsInput struct {
	DefaultModel         *string               `json:"defaultModel,omitempty"`
	CompactionModel      *string               `json:"compactionModel,omitempty"`
	ModelOverrides       []ModelOverride       `json:"modelOverrides,omitempty"`
	PromptCacheRetention *PromptCacheRetention `json:"promptCacheRetention,omitempty"`
	ReadClaudeConfigs    *bool                 `json:"readClaudeConfigs,omitempty"`
}

// Validate checks server settings.
func (settings ServerSettings) Validate() error {
	for name, selector := range map[string]string{"default model": settings.DefaultModel, "compaction model": settings.CompactionModel, "new session model": settings.NewSessionModel} {
		if selector != "" && !validModelSelector(selector) {
			return fmt.Errorf("%s must be an exact provider/model id", name)
		}
	}
	if level := settings.NewSessionThinkingLevel; level != "" {
		if err := level.Validate(); err != nil {
			return err
		}
	}
	if (settings.NewSessionModel == "") != (settings.NewSessionThinkingLevel == "") {
		return fmt.Errorf("a new session's model and thinking level are reported together")
	}
	if err := validModelOverrides(settings.ModelOverrides, false); err != nil {
		return err
	}
	for index := 1; index < len(settings.ModelOverrides); index++ {
		if settings.ModelOverrides[index-1].Model >= settings.ModelOverrides[index].Model {
			return fmt.Errorf("model overrides must be ordered by model")
		}
	}
	return validPromptCacheRetention(settings.PromptCacheRetention)
}

// Validate checks a settings update.
func (input UpdateServerSettingsInput) Validate() error {
	for name, selector := range map[string]*string{"default model": input.DefaultModel, "compaction model": input.CompactionModel} {
		if selector != nil && *selector != "" && !validModelSelector(*selector) {
			return fmt.Errorf("%s must be an exact provider/model id, or empty to clear it", name)
		}
	}
	if err := validModelOverrides(input.ModelOverrides, true); err != nil {
		return err
	}
	if input.PromptCacheRetention != nil {
		return validPromptCacheRetention(*input.PromptCacheRetention)
	}
	return nil
}

func validModelOverrides(overrides []ModelOverride, removable bool) error {
	if len(overrides) > MaxModelOverrides {
		return fmt.Errorf("at most %d model overrides are allowed", MaxModelOverrides)
	}
	seen := make(map[string]bool, len(overrides))
	minimum := 1
	if removable {
		minimum = 0
	}
	for _, override := range overrides {
		if !validModelSelector(override.Model) || seen[override.Model] {
			return fmt.Errorf("model override %q must name one exact provider/model id once", override.Model)
		}
		seen[override.Model] = true
		if override.ContextWindow < minimum || override.ContextWindow > MaxContextWindow {
			return fmt.Errorf("model override %q context window must be between %d and %d tokens", override.Model, minimum, MaxContextWindow)
		}
	}
	return nil
}

func validPromptCacheRetention(retention PromptCacheRetention) error {
	if retention != PromptCacheShort && retention != PromptCacheLong {
		return fmt.Errorf("prompt cache retention must be %q or %q", PromptCacheShort, PromptCacheLong)
	}
	return nil
}

// validModelSelector checks an exact provider/model id.
func validModelSelector(selector string) bool {
	provider, model, ok := strings.Cut(selector, "/")
	return ok && validRendererText(provider, 128) && validRendererText(model, 256) &&
		strings.TrimSpace(provider) == provider && strings.TrimSpace(model) == model
}
