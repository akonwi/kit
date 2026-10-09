package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/akonwi/kit/droids/anthropicoauth"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// provider_anthropic.go — the Anthropic (Messages API) provider, backed by the
// official anthropic-sdk-go.

const (
	defaultAnthropicBaseURL  = "https://api.anthropic.com"
	anthropicOAuthExpirySkew = 30 * time.Second
	// OAuth requests identify as Claude Code. Opus 5.5 rejects clients older than 2.1.280.
	anthropicClaudeCodeVersion               = "2.1.280"
	anthropicFineGrainedToolsBeta            = "fine-grained-tool-streaming-2025-05-14"
	anthropicLongContextBeta                 = "context-1m-2025-08-07"
	anthropicMidConversationOutputConfigBeta = "mid-conversation-output-config-2026-07-01"
	anthropicThinkingBindingControlsBeta     = "thinking-binding-controls-2026-08-01"
)

// AnthropicCredentials is either an API key or the OAuth state needed to use a
// Claude Pro/Max subscription. APIKey and OAuth fields are mutually exclusive.
type AnthropicCredentials struct {
	APIKey       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// ErrAnthropicCredentialsChanged prevents a refresh from overwriting a newer
// login, logout, or refresh generation.
var ErrAnthropicCredentialsChanged = errors.New("Anthropic credentials changed")

// AnthropicCredentialRecord is one application-owned credential generation.
type AnthropicCredentialRecord struct {
	Credentials AnthropicCredentials
	Revision    string
}

// AnthropicCredentialStore provides dynamic Anthropic credentials and
// compare-and-swap persistence for refreshed OAuth tokens.
type AnthropicCredentialStore interface {
	LoadAnthropicCredentials(context.Context) (AnthropicCredentialRecord, error)
	SaveAnthropicCredentials(context.Context, string, AnthropicCredentials) (string, error)
}

// Anthropic configures an Anthropic Messages provider.
type Anthropic struct {
	// APIKey authenticates requests (x-api-key header).
	APIKey string
	// APIKeySource resolves a current key for each request. Configure at most one
	// of APIKey, APIKeySource, Credentials, or CredentialStore.
	APIKeySource APIKeySource
	// Credentials configures in-memory Claude subscription OAuth credentials.
	Credentials AnthropicCredentials
	// CredentialStore dynamically loads API-key or OAuth credentials and saves
	// refreshed OAuth generations.
	CredentialStore AnthropicCredentialStore
	// HTTPClient optionally customizes OAuth refresh transport. Redirects are
	// disabled before credentials are sent.
	HTTPClient *http.Client
	// BaseURL overrides the default endpoint (https://api.anthropic.com).
	BaseURL string
	// Headers are extra headers merged into every request.
	Headers map[string]string
	// ID overrides the provider id. Default: "anthropic".
	ID string
	// Options are extra SDK request options, applied after the built-ins.
	// They must not set cache_control, which the provider owns when prompt
	// caching is enabled.
	Options []option.RequestOption
	// PromptCacheRetention returns the cache lifetime for each request:
	// PromptCacheShort (five minutes) or PromptCacheLong (one hour). Nil, or
	// any other value, selects PromptCacheShort.
	PromptCacheRetention func() PromptCacheRetention
	// DisablePromptCaching omits cache_control from requests. By default,
	// requests sent to Anthropic's API (https://api.anthropic.com, including
	// all subscription requests) use automatic prompt caching plus a
	// breakpoint at the end of the system prompt. Requests to any other
	// BaseURL never carry cache_control, because gateways may reject it.
	DisablePromptCaching bool
}

func (c Anthropic) build() (providerEntry, error) {
	id := c.ID
	if id == "" {
		id = "anthropic"
	}
	configured := 0
	if c.APIKey != "" {
		configured++
	}
	if c.APIKeySource != nil {
		configured++
	}
	if anthropicCredentialsConfigured(c.Credentials) {
		configured++
	}
	if c.CredentialStore != nil {
		configured++
	}
	if configured > 1 {
		return providerEntry{}, fmt.Errorf("droids: Anthropic requires at most one credential source")
	}
	if err := validateAnthropicCredentials(c.Credentials); err != nil {
		return providerEntry{}, err
	}

	opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithMaxRetries(0)}
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

	var credentials *anthropicCredentialManager
	if anthropicCredentialsConfigured(c.Credentials) || c.CredentialStore != nil {
		gate := make(chan struct{}, 1)
		gate <- struct{}{}
		credentials = &anthropicCredentialManager{
			current: c.Credentials, loaded: anthropicCredentialsConfigured(c.Credentials),
			store: c.CredentialStore, refresher: anthropicoauth.NewClient(c.HTTPClient), now: time.Now, gate: gate,
		}
	}
	impl := &anthropicProvider{client: &client, apiKeySource: c.APIKeySource, credentials: credentials, options: opts,
		promptCaching: !c.DisablePromptCaching, firstPartyOrigin: isAnthropicAPIOrigin(baseURL),
		promptCacheRetention: c.PromptCacheRetention}
	return providerEntry{
		id:        id,
		catalogID: "anthropic",
		baseURL:   baseURL,
		models:    models,
		stream:    impl.stream,
		validateReplay: func(_ context.Context, model Model, messages []Message) error {
			return validateAnthropicContent(model, messages)
		},
		imagePolicy: anthropicImagePolicy,
	}, nil
}

