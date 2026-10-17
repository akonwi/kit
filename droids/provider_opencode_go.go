package droids

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openaioption "github.com/openai/openai-go/v3/option"
)

const defaultOpenCodeGoBaseURL = "https://opencode.ai/zen/go/v1"

// OpenCodeGo configures the OpenCode Go subscription provider.
type OpenCodeGo struct {
	APIKey       string
	APIKeySource APIKeySource
	BaseURL      string
	HTTPClient   *http.Client
	// ErrorReporter receives raw provider transport and stream failures.
	ErrorReporter ProviderErrorReporter
}

func (c OpenCodeGo) build() (providerEntry, error) {
	return buildOpenCodeProvider("opencode-go", "opencode-go", "OpenCode Go", defaultOpenCodeGoBaseURL,
		c.APIKey, c.APIKeySource, c.BaseURL, c.HTTPClient, c.ErrorReporter, openCodeGoImagePolicy)
}

func buildOpenCodeProvider(id, catalogID, name, defaultBaseURL, apiKey string, apiKeySource APIKeySource, configuredBaseURL string, httpClient *http.Client, errorReporter ProviderErrorReporter, imagePolicy func(Model) ImagePolicy) (providerEntry, error) {
	if apiKey != "" && apiKeySource != nil {
		return providerEntry{}, fmt.Errorf("droids: %s requires at most one API key source", name)
	}
	baseURL := strings.TrimRight(configuredBaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	source := apiKeySource
	if apiKey != "" {
		source = func(context.Context) (string, error) { return apiKey, nil }
	}
	headers := map[string]string{"User-Agent": "kit/1.0"}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	wrappedClient := *httpClient
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	wrappedClient.Transport = openCodeSessionTransport{base: transport}
	openAIEntry, err := (OpenAI{APIKeySource: source, BaseURL: baseURL, ID: id, Headers: headers,
		Options: []openaioption.RequestOption{openaioption.WithHTTPClient(&wrappedClient)}, ErrorReporter: errorReporter}).build()
	if err != nil {
		return providerEntry{}, err
	}
	// The Anthropic SDK appends v1/messages while the other protocols append
	// directly beneath the advertised /v1 base URL.
	anthropicBaseURL := strings.TrimSuffix(baseURL, "/v1")
	// Upstream vendors behind the gateway do not all accept cache_control.
	anthropicEntry, err := (Anthropic{APIKeySource: source, BaseURL: anthropicBaseURL, ID: id, Headers: headers,
		Options:              []anthropicoption.RequestOption{anthropicoption.WithoutEnvironmentDefaults(), anthropicoption.WithHTTPClient(&wrappedClient)},
		DisablePromptCaching: true}).build()
	if err != nil {
		return providerEntry{}, err
	}
	models := map[string]Model{}
	for _, model := range catalogModels(builtinModelCatalog, catalogID, id) {
		model.BaseURL = baseURL
		models[model.ID] = model
	}
	impl := &openCodeGoProvider{name: name, baseURL: baseURL, keySource: source, client: &wrappedClient,
		responses: openAIEntry.stream, messages: anthropicEntry.stream, errorReporter: errorReporter}
	if impl.client == nil {
		impl.client = http.DefaultClient
	}
	return providerEntry{id: id, catalogID: catalogID, baseURL: baseURL, models: models,
		stream: impl.stream, validateReplay: impl.validateReplay, imagePolicy: imagePolicy}, nil
}

// OpenCode Go image envelope (ADR 0037). The gateway forwards requests to one
// of several unpublished upstream providers per model and enforces no image
// limits itself, so the envelope fits every upstream vendor's documented
// limits: JPEG and PNG only (xAI, Z.ai), 5 MB per image (Z.ai), and a long
// edge of 2048 px, which also satisfies DeepSeek's 4096 px many-image limit.
const (
	openCodeGoMaxImageEdge         = 2048
	openCodeGoMaxImageBytes        = 5_000_000
	openCodeGoMaxRequestImageBytes = 32_000_000
)

// openCodeGoImageException adjusts the envelope for one model. It applies only
// while the model is served over the wire API it was reviewed for.
type openCodeGoImageException struct {
	api ModelAPI
	// openAI selects the OpenAI policy for OpenAI models.
	openAI bool
	// placements replaces the envelope's user-only placement.
	placements []ImagePlacement
	// maxRequestImageBytes replaces the envelope's request image budget.
	maxRequestImageBytes int64
}

// openCodeGoImageExceptions is reviewed against each vendor's documentation.
// Models absent from it receive the envelope with user-only placement.
var openCodeGoImageExceptions = map[string]openCodeGoImageException{
	// MiniMax documents images in tool results over Anthropic Messages.
	"minimax-m3": {api: ModelAPIAnthropicMessages, placements: []ImagePlacement{
		ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult,
	}},
	// Alibaba limits Anthropic-compatible request bodies to 6 MB.
	"qwen3.8-flash": {api: ModelAPIAnthropicMessages, maxRequestImageBytes: 4_000_000},
	"gpt-6-luna":    {api: ModelAPIOpenAIResponses, openAI: true},
	"gpt-5.6-luna":  {api: ModelAPIOpenAIResponses, openAI: true},
}

// openCodeGoImagePolicy returns the envelope, adjusted by any exception that
// matches the model's wire API.
func openCodeGoImagePolicy(model Model) ImagePolicy {
	if !containsString(model.Input, "image") {
		return ImagePolicy{}
	}
	policy := ImagePolicy{
		Placements:           []ImagePlacement{ImagePlacementUser},
		Formats:              []string{ImageJPEG, ImagePNG},
		Sources:              []ImageSourceKind{ImageSourceData},
		Fit:                  longEdgeImageFit(openCodeGoMaxImageEdge),
		MaxEncodedBytes:      openCodeGoMaxImageBytes,
		MaxRequestImageBytes: openCodeGoMaxRequestImageBytes,
	}
	exception, ok := openCodeGoImageExceptions[model.ID]
	if !ok || exception.api != model.API {
		return policy
	}
	if exception.openAI {
		return openAIImagePolicy(model)
	}
	if exception.placements != nil {
		policy.Placements = append([]ImagePlacement(nil), exception.placements...)
	}
	if exception.maxRequestImageBytes > 0 {
		policy.MaxRequestImageBytes = exception.maxRequestImageBytes
	}
	return policy
}

type openCodeGoProvider struct {
	name          string
	baseURL       string
	keySource     APIKeySource
	client        *http.Client
	responses     streamFn
	messages      streamFn
	errorReporter ProviderErrorReporter
}

func (p *openCodeGoProvider) validateReplay(ctx context.Context, model Model, messages []Message) error {
	switch model.API {
	case ModelAPIOpenAIResponses:
		_, err := toOpenAIInputForModel(messages, model)
		return err
	case ModelAPIAnthropicMessages:
		return validateAnthropicContent(model, messages)
	case ModelAPIOpenAIChat:
		_, err := openCodeChatMessagesForProvider(p.name, "", messages)
		return err
	default:
		return fmt.Errorf("droids: unsupported %s API %q", p.name, model.API)
	}
}

type openCodeSessionContextKey struct{}

type openCodeSessionTransport struct{ base http.RoundTripper }

func (t openCodeSessionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if session, _ := request.Context().Value(openCodeSessionContextKey{}).(string); session != "" {
		cloned := request.Clone(request.Context())
		cloned.Header.Set("x-opencode-session", session)
		request = cloned
	}
	return t.base.RoundTrip(request)
}

func (p *openCodeGoProvider) stream(ctx context.Context, model Model, req Request, opts callOptions) Stream {
	ctx = context.WithValue(ctx, openCodeSessionContextKey{}, req.SessionID)
	switch model.API {
	case ModelAPIOpenAIResponses:
		return p.responses(ctx, model, req, opts)
	case ModelAPIAnthropicMessages:
		return p.messages(ctx, model, req, opts)
	case ModelAPIOpenAIChat:
		s := newPipeStream()
		go p.runChat(ctx, model, req, s)
		return s
	default:
		s := newPipeStream()
		go func() {
			defer s.finish()
			final := responseErrorMessage(model, ctx, fmt.Sprintf("unsupported %s API %q", p.name, model.API))
			final.ErrorKind = ErrorProtocol
			s.final = final
			s.emit(StreamError{Message: final})
		}()
		return s
	}
}

func (p *openCodeGoProvider) runChat(ctx context.Context, model Model, request Request, s *pipeStream) {
	defer s.finish()
	emitOpenAIStreamStart(s, model)
	key := ""
	var err error
	if p.keySource != nil {
		key, err = p.keySource(ctx)
	}
	if err != nil || key == "" {
		p.chatError(ctx, model, s, ErrorAuthentication, p.name+" credentials are unavailable")
		return
	}
	messages, err := openCodeChatMessagesForProvider(p.name, request.SystemPrompt, request.Messages)
	if err != nil {
		p.chatError(ctx, model, s, ErrorProtocol, err.Error())
		return
	}
	maxTokens, err := resolveRequestMaxTokens(model, request.MaxTokens, request.Reasoning)
	if err != nil {
		p.chatError(ctx, model, s, ErrorProtocol, err.Error())
		return
	}
	body := map[string]any{"model": model.ID, "messages": messages, "stream": true,
		"stream_options": map[string]any{"include_usage": true}, "max_tokens": maxTokens}
	if request.Temperature != nil {
		body["temperature"] = *request.Temperature
	}
	if request.Reasoning != "" {
		body["reasoning_effort"] = strings.ReplaceAll(request.Reasoning, "off", "none")
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}})
		}
		body["tools"] = tools
	}
	encoded, _ := json.Marshal(body)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		p.chatError(ctx, model, s, ErrorTransport, err.Error())
		return
	}
	httpRequest.Header.Set("Authorization", "Bearer "+key)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest.Header.Set("User-Agent", "kit/1.0")
	if request.SessionID != "" {
		httpRequest.Header.Set("x-opencode-session", request.SessionID)
	}
	response, err := p.client.Do(httpRequest)
	if err != nil {
		p.reportError(ctx, model, err)
		p.chatError(ctx, model, s, ErrorTransport, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		message := strings.TrimSpace(string(data))
		p.reportError(ctx, model, fmt.Errorf("%s returned HTTP %d: %s", p.name, response.StatusCode, boundedDiagnostic(message)))
		kind := ProviderInvalidRequest
		if response.StatusCode == 401 {
			kind = ErrorAuthentication
		} else if response.StatusCode == 403 {
			kind = ErrorEntitlement
		} else if response.StatusCode == 429 {
			kind = ErrorRateLimit
		} else if response.StatusCode >= 500 {
			kind = ProviderInternal
		}
		p.chatError(ctx, model, s, kind, message)
		return
	}
	p.consumeChat(ctx, model, response.Body, s)
}

type openCodeChatChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Finish string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		Prompt        int `json:"prompt_tokens"`
		Completion    int `json:"completion_tokens"`
		Total         int `json:"total_tokens"`
		PromptDetails struct {
			Cached int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			Reasoning int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

func (p *openCodeGoProvider) consumeChat(ctx context.Context, model Model, body io.Reader, s *pipeStream) {
	final := AssistantMessage{Provider: model.Provider, Model: model.ID, Timestamp: time.Now().UnixMilli(), StopReason: StopReasonStop}
	textIndex, thinkingIndex := -1, -1
	toolIndexes := map[int]int{}
	toolCalls := map[int]ToolCall{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk openCodeChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			p.chatError(ctx, model, s, ErrorProtocol, "invalid "+p.name+" stream event")
			return
		}
		if chunk.ID != "" {
			final.ResponseID = chunk.ID
		}
		if chunk.Model != "" {
			final.ResponseModel = chunk.Model
		}
		if chunk.Usage != nil {
			final.Usage = Usage{Input: chunk.Usage.Prompt, Output: chunk.Usage.Completion, TotalTokens: chunk.Usage.Total, CacheRead: chunk.Usage.PromptDetails.Cached, Reasoning: chunk.Usage.CompletionDetails.Reasoning}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Reasoning != "" {
				if thinkingIndex < 0 {
					thinkingIndex = len(final.Content)
					final.Content = append(final.Content, ThinkingContent{})
				}
				tc := final.Content[thinkingIndex].(ThinkingContent)
				tc.Thinking += choice.Delta.Reasoning
				final.Content[thinkingIndex] = tc
				s.emit(StreamThinkingDelta{ContentIndex: thinkingIndex, Delta: choice.Delta.Reasoning})
			}
			if choice.Delta.Content != "" {
				if textIndex < 0 {
					textIndex = len(final.Content)
					final.Content = append(final.Content, TextContent{})
					s.emit(StreamTextStart{ContentIndex: textIndex})
				}
				tc := final.Content[textIndex].(TextContent)
				tc.Text += choice.Delta.Content
				final.Content[textIndex] = tc
				s.emit(StreamTextDelta{ContentIndex: textIndex, Delta: choice.Delta.Content})
			}
			for _, delta := range choice.Delta.ToolCalls {
				index, ok := toolIndexes[delta.Index]
				if !ok {
					index = len(final.Content)
					toolIndexes[delta.Index] = index
					call := ToolCall{ID: ToolCallID(delta.ID), ProviderCallID: delta.ID, Name: delta.Function.Name}
					toolCalls[delta.Index] = call
					final.Content = append(final.Content, call)
					s.emit(StreamToolCallStart{ContentIndex: index, ID: delta.ID, Name: delta.Function.Name})
				}
				call := toolCalls[delta.Index]
				if delta.ID != "" {
					call.ID = ToolCallID(delta.ID)
					call.ProviderCallID = delta.ID
				}
				if delta.Function.Name != "" {
					call.Name = delta.Function.Name
				}
				call.Arguments = append(call.Arguments, delta.Function.Arguments...)
				toolCalls[delta.Index] = call
				final.Content[index] = call
				if delta.Function.Arguments != "" {
					s.emit(StreamToolCallDelta{ContentIndex: index, Delta: delta.Function.Arguments})
				}
			}
			if choice.Finish == "length" {
				final.StopReason = StopReasonLength
			} else if choice.Finish == "tool_calls" {
				final.StopReason = StopReasonToolUse
			}
		}
	}
	if err := scanner.Err(); err != nil {
		p.reportError(ctx, model, err)
		p.chatError(ctx, model, s, ErrorTransport, err.Error())
		return
	}
	if textIndex >= 0 {
		s.emit(StreamTextEnd{ContentIndex: textIndex, Text: final.Content[textIndex].(TextContent).Text})
	}
	for deltaIndex, index := range toolIndexes {
		s.emit(StreamToolCallEnd{ContentIndex: index, ToolCall: toolCalls[deltaIndex]})
	}
	calculateCost(model, &final.Usage)
	s.final = final
	s.emit(StreamDone{Message: final})
}

