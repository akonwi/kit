package droids

import "fmt"

// model.go — model metadata and token accounting.

// ModelAPI identifies the provider wire protocol used by a model.
type ModelAPI string

const (
	ModelAPIOpenAIResponses      ModelAPI = "openai-responses"
	ModelAPIOpenAICodexResponses ModelAPI = "openai-codex-responses"
	ModelAPIOpenAIChat           ModelAPI = "openai-chat-completions"
	ModelAPIAnthropicMessages    ModelAPI = "anthropic-messages"
)

// OutputLimitMode describes whether a provider accepts a per-request output
// token ceiling. The zero value means a request-level limit is supported.
type OutputLimitMode string

// ReasoningMode describes how a provider configures model reasoning.
type ReasoningMode string

const (
	ReasoningModeBudget   ReasoningMode = "budget"
	ReasoningModeAdaptive ReasoningMode = "adaptive"
)

const (
	// OutputLimitProviderControlled means the provider controls the actual
	// output ceiling; Droids uses its default only for context reservation.
	OutputLimitProviderControlled OutputLimitMode = "provider-controlled"
)

// Model describes a single model exposed by a provider. The registry resolves
// a user-facing model id string to one of these, tagged with its owning
// provider.
type Model struct {
	ID       string
	Name     string
	Provider string // owning provider id
	API      ModelAPI
	BaseURL  string

	// provider is the capability snapshot that resolved this model. Keeping the
	// binding private prevents callers from pairing model metadata with a
	// different provider implementation.
	provider              Provider
	catalogContextWindow  int
	catalogMaxInputTokens int

	Reasoning                     bool
	ReasoningMode                 ReasoningMode
	ReasoningLevels               []string // supported explicit Droids levels
	SupportsMidConversationEffort bool     // requires managed effort configuration in message history
	Input                         []string // "text", "image"
	ContextWindow                 int      // combined input and output capacity
	MaxInputTokens                int      // optional stricter input-only capability
	MaxOutputTokens               int      // provider capability, not the per-request allowance
	OutputLimitMode               OutputLimitMode

	Cost Cost
}

// WithContextWindow returns a copy of m with the context-window override.
// A non-positive value restores the catalog limits. Provider capability
// validation remains authoritative when a request is made.
func (m Model) WithContextWindow(contextWindow int) Model {
	m = cloneModel(m)
	if contextWindow <= 0 {
		m.ContextWindow = m.catalogContextWindow
		m.MaxInputTokens = m.catalogMaxInputTokens
		return m
	}
	m.ContextWindow = contextWindow
	m.MaxInputTokens = contextWindow
	return m
}

func (m Model) boundProvider() Provider { return m.provider }

// IsZero reports whether m is the zero model value.
func (m Model) IsZero() bool { return m.ID == "" && m.Provider == "" && m.provider == nil }

func (m Model) metadata() Model {
	m = cloneModel(m)
	m.provider = nil
	m.catalogContextWindow = 0
	m.catalogMaxInputTokens = 0
	return m
}

// BindModel returns model bound to its owning provider. It is intended for
// custom Providers implementations; NewProviders binds resolved models
// automatically. Binding validates identity only, not model capabilities.
func BindModel(provider Provider, model Model) (Model, error) {
	if provider == nil || model.Provider == "" || model.ID == "" {
		return Model{}, fmt.Errorf("droids: model binding requires provider and model identity")
	}
	if provider.ID() != model.Provider {
		return Model{}, fmt.Errorf("droids: provider %q does not match model provider %q", provider.ID(), model.Provider)
	}
	model = cloneModel(model)
	model.provider = provider
	model.catalogContextWindow = model.ContextWindow
	model.catalogMaxInputTokens = model.MaxInputTokens
	return model, nil
}

const defaultRequestMaxTokens = 4096

func resolveRequestMaxTokens(model Model, requested int, reasoning string) (int, error) {
	if err := validateReasoning(model, reasoning); err != nil {
		return 0, err
	}
	if requested < 0 {
		return 0, fmt.Errorf("droids: MaxTokens must not be negative")
	}
	if requested > 0 && model.OutputLimitMode == OutputLimitProviderControlled {
		return 0, fmt.Errorf("droids: model %q does not support a per-request MaxTokens limit", model.ID)
	}
	minimum := 1
	if model.API == ModelAPIAnthropicMessages && model.Reasoning && model.ReasoningMode != ReasoningModeAdaptive {
		if budget := int(reasoningTokenBudget(reasoning)); budget > 0 {
			minimum = budget + 1024
		}
	}
	if requested == 0 {
		requested = defaultRequestMaxTokens
		if model.MaxOutputTokens > 0 && requested > model.MaxOutputTokens {
			requested = model.MaxOutputTokens
		}
	}
	if requested < minimum {
		return 0, fmt.Errorf(
			"droids: MaxTokens (%d) must exceed the %q reasoning budget (%d)",
			requested, reasoning, minimum-1024,
		)
	}
	if model.MaxOutputTokens > 0 && requested > model.MaxOutputTokens {
		return 0, fmt.Errorf(
			"droids: MaxTokens (%d) exceeds model %q output limit (%d)",
			requested, model.ID, model.MaxOutputTokens,
		)
	}
	return requested, nil
}

func validateReasoning(model Model, level string) error {
	if level == "" {
		return nil
	}
	if level == "none" || level == "off" {
		if (model.API != ModelAPIOpenAIResponses && model.API != ModelAPIOpenAICodexResponses) || !model.Reasoning || containsString(model.ReasoningLevels, "none") {
			return nil
		}
		return fmt.Errorf("droids: model %q does not support disabling reasoning", model.ID)
	}
	if !model.Reasoning {
		return fmt.Errorf("droids: model %q does not support reasoning", model.ID)
	}
	for _, supported := range model.ReasoningLevels {
		if supported == level {
			return nil
		}
	}
	return fmt.Errorf("droids: model %q does not support reasoning level %q", model.ID, level)
}

// Cost is the per-million-token pricing for a model.
type Cost struct {
	Input      float64 // $/million input tokens
	Output     float64 // $/million output tokens
	CacheRead  float64
	CacheWrite float64
}

// Usage is token and cost accounting for one canonical provider response.
type Usage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	Reasoning   int // subset of Output, when the provider reports it
	TotalTokens int
	Cost        UsageCost
}

// UsageCost is the computed dollar cost of a turn, derived from Usage + Model.Cost.
type UsageCost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Total      float64
}

// calculateCost fills in u.Cost from the model pricing. Mirrors pi's
// calculateCost; kept simple (no Anthropic 1h split yet).
func calculateCost(m Model, u *Usage) {
	u.Cost.Input = m.Cost.Input / 1_000_000 * float64(u.Input)
	u.Cost.Output = m.Cost.Output / 1_000_000 * float64(u.Output)
	u.Cost.CacheRead = m.Cost.CacheRead / 1_000_000 * float64(u.CacheRead)
	u.Cost.CacheWrite = m.Cost.CacheWrite / 1_000_000 * float64(u.CacheWrite)
	u.Cost.Total = u.Cost.Input + u.Cost.Output + u.Cost.CacheRead + u.Cost.CacheWrite
}

// reasoningTokenBudget maps Droids' provider-neutral reasoning levels to the
// minimum explicit budget required by providers that allocate thinking tokens
// separately from ordinary output. Zero disables an explicit budget.
func reasoningTokenBudget(level string) int64 {
	switch level {
	case "minimal":
		return 1024
	case "low":
		return 4096
	case "medium":
		return 8192
	case "high", "xhigh", "max":
		return 16384
	default:
		return 0
	}
}