// Anthropic image limits, from the Claude API vision documentation.
const (
	// anthropicMaxImageBytes is the per-image limit, base64-encoded.
	anthropicMaxImageBytes = 10_000_000
	// anthropicMaxRequestImageBytes leaves headroom for text below the 32 MB
	// request limit.
	anthropicMaxRequestImageBytes = 24_000_000
	// anthropicManyImageThreshold is the image count above which every image
	// in a request must fit anthropicManyImageEdge.
	anthropicManyImageThreshold = 20
	anthropicManyImageEdge      = 2000
)

// anthropicImagePolicy declares the Messages API image contract. Images are
// prepared to the model tier's native size: Anthropic downscales anything
// larger, so preparing loses no fidelity the model would use, and coordinates
// the model returns map directly onto the image it received.
func anthropicImagePolicy(model Model) ImagePolicy {
	if !containsString(model.Input, "image") {
		return ImagePolicy{}
	}
	edge, tokens := 1568, 1568
	if anthropicHighResolution(model.ID) {
		edge, tokens = 2576, 4784
	}
	maxImages := 600
	if model.ContextWindow > 0 && model.ContextWindow <= 200_000 {
		maxImages = 100
	}
	return ImagePolicy{
		Placements:           []ImagePlacement{ImagePlacementUser, ImagePlacementContext, ImagePlacementToolResult},
		Formats:              []string{ImageJPEG, ImagePNG, ImageGIF, ImageWebP},
		Sources:              []ImageSourceKind{ImageSourceData, ImageSourceHTTPS},
		Fit:                  anthropicImageFit(edge, tokens),
		MaxEncodedBytes:      anthropicMaxImageBytes,
		MaxImages:            maxImages,
		MaxRequestImageBytes: anthropicMaxRequestImageBytes,
		ManyImages:           &ManyImageRule{Above: anthropicManyImageThreshold, Fit: longEdgeImageFit(anthropicManyImageEdge)},
	}
}

type anthropicProvider struct {
	client       *anthropic.Client
	apiKeySource APIKeySource
	credentials  *anthropicCredentialManager
	options      []option.RequestOption
	// promptCaching enables cache_control breakpoints on requests sent to
	// Anthropic's API: subscription requests, which are always sent there,
	// and API-key requests when firstPartyOrigin is set.
	promptCaching bool
	// firstPartyOrigin reports whether the configured base URL is Anthropic's
	// API rather than a gateway.
	firstPartyOrigin bool
	// promptCacheRetention optionally selects each request's cache lifetime.
	promptCacheRetention func() PromptCacheRetention
}

type anthropicOAuthRefresher interface {
	Refresh(context.Context, anthropicoauth.Credentials) (anthropicoauth.Credentials, error)
}

type anthropicCredentialManager struct {
	current   AnthropicCredentials
	revision  string
	loaded    bool
	store     AnthropicCredentialStore
	refresher anthropicOAuthRefresher
	now       func() time.Time
	gate      chan struct{}
}

