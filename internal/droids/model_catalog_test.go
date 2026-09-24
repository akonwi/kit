package droids

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuiltInProviderCatalogs(t *testing.T) {
	openAI := OpenAIModels()
	anthropic := AnthropicModels()
	codex := OpenAICodexModels()
	if len(openAI) == 0 || len(anthropic) == 0 || len(codex) == 0 {
		t.Fatalf("catalog sizes: openai=%d anthropic=%d codex=%d", len(openAI), len(anthropic), len(codex))
	}

	model, ok := OpenAIModel("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini missing from built-in catalog")
	}
	if model.Provider != "openai" || model.API != ModelAPIOpenAIResponses || model.ContextWindow != 128_000 || model.MaxOutputTokens != 16_384 {
		t.Fatalf("model = %#v", model)
	}
	for _, candidate := range openAI {
		if candidate.API != ModelAPIOpenAIResponses || candidate.ContextWindow <= 0 || candidate.MaxOutputTokens <= 0 || !containsString(candidate.Input, "text") {
			t.Fatalf("invalid OpenAI catalog model %#v", candidate)
		}
		if strings.Contains(candidate.ID, "realtime") || candidate.ID == "gpt-5.6" {
			t.Fatalf("Responses catalog includes unsupported model %q", candidate.ID)
		}
	}
	for _, candidate := range anthropic {
		if candidate.API != ModelAPIAnthropicMessages || candidate.ContextWindow <= 0 || candidate.MaxOutputTokens <= 0 || !containsString(candidate.Input, "text") {
			t.Fatalf("invalid Anthropic catalog model %#v", candidate)
		}
	}
	if model.Cost.Input == 0 || model.Cost.Output == 0 {
		t.Fatalf("model cost = %#v", model.Cost)
	}
	for _, candidate := range codex {
		if candidate.Provider != "openai-codex" || candidate.API != ModelAPIOpenAICodexResponses || candidate.BaseURL != defaultOpenAICodexBaseURL || candidate.OutputLimitMode != OutputLimitProviderControlled {
			t.Fatalf("invalid Codex catalog model %#v", candidate)
		}
		if candidate.Cost != (Cost{}) {
			t.Fatalf("Codex subscription model has API pricing %#v", candidate.Cost)
		}
	}
	astra, ok := OpenAICodexModel("gpt-6-astra")
	if !ok || astra.Name != "GPT-6 Astra" || astra.ContextWindow != 272_000 || astra.MaxOutputTokens != 128_000 {
		t.Fatalf("Codex Astra model = %#v, %v", astra, ok)
	}
	if got, want := strings.Join(astra.ReasoningLevels, ","), "low,medium,high,xhigh,max"; got != want {
		t.Fatalf("Codex Astra reasoning levels = %q, want %q", got, want)
	}
	codex[0].ReasoningLevels[0] = "mutated"
	codexAgain, _ := OpenAICodexModel(codex[0].ID)
	if codexAgain.ReasoningLevels[0] == "mutated" {
		t.Fatal("Codex catalog returned shared nested storage")
	}

	// Catalog accessors return copies, including nested slices.
	model.Input[0] = "mutated"
	again, _ := OpenAIModel("gpt-4o-mini")
	if again.Input[0] != "text" {
		t.Fatalf("catalog model was mutated: %#v", again.Input)
	}
}

