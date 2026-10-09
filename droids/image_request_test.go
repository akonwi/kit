package droids

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids/internal/imageprep"
)

func TestDecodeImageDataURLRejectsOversizeBeforeDecoding(t *testing.T) {
	payload := strings.Repeat("%", base64.StdEncoding.EncodedLen(MaxImageBytes)+1)
	if _, err := decodeImageDataURL("data:image/png;base64," + payload); !errors.Is(err, imageprep.ErrSourceTooLarge) {
		t.Fatalf("decode oversized image error = %v, want source too large", err)
	}
}

func TestDecodeImageDataURLRejectsOversizedHeader(t *testing.T) {
	header := "data:image/png;" + strings.Repeat("x", maxImageDataURLHeaderBytes) + ";base64,"
	if _, err := decodeImageDataURL(header + "AA=="); err == nil {
		t.Fatal("decode image with oversized header succeeded")
	}
}

func testPNGDataURL(t *testing.T, w, h int) string {
	t.Helper()
	return imageDataURL(ImagePNG, testPNG(t, w, h))
}

// testPNG encodes an opaque white PNG, which preparation sends unchanged when
// it fits the model's policy.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for index := range img.Pix {
		img.Pix[index] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageDimensions(t *testing.T, rawURL string) (string, int, int) {
	t.Helper()
	data, err := decodeImageDataURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return format, config.Width, config.Height
}

func longEdgeFit(limit int) ImageFit {
	return func(w, h int) (int, int) {
		if w <= limit && h <= limit {
			return w, h
		}
		if w >= h {
			return limit, max(1, h*limit/w)
		}
		return max(1, w*limit/h), limit
	}
}

func testImagePolicy() ImagePolicy {
	return ImagePolicy{
		Placements: []ImagePlacement{ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult},
		Formats:    []string{ImageJPEG, ImagePNG},
		Sources:    []ImageSourceKind{ImageSourceData},
	}
}

func toolResultWithImages(urls ...string) ToolResultMessage {
	content := []ResultContent{TextContent{Text: "captured"}}
	for _, url := range urls {
		content = append(content, FileContent{MediaType: ImagePNG, URL: url})
	}
	return ToolResultMessage{ToolCallID: "call", ToolName: "screenshot", Content: content}
}

func TestPrepareRequestImagesWithoutImagesReturnsMessages(t *testing.T) {
	messages := []Message{UserMessage{Content: []InputContent{TextInput{Text: "hi"}}}}
	prepared := prepareRequestImages(ImagePolicy{}, messages)
	if &prepared[0] != &messages[0] {
		t.Fatalf("prepared = %#v, want the original messages", prepared)
	}
}

