package droids

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const modelsDevCatalogURL = "https://models.dev/api.json"

//go:generate go run ./internal/cmd/modelcatalog

//go:embed model_catalog.json
var embeddedModelCatalog []byte

type modelsDevCatalog map[string]modelsDevProvider

type modelsDevProvider struct {
	NPM    string                    `json:"npm"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModelProvider struct {
	NPM string `json:"npm"`
}

type modelsDevReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Min    int      `json:"min"`
}

type modelsDevModel struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Family           string                     `json:"family"`
	Reasoning        bool                       `json:"reasoning"`
	ReasoningOptions []modelsDevReasoningOption `json:"reasoning_options"`
	ToolCall         bool                       `json:"tool_call"`
	Modalities       struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Input   int `json:"input"`
		Output  int `json:"output"`
	} `json:"limit"`
	Provider *modelsDevModelProvider `json:"provider"`
	Cost     struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
}

var builtinModelCatalog = mustParseModelCatalog(embeddedModelCatalog)

func mustParseModelCatalog(data []byte) modelsDevCatalog {
	var catalog modelsDevCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(fmt.Sprintf("droids: parse embedded model catalog: %v", err))
	}
	return catalog
}

// OpenAIModels returns a fresh copy of the built-in OpenAI model catalog.
func OpenAIModels() []Model {
	models := catalogModels(builtinModelCatalog, "openai", "openai")
	setModelBaseURL(models, defaultOpenAIBaseURL)
	return models
}

// OpenAIModel returns one model from the built-in OpenAI catalog.
func OpenAIModel(id string) (Model, bool) {
	model, ok := catalogModel(builtinModelCatalog, "openai", "openai", id)
	model.BaseURL = defaultOpenAIBaseURL
	return model, ok
}

// OpenCodeGoModels returns a fresh copy of the built-in OpenCode Go catalog.
func OpenCodeGoModels() []Model {
	models := catalogModels(builtinModelCatalog, "opencode-go", "opencode-go")
	setModelBaseURL(models, defaultOpenCodeGoBaseURL)
	return models
}

// OpenCodeGoModel returns one model from the built-in OpenCode Go catalog.
func OpenCodeGoModel(id string) (Model, bool) {
	model, ok := catalogModel(builtinModelCatalog, "opencode-go", "opencode-go", id)
	model.BaseURL = defaultOpenCodeGoBaseURL
	return model, ok
}

// AnthropicModels returns a fresh copy of the built-in Anthropic model catalog.
func AnthropicModels() []Model {
	models := catalogModels(builtinModelCatalog, "anthropic", "anthropic")
	setModelBaseURL(models, defaultAnthropicBaseURL)
	return models
}

// AnthropicModel returns one model from the built-in Anthropic catalog.
func AnthropicModel(id string) (Model, bool) {
	model, ok := catalogModel(builtinModelCatalog, "anthropic", "anthropic", id)
	model.BaseURL = defaultAnthropicBaseURL
	return model, ok
}

func setModelBaseURL(models []Model, baseURL string) {
	for i := range models {
		models[i].BaseURL = baseURL
	}
}

func catalogModel(catalog modelsDevCatalog, catalogID, providerID, id string) (Model, bool) {
	provider, ok := catalog[catalogID]
	if !ok {
		return Model{}, false
	}
	source, ok := provider.Models[id]
	if !ok || source.ID != id {
		return Model{}, false
	}
	model, ok := modelFromCatalog(provider, catalogID, providerID, source)
	return model, ok
}

func catalogModels(catalog modelsDevCatalog, catalogID, providerID string) []Model {
	provider, ok := catalog[catalogID]
	if !ok {
		return nil
	}
	models := make([]Model, 0, len(provider.Models))
	for id, source := range provider.Models {
		if source.ID != id {
			continue
		}
		if model, ok := modelFromCatalog(provider, catalogID, providerID, source); ok {
			models = append(models, model)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func catalogProviderNPM(provider modelsDevProvider, model modelsDevModel) string {
	if model.Provider != nil && model.Provider.NPM != "" {
		return model.Provider.NPM
	}
	return provider.NPM
}

func modelFromCatalog(provider modelsDevProvider, catalogID, providerID string, source modelsDevModel) (Model, bool) {
	if source.ID == "" || !source.ToolCall || source.Limit.Context <= 0 || source.Limit.Output <= 0 {
		return Model{}, false
	}
	if !containsString(source.Modalities.Input, "text") || !containsString(source.Modalities.Output, "text") {
		return Model{}, false
	}
	if source.Limit.Input < 0 || source.Limit.Input > source.Limit.Context || source.Limit.Output > source.Limit.Context {
		return Model{}, false
	}
	if source.Cost.Input < 0 || source.Cost.Output < 0 || source.Cost.CacheRead < 0 || source.Cost.CacheWrite < 0 {
		return Model{}, false
	}
	api := ModelAPI("")
	var reasoningLevels []string
	switch catalogID {
	case "openai":
		api = ModelAPIOpenAIResponses
		if !openAIResponsesCatalogModel(source) {
			return Model{}, false
		}
		reasoningLevels = catalogEffortLevels(source.ReasoningOptions)
	case "anthropic":
		api = ModelAPIAnthropicMessages
		reasoningLevels = catalogBudgetReasoningLevels(source.ReasoningOptions)
	case "opencode-go":
		npm := catalogProviderNPM(provider, source)
		switch npm {
		case "@ai-sdk/openai":
			api = ModelAPIOpenAIResponses
			reasoningLevels = catalogEffortLevels(source.ReasoningOptions)
		case "@ai-sdk/anthropic":
			api = ModelAPIAnthropicMessages
			reasoningLevels = catalogBudgetReasoningLevels(source.ReasoningOptions)
		case "@ai-sdk/openai-compatible":
			api = ModelAPIOpenAIChat
			reasoningLevels = catalogEffortLevels(source.ReasoningOptions)
		default:
			return Model{}, false
		}
	default:
		return Model{}, false
	}
	contextWindow := source.Limit.Context
	input := []string{"text"}
	if containsString(source.Modalities.Input, "image") {
		input = append(input, "image")
	}
	name := source.Name
	if name == "" {
		name = source.ID
	}
	return Model{
		ID:              source.ID,
		Name:            name,
		Provider:        providerID,
		API:             api,
		Reasoning:       source.Reasoning,
		ReasoningLevels: reasoningLevels,
		Input:           input,
		ContextWindow:   contextWindow,
		MaxInputTokens:  source.Limit.Input,
		MaxOutputTokens: source.Limit.Output,
		Cost: Cost{
			Input:      source.Cost.Input,
			Output:     source.Cost.Output,
			CacheRead:  source.Cost.CacheRead,
			CacheWrite: source.Cost.CacheWrite,
		},
	}, true
}

func openAIResponsesCatalogModel(source modelsDevModel) bool {
	// models.dev does not currently expose an API-family field. Maintain an
	// explicit family allowlist matching the Responses provider, plus narrow
	// known exclusions verified by pi-ai's catalog generator.
	allowedFamily := map[string]bool{
		"gpt": true, "gpt-mini": true, "gpt-nano": true, "gpt-pro": true,
		"gpt-codex": true, "gpt-codex-spark": true,
		"gpt-sol": true, "gpt-luna": true, "gpt-terra": true,
		"o": true, "o-pro": true, "o-mini": true,
	}
	if !allowedFamily[source.Family] {
		return false
	}
	return !strings.Contains(source.ID, "realtime") && source.ID != "gpt-5.6"
}

func catalogEffortLevels(options []modelsDevReasoningOption) []string {
	var levels []string
	for _, option := range options {
		if option.Type != "effort" {
			continue
		}
		for _, level := range option.Values {
			if containsString([]string{"none", "minimal", "low", "medium", "high", "xhigh"}, level) && !containsString(levels, level) {
				levels = append(levels, level)
			}
		}
	}
	return levels
}

func catalogBudgetReasoningLevels(options []modelsDevReasoningOption) []string {
	minimum := 0
	found := false
	for _, option := range options {
		if option.Type == "budget_tokens" {
			minimum = option.Min
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var levels []string
	for _, level := range []string{"minimal", "low", "medium", "high", "xhigh"} {
		if int(reasoningTokenBudget(level)) >= minimum {
			levels = append(levels, level)
		}
	}
	return levels
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneModel(model Model) Model {
	model.Input = append([]string(nil), model.Input...)
	model.ReasoningLevels = append([]string(nil), model.ReasoningLevels...)
	return model
}

func fetchModelCatalog(ctx context.Context, rawURL string) (modelsDevCatalog, error) {
	return fetchModelCatalogWithClient(ctx, rawURL, http.DefaultClient)
}

func fetchModelCatalogWithClient(ctx context.Context, rawURL string, baseClient *http.Client) (modelsDevCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("droids: create model catalog request: %w", err)
	}
	requireHTTPS := req.URL.Scheme == "https"
	client := *baseClient
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if requireHTTPS && next.URL.Scheme != "https" {
			return fmt.Errorf("droids: model catalog redirect downgraded to %s", next.URL.Scheme)
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("droids: stopped after 10 model catalog redirects")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("droids: fetch model catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("droids: fetch model catalog: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("droids: read model catalog: %w", err)
	}
	if len(data) > 20<<20 {
		return nil, fmt.Errorf("droids: model catalog exceeds 20 MiB")
	}
	var catalog modelsDevCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("droids: parse model catalog: %w", err)
	}
	return catalog, nil
}
