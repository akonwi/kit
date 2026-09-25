package droids

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	codexauth "github.com/akonwi/kit/internal/droids/openaicodex"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"golang.org/x/sync/singleflight"
)

const (
	defaultOpenAICodexResponsesBaseURL    = defaultOpenAICodexBaseURL + "/codex"
	defaultOpenAICodexOriginator          = "droids"
	openAICodexExpirySkew                 = 30 * time.Second
	openAICodexCredentialOperationTimeout = 45 * time.Second
	openAICodexCredentialReloadLimit      = 3
)

// OpenAICodexCredentials contains the OAuth state needed to authenticate and
// automatically refresh ChatGPT Codex access. It aliases the protocol type so
// applications can pass credentials directly without importing the helper
// package merely to configure a provider.
type OpenAICodexCredentials = codexauth.Credentials

// ErrOpenAICodexCredentialsChanged indicates that stored credentials changed
// since they were loaded. Stores must return it instead of overwriting a newer
// login, logout, or refresh generation.
var ErrOpenAICodexCredentialsChanged = errors.New("OpenAI Codex credentials changed")

// OpenAICodexCredentialRecord is one application-owned credential generation.
// Revision is an opaque compare-and-swap token and contains no secret material.
type OpenAICodexCredentialRecord struct {
	Credentials OpenAICodexCredentials
	Revision    string
}

// OpenAICodexCredentialStore is the optional application-owned persistence
// seam. Load must report a non-empty revision for configured credentials and
// return a fully empty record when no credential exists. Save only replaces a
// non-empty expected revision that is still current and returns
// ErrOpenAICodexCredentialsChanged otherwise. Credential creation belongs to
// an application login operation, not Save.
type OpenAICodexCredentialStore interface {
	LoadOpenAICodexCredentials(context.Context) (OpenAICodexCredentialRecord, error)
	SaveOpenAICodexCredentials(context.Context, string, OpenAICodexCredentials) (string, error)
}

// OpenAICodexCredentialError exposes a stable recovery class and an explicitly
// safe public message. Store and refresh implementation details are redacted.
type OpenAICodexCredentialError struct {
	Kind          ErrorKind
	PublicMessage string
}

func (e *OpenAICodexCredentialError) Error() string {
	if e == nil || e.PublicMessage == "" {
		return "OpenAI Codex credentials are unavailable"
	}
	return e.PublicMessage
}

// OpenAICodex configures the experimental ChatGPT-backed Codex Responses
// provider. Pass Credentials for in-memory use or CredentialStore when the
// application owns durable credentials. Expiring credentials are refreshed
// automatically and concurrent requests share one refresh.
type OpenAICodex struct {
	Credentials     OpenAICodexCredentials
	CredentialStore OpenAICodexCredentialStore
	// Originator identifies the application making the request. It defaults to
	// "droids"; applications embedding Droids should use their own truthful id.
	Originator string
	// HTTPClient optionally customizes transport behavior. Redirects are always
	// disabled so bearer and refresh credentials cannot be forwarded.
	HTTPClient *http.Client

	// Unexported fields are same-package test seams. Production callers cannot
	// redirect credentials or replace the OAuth protocol implementation.
	responsesBaseURL string
	now              func() time.Time
	refresher        openAICodexRefresher
}

type openAICodexRefresher interface {
	Refresh(context.Context, codexauth.Credentials) (codexauth.Credentials, error)
}