func TestPrepareRequestImagesOmitsImagesTheModelCannotReceive(t *testing.T) {
	small := testPNGDataURL(t, 4, 4)
	userOnly := testImagePolicy()
	userOnly.Placements = []ImagePlacement{ImagePlacementUser}
	tests := []struct {
		name    string
		policy  ImagePolicy
		message Message
		want    Message
	}{
		{
			name:    "text-only model",
			policy:  ImagePolicy{},
			message: UserMessage{Content: []InputContent{TextInput{Text: "look"}, FileInput{Filename: "shot.png", MediaType: ImagePNG, URL: small}}},
			want: UserMessage{Content: []InputContent{TextInput{Text: "look"},
				TextInput{Text: `[Image "shot.png" omitted: this model does not accept image input.]`}}},
		},
		{
			name:    "unsupported placement",
			policy:  userOnly,
			message: toolResultWithImages(small),
			want: ToolResultMessage{ToolCallID: "call", ToolName: "screenshot", Content: []ResultContent{TextContent{Text: "captured"},
				TextContent{Text: "[Image omitted: this model does not accept images in tool results.]"}}},
		},
		{
			name:    "remote URL",
			policy:  testImagePolicy(),
			message: ContextMessage{Content: []InputContent{FileInput{MediaType: ImagePNG, URL: "https://example.com/a.png"}}},
			want:    ContextMessage{Content: []InputContent{TextInput{Text: "[Image omitted: this model does not accept remote image URLs.]"}}},
		},
		{
			name:    "invalid data",
			policy:  testImagePolicy(),
			message: toolResultWithImages("data:image/png;base64,!!!"),
			want: ToolResultMessage{ToolCallID: "call", ToolName: "screenshot", Content: []ResultContent{TextContent{Text: "captured"},
				TextContent{Text: "[Image omitted: the image data is invalid.]"}}},
		},
		{
			name:    "undecodable image",
			policy:  testImagePolicy(),
			message: toolResultWithImages(imageDataURL(ImagePNG, []byte("not a png"))),
			want: ToolResultMessage{ToolCallID: "call", ToolName: "screenshot", Content: []ResultContent{TextContent{Text: "captured"},
				TextContent{Text: "[Image omitted: the image format is not supported.]"}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared := prepareRequestImages(test.policy, []Message{test.message})
			if !reflect.DeepEqual(prepared, []Message{test.want}) {
				t.Fatalf("prepared = %#v\nwant %#v", prepared, []Message{test.want})
			}
		})
	}
}

func TestPrepareRequestImagesPassesAcceptedRemoteURLs(t *testing.T) {
	policy := testImagePolicy()
	policy.Sources = append(policy.Sources, ImageSourceHTTPS)
	messages := []Message{UserMessage{Content: []InputContent{FileInput{MediaType: ImagePNG, URL: "https://example.com/a.png"}}}}
	prepared := prepareRequestImages(policy, messages)
	if !reflect.DeepEqual(prepared, messages) {
		t.Fatalf("prepared = %#v, want unchanged", prepared)
	}
}

func TestPrepareRequestImagesResizesWithNoticeAndKeepsHistory(t *testing.T) {
	large, small := testPNGDataURL(t, 400, 200), testPNGDataURL(t, 40, 20)
	policy := testImagePolicy()
	policy.Fit = longEdgeFit(100)
	original := toolResultWithImages(small, large)
	messages := []Message{original}

	prepared := prepareRequestImages(policy, messages)
	result := prepared[0].(ToolResultMessage)
	if len(result.Content) != 4 {
		t.Fatalf("content = %#v, want text, unchanged image, resized image, notice", result.Content)
	}
	if result.Content[0] != (TextContent{Text: "captured"}) || result.Content[1] != (FileContent{MediaType: ImagePNG, URL: small}) {
		t.Fatalf("leading content = %#v", result.Content[:2])
	}
	resized := result.Content[2].(FileContent)
	if format, w, h := imageDimensions(t, resized.URL); resized.MediaType != ImagePNG || format != "png" || w != 100 || h != 50 {
		t.Fatalf("resized = %s %s %dx%d, want PNG 100x50", resized.MediaType, format, w, h)
	}
	if want := (TextContent{Text: "[Image 2 of 2 was resized from 400×200 to 100×50 pixels.]"}); result.Content[3] != want {
		t.Fatalf("notice = %#v, want %#v", result.Content[3], want)
	}
	if !reflect.DeepEqual(messages[0], original) || len(original.Content) != 3 || original.Content[2].(FileContent).URL != large {
		t.Fatal("preparation mutated the source messages")
	}
	again := prepareRequestImages(policy, messages)
	if !reflect.DeepEqual(again, prepared) {
		t.Fatal("preparation is not deterministic")
	}
}

func TestPrepareRequestImagesConvertsUnacceptedFormatsWithoutNotice(t *testing.T) {
	var buf bytes.Buffer
	palette := color.Palette{color.White, color.Black}
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 8, 8), palette), nil); err != nil {
		t.Fatal(err)
	}
	messages := []Message{UserMessage{Content: []InputContent{FileInput{Filename: "a.gif", MediaType: ImageGIF, URL: imageDataURL(ImageGIF, buf.Bytes())}}}}
	prepared := prepareRequestImages(testImagePolicy(), messages)
	content := prepared[0].(UserMessage).Content
	file, ok := content[0].(FileInput)
	if len(content) != 1 || !ok || file.Filename != "a.gif" || file.MediaType != ImagePNG || !strings.HasPrefix(file.URL, "data:image/png;base64,") {
		t.Fatalf("content = %#v, want one converted PNG and no notice", content)
	}
	if _, w, h := imageDimensions(t, file.URL); w != 8 || h != 8 {
		t.Fatalf("converted image = %dx%d, want 8x8", w, h)
	}
}

