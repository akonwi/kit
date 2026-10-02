package droids

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func fullImagePolicy() ImagePolicy {
	return ImagePolicy{
		Placements: []ImagePlacement{ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult},
		Formats:    []string{ImageJPEG, ImagePNG, ImageGIF, ImageWebP},
		Sources:    []ImageSourceKind{ImageSourceData, ImageSourceHTTPS},
	}
}

func userOnlyImagePolicy() ImagePolicy {
	policy := fullImagePolicy()
	policy.Placements = []ImagePlacement{ImagePlacementUser}
	return policy
}

func TestImagePolicyValidation(t *testing.T) {
	fit := func(w, h int) (int, int) { return w, h }
	tests := []struct {
		name   string
		policy ImagePolicy
		want   string
	}{
		{"text-only zero value", ImagePolicy{}, ""},
		{"full policy", ImagePolicy{
			Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG}, Sources: []ImageSourceKind{ImageSourceData},
			Fit: fit, MaxEncodedBytes: 10, MaxImages: 2, MaxRequestImageBytes: 20, ManyImages: &ManyImageRule{Above: 1, Fit: fit},
		}, ""},
		{"constraints without placements", ImagePolicy{Formats: []string{ImagePNG}}, "image constraints are declared without placements"},
		{"unknown placement", ImagePolicy{Placements: []ImagePlacement{"system"}, Formats: []string{ImagePNG}, Sources: []ImageSourceKind{ImageSourceData}}, "unsupported placement system"},
		{"duplicate format", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG, ImagePNG}, Sources: []ImageSourceKind{ImageSourceData}}, "duplicate format image/png"},
		{"unknown format", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{"image/bmp"}, Sources: []ImageSourceKind{ImageSourceData}}, "unsupported format image/bmp"},
		{"no preparable format", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImageWebP}, Sources: []ImageSourceKind{ImageSourceData}}, "formats must include image/jpeg or image/png"},
		{"no sources", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG}}, "at least one source is required"},
		{"https only", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG}, Sources: []ImageSourceKind{ImageSourceHTTPS}}, `sources must include "data"`},
		{"negative limit", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG}, Sources: []ImageSourceKind{ImageSourceData}, MaxImages: -1}, "image limits must not be negative"},
		{"many-image rule without fit", ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}, Formats: []string{ImagePNG}, Sources: []ImageSourceKind{ImageSourceData}, ManyImages: &ManyImageRule{Above: 20}}, "many-image rule requires a positive threshold and a fit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.policy.validate()
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != test.want {
				t.Fatalf("validate() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBindModelRequiresConsistentImagePolicy(t *testing.T) {
	textModel := Model{Provider: "test", ID: "text", Input: []string{"text"}}
	imageModel := Model{Provider: "test", ID: "vision", Input: []string{"text", "image"}}
	imagePolicy := func(model Model) ImagePolicy {
		if containsString(model.Input, "image") {
			return userOnlyImagePolicy()
		}
		return ImagePolicy{}
	}
	tests := []struct {
		name     string
		provider Provider
		model    Model
		want     string
	}{
		{"adapted text model", AdaptProvider("test", []Model{textModel}, nil), textModel, ""},
		{"adapted image model without policy", AdaptProvider("test", []Model{imageModel}, nil), imageModel,
			"droids: model test/vision advertises image input but its image policy accepts no images"},
		{"adapted image model with policy", AdaptProviderWithImagePolicy("test", []Model{imageModel}, nil, imagePolicy), imageModel, ""},
		{"policy accepts images for text model", AdaptProviderWithImagePolicy("test", []Model{textModel}, nil, func(Model) ImagePolicy { return userOnlyImagePolicy() }), textModel,
			"droids: model test/text accepts images in its image policy but does not advertise image input"},
		{"malformed policy", AdaptProviderWithImagePolicy("test", []Model{imageModel}, nil, func(Model) ImagePolicy {
			return ImagePolicy{Placements: []ImagePlacement{ImagePlacementUser}}
		}), imageModel, "droids: model test/vision image policy: at least one format is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BindModel(test.provider, test.model)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != test.want {
				t.Fatalf("BindModel error = %q, want %q", got, test.want)
			}
		})
	}
}