func anthropicCredentialsConfigured(credentials AnthropicCredentials) bool {
	return credentials.APIKey != "" || credentials.AccessToken != "" || credentials.RefreshToken != "" || !credentials.ExpiresAt.IsZero()
}

func validateAnthropicCredentials(credentials AnthropicCredentials) error {
	if credentials.APIKey != "" && (credentials.AccessToken != "" || credentials.RefreshToken != "" || !credentials.ExpiresAt.IsZero()) {
		return fmt.Errorf("droids: Anthropic credentials mix API-key and OAuth fields")
	}
	if credentials.AccessToken == "" && credentials.RefreshToken == "" && !credentials.ExpiresAt.IsZero() {
		return fmt.Errorf("droids: Anthropic OAuth credentials have an expiry without tokens")
	}
	return nil
}

func (m *anthropicCredentialManager) resolve(ctx context.Context) (AnthropicCredentials, error) {
	select {
	case <-ctx.Done():
		return AnthropicCredentials{}, ctx.Err()
	case <-m.gate:
	}
	defer func() { m.gate <- struct{}{} }()
	return m.resolveOwned(ctx)
}

func (m *anthropicCredentialManager) resolveOwned(ctx context.Context) (AnthropicCredentials, error) {
	for range 3 {
		if m.store != nil {
			record, err := m.store.LoadAnthropicCredentials(ctx)
			if err != nil {
				return AnthropicCredentials{}, fmt.Errorf("load Anthropic credentials")
			}
			if anthropicCredentialsConfigured(record.Credentials) != (record.Revision != "") {
				return AnthropicCredentials{}, fmt.Errorf("Anthropic credential store returned an invalid generation")
			}
			if !m.loaded || record.Revision != m.revision {
				m.current, m.revision, m.loaded = record.Credentials, record.Revision, true
			}
		}
		if err := validateAnthropicCredentials(m.current); err != nil {
			return AnthropicCredentials{}, err
		}
		if m.current.APIKey != "" {
			return m.current, nil
		}
		if m.current.AccessToken == "" && m.current.RefreshToken == "" {
			return AnthropicCredentials{}, fmt.Errorf("Anthropic credentials are not configured")
		}
		if m.current.AccessToken != "" && (m.current.ExpiresAt.IsZero() || m.now().Add(anthropicOAuthExpirySkew).Before(m.current.ExpiresAt)) {
			return m.current, nil
		}
		if m.current.RefreshToken == "" {
			return AnthropicCredentials{}, fmt.Errorf("Anthropic OAuth credentials expired and cannot be refreshed")
		}
		refreshed, err := m.refresher.Refresh(ctx, anthropicoauth.Credentials{
			AccessToken: m.current.AccessToken, RefreshToken: m.current.RefreshToken, ExpiresAt: m.current.ExpiresAt,
		})
		if err != nil {
			return AnthropicCredentials{}, fmt.Errorf("refresh Anthropic OAuth credentials")
		}
		next := AnthropicCredentials{AccessToken: refreshed.AccessToken, RefreshToken: refreshed.RefreshToken, ExpiresAt: refreshed.ExpiresAt}
		if m.store != nil {
			revision, err := m.store.SaveAnthropicCredentials(ctx, m.revision, next)
			if errors.Is(err, ErrAnthropicCredentialsChanged) {
				m.loaded = false
				continue
			}
			if err != nil {
				return AnthropicCredentials{}, fmt.Errorf("save refreshed Anthropic credentials")
			}
			m.revision = revision
		}
		m.current = next
		return next, nil
	}
	return AnthropicCredentials{}, fmt.Errorf("Anthropic credentials changed repeatedly during refresh")
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

	messages, err := toAnthropicRequestMessages(model, req.Messages)
	if err != nil {
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
		maxTokens = int64(defaultMaxTokens(model))
	}

	client, oauth, err := p.clientForRequest(ctx)
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

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model.ID),
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if oauth {
		params.System = append(params.System, anthropic.TextBlockParam{Text: "You are Claude Code, Anthropic's official CLI for Claude."})
	}
	if req.SystemPrompt != "" {
		params.System = append(params.System, anthropic.TextBlockParam{Text: req.SystemPrompt})
	}
	if len(req.Tools) > 0 {
		params.Tools = toAnthropicTools(req.Tools)
	}
	if p.promptCaching && (oauth || p.firstPartyOrigin) {
		applyAnthropicPromptCaching(&params, p.cacheRetention())
	}
	if req.Temperature != nil && !model.SupportsMidConversationEffort {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	requestOptions := []option.RequestOption{}
	if beta := anthropicBetaFeatures(oauth, model, len(req.Tools) > 0); beta != "" {
		requestOptions = append(requestOptions, option.WithHeader("anthropic-beta", beta))
	}
	// Managed-effort models hold a stable high baseline across a conversation.
	// An explicit "off" is a standalone request that needs no thinking at all.
	thinkingOff := req.Reasoning == "off" || req.Reasoning == "none"
	if model.SupportsMidConversationEffort && !thinkingOff {
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortHigh}
		requestOptions = append(requestOptions,
			option.WithJSONSet("thinking.block_binding.prefix_mismatch_behavior", "drop_block"),
			option.WithJSONSet("messages", anthropicManagedEffortMessages(req.Messages, params.Messages, model.Provider, "high")),
		)
	} else if model.ReasoningMode == ReasoningModeAdaptive && model.Reasoning && req.Reasoning != "" && !thinkingOff {
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropicEffort(req.Reasoning)}
	} else if budget := reasoningTokenBudget(req.Reasoning); budget > 0 && model.Reasoning {
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

	stream := client.Messages.NewStreaming(ctx, params, requestOptions...)
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
		kind := classifyAnthropicError(err)
		if reason == StopReasonContextWindow {
			kind = ProviderContextWindow
		}
		if reason == StopReasonAborted {
			kind = ""
		}
		message := err.Error()
		if kind == ProviderUsageLimit {
			if detail := anthropicProviderMessage(err); detail != "" {
				message = detail
			}
		}
		final := AssistantMessage{
			Provider:     model.Provider,
			Model:        model.ID,
			StopReason:   reason,
			ErrorKind:    kind,
			ErrorMessage: message,
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

func classifyAnthropicError(err error) ProviderErrorKind {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return ProviderTransport
	}
	switch apiErr.StatusCode {
	case 401:
		return ProviderAuthentication
	case 403:
		return ProviderEntitlement
	case 429:
		return ProviderRateLimit
	case 400, 404, 413, 422:
		if anthropicUsageLimit(apiErr) {
			return ProviderUsageLimit
		}
		return ProviderInvalidRequest
	default:
		if apiErr.StatusCode >= 500 {
			return ProviderInternal
		}
		return ProviderProtocol
	}
}

func (p *anthropicProvider) cacheRetention() PromptCacheRetention {
	if p.promptCacheRetention == nil {
		return PromptCacheShort
	}
	return p.promptCacheRetention()
}

// applyAnthropicPromptCaching enables automatic prompt caching, which places
// a breakpoint on the request's last cacheable block so each request reads the
// prefix the previous one wrote. A second breakpoint at the end of the system
// prompt keeps tools and system reusable when the messages change, such as
// after compaction. Both use the same lifetime, since Anthropic requires
// longer-lived breakpoints to precede shorter ones.
func applyAnthropicPromptCaching(params *anthropic.MessageNewParams, retention PromptCacheRetention) {
	control := anthropic.NewCacheControlEphemeralParam()
	if retention == PromptCacheLong {
		control.TTL = anthropic.CacheControlEphemeralTTLTTL1h
	}
	params.CacheControl = control
	if last := len(params.System) - 1; last >= 0 {
		params.System[last].CacheControl = control
	}
}

func anthropicUsageLimit(err *anthropic.Error) bool {
	message := strings.ToLower(anthropicProviderMessage(err))
	return strings.Contains(message, "extra usage") || strings.Contains(message, "usage limit")
}

func anthropicProviderMessage(err error) string {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr == nil {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.RawJSON()), &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Error.Message)
}