func TestPrepareRequestImagesRetainsRecentImagesInBatches(t *testing.T) {
	url := testPNGDataURL(t, 2, 2)
	tests := []struct {
		images    int
		maxImages int
		omitted   int
	}{
		{images: 20, omitted: 0},
		{images: 21, omitted: 10},
		{images: 30, omitted: 10},
		{images: 31, omitted: 20},
		{images: 5, maxImages: 4, omitted: 2},
	}
	for _, test := range tests {
		policy := testImagePolicy()
		policy.MaxImages = test.maxImages
		var messages []Message
		for range test.images {
			messages = append(messages, toolResultWithImages(url))
		}
		prepared := prepareRequestImages(policy, messages)
		omitted := TextContent{Text: "[Image omitted: only the most recent images in the conversation are sent.]"}
		for index, message := range prepared {
			image := message.(ToolResultMessage).Content[1]
			if index < test.omitted && image != omitted {
				t.Fatalf("%d images, max %d: message %d = %#v, want %#v", test.images, test.maxImages, index, image, omitted)
			}
			if _, kept := image.(FileContent); index >= test.omitted && !kept {
				t.Fatalf("%d images, max %d: message %d = %#v, want the image", test.images, test.maxImages, index, image)
			}
		}
	}
}

func TestPrepareRequestImagesAppliesManyImageRule(t *testing.T) {
	url := testPNGDataURL(t, 200, 100)
	policy := testImagePolicy()
	policy.Fit = longEdgeFit(100)
	policy.ManyImages = &ManyImageRule{Above: 2, Fit: longEdgeFit(50)}

	for _, test := range []struct {
		images, width int
	}{{2, 100}, {3, 50}} {
		var messages []Message
		for range test.images {
			messages = append(messages, toolResultWithImages(url))
		}
		prepared := prepareRequestImages(policy, messages)
		for _, message := range prepared {
			if _, w, _ := imageDimensions(t, message.(ToolResultMessage).Content[1].(FileContent).URL); w != test.width {
				t.Fatalf("%d images: width = %d, want %d", test.images, w, test.width)
			}
		}
	}
}

func TestPrepareRequestImagesOmitsOldestImagesOverRequestByteLimit(t *testing.T) {
	first, second := testPNGDataURL(t, 30, 30), testPNGDataURL(t, 31, 31)
	secondData, err := decodeImageDataURL(second)
	if err != nil {
		t.Fatal(err)
	}
	policy := testImagePolicy()
	policy.MaxRequestImageBytes = int64(len(second) - len("data:image/png;base64,"))
	if int64(len(secondData)) >= policy.MaxRequestImageBytes {
		t.Fatal("test limit must admit the second image")
	}
	prepared := prepareRequestImages(policy, []Message{toolResultWithImages(first), toolResultWithImages(second)})
	if got := prepared[0].(ToolResultMessage).Content[1]; got != (TextContent{Text: "[Image omitted: the request's total image size limit was reached.]"}) {
		t.Fatalf("first image = %#v, want size-limit placeholder", got)
	}
	if got := prepared[1].(ToolResultMessage).Content[1]; got != (FileContent{MediaType: ImagePNG, URL: second}) {
		t.Fatalf("second image = %#v, want unchanged", got)
	}
}

// A history image that a text-only model cannot receive must not make replay
// validation, and therefore a model switch, fail.
func TestReplayValidationPreparesHistoryImages(t *testing.T) {
	providers, err := NewProviders(OpenCodeGo{})
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("opencode-go/minimax-m2.7") // Anthropic Messages, text-only
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Messages: []Message{
		UserMessage{Content: []InputContent{TextInput{Text: "look"}}},
		AssistantMessage{StopReason: StopReasonToolUse, Content: []AssistantContent{ToolCall{ID: "call", Name: "screenshot", Arguments: []byte(`{}`)}}},
		toolResultWithImages(testPNGDataURL(t, 4, 4)),
	}}
	provider := model.boundProvider()
	if err := validatePreparedRequestReplay(t.Context(), provider, model, request); err == nil {
		t.Fatal("unprepared image replay was accepted; the test no longer exercises preparation")
	}
	if err := validateRequestReplay(t.Context(), provider, model, request); err != nil {
		t.Fatalf("replay validation = %v, want prepared replay accepted", err)
	}
}