func (c OpenAICodex) build() (providerEntry, error) {
	hasCredentials := openAICodexCredentialsConfigured(c.Credentials)
	if hasCredentials == (c.CredentialStore != nil) {
		return providerEntry{}, fmt.Errorf("droids: OpenAICodex requires exactly one of Credentials or CredentialStore")
	}
	originator := c.Originator
	if originator == "" {
		originator = defaultOpenAICodexOriginator
	}
	if !validCodexHeaderValue(originator) {
		return providerEntry{}, fmt.Errorf("droids: OpenAICodex.Originator is not a valid header value")
	}
	baseURL := c.responsesBaseURL
	if baseURL == "" {
		baseURL = defaultOpenAICodexResponsesBaseURL
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	refresher := c.refresher
	if refresher == nil {
		oauthClient, err := codexauth.NewClient(c.HTTPClient, originator)
		if err != nil {
			return providerEntry{}, fmt.Errorf("droids: configure OpenAI Codex OAuth: %w", err)
		}
		refresher = oauthClient
	}

	models := make(map[string]Model, len(builtinOpenAICodexModels))
	for _, model := range builtinOpenAICodexModels {
		models[model.ID] = cloneModel(model)
	}
	credentials := &openAICodexCredentialManager{
		current:   c.Credentials,
		loaded:    hasCredentials,
		store:     c.CredentialStore,
		refresher: refresher,
		now:       now,
	}
	impl := &openAICodexProvider{
		credentials: credentials,
		originator:  originator,
		httpClient:  noRedirectHTTPClient(c.HTTPClient),
		baseURL:     baseURL,
		now:         now,
	}
	return providerEntry{
		id:              "openai-codex",
		models:          models,
		canonicalModels: true,
		stream:          impl.stream,
		validateReplay:  impl.validateReplay,
		baseURL:         defaultOpenAICodexBaseURL,
	}, nil
}

func openAICodexCredentialsConfigured(credentials OpenAICodexCredentials) bool {
	return credentials.AccessToken != "" || credentials.RefreshToken != "" ||
		credentials.IDToken != "" || credentials.AccountID != "" ||
		!credentials.ExpiresAt.IsZero() || credentials.FedRAMP
}

type openAICodexCredentialManager struct {
	current   OpenAICodexCredentials
	revision  string
	loaded    bool
	dirty     bool
	store     OpenAICodexCredentialStore
	refresher openAICodexRefresher
	now       func() time.Time
	group     singleflight.Group
}

func (m *openAICodexCredentialManager) resolve(ctx context.Context) (OpenAICodexCredentials, error) {
	result := m.group.DoChan("credentials", func() (any, error) {
		operationContext, cancel := context.WithTimeout(
			context.Background(), openAICodexCredentialOperationTimeout,
		)
		defer cancel()
		return m.resolveOwned(operationContext)
	})
	select {
	case resolved := <-result:
		if resolved.Err != nil {
			return OpenAICodexCredentials{}, resolved.Err
		}
		return resolved.Val.(OpenAICodexCredentials), nil
	case <-ctx.Done():
		return OpenAICodexCredentials{}, ctx.Err()
	}
}

func (m *openAICodexCredentialManager) resolveOwned(ctx context.Context) (OpenAICodexCredentials, error) {
	for range openAICodexCredentialReloadLimit {
		if m.store != nil {
			if m.dirty {
				changed, err := m.persist(ctx)
				if err != nil {
					return OpenAICodexCredentials{}, err
				}
				if changed {
					m.resetStoredCredentials()
					continue
				}
			}
			record, err := m.store.LoadOpenAICodexCredentials(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return OpenAICodexCredentials{}, ctx.Err()
				}
				return OpenAICodexCredentials{}, codexCredentialFailure(
					ErrorTransport, "OpenAI Codex credentials could not be loaded",
				)
			}
			if openAICodexCredentialsConfigured(record.Credentials) && record.Revision == "" {
				return OpenAICodexCredentials{}, codexCredentialFailure(
					ErrorProtocol, "OpenAI Codex credential store returned configured credentials without a revision",
				)
			}
			if !openAICodexCredentialsConfigured(record.Credentials) && record.Revision != "" {
				return OpenAICodexCredentials{}, codexCredentialFailure(
					ErrorProtocol, "OpenAI Codex credential store returned an empty credential generation",
				)
			}
			if !m.loaded || record.Revision != m.revision {
				m.current = record.Credentials
				m.revision = record.Revision
				m.loaded = true
			}
		}

		credentials, err := normalizeOpenAICodexCredentials(m.current)
		if err != nil {
			return OpenAICodexCredentials{}, err
		}
		m.current = credentials
		if credentials.AccessToken == "" && credentials.RefreshToken == "" {
			return OpenAICodexCredentials{}, codexCredentialFailure(
				ErrorAuthentication, "OpenAI Codex credentials are not configured",
			)
		}
		if !openAICodexNeedsRefresh(credentials, m.now()) {
			if err := validateOpenAICodexAccess(credentials, m.now()); err != nil {
				return OpenAICodexCredentials{}, codexCredentialFailure(ErrorAuthentication, err.Error())
			}
			return credentials, nil
		}
		if credentials.RefreshToken == "" {
			return OpenAICodexCredentials{}, codexCredentialFailure(
				ErrorAuthentication, "OpenAI Codex credentials expired and cannot be refreshed",
			)
		}

		refreshed, err := m.refresher.Refresh(ctx, credentials)
		if err != nil {
			if ctx.Err() != nil {
				return OpenAICodexCredentials{}, ctx.Err()
			}
			return OpenAICodexCredentials{}, classifyOpenAICodexRefreshError(err)
		}
		refreshed, err = normalizeOpenAICodexCredentials(refreshed)
		if err != nil {
			return OpenAICodexCredentials{}, err
		}
		if err := validateOpenAICodexAccess(refreshed, m.now()); err != nil {
			return OpenAICodexCredentials{}, codexCredentialFailure(ErrorAuthentication, err.Error())
		}
		m.current = refreshed
		if m.store != nil {
			m.dirty = true
			changed, err := m.persist(ctx)
			if err != nil {
				return OpenAICodexCredentials{}, err
			}
			if changed {
				m.resetStoredCredentials()
				continue
			}
		}
		return refreshed, nil
	}
	return OpenAICodexCredentials{}, codexCredentialFailure(
		ErrorTransport, "OpenAI Codex credentials changed repeatedly during refresh",
	)
}