func (p *anthropicProvider) clientForRequest(ctx context.Context) (*anthropic.Client, bool, error) {
	if p.credentials != nil {
		credentials, err := p.credentials.resolve(ctx)
		if err != nil {
			return nil, false, err
		}
		if credentials.APIKey == "" {
			// Subscription bearer tokens are intentionally restricted to
			// Anthropic's fixed API origin and fixed identity headers.
			client := anthropic.NewClient(
				option.WithoutEnvironmentDefaults(),
				option.WithMaxRetries(0),
				option.WithBaseURL(defaultAnthropicBaseURL),
				option.WithHeader("Authorization", "Bearer "+credentials.AccessToken),
				option.WithHeader("user-agent", "claude-cli/"+anthropicClaudeCodeVersion),
				option.WithHeader("x-app", "cli"),
			)
			return &client, true, nil
		}
		options := make([]option.RequestOption, 0, len(p.options)+1)
		options = append(options, option.WithAPIKey(credentials.APIKey))
		options = append(options, p.options...)
		client := anthropic.NewClient(options...)
		return &client, false, nil
	}
	if p.apiKeySource == nil {
		return p.client, false, nil
	}
	apiKey, err := p.apiKeySource(ctx)
	if err != nil || apiKey == "" {
		return nil, false, fmt.Errorf("resolve Anthropic API key")
	}
	options := make([]option.RequestOption, 0, len(p.options)+1)
	options = append(options, option.WithAPIKey(apiKey))
	options = append(options, p.options...)
	client := anthropic.NewClient(options...)
	return &client, false, nil
}