func TestDispatchSendsPreparedToolResultImages(t *testing.T) {
	large := testPNGDataURL(t, 400, 200)
	largeData, err := decodeImageDataURL(large)
	if err != nil {
		t.Fatal(err)
	}
	policy := testImagePolicy()
	policy.Fit = longEdgeFit(100)

	var requests []Request
	stream := func(_ context.Context, model Model, request Request) Stream {
		requests = append(requests, request)
		message := AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "done"}}}
		if len(requests) == 1 {
			message.StopReason = StopReasonToolUse
			message.Content = []AssistantContent{ToolCall{ID: "call", Name: "screenshot", Arguments: []byte(`{}`)}}
		}
		return reconfigureStream{message: message}
	}
	model := Model{Provider: "test", ID: "vision", Input: []string{"text", "image"}, ContextWindow: 200_000, MaxOutputTokens: 8_000}
	bound, err := BindModel(AdaptProviderWithImagePolicy("test", []Model{model}, stream, func(Model) ImagePolicy { return policy }), model)
	if err != nil {
		t.Fatal(err)
	}
	tool := MustTool(Tool[struct{}]{Name: "screenshot", Execute: func(context.Context, ToolContext, struct{}, ToolUpdate) (ToolResult, error) {
		return ToolResult{Content: []ResultContent{TextContent{Text: "captured"}, NewImageData(ImagePNG, largeData)}}, nil
	}})
	d, err := Spawn(t.Context(), "image_dispatch", Config{Store: NewMemoryStore(), Model: bound, Tools: []AnyTool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	execution, err := d.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "take a screenshot"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := execution.Wait(t.Context()); err != nil || outcome.Status != ExecutionCompleted {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}

	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	sent := requests[1].Messages[len(requests[1].Messages)-1].(ToolResultMessage)
	if len(sent.Content) != 3 || sent.Content[0] != (TextContent{Text: "captured"}) ||
		sent.Content[2] != (TextContent{Text: "[Image 1 of 1 was resized from 400×200 to 100×50 pixels.]"}) {
		t.Fatalf("sent tool result = %#v", sent.Content)
	}
	if _, w, h := imageDimensions(t, sent.Content[1].(FileContent).URL); w != 100 || h != 50 {
		t.Fatalf("sent image = %dx%d, want 100x50", w, h)
	}

	d.sdk.mu.Lock()
	envelopes, err := runtimeMessageEnvelopes(d.sdk.state)
	d.sdk.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var stored ToolResultMessage
	for _, envelope := range envelopes {
		if result, ok := envelope.Message.(ToolResultMessage); ok {
			stored = result
		}
	}
	if len(stored.Content) != 2 || stored.Content[1].(FileContent).URL != large {
		t.Fatalf("stored tool result = %#v, want the original image", stored.Content)
	}
}

// Regression for a session whose history held a tool-result screenshot that
// every later request failed to translate. Replay must succeed on every
// built-in provider, whatever the model's image support.
func TestReplayWithToolResultScreenshotSucceedsOnEveryProvider(t *testing.T) {
	providers, err := NewProviders(Anthropic{}, OpenAI{}, OpenAICodex{Credentials: staticCodexCredentials()}, OpenCodeGo{})
	if err != nil {
		t.Fatal(err)
	}
	screenshot := testPNGDataURL(t, 1280, 917)
	for _, selector := range []string{
		"anthropic/claude-opus-5-5",
		"openai/gpt-5.4",
		"openai-codex/gpt-5.4",
		"openai-codex/gpt-5.3-codex-spark", // text-only
		"opencode-go/minimax-m3",           // Anthropic Messages with images
		"opencode-go/minimax-m2.7",         // Anthropic Messages, text-only
		"opencode-go/grok-4.7",             // OpenAI Responses
		"opencode-go/kimi-k2.6",            // Chat Completions, user images only
	} {
		t.Run(selector, func(t *testing.T) {
			model, err := providers.Resolve(selector)
			if err != nil {
				t.Fatal(err)
			}
			assistant := AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonToolUse, Content: []AssistantContent{
				ToolCall{ID: "call", ProviderCallID: "call", Name: "computer", Arguments: []byte(`{}`)},
			}}
			if model.Provider == "openai-codex" {
				assistant.ProviderScope = openAICodexProviderScope(staticCodexCredentials().AccountID)
			}
			request := Request{Messages: []Message{
				UserMessage{Content: []InputContent{TextInput{Text: "check the window"}}},
				assistant,
				ToolResultMessage{ToolCallID: "call", ProviderCallID: "call", ToolName: "computer", Content: []ResultContent{
					TextContent{Text: "Window: Kit"}, FileContent{MediaType: ImagePNG, URL: screenshot},
				}},
			}}
			if err := validateRequestReplay(t.Context(), model.boundProvider(), model, request); err != nil {
				t.Fatalf("replay validation = %v", err)
			}
		})
	}
}