func (m *openAICodexCredentialManager) persist(ctx context.Context) (bool, error) {
	revision, err := m.store.SaveOpenAICodexCredentials(ctx, m.revision, m.current)
	if errors.Is(err, ErrOpenAICodexCredentialsChanged) {
		return true, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, codexCredentialFailure(
			ErrorTransport, "refreshed OpenAI Codex credentials could not be saved",
		)
	}
	if revision == "" {
		return false, codexCredentialFailure(
			ErrorProtocol, "OpenAI Codex credential store returned an empty revision",
		)
	}
	m.revision = revision
	m.dirty = false
	return false, nil
}

func (m *openAICodexCredentialManager) resetStoredCredentials() {
	m.current = OpenAICodexCredentials{}
	m.revision = ""
	m.loaded = false
	m.dirty = false
}

func normalizeOpenAICodexCredentials(credentials OpenAICodexCredentials) (OpenAICodexCredentials, error) {
	accessAccount, hasAccessAccount, accessFedRAMP, hasAccessFedRAMP, err := openAICodexClaimedAccount(credentials.AccessToken)
	if err != nil {
		return OpenAICodexCredentials{}, codexCredentialFailure(
			ErrorAuthentication, "OpenAI Codex access token has malformed account metadata",
		)
	}
	idAccount, hasIDAccount, idFedRAMP, hasIDFedRAMP, err := openAICodexClaimedAccount(credentials.IDToken)
	if err != nil {
		return OpenAICodexCredentials{}, codexCredentialFailure(
			ErrorAuthentication, "OpenAI Codex ID token has malformed account metadata",
		)
	}
	if hasAccessAccount && hasIDAccount && accessAccount != idAccount {
		return OpenAICodexCredentials{}, codexCredentialFailure(
			ErrorAuthentication, "OpenAI Codex access and ID tokens belong to different accounts",
		)
	}
	if hasAccessFedRAMP && hasIDFedRAMP && accessFedRAMP != idFedRAMP {
		return OpenAICodexCredentials{}, codexCredentialFailure(
			ErrorAuthentication, "OpenAI Codex access and ID tokens disagree on FedRAMP routing",
		)
	}
	for _, claimed := range []struct {
		id  string
		has bool
	}{{accessAccount, hasAccessAccount}, {idAccount, hasIDAccount}} {
		if !claimed.has {
			continue
		}
		if credentials.AccountID != "" && credentials.AccountID != claimed.id {
			return OpenAICodexCredentials{}, codexCredentialFailure(
				ErrorAuthentication, "OpenAI Codex token and account id do not match",
			)
		}
		credentials.AccountID = claimed.id
	}
	if credentials.AccessToken != "" && credentials.AccountID == "" {
		return OpenAICodexCredentials{}, codexCredentialFailure(
			ErrorAuthentication, "OpenAI Codex account id is missing",
		)
	}
	credentials.FedRAMP = credentials.FedRAMP ||
		(hasAccessFedRAMP && accessFedRAMP) || (hasIDFedRAMP && idFedRAMP)
	if credentials.ExpiresAt.IsZero() && strings.Count(credentials.AccessToken, ".") == 2 {
		expiresAt, ok, err := codexauth.AccessTokenExpiry(credentials.AccessToken)
		if err != nil {
			return OpenAICodexCredentials{}, codexCredentialFailure(
				ErrorAuthentication, "OpenAI Codex access token has malformed expiry metadata",
			)
		}
		if ok {
			credentials.ExpiresAt = expiresAt
		}
	}
	return credentials, nil
}

