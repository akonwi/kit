package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// provider_anthropic.go — the Anthropic (Messages API) provider, backed by the
// official anthropic-sdk-go. API-key auth only for now.

const defaultAnthropicBaseURL = "https://api.anthropic.com"

// Anthropic configures an Anthropic Messages provider.
type Anthropic struct {
	// APIKey authenticates requests (x-api-key header).
	APIKey string
	// APIKeySource resolves a current key for each request. Configure at most one
	// of APIKey and APIKeySource.
	APIKeySource APIKeySource
	// BaseURL overrides the default endpoint (https://api.anthropic.com).
	BaseURL string
	// Headers are extra headers merged into every request.
	Headers map[string]string
	// ID overrides the provider id. Default: "anthropic".
	ID string
	// Options are extra SDK request options, applied after the built-ins.
	Options []option.RequestOption
}

func (c Anthropic) build() (providerEntry, error) {
	id := c.ID
	if id == "" {
		id = "anthropic"
	}
	if c.APIKey != "" && c.APIKeySource != nil {
		return providerEntry{}, fmt.Errorf("droids: Anthropic requires at most one of APIKey or APIKeySource")
	}

	opts := []option.RequestOption{option.WithoutEnvironmentDefaults()}
	if c.APIKey != "" {
		opts = append(opts, option.WithAPIKey(c.APIKey))
	}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	for k, v := range c.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	opts = append(opts, c.Options...)

	client := anthropic.NewClient(opts...)

	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	models := map[string]Model{}
	for _, model := range catalogModels(builtinModelCatalog, "anthropic", id) {
		model.BaseURL = baseURL
		models[model.ID] = model
	}

	impl := &anthropicProvider{client: &client, apiKeySource: c.APIKeySource, options: opts}
	return providerEntry{
		id:        id,
		catalogID: "anthropic",
		baseURL:   baseURL,
		models:    models,
		stream:    impl.stream,
	}, nil
}

type anthropicProvider struct {
	client       *anthropic.Client
	apiKeySource APIKeySource
	options      []option.RequestOption
}

func (p *anthropicProvider) stream(ctx context.Context, model Model, req Request, _ callOptions) Stream {
	s := newPipeStream()
	go p.run(ctx, model, req, s)
	return s
}

func (p *anthropicProvider) run(ctx context.Context, model Model, req Request, s *pipeStream) {
	defer s.finish()

	partial := AssistantMessage{Provider: model.Provider, Model: model.ID, Timestamp: time.Now().UnixMilli()}
	s.emit(StreamStart{Partial: partial})

	if err := validateReasoning(model, req.Reasoning); err != nil {
		final := AssistantMessage{
			Provider:     model.Provider,
			Model:        model.ID,
			StopReason:   StopReasonError,
			ErrorMessage: err.Error(),
			Timestamp:    time.Now().UnixMilli(),
		}
		s.final = final
		s.emit(StreamError{Message: final})
		return
	}

	if err := validateAnthropicContent(req.Messages); err != nil {
		final := AssistantMessage{
			Provider:     model.Provider,
			Model:        model.ID,
			StopReason:   StopReasonError,
			ErrorMessage: err.Error(),
			Timestamp:    time.Now().UnixMilli(),
		}
		s.final = final
		s.emit(StreamError{Message: final})
		return
	}

	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = defaultRequestMaxTokens
		if model.MaxOutputTokens > 0 && maxTokens > int64(model.MaxOutputTokens) {
			maxTokens = int64(model.MaxOutputTokens)
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model.ID),
		MaxTokens: maxTokens,
		Messages:  toAnthropicMessages(req.Messages),
	}
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt}}
	}
	if len(req.Tools) > 0 {
		params.Tools = toAnthropicTools(req.Tools)
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	if budget := reasoningTokenBudget(req.Reasoning); budget > 0 && model.Reasoning {
		// Anthropic requires max_tokens to include and exceed the thinking
		// budget. Do not silently change the caller's output allowance.
		if params.MaxTokens <= budget {
			final := AssistantMessage{
				Provider:     model.Provider,
				Model:        model.ID,
				StopReason:   StopReasonError,
				ErrorMessage: fmt.Sprintf("anthropic: max tokens %d must exceed reasoning budget %d", params.MaxTokens, budget),
				Timestamp:    time.Now().UnixMilli(),
			}
			s.final = final
			s.emit(StreamError{Message: final})
			return
		}
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
	}

	client, err := p.clientForRequest(ctx)
	if err != nil {
		reason := stopReasonForError(ctx)
		kind := ErrorAuthentication
		message := "Anthropic credentials are unavailable"
		if reason == StopReasonAborted {
			kind = ""
			message = "Anthropic request aborted"
		}
		final := AssistantMessage{
			Provider: model.Provider, Model: model.ID, StopReason: reason,
			ErrorKind: kind, ErrorMessage: message, Timestamp: time.Now().UnixMilli(),
		}
		s.final = final
		s.emit(StreamError{Message: final})
		return
	}
	stream := client.Messages.NewStreaming(ctx, params)
	var acc anthropic.Message

	for stream.Next() {
		event := stream.Current()
		if err := acc.Accumulate(event); err != nil {
			continue
		}
		p.emitDelta(s, event)
	}

	if err := stream.Err(); err != nil {
		reason := stopReasonForError(ctx)
		if reason != StopReasonAborted && isContextWindowError("", err.Error()) {
			reason = StopReasonContextWindow
		}
		final := AssistantMessage{
			Provider:     model.Provider,
			Model:        model.ID,
			StopReason:   reason,
			ErrorMessage: err.Error(),
			Timestamp:    time.Now().UnixMilli(),
		}
		s.final = final
		s.emit(StreamError{Message: final})
		return
	}

	final := assembleAnthropicMessage(model, acc)
	s.final = final
	s.emit(anthropicTerminalEvent(final))
}