func TestBuiltInPoliciesPrepareRetinaScreenshots(t *testing.T) {
	providers, err := NewProviders(Anthropic{}, OpenAI{}, OpenAICodex{Credentials: staticCodexCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	screenshot := testPNGDataURL(t, 2880, 1800)
	for _, test := range []struct {
		selector string
		w, h     int
	}{
		{"anthropic/claude-opus-5-5", 2420, 1512}, // high-resolution tier; Anthropic reference resize
		{"anthropic/claude-haiku-4-5", 1389, 868}, // standard tier; Anthropic reference resize
		{"openai/gpt-5.4", 1996, 1248},            // 2048 px and 2,500 patches
		{"openai-codex/gpt-5.4", 1996, 1248},
	} {
		t.Run(test.selector, func(t *testing.T) {
			model, err := providers.Resolve(test.selector)
			if err != nil {
				t.Fatal(err)
			}
			policy := model.ImagePolicy()
			prepared := prepareRequestImages(policy, []Message{toolResultWithImages(screenshot)})
			content := prepared[0].(ToolResultMessage).Content
			image := content[1].(FileContent)
			if _, w, h := imageDimensions(t, image.URL); w != test.w || h != test.h {
				t.Fatalf("prepared screenshot = %dx%d, want %dx%d", w, h, test.w, test.h)
			}
			if policy.MaxEncodedBytes > 0 && int64(len(image.URL)) > policy.MaxEncodedBytes+int64(len("data:image/png;base64,")) {
				t.Fatalf("prepared screenshot exceeds %d encoded bytes", policy.MaxEncodedBytes)
			}
			if want := fmt.Sprintf("[Image 1 of 1 was resized from 2880×1800 to %d×%d pixels.]", test.w, test.h); content[2] != (TextContent{Text: want}) {
				t.Fatalf("notice = %#v, want %q", content[2], want)
			}
		})
	}
}

func TestModelCheckUserImage(t *testing.T) {
	bind := func(t *testing.T, input []string, policy ImagePolicy) Model {
		t.Helper()
		model := Model{Provider: "test", ID: "check", Input: input, ContextWindow: 200_000, MaxOutputTokens: 8_000}
		bound, err := BindModel(AdaptProviderWithImagePolicy("test", []Model{model}, nil, func(Model) ImagePolicy { return policy }), model)
		if err != nil {
			t.Fatal(err)
		}
		return bound
	}
	vision := []string{"text", "image"}
	smallImages := testImagePolicy()
	smallImages.MaxEncodedBytes = 64
	smallRequests := testImagePolicy()
	smallRequests.MaxRequestImageBytes = 64
	resized := testImagePolicy()
	resized.Fit = longEdgeFit(320)

	screenshot := testPNG(t, 640, 480)
	for _, test := range []struct {
		name  string
		model Model
		data  []byte
		want  string
	}{
		{"accepted", bind(t, vision, testImagePolicy()), screenshot, ""},
		{"accepted after resizing", bind(t, vision, resized), screenshot, ""},
		{"text-only model", bind(t, []string{"text"}, ImagePolicy{}), screenshot, "this model does not accept image input"},
		{"unrecognized format", bind(t, vision, testImagePolicy()), []byte("not an image"), "the image format is not supported"},
		{"truncated", bind(t, vision, testImagePolicy()), screenshot[:len(screenshot)/2], "the image could not be decoded"},
		{"over per-image limit", bind(t, vision, smallImages), screenshot, "the image exceeds this model's size limit"},
		{"over request image budget", bind(t, vision, smallRequests), screenshot, "the image exceeds this model's request size limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.model.CheckUserImage(test.data)
			if test.want == "" {
				if err != nil {
					t.Fatalf("CheckUserImage() = %v, want nil", err)
				}
				return
			}
			var rejected *ImageInputError
			if !errors.As(err, &rejected) || rejected.Reason != test.want {
				t.Fatalf("CheckUserImage() = %v, want an ImageInputError with reason %q", err, test.want)
			}
		})
	}
}