func openAICodexNeedsRefresh(credentials OpenAICodexCredentials, now time.Time) bool {
	return credentials.AccessToken == "" ||
		(!credentials.ExpiresAt.IsZero() && !credentials.ExpiresAt.After(now.Add(openAICodexExpirySkew)))
}

func classifyOpenAICodexRefreshError(err error) error {
	kind := ErrorTransport
	message := "OpenAI Codex credentials could not be refreshed"
	var oauthErr *codexauth.Error
	if errors.As(err, &oauthErr) {
		code := strings.ToLower(oauthErr.Code)
		switch {
		case oauthErr.StatusCode == http.StatusUnauthorized,
			code == "invalid_grant",
			code == "invalid_token",
			code == "token_expired",
			code == "account_changed",
			code == "routing_changed":
			kind = ErrorAuthentication
			message = "OpenAI Codex login expired; authenticate again"
		case oauthErr.StatusCode == http.StatusTooManyRequests,
			code == "slow_down",
			strings.Contains(code, "rate_limit"):
			kind = ErrorRateLimit
			message = "OpenAI Codex credential refresh is rate limited"
		case oauthErr.StatusCode == http.StatusBadRequest:
			kind = ErrorProtocol
			message = "OpenAI Codex credential refresh was rejected"
		}
	}
	return codexCredentialFailure(kind, message)
}

func codexCredentialFailure(kind ErrorKind, message string) error {
	return &OpenAICodexCredentialError{Kind: kind, PublicMessage: message}
}

type openAICodexProvider struct {
	credentials *openAICodexCredentialManager
	originator  string
	httpClient  *http.Client
	baseURL     string
	now         func() time.Time
}

func (p *openAICodexProvider) validateReplay(ctx context.Context, model Model, messages []Message) error {
	credentials, err := p.credentials.resolve(ctx)
	if err != nil {
		return err
	}
	if err := validateOpenAICodexAccess(credentials, p.now()); err != nil {
		return err
	}
	if err := validateOpenAICodexTranscriptScope(messages, model.Provider, openAICodexProviderScope(credentials.AccountID)); err != nil {
		return err
	}
	return validateOpenAICodexContent(model, messages)
}

func (p *openAICodexProvider) stream(ctx context.Context, model Model, req Request, _ callOptions) Stream {
	s := newPipeStream()
	go p.run(ctx, model, req, s)
	return s
}

