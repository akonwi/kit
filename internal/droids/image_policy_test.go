package droids

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func userOnlyImagePolicy() ImagePolicy {
	return ImagePolicy{
		Placements: []ImagePlacement{ImagePlacementUser},
		Formats:    []string{ImageJPEG, ImagePNG, ImageGIF, ImageWebP},
		Sources:    []ImageSourceKind{ImageSourceData, ImageSourceHTTPS},
	}
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

// policySummary is the comparable form of an ImagePolicy: its declared values
// plus the dimensions its fits produce for reference images.
type policySummary struct {
	Placements           []ImagePlacement
	Formats              []string
	Sources              []ImageSourceKind
	MaxEncodedBytes      int64
	MaxImages            int
	MaxRequestImageBytes int64
	// Fit4K is Fit applied to a 3840×2160 image.
	Fit4K [2]int
	// ManyAbove and ManyFit describe the many-image rule; ManyFit is its fit
	// applied to a 2576×1449 image.
	ManyAbove int
	ManyFit   [2]int
}

func summarizePolicy(policy ImagePolicy) policySummary {
	summary := policySummary{
		Placements: policy.Placements, Formats: policy.Formats, Sources: policy.Sources,
		MaxEncodedBytes: policy.MaxEncodedBytes, MaxImages: policy.MaxImages, MaxRequestImageBytes: policy.MaxRequestImageBytes,
	}
	if policy.Fit != nil {
		summary.Fit4K[0], summary.Fit4K[1] = policy.Fit(3840, 2160)
	}
	if policy.ManyImages != nil {
		summary.ManyAbove = policy.ManyImages.Above
		summary.ManyFit[0], summary.ManyFit[1] = policy.ManyImages.Fit(2576, 1449)
	}
	return summary
}

func TestBuiltInProviderImagePolicies(t *testing.T) {
	providers, err := NewProviders(
		Anthropic{}, OpenAI{}, OpenAICodex{Credentials: staticCodexCredentials()}, OpenCodeGo{},
	)
	if err != nil {
		t.Fatal(err)
	}
	all := []ImagePlacement{ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult}
	formats := []string{ImageJPEG, ImagePNG, ImageGIF, ImageWebP}
	inlineOrRemote := []ImageSourceKind{ImageSourceData, ImageSourceHTTPS}
	anthropic := func(fit [2]int, maxImages int) policySummary {
		return policySummary{
			Placements: all, Formats: formats, Sources: inlineOrRemote,
			MaxEncodedBytes: 10_000_000, MaxImages: maxImages, MaxRequestImageBytes: 24_000_000,
			Fit4K: fit, ManyAbove: 20, ManyFit: [2]int{2000, 1125},
		}
	}
	openAI := func(sources []ImageSourceKind, fit [2]int) policySummary {
		return policySummary{
			Placements: all, Formats: formats, Sources: sources,
			MaxImages: 1500, MaxRequestImageBytes: 480_000_000, Fit4K: fit,
		}
	}
	defaultHigh := [2]int{2048, 1152} // 2048 px leaves 64 × 36 = 2,304 patches
	userOnly := []ImagePlacement{ImagePlacementUser}
	openCodeGo := func(placements []ImagePlacement, requestBytes int64) policySummary {
		return policySummary{
			Placements: placements, Formats: []string{ImageJPEG, ImagePNG}, Sources: []ImageSourceKind{ImageSourceData},
			MaxEncodedBytes: 5_000_000, MaxRequestImageBytes: requestBytes, Fit4K: [2]int{2048, 1152},
		}
	}
	tests := []struct {
		selector string
		want     policySummary
	}{
		{"anthropic/claude-opus-5-5", anthropic([2]int{2576, 1449}, 600)}, // high-resolution tier
		{"anthropic/claude-haiku-4-5", anthropic([2]int{1456, 819}, 100)}, // standard tier, 200k context
		{"openai/gpt-5.4", openAI(inlineOrRemote, defaultHigh)},
		{"openai/gpt-6-astra", openAI(inlineOrRemote, [2]int{2104, 1184})}, // 66 × 37 = 2,442 patches
		{"openai/gpt-4o", openAI(inlineOrRemote, [2]int{1365, 768})},       // tile-based
		{"openai-codex/gpt-5.4", openAI([]ImageSourceKind{ImageSourceData}, defaultHigh)},
		{"openai-codex/gpt-5.3-codex-spark", policySummary{}},
		{"opencode-go/minimax-m3", openCodeGo(all, 32_000_000)},         // tool results documented
		{"opencode-go/qwen3.8-flash", openCodeGo(userOnly, 4_000_000)},  // 6 MB Anthropic-compatible body
		{"opencode-go/grok-4.7", openCodeGo(userOnly, 32_000_000)},      // xAI over Responses
		{"opencode-go/kimi-k2.6", openCodeGo(userOnly, 32_000_000)},     // Chat Completions
		{"opencode-go/gpt-6-luna", openAI(inlineOrRemote, defaultHigh)}, // OpenAI over Responses
		{"opencode-go/minimax-m2.7", policySummary{}},
		{"opencode-go/glm-5.3", policySummary{}},
	}
	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			model, err := providers.Resolve(test.selector)
			if err != nil {
				t.Fatal(err)
			}
			if got := summarizePolicy(model.ImagePolicy()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ImagePolicy() = %+v\nwant %+v", got, test.want)
			}
		})
	}
}

func TestOpenCodeGoImageExceptionsMatchCatalog(t *testing.T) {
	providers, err := NewProviders(OpenCodeGo{})
	if err != nil {
		t.Fatal(err)
	}
	for id, exception := range openCodeGoImageExceptions {
		model, ok := providers.Model("opencode-go/" + id)
		if !ok || model.API != exception.api || !containsString(model.Input, "image") {
			t.Errorf("exception %q reviewed for %s: catalog model = %+v (found %v)", id, exception.api, model, ok)
		}
	}
}

func TestOpenCodeGoImageExceptionRequiresReviewedAPI(t *testing.T) {
	model := Model{ID: "minimax-m3", API: ModelAPIOpenAIChat, Input: []string{"text", "image"}}
	if got := openCodeGoImagePolicy(model).Placements; !reflect.DeepEqual(got, []ImagePlacement{ImagePlacementUser}) {
		t.Fatalf("placements over an unreviewed API = %v, want the user-only envelope", got)
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
	policy.ManyImages.Above = 1
	again := model.ImagePolicy()
	if again.Placements[0] != ImagePlacementUser || again.ManyImages.Above != 20 {
		t.Fatalf("policy after caller mutation = %+v, want the provider's declaration", summarizePolicy(again))
	}
	if got := (Model{Provider: "anthropic", ID: "unbound"}).ImagePolicy(); !reflect.DeepEqual(got, ImagePolicy{}) {
		t.Fatalf("unbound model policy = %#v, want zero", got)
	}
}

func TestOpenAIToolResultImagesUseHighDetail(t *testing.T) {
	output, err := openAIToolOutput([]ResultContent{NewImageData(ImagePNG, []byte("png"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 || output[0].OfInputImage == nil || output[0].OfInputImage.Detail != "high" {
		t.Fatalf("tool output = %#v, want one high-detail image", output)
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