func (p *openCodeGoProvider) reportError(ctx context.Context, model Model, err error) {
	if p.errorReporter != nil {
		p.errorReporter(ctx, model, err)
	}
}

func (p *openCodeGoProvider) chatError(ctx context.Context, model Model, s *pipeStream, kind ErrorKind, message string) {
	if message == "" {
		message = p.name + " request failed"
	}
	final := responseErrorMessage(model, ctx, message)
	if final.StopReason != StopReasonAborted {
		final.ErrorKind = kind
	}
	s.final = final
	s.emit(StreamError{Message: final})
}

func openCodeChatMessages(system string, messages []Message) ([]any, error) {
	return openCodeChatMessagesForProvider("OpenCode Go", system, messages)
}

func openCodeChatMessagesForProvider(providerName, system string, messages []Message) ([]any, error) {
	result := []any{}
	if system != "" {
		result = append(result, map[string]any{"role": "system", "content": system})
	}
	for _, message := range messages {
		switch value := message.(type) {
		case UserMessage:
			parts := []any{}
			for _, content := range value.Content {
				switch item := content.(type) {
				case TextInput:
					parts = append(parts, map[string]any{"type": "text", "text": item.Text})
				case AnnotationInput:
					parts = append(parts, map[string]any{"type": "text", "text": item.Text})
				case PromptCommandInput:
					parts = append(parts, map[string]any{"type": "text", "text": item.Text})
				case FileInput:
					if !strings.HasPrefix(strings.ToLower(item.MediaType), "image/") {
						return nil, fmt.Errorf("droids: %s Chat Completions supports only text and image input", providerName)
					}
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": item.URL}})
				}
			}
			result = append(result, map[string]any{"role": "user", "content": parts})
		case ContextMessage:
			var text []string
			for _, content := range value.Content {
				switch item := content.(type) {
				case TextInput:
					text = append(text, item.Text)
				case FileInput:
					// Image preparation replaces context images with a placeholder.
					return nil, fmt.Errorf("droids: %s Chat Completions cannot send %s files in context messages", providerName, item.MediaType)
				}
			}
			result = append(result, map[string]any{"role": "user", "content": strings.Join(text, "\n")})
		case AssistantMessage:
			entry := map[string]any{"role": "assistant"}
			var text []string
			var calls []any
			for _, content := range value.Content {
				switch item := content.(type) {
				case TextContent:
					text = append(text, item.Text)
				case ToolCall:
					calls = append(calls, map[string]any{"id": item.ProviderCallID, "type": "function", "function": map[string]any{"name": item.Name, "arguments": string(item.Arguments)}})
				}
			}
			entry["content"] = strings.Join(text, "\n")
			if len(calls) > 0 {
				entry["tool_calls"] = calls
			}
			result = append(result, entry)
		case ToolResultMessage:
			var text []string
			for _, content := range value.Content {
				switch item := content.(type) {
				case TextContent:
					text = append(text, item.Text)
				case FileContent:
					// Image preparation replaces tool-result images with a placeholder.
					return nil, fmt.Errorf("droids: %s Chat Completions cannot send %s files in tool results", providerName, item.MediaType)
				}
			}
			result = append(result, map[string]any{"role": "tool", "tool_call_id": value.ProviderCallID, "content": strings.Join(text, "\n")})
		}
	}
	return result, nil
}
