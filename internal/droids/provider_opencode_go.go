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
}

func (c OpenCodeGo) build() (providerEntry, error) {
	if c.APIKey != "" && c.APIKeySource != nil {
		return providerEntry{}, fmt.Errorf("droids: OpenCode Go requires at most one API key source")
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultOpenCodeGoBaseURL
	}
	source := c.APIKeySource
	if c.APIKey != "" {
		source = func(context.Context) (string, error) { return c.APIKey, nil }
	}
	headers := map[string]string{"User-Agent": "kit/1.0"}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	wrappedClient := *httpClient
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	wrappedClient.Transport = openCodeSessionTransport{base: transport}
	openAIEntry, err := (OpenAI{APIKeySource: source, BaseURL: baseURL, ID: "opencode-go", Headers: headers,
		Options: []openaioption.RequestOption{openaioption.WithHTTPClient(&wrappedClient)}}).build()
	if err != nil {
		return providerEntry{}, err
	}
	// The Anthropic SDK appends v1/messages while the other protocols append
	// directly beneath the advertised /v1 base URL.
	anthropicBaseURL := strings.TrimSuffix(baseURL, "/v1")
	anthropicEntry, err := (Anthropic{APIKeySource: source, BaseURL: anthropicBaseURL, ID: "opencode-go", Headers: headers,
		Options: []anthropicoption.RequestOption{anthropicoption.WithoutEnvironmentDefaults(), anthropicoption.WithHTTPClient(&wrappedClient)}}).build()
	if err != nil {
		return providerEntry{}, err
	}
	models := map[string]Model{}
	for _, model := range catalogModels(builtinModelCatalog, "opencode-go", "opencode-go") {
		model.BaseURL = baseURL
		models[model.ID] = model
	}
	impl := &openCodeGoProvider{baseURL: baseURL, keySource: source, client: &wrappedClient,
		responses: openAIEntry.stream, messages: anthropicEntry.stream}
	if impl.client == nil {
		impl.client = http.DefaultClient
	}
	return providerEntry{id: "opencode-go", catalogID: "opencode-go", baseURL: baseURL, models: models,
		stream: impl.stream, validateReplay: impl.validateReplay}, nil
}

type openCodeGoProvider struct {
	baseURL   string
	keySource APIKeySource
	client    *http.Client
	responses streamFn
	messages  streamFn
}

func (p *openCodeGoProvider) validateReplay(ctx context.Context, model Model, messages []Message) error {
	switch model.API {
	case ModelAPIOpenAIResponses:
		_, err := toOpenAIInputForModel(messages, model)
		return err
	case ModelAPIAnthropicMessages:
		return validateAnthropicContent(messages)
	case ModelAPIOpenAIChat:
		_, err := openCodeChatMessages("", messages)
		return err
	default:
		return fmt.Errorf("droids: unsupported OpenCode Go API %q", model.API)
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
			final := responseErrorMessage(model, ctx, fmt.Sprintf("unsupported OpenCode Go API %q", model.API))
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
		p.chatError(ctx, model, s, ErrorAuthentication, "OpenCode Go credentials are unavailable")
		return
	}
	messages, err := openCodeChatMessages(request.SystemPrompt, request.Messages)
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
		p.chatError(ctx, model, s, ErrorTransport, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		kind := ErrorTransport
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = ErrorAuthentication
		} else if response.StatusCode == 429 {
			kind = ErrorRateLimit
		}
		p.chatError(ctx, model, s, kind, strings.TrimSpace(string(data)))
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
			p.chatError(ctx, model, s, ErrorProtocol, "invalid OpenCode Go stream event")
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

func (p *openCodeGoProvider) chatError(ctx context.Context, model Model, s *pipeStream, kind ErrorKind, message string) {
	if message == "" {
		message = "OpenCode Go request failed"
	}
	final := responseErrorMessage(model, ctx, message)
	if final.StopReason != StopReasonAborted {
		final.ErrorKind = kind
	}
	s.final = final
	s.emit(StreamError{Message: final})
}

func openCodeChatMessages(system string, messages []Message) ([]any, error) {
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
				case FileInput:
					if !strings.HasPrefix(strings.ToLower(item.MediaType), "image/") {
						return nil, fmt.Errorf("droids: OpenCode Go Chat Completions supports only text and image input")
					}
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": item.URL}})
				}
			}
			result = append(result, map[string]any{"role": "user", "content": parts})
		case ContextMessage:
			var text []string
			for _, content := range value.Content {
				if item, ok := content.(TextInput); ok {
					text = append(text, item.Text)
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
				if item, ok := content.(TextContent); ok {
					text = append(text, item.Text)
				}
			}
			result = append(result, map[string]any{"role": "tool", "tool_call_id": value.ProviderCallID, "content": strings.Join(text, "\n")})
		}
	}
	return result, nil
}