func TestProviderRegistersBuiltInCatalog(t *testing.T) {
	providers, err := NewProviders(OpenAI{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai/gpt-4o-mini")
	if !ok {
		t.Fatal("built-in model did not resolve")
	}
	if model.BaseURL != defaultOpenAIBaseURL || model.ContextWindow == 0 {
		t.Fatalf("model = %#v", model)
	}
}

func TestProvidersStreamUsesCanonicalProviderModel(t *testing.T) {
	providers, err := NewProviders(OpenAICodex{Credentials: staticCodexCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("openai-codex/gpt-5.6-sol")
	if !ok {
		t.Fatal("gpt-5.6-sol missing")
	}
	forged := model
	forged.MaxOutputTokens = 1_000_000
	stream := providers.Stream(context.Background(), forged, Request{MaxTokens: model.MaxOutputTokens + 1})
	for range stream.Events() {
	}
	if got := stream.Result(); got.StopReason != StopReasonError || !strings.Contains(got.ErrorMessage, "does not support") {
		t.Fatalf("forged model bypassed canonical capabilities: %#v", got)
	}

	forged.ID = "not-owned"
	stream = providers.Stream(context.Background(), forged, Request{})
	for range stream.Events() {
	}
	if got := stream.Result(); !strings.Contains(got.ErrorMessage, "does not own") {
		t.Fatalf("unowned model result = %#v", got)
	}
}

func TestOpenAICodexStaticCatalogSkipsDynamicRefresh(t *testing.T) {
	providers, err := NewProviders(OpenAICodex{Credentials: staticCodexCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.(*registry).refreshModels(context.Background(), "://invalid"); err != nil {
		t.Fatalf("static Codex catalog attempted a dynamic refresh: %v", err)
	}
}

func TestOpenAICodexModelsRequireNamespaceWhenAmbiguous(t *testing.T) {
	providers, err := NewProviders(OpenAI{}, OpenAICodex{Credentials: staticCodexCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers.Model("gpt-5.4"); ok {
		t.Fatal("ambiguous OpenAI/Codex model resolved without a namespace")
	}
	if model, ok := providers.Model("openai-codex/gpt-5.4"); !ok || model.API != ModelAPIOpenAICodexResponses {
		t.Fatalf("namespaced Codex model = %#v, %v", model, ok)
	}
}

func TestProvidersStreamValidatesModelCapabilities(t *testing.T) {
	providers, err := NewProviders(OpenAI{})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini missing")
	}
	for _, request := range []Request{
		{Reasoning: "low", MaxTokens: 100},
		{MaxTokens: model.MaxOutputTokens + 1},
	} {
		stream := providers.Stream(context.Background(), model, request)
		for range stream.Events() {
		}
		if message := stream.Result(); message.StopReason != StopReasonError {
			t.Fatalf("request %#v returned %#v", request, message)
		}
	}
}

func TestResolvedModelBindingAndContextWindowCopy(t *testing.T) {
	providers, err := NewProviders(OpenAI{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("openai/gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}
	provider := model.boundProvider()
	if provider == nil {
		t.Fatal("resolved model did not retain its provider binding")
	}
	originalWindow := model.ContextWindow
	overridden := model.WithContextWindow(1_000_000)
	if model.ContextWindow != originalWindow || overridden.ContextWindow != 1_000_000 {
		t.Fatalf("WithContextWindow mutated source: source=%d copy=%d", model.ContextWindow, overridden.ContextWindow)
	}
	if reset := overridden.WithContextWindow(0); reset.ContextWindow != originalWindow {
		t.Fatalf("reset context window = %d, want %d", reset.ContextWindow, originalWindow)
	}
	if overridden.boundProvider() != provider {
		t.Fatal("WithContextWindow dropped provider binding")
	}
}

func TestProviderCatalogUsesConfiguredIdentityAndBaseURL(t *testing.T) {
	providers, err := NewProviders(OpenAI{
		ID:      "gateway",
		APIKey:  "test",
		BaseURL: "https://gateway.example/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("gateway/gpt-4o-mini")
	if !ok {
		t.Fatal("gateway model did not resolve")
	}
	if model.Provider != "gateway" || model.BaseURL != "https://gateway.example/v1" {
		t.Fatalf("model = %#v", model)
	}
}

func TestRefreshModelsOverlaysCompleteCatalogEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"openai":{"models":{
				"gpt-4o-mini":{
					"id":"gpt-4o-mini","name":"GPT-4o mini refreshed","family":"gpt-mini","tool_call":true,
					"reasoning":false,"modalities":{"input":["text","image"],"output":["text"]},
					"limit":{"context":256000,"output":32000},
					"cost":{"input":1,"output":2,"cache_read":0.5}
				},
				"gpt-new":{
					"id":"gpt-new","name":"GPT New","family":"gpt","tool_call":true,"reasoning":true,
					"modalities":{"input":["text"],"output":["text"]},
					"limit":{"context":400000,"input":272000,"output":128000},
					"cost":{"input":3,"output":12}
				},
				"incomplete":{"id":"incomplete","name":"Incomplete","tool_call":true}
			}},
			"anthropic":{"models":{}}
		}`))
	}))
	defer server.Close()

	providers, err := NewProviders(OpenAI{ID: "gateway", BaseURL: "https://gateway.example/v1"})
	if err != nil {
		t.Fatal(err)
	}
	droid, err := Spawn(context.Background(), "catalog-refresh", Config{
		Store: NewMemoryStore(), Model: resolvedTestModel(providers, "gateway/gpt-4o-mini"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer droid.Close()
	originalContextWindow := droid.model.ContextWindow

	registry := providers.(*registry)
	if err := registry.refreshModels(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}

	refreshed, ok := providers.Model("gateway/gpt-4o-mini")
	if !ok || refreshed.Name != "GPT-4o mini refreshed" || refreshed.ContextWindow != 256_000 {
		t.Fatalf("refreshed model = %#v", refreshed)
	}
	if droid.model.ContextWindow != originalContextWindow || droid.model.ContextWindow == refreshed.ContextWindow {
		t.Fatalf("existing Droid model changed after refresh: %#v", droid.model)
	}
	added, ok := providers.Model("gateway/gpt-new")
	if !ok {
		t.Fatal("new dynamic model did not resolve")
	}
	if added.ContextWindow != 400_000 || added.MaxInputTokens != 272_000 || added.MaxOutputTokens != 128_000 {
		t.Fatalf("new model limits = %#v", added)
	}
	if added.Provider != "gateway" || added.BaseURL != "https://gateway.example/v1" {
		t.Fatalf("new model routing = %#v", added)
	}
	if _, ok := providers.Model("gateway/incomplete"); ok {
		t.Fatal("incomplete dynamic model should not be registered")
	}
}

func TestCatalogRejectsMalformedEntries(t *testing.T) {
	valid := modelsDevModel{ID: "valid", Name: "Valid", Family: "gpt", ToolCall: true}
	valid.Modalities.Input = []string{"text"}
	valid.Modalities.Output = []string{"text"}
	valid.Limit.Context = 1000
	valid.Limit.Output = 100

	tests := []struct {
		name   string
		mutate func(*modelsDevModel)
	}{
		{name: "no text input", mutate: func(m *modelsDevModel) { m.Modalities.Input = []string{"image"} }},
		{name: "negative input limit", mutate: func(m *modelsDevModel) { m.Limit.Input = -1 }},
		{name: "input exceeds context", mutate: func(m *modelsDevModel) { m.Limit.Input = 1001 }},
		{name: "output exceeds context", mutate: func(m *modelsDevModel) { m.Limit.Output = 1001 }},
		{name: "negative cost", mutate: func(m *modelsDevModel) { m.Cost.Input = -1 }},
		{name: "wrong API family", mutate: func(m *modelsDevModel) { m.ID = "gpt-realtime-test" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := valid
			model.Modalities.Input = append([]string(nil), valid.Modalities.Input...)
			model.Modalities.Output = append([]string(nil), valid.Modalities.Output...)
			tt.mutate(&model)
			if _, ok := modelFromCatalog(modelsDevProvider{NPM: "@ai-sdk/openai"}, "openai", "openai", model); ok {
				t.Fatalf("accepted malformed model %#v", model)
			}
		})
	}
}

func TestModelCatalogRejectsHTTPSDowngradeRedirect(t *testing.T) {
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, insecure.URL, http.StatusFound)
	}))
	defer secure.Close()

	_, err := fetchModelCatalogWithClient(context.Background(), secure.URL, secure.Client())
	if err == nil || !strings.Contains(err.Error(), "downgraded") {
		t.Fatalf("error = %v", err)
	}
}

func TestRefreshModelsFailurePreservesCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	providers, err := NewProviders(Anthropic{})
	if err != nil {
		t.Fatal(err)
	}
	before := len(providers.Models())
	err = providers.(*registry).refreshModels(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("refresh error = %v", err)
	}
	if after := len(providers.Models()); after != before {
		t.Fatalf("catalog changed after failed refresh: before=%d after=%d", before, after)
	}
}

func TestCatalogReasoningCapabilities(t *testing.T) {
	nonReasoning, ok := OpenAIModel("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini missing")
	}
	if err := validateReasoning(nonReasoning, "low"); err == nil {
		t.Fatal("non-reasoning model accepted explicit reasoning")
	}

	restricted, ok := OpenAIModel("gpt-5.4")
	if !ok {
		t.Fatal("gpt-5.4 missing")
	}
	if err := validateReasoning(restricted, "minimal"); err == nil {
		t.Fatal("model accepted reasoning level absent from catalog")
	}
	if err := validateReasoning(restricted, "high"); err != nil {
		t.Fatalf("supported reasoning level rejected: %v", err)
	}

	effortOnly, ok := AnthropicModel("claude-opus-4-7")
	if !ok {
		t.Fatal("claude-opus-4-7 missing")
	}
	if effortOnly.ReasoningMode != ReasoningModeAdaptive {
		t.Fatalf("effort-only reasoning mode = %q", effortOnly.ReasoningMode)
	}
	if err := validateReasoning(effortOnly, "high"); err != nil {
		t.Fatalf("adaptive effort reasoning rejected: %v", err)
	}

	fable, ok := AnthropicModel("claude-fable-5-1")
	if !ok {
		t.Fatal("claude-fable-5-1 missing")
	}
	if fable.ReasoningMode != ReasoningModeAdaptive || !fable.SupportsMidConversationEffort || !containsString(fable.ReasoningLevels, "medium") {
		t.Fatalf("Fable reasoning capabilities = %#v", fable)
	}

	opus, ok := AnthropicModel("claude-opus-5-5")
	if !ok {
		t.Fatal("claude-opus-5-5 missing")
	}
	if opus.ReasoningMode != ReasoningModeAdaptive || !opus.SupportsMidConversationEffort || !containsString(opus.ReasoningLevels, "xhigh") {
		t.Fatalf("Opus 5.5 reasoning capabilities = %#v", opus)
	}

	budgetTokens, ok := AnthropicModel("claude-haiku-4-5")
	if !ok {
		t.Fatal("claude-haiku-4-5 missing")
	}
	if err := validateReasoning(budgetTokens, "high"); err != nil {
		t.Fatalf("budget-token reasoning rejected: %v", err)
	}

	levels := catalogBudgetReasoningLevels([]modelsDevReasoningOption{{Type: "budget_tokens", Min: 5000}})
	if containsString(levels, "minimal") || containsString(levels, "low") || !containsString(levels, "medium") {
		t.Fatalf("levels for 5000-token minimum = %#v", levels)
	}
}

func TestResolveRequestMaxTokens(t *testing.T) {
	model := Model{ID: "m", MaxOutputTokens: 8_000}
	got, err := resolveRequestMaxTokens(model, 0, "")
	if err != nil || got != defaultRequestMaxTokens {
		t.Fatalf("default = %d, %v", got, err)
	}
	got, err = resolveRequestMaxTokens(Model{ID: "small", MaxOutputTokens: 2_000}, 0, "")
	if err != nil || got != 2_000 {
		t.Fatalf("capped default = %d, %v", got, err)
	}
	if _, err := resolveRequestMaxTokens(model, 9_000, ""); err == nil {
		t.Fatal("expected output capability validation error")
	}
}