type imagePolicyTestConfig struct{ entry providerEntry }

func (c imagePolicyTestConfig) build() (providerEntry, error) { return c.entry, nil }

func TestNewProvidersRequiresImagePolicies(t *testing.T) {
	stream := func(context.Context, Model, Request, callOptions) Stream { return nil }
	imageModel := Model{Provider: "custom", ID: "vision", Input: []string{"text", "image"}}
	tests := []struct {
		name  string
		entry providerEntry
		want  string
	}{
		{"undeclared policy", providerEntry{id: "custom", models: map[string]Model{}, stream: stream},
			`droids: provider "custom" does not declare an image policy`},
		{"inconsistent model", providerEntry{id: "custom", models: map[string]Model{"vision": imageModel}, stream: stream,
			imagePolicy: func(Model) ImagePolicy { return ImagePolicy{} }},
			"droids: model custom/vision advertises image input but its image policy accepts no images"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewProviders(imagePolicyTestConfig{entry: test.entry})
			if err == nil || err.Error() != test.want {
				t.Fatalf("NewProviders error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuiltInProviderImagePolicies(t *testing.T) {
	providers, err := NewProviders(
		Anthropic{}, OpenAI{}, OpenAICodex{Credentials: staticCodexCredentials()}, OpenCodeGo{},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		selector string
		want     ImagePolicy
	}{
		{"anthropic/claude-opus-5-5", fullImagePolicy()},
		{"openai/gpt-5.4", fullImagePolicy()},
		{"openai-codex/gpt-5.4", fullImagePolicy()},
		{"openai-codex/gpt-5.3-codex-spark", ImagePolicy{}},
		{"opencode-go/minimax-m3", fullImagePolicy()},    // Anthropic Messages
		{"opencode-go/grok-4.7", fullImagePolicy()},      // OpenAI Responses
		{"opencode-go/kimi-k2.6", userOnlyImagePolicy()}, // Chat Completions
		{"opencode-go/minimax-m2.7", ImagePolicy{}},      // text-only
		{"opencode-go/glm-5.3", ImagePolicy{}},           // text-only
	}
	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			model, err := providers.Resolve(test.selector)
			if err != nil {
				t.Fatal(err)
			}
			if got := model.ImagePolicy(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ImagePolicy() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestModelImagePolicyIsACopy(t *testing.T) {
	providers, err := NewProviders(Anthropic{})
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("anthropic/claude-opus-5-5")
	if err != nil {
		t.Fatal(err)
	}
	policy := model.ImagePolicy()
	policy.Placements[0] = ImagePlacementToolResult
	if got := model.ImagePolicy(); !reflect.DeepEqual(got, fullImagePolicy()) {
		t.Fatalf("policy after caller mutation = %#v, want %#v", got, fullImagePolicy())
	}
	if got := (Model{Provider: "anthropic", ID: "unbound"}).ImagePolicy(); !reflect.DeepEqual(got, ImagePolicy{}) {
		t.Fatalf("unbound model policy = %#v, want zero", got)
	}
}

func TestRefreshModelsSkipsModelsWithInconsistentImagePolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"openai":{"models":{
			"gpt-text":{"id":"gpt-text","name":"Text","family":"gpt","tool_call":true,"modalities":{"input":["text"],"output":["text"]},
				"limit":{"context":128000,"output":16000},"cost":{"input":1,"output":2}},
			"gpt-vision":{"id":"gpt-vision","name":"Vision","family":"gpt","tool_call":true,"modalities":{"input":["text","image"],"output":["text"]},
				"limit":{"context":128000,"output":16000},"cost":{"input":1,"output":2}}
		}}}`))
	}))
	defer server.Close()

	textOnly := providerEntry{
		id: "custom", catalogID: "openai", models: map[string]Model{},
		stream:      func(context.Context, Model, Request, callOptions) Stream { return nil },
		imagePolicy: func(Model) ImagePolicy { return ImagePolicy{} },
	}
	providers, err := NewProviders(imagePolicyTestConfig{entry: textOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.(*registry).refreshModels(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range providers.Models() {
		ids = append(ids, model.Provider+"/"+model.ID)
	}
	if want := []string{"custom/gpt-text"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("models after refresh = %v, want %v", ids, want)
	}
}