func (p *openAICodexProvider) run(ctx context.Context, model Model, req Request, s *pipeStream) {
	defer s.finish()
	emitOpenAIStreamStart(s, model)

	credentials, err := p.credentials.resolve(ctx)
	if err != nil {
		message := "OpenAI Codex credentials are unavailable"
		kind := ErrorTransport
		var credentialErr *OpenAICodexCredentialError
		if errors.As(err, &credentialErr) {
			if credentialErr.PublicMessage != "" {
				message = credentialErr.PublicMessage
			}
			if credentialErr.Kind != "" {
				kind = credentialErr.Kind
			}
		}
		final := responseErrorMessage(model, ctx, message)
		if ctx.Err() == nil {
			final.ErrorKind = kind
		}
		emitOpenAITerminal(s, final, true)
		return
	}
	if err := validateOpenAICodexAccess(credentials, p.now()); err != nil {
		final := responseErrorMessage(model, ctx, err.Error())
		final.ErrorKind = ErrorAuthentication
		emitOpenAITerminal(s, final, true)
		return
	}
	providerScope := openAICodexProviderScope(credentials.AccountID)
	if err := validateOpenAICodexTranscriptScope(req.Messages, model.Provider, providerScope); err != nil {
		final := responseErrorMessage(model, ctx, err.Error())
		final.ProviderScope = providerScope
		final.ErrorKind = ErrorAuthentication
		emitOpenAITerminal(s, final, true)
		return
	}
	if err := validateOpenAICodexContent(model, req.Messages); err != nil {
		final := responseErrorMessage(model, ctx, err.Error())
		final.ErrorKind = ErrorProtocol
		emitOpenAITerminal(s, final, true)
		return
	}
	params, err := buildOpenAICodexResponseParams(model, req)
	if err != nil {
		final := responseErrorMessage(model, ctx, err.Error())
		final.ErrorKind = ErrorProtocol
		emitOpenAITerminal(s, final, true)
		return
	}

	clientOptions := []option.RequestOption{
		option.WithAPIKey(credentials.AccessToken),
		// Codex model requests are not known to be idempotent. Never let the SDK
		// replay a POST after a transport, rate-limit, or server failure.
		option.WithMaxRetries(0),
		option.WithBaseURL(p.baseURL),
		option.WithHTTPClient(p.httpClient),
		option.WithHeader("chatgpt-account-id", credentials.AccountID),
		option.WithHeader("originator", p.originator),
		option.WithHeader("OpenAI-Beta", "responses=experimental"),
		option.WithHeader("Accept", "text/event-stream"),
		option.WithHeader("User-Agent", "droids/openai-codex"),
	}
	if openAICodexFedRAMP(credentials) {
		clientOptions = append(clientOptions, option.WithHeader("X-OpenAI-Fedramp", "true"))
	}
	client := openai.NewClient(clientOptions...)
	stream := client.Responses.NewStreaming(ctx, params)
	defer stream.Close()
	consumeOpenAIResponseStream(ctx, model, stream, s, openAIResponsesProfile{
		name:          "OpenAI Codex",
		providerScope: providerScope,
		classify:      classifyOpenAICodexError,
	})
}

func buildOpenAICodexResponseParams(model Model, req Request) (responses.ResponseNewParams, error) {
	if req.ReasoningHistory != nil {
		return responses.ResponseNewParams{}, fmt.Errorf("droids: Codex does not support ordered reasoning history")
	}
	params, err := buildOpenAIBaseResponseParams(model, req)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	if req.SystemPrompt == "" {
		params.Instructions = param.NewOpt("You are a helpful assistant.")
	}
	// ChatGPT's Codex endpoint currently rejects the public Responses API's
	// max_output_tokens field. Droids rejects explicit allowances for these
	// models and uses its internal default only for context reservation.
	params.MaxOutputTokens = param.Opt[int64]{}
	params.ParallelToolCalls = param.NewOpt(true)
	params.ToolChoice.OfToolChoiceMode = param.NewOpt(responses.ToolChoiceOptionsAuto)
	params.Text.Verbosity = responses.ResponseTextConfigVerbosityLow

	// The ChatGPT Codex dialect maps Droids' smallest explicit effort to low.
	switch req.Reasoning {
	case "none", "off":
		params.Reasoning.Effort = shared.ReasoningEffortNone
		params.Reasoning.Summary = ""
	case "minimal":
		params.Reasoning.Effort = shared.ReasoningEffortLow
		params.Reasoning.Summary = shared.ReasoningSummaryAuto
	}
	// Codex catalog policies remain unspecified until endpoint support is verified.
	applyOpenAITemperaturePolicy(model, &params, params.Reasoning.Effort)
	return params, nil
}

func validateOpenAICodexAccess(credentials OpenAICodexCredentials, now time.Time) error {
	if credentials.AccessToken == "" || credentials.AccessToken != strings.TrimSpace(credentials.AccessToken) {
		return fmt.Errorf("OpenAI Codex access token is missing or malformed")
	}
	if credentials.AccountID == "" || credentials.AccountID != strings.TrimSpace(credentials.AccountID) {
		return fmt.Errorf("OpenAI Codex account id is missing or malformed")
	}
	if !credentials.ExpiresAt.IsZero() && !credentials.ExpiresAt.After(now.Add(openAICodexExpirySkew)) {
		return fmt.Errorf("OpenAI Codex access token is expired or expires too soon")
	}
	if claimed, ok, _, _, err := openAICodexClaimedAccount(credentials.AccessToken); err != nil {
		return fmt.Errorf("OpenAI Codex access token has malformed account metadata")
	} else if ok && claimed != credentials.AccountID {
		return fmt.Errorf("OpenAI Codex access token and account id do not match")
	}
	return nil
}