func (p *anthropicProvider) clientForRequest(ctx context.Context) (*anthropic.Client, error) {
	if p.apiKeySource == nil {
		return p.client, nil
	}
	apiKey, err := p.apiKeySource(ctx)
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("resolve Anthropic API key")
	}
	options := make([]option.RequestOption, 0, len(p.options)+1)
	options = append(options, option.WithAPIKey(apiKey))
	options = append(options, p.options...)
	client := anthropic.NewClient(options...)
	return &client, nil
}

func anthropicTerminalEvent(message AssistantMessage) StreamEvent {
	if message.StopReason == StopReasonError || message.StopReason == StopReasonContextWindow || message.StopReason == StopReasonAborted {
		return StreamError{Message: message}
	}
	return StreamDone{Message: message}
}

// emitDelta translates one SDK stream event into droids StreamEvents.
func (p *anthropicProvider) emitDelta(s *pipeStream, event anthropic.MessageStreamEventUnion) {
	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockStartEvent:
		idx := int(e.Index)
		switch e.ContentBlock.Type {
		case "text":
			s.emit(StreamTextStart{ContentIndex: idx})
		case "tool_use":
			tu := e.ContentBlock.AsToolUse()
			s.emit(StreamToolCallStart{ContentIndex: idx, ID: tu.ID, Name: tu.Name})
		}
	case anthropic.ContentBlockDeltaEvent:
		idx := int(e.Index)
		switch e.Delta.Type {
		case "text_delta":
			s.emit(StreamTextDelta{ContentIndex: idx, Delta: e.Delta.Text})
		case "input_json_delta":
			s.emit(StreamToolCallDelta{ContentIndex: idx, Delta: e.Delta.PartialJSON})
		case "thinking_delta":
			s.emit(StreamThinkingDelta{ContentIndex: idx, Delta: e.Delta.Thinking})
		}
	}
}

// assembleAnthropicMessage builds the neutral AssistantMessage from the
// accumulated Message.
func assembleAnthropicMessage(model Model, acc anthropic.Message) AssistantMessage {
	msg := AssistantMessage{
		Provider:  model.Provider,
		Model:     model.ID,
		Timestamp: time.Now().UnixMilli(),
	}
	for _, block := range acc.Content {
		switch block.Type {
		case "text":
			msg.Content = append(msg.Content, TextContent{Text: block.AsText().Text})
		case "thinking":
			th := block.AsThinking()
			msg.Content = append(msg.Content, ThinkingContent{Thinking: th.Thinking, Signature: th.Signature})
		case "tool_use":
			tu := block.AsToolUse()
			msg.Content = append(msg.Content, ToolCall{ID: tu.ID, Name: tu.Name, Arguments: []byte(tu.Input)})
		}
	}

	switch acc.StopReason {
	case anthropic.StopReasonToolUse:
		msg.StopReason = StopReasonToolUse
	case anthropic.StopReasonMaxTokens:
		msg.StopReason = StopReasonLength
	case anthropic.StopReasonModelContextWindowExceeded:
		msg.StopReason = StopReasonContextWindow
		msg.ErrorMessage = "anthropic: model context window exceeded"
	default:
		msg.StopReason = StopReasonStop
	}

	msg.Usage = Usage{
		Input:       int(acc.Usage.InputTokens),
		Output:      int(acc.Usage.OutputTokens),
		CacheRead:   int(acc.Usage.CacheReadInputTokens),
		CacheWrite:  int(acc.Usage.CacheCreationInputTokens),
		TotalTokens: int(acc.Usage.InputTokens + acc.Usage.OutputTokens),
	}
	return msg
}

func validateAnthropicContent(messages []Message) error {
	for _, message := range messages {
		var role string
		var content []Content
		switch msg := message.(type) {
		case UserMessage:
			role, content = "user", msg.Content
		case ToolResultMessage:
			role, content = "tool result", msg.Content
		case AssistantMessage:
			role, content = "assistant", msg.Content
		}
		for i, block := range content {
			switch block.(type) {
			case ImageContent, FileContent:
				return fmt.Errorf("anthropic: unsupported %s content at index %d (%T): native attachment translation is not implemented", role, i, block)
			}
		}
	}
	return nil
}

func toAnthropicMessages(messages []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range messages {
		switch msg := m.(type) {
		case UserMessage:
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(textOfContent(msg.Content))))
		case ToolResultMessage:
			// Tool results are carried in a user turn; consecutive user turns
			// are combined by the API.
			out = append(out, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolCallID, textOfContent(msg.Content), msg.IsError),
			))
		case AssistantMessage:
			out = append(out, anthropic.NewAssistantMessage(assistantBlocks(msg)...))
		}
	}
	return out
}

func assistantBlocks(msg AssistantMessage) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	if txt := textOfContent(msg.Content); txt != "" {
		blocks = append(blocks, anthropic.NewTextBlock(txt))
	}
	for _, tc := range msg.ToolCalls() {
		var input any
		if len(tc.Arguments) > 0 {
			_ = json.Unmarshal(tc.Arguments, &input)
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
	}
	return blocks
}

func toAnthropicTools(tools []ToolSchema) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{}
		if props, ok := t.Parameters["properties"]; ok {
			schema.Properties = props
		}
		if req, ok := t.Parameters["required"].([]string); ok {
			schema.Required = req
		} else if req, ok := t.Parameters["required"].([]any); ok {
			schema.Required = toStringSlice(req)
		}
		tp := anthropic.ToolParam{Name: t.Name, InputSchema: schema}
		if t.Description != "" {
			tp.Description = param.NewOpt(t.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out
}

func toStringSlice(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