func anthropicBetaFeatures(oauth bool, model Model, hasTools bool) string {
	features := make([]string, 0, 6)
	if oauth {
		features = append(features, "claude-code-20250219", "oauth-2025-04-20")
	}
	if hasTools {
		features = append(features, anthropicFineGrainedToolsBeta)
	}
	if model.ContextWindow > 200_000 {
		features = append(features, anthropicLongContextBeta)
	}
	if model.SupportsMidConversationEffort {
		features = append(features, anthropicMidConversationOutputConfigBeta, anthropicThinkingBindingControlsBeta)
	}
	return strings.Join(features, ",")
}

func anthropicEffort(reasoning string) anthropic.OutputConfigEffort {
	switch reasoning {
	case "minimal", "low":
		return anthropic.OutputConfigEffortLow
	case "medium":
		return anthropic.OutputConfigEffortMedium
	case "xhigh":
		return anthropic.OutputConfigEffortXhigh
	case "max":
		return anthropic.OutputConfigEffortMax
	default:
		return anthropic.OutputConfigEffortHigh
	}
}

func anthropicManagedEffortMessages(source []Message, messages []anthropic.MessageParam, provider, effort string) []any {
	managed := make([]any, 0, len(messages)*2+1)
	for index, message := range messages {
		if index < len(source) {
			if assistant, ok := source[index].(AssistantMessage); ok && assistant.Provider == provider {
				managed = append(managed, map[string]any{
					"role": "system", "content": []any{}, "output_config": map[string]any{"effort": effort},
				})
			}
		}
		body, err := json.Marshal(message)
		if err != nil {
			continue
		}
		var value any
		if json.Unmarshal(body, &value) == nil {
			managed = append(managed, value)
		}
	}
	managed = append(managed, map[string]any{
		"role": "system", "content": []any{}, "output_config": map[string]any{"effort": effort},
	})
	return managed
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
			msg.Content = append(msg.Content, ToolCall{ID: ToolCallID(tu.ID), Name: tu.Name, Arguments: []byte(tu.Input)})
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
		Input:      int(acc.Usage.InputTokens),
		Output:     int(acc.Usage.OutputTokens),
		CacheRead:  int(acc.Usage.CacheReadInputTokens),
		CacheWrite: int(acc.Usage.CacheCreationInputTokens),
		// Anthropic reports one-hour writes within cache_creation_input_tokens.
		CacheWrite1h: int(min(acc.Usage.CacheCreation.Ephemeral1hInputTokens, acc.Usage.CacheCreationInputTokens)),
		TotalTokens:  int(acc.Usage.InputTokens + acc.Usage.CacheReadInputTokens + acc.Usage.CacheCreationInputTokens + acc.Usage.OutputTokens),
	}
	return msg
}