func openAICodexFedRAMP(credentials OpenAICodexCredentials) bool {
	_, _, claimed, known, err := openAICodexClaimedAccount(credentials.AccessToken)
	return credentials.FedRAMP || (err == nil && known && claimed)
}

func openAICodexClaimedAccount(token string) (accountID string, hasAccount, fedRAMP, hasFedRAMP bool, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false, false, false, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false, false, false, err
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false, false, false, err
	}
	value, exists := claims["https://api.openai.com/auth"]
	if !exists {
		return "", false, false, false, nil
	}
	var auth struct {
		AccountID string `json:"chatgpt_account_id"`
		FedRAMP   *bool  `json:"chatgpt_account_is_fedramp"`
	}
	if err := json.Unmarshal(value, &auth); err != nil {
		return "", false, false, false, err
	}
	if auth.FedRAMP != nil {
		fedRAMP, hasFedRAMP = *auth.FedRAMP, true
	}
	return auth.AccountID, auth.AccountID != "", fedRAMP, hasFedRAMP, nil
}

func openAICodexProviderScope(accountID string) string {
	hash := sha256.Sum256([]byte("openai-codex\x00" + accountID))
	return "account:" + base64.RawURLEncoding.EncodeToString(hash[:])
}

func validateOpenAICodexTranscriptScope(messages []Message, provider, scope string) error {
	for _, message := range messages {
		assistant, ok := message.(AssistantMessage)
		if !ok || assistant.Provider != provider {
			continue
		}
		if assistant.ProviderScope == "" {
			return fmt.Errorf("OpenAI Codex transcript is missing its account binding")
		}
		if assistant.ProviderScope != scope {
			return fmt.Errorf("OpenAI Codex transcript belongs to different credentials")
		}
	}
	return nil
}

func validateOpenAICodexContent(model Model, messages []Message) error {
	for _, message := range messages {
		var err error
		switch value := message.(type) {
		case UserMessage:
			err = validateOpenAICodexBlocks(model, value.Content)
		case ContextMessage:
			err = validateOpenAICodexBlocks(model, value.Content)
		case ToolResultMessage:
			err = validateOpenAICodexBlocks(model, value.Content)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func validateOpenAICodexBlocks[T any](model Model, content []T) error {
	for _, block := range content {
		switch value := any(block).(type) {
		case FileInput:
			if !isImageMediaType(value.MediaType) {
				return fmt.Errorf("openai-codex: files other than images are not supported")
			}
			if !containsString(model.Input, "image") {
				return fmt.Errorf("openai-codex: model %q does not support image input", model.ID)
			}
		case FileContent:
			if !isImageMediaType(value.MediaType) {
				return fmt.Errorf("openai-codex: files other than images are not supported")
			}
			if !containsString(model.Input, "image") {
				return fmt.Errorf("openai-codex: model %q does not support image input", model.ID)
			}
		}
	}
	return nil
}

func classifyOpenAICodexError(status int, code, message string) ErrorKind {
	value := strings.ToLower(code + " " + message)
	switch {
	case status == http.StatusUnauthorized,
		strings.Contains(value, "invalid_token"),
		strings.Contains(value, "invalid api key"),
		strings.Contains(value, "authentication"):
		return ErrorAuthentication
	case strings.Contains(value, "usage_limit_reached"),
		strings.Contains(value, "usage_not_included"),
		strings.Contains(value, "chatgpt usage limit"):
		return ErrorUsageLimit
	case status == http.StatusForbidden,
		strings.Contains(value, "entitlement"),
		strings.Contains(value, "permission"):
		return ErrorEntitlement
	case status == http.StatusTooManyRequests,
		strings.Contains(value, "rate_limit"):
		return ErrorRateLimit
	case status >= 500:
		return ErrorTransport
	default:
		return ErrorProtocol
	}
}

func noRedirectHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fmt.Errorf("droids: OpenAI Codex endpoint redirects are disabled")
	}
	return &client
}

func validCodexHeaderValue(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