// validateAnthropicContent reports whether messages can be replayed to model
// through the Messages API. It performs the same translation as a request so
// replay validation and request construction cannot disagree.
func validateAnthropicContent(model Model, messages []Message) error {
	_, err := toAnthropicRequestMessages(model, messages)
	return err
}

// toAnthropicRequestMessages translates messages for a request to model,
// rejecting images when the model does not accept image input.
func toAnthropicRequestMessages(model Model, messages []Message) ([]anthropic.MessageParam, error) {
	if !containsString(model.Input, "image") {
		if err := rejectAnthropicImages(model, messages); err != nil {
			return nil, err
		}
	}
	return toAnthropicMessages(model, messages)
}

func rejectAnthropicImages(model Model, messages []Message) error {
	check := func(content []FileContent) error {
		if slices.ContainsFunc(content, func(file FileContent) bool { return isImageMediaType(file.MediaType) }) {
			return fmt.Errorf("anthropic: model %q does not support image input", model.ID)
		}
		return nil
	}
	for _, message := range messages {
		var err error
		switch msg := message.(type) {
		case UserMessage:
			err = check(anthropicFiles(msg.Content))
		case ContextMessage:
			err = check(anthropicFiles(msg.Content))
		case ToolResultMessage:
			err = check(anthropicFiles(msg.Content))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func anthropicFiles[T any](content []T) []FileContent {
	var files []FileContent
	for _, block := range content {
		switch value := any(block).(type) {
		case FileInput:
			files = append(files, FileContent{Filename: value.Filename, MediaType: value.MediaType, URL: value.URL})
		case FileContent:
			files = append(files, value)
		}
	}
	return files
}

func toAnthropicMessages(model Model, messages []Message) ([]anthropic.MessageParam, error) {
	var out []anthropic.MessageParam
	for _, m := range messages {
		switch msg := m.(type) {
		case UserMessage:
			blocks, err := anthropicUserBlocks("user", msg.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		case ContextMessage:
			blocks, err := anthropicUserBlocks("context", msg.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		case ToolResultMessage:
			parts, err := anthropicParts("tool result", msg.Content)
			if err != nil {
				return nil, err
			}
			result := anthropic.ToolResultBlockParam{
				ToolUseID: providerCallID(msg.ToolCallID, msg.ProviderCallID),
				IsError:   anthropic.Bool(msg.IsError),
			}
			for _, part := range parts {
				if part.image != nil {
					result.Content = append(result.Content, anthropic.ToolResultBlockParamContentUnion{OfImage: part.image})
				} else {
					result.Content = append(result.Content, anthropic.ToolResultBlockParamContentUnion{OfText: &anthropic.TextBlockParam{Text: part.text}})
				}
			}
			// Tool results are carried in a user turn; consecutive user turns
			// are combined by the API.
			out = append(out, anthropic.NewUserMessage(anthropic.ContentBlockParamUnion{OfToolResult: &result}))
		case AssistantMessage:
			blocks := assistantBlocks(model, msg)
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		}
	}
	return out, nil
}

func anthropicUserBlocks[T any](role string, content []T) ([]anthropic.ContentBlockParamUnion, error) {
	parts, err := anthropicParts(role, content)
	if err != nil {
		return nil, err
	}
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		if part.image != nil {
			blocks = append(blocks, anthropic.ContentBlockParamUnion{OfImage: part.image})
		} else {
			blocks = append(blocks, anthropic.NewTextBlock(part.text))
		}
	}
	return blocks, nil
}

// anthropicPart is one Messages API content block: either text or an image.
type anthropicPart struct {
	text  string
	image *anthropic.ImageBlockParam
}

// anthropicParts translates neutral content into ordered text and image
// parts. Adjacent non-empty text is joined with newlines; images split text
// runs so their position relative to surrounding text is preserved. Content
// without any parts yields a single empty text part.
func anthropicParts[T any](role string, content []T) ([]anthropicPart, error) {
	var parts []anthropicPart
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, anthropicPart{text: text.String()})
			text.Reset()
		}
	}
	for i, block := range content {
		var value string
		var file FileContent
		isFile := false
		switch item := any(block).(type) {
		case TextContent:
			value = item.Text
		case TextInput:
			value = item.Text
		case AnnotationInput:
			value = item.Text
		case PromptCommandInput:
			value = item.Text
		case FileInput:
			file, isFile = FileContent{Filename: item.Filename, MediaType: item.MediaType, URL: item.URL}, true
		case FileContent:
			file, isFile = item, true
		}
		if isFile {
			image, err := anthropicImage(file)
			if err != nil {
				return nil, fmt.Errorf("anthropic: unsupported %s content at index %d (%T): %w", role, i, block, err)
			}
			flush()
			parts = append(parts, anthropicPart{image: image})
			continue
		}
		if value == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(value)
	}
	flush()
	if len(parts) == 0 {
		parts = append(parts, anthropicPart{})
	}
	return parts, nil
}

// anthropicImage converts image file content into an image block. The
// Messages API accepts JPEG, PNG, GIF, and WebP from base64 data or HTTPS URLs;
// other file types are rejected.
func anthropicImage(file FileContent) (*anthropic.ImageBlockParam, error) {
	if !isImageMediaType(file.MediaType) {
		return nil, fmt.Errorf("media type %q is not supported; only images are supported", file.MediaType)
	}
	if err := validateImageContent(file); err != nil {
		return nil, err
	}
	mediaType, _, _ := mime.ParseMediaType(file.MediaType)
	mediaType = strings.ToLower(mediaType)
	switch anthropic.Base64ImageSourceMediaType(mediaType) {
	case anthropic.Base64ImageSourceMediaTypeImageJPEG,
		anthropic.Base64ImageSourceMediaTypeImagePNG,
		anthropic.Base64ImageSourceMediaTypeImageGIF,
		anthropic.Base64ImageSourceMediaTypeImageWebP:
	default:
		return nil, fmt.Errorf("image media type %q is not supported; use JPEG, PNG, GIF, or WebP", file.MediaType)
	}
	scheme, err := contentSourceScheme(file.URL)
	if err != nil {
		return nil, err
	}
	image := &anthropic.ImageBlockParam{}
	if scheme == "data" {
		_, payload, _ := strings.Cut(file.URL, ",")
		image.Source.OfBase64 = &anthropic.Base64ImageSourceParam{
			Data:      payload,
			MediaType: anthropic.Base64ImageSourceMediaType(mediaType),
		}
	} else {
		image.Source.OfURL = &anthropic.URLImageSourceParam{URL: file.URL}
	}
	return image, nil
}

func assistantBlocks(model Model, msg AssistantMessage) []anthropic.ContentBlockParamUnion {
	replayThinking := anthropicThinkingReplayable(model, msg)
	var blocks []anthropic.ContentBlockParamUnion
	for _, content := range msg.Content {
		switch block := content.(type) {
		case TextContent:
			if block.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(block.Text))
			}
		case ThinkingContent:
			// Signed thinking is only valid for the model that produced it.
			// Replaying another provider's signature is an invalid request.
			if !replayThinking {
				continue
			}
			if block.Signature != "" {
				blocks = append(blocks, anthropic.NewThinkingBlock(block.Signature, block.Thinking))
			} else if block.Thinking != "" {
				blocks = append(blocks, anthropic.NewTextBlock(block.Thinking))
			}
		case ToolCall:
			var input any
			if len(block.Arguments) > 0 {
				_ = json.Unmarshal(block.Arguments, &input)
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(providerCallID(block.ID, block.ProviderCallID), input, block.Name))
		}
	}
	return blocks
}

func anthropicThinkingReplayable(model Model, msg AssistantMessage) bool {
	if msg.Provider == "" {
		return true
	}
	if model.Provider != "" && msg.Provider != model.Provider {
		return false
	}
	if model.ID != "" && msg.Model != "" && msg.Model != model.ID {
		return false
	}
	return true
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

// isAnthropicAPIOrigin reports whether baseURL addresses Anthropic's API.
func isAnthropicAPIOrigin(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "api.anthropic.com") &&
		(parsed.Port() == "" || parsed.Port() == "443")
}
