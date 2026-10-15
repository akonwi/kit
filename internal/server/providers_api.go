package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/providerlogin"
)

// providerAccess serves provider credentials: listing providers, logins, API
// keys, and logout. Credentials are written only by the server, to its own
// credential store.
type providerAccess struct {
	logins *providerlogin.Registry
	store  *auth.Store
	// sources says where each provider's credential comes from; providers
	// absent from it use the store.
	sources map[string]localdiscovery.CredentialSource
	// configured lists the providers the server has credentials for.
	configured func(context.Context) []string
}

// authLoginFlows runs logins with the providers' real endpoints, storing
// credentials in store.
func authLoginFlows(store *auth.Store) providerlogin.Flows {
	return providerlogin.Flows{
		Device: func(ctx context.Context, provider string, notify func(providerlogin.DeviceInstructions)) error {
			if provider != auth.OpenAICodexProviderID {
				return fmt.Errorf("%s has no device login", provider)
			}
			login, err := auth.NewOpenAICodexDeviceLogin(auth.OpenAICodexDeviceLoginOptions{Store: store})
			if err != nil {
				return err
			}
			return login.Login(ctx, func(instructions auth.OpenAICodexDeviceInstructions) error {
				notify(providerlogin.DeviceInstructions{
					VerificationURI: instructions.VerificationURI, UserCode: instructions.UserCode, ExpiresAt: instructions.ExpiresAt,
				})
				return nil
			})
		},
		Browser: func(ctx context.Context, provider string, authorization <-chan string, notify func(providerlogin.BrowserInstructions)) error {
			if provider != auth.AnthropicProviderID {
				return fmt.Errorf("%s has no browser login", provider)
			}
			login, err := auth.NewAnthropicOAuthLogin(auth.AnthropicOAuthLoginOptions{Store: store})
			if err != nil {
				return err
			}
			return login.Login(ctx, authorization, func(instructions auth.AnthropicLoginInstructions) error {
				notify(providerlogin.BrowserInstructions{AuthorizationURL: instructions.AuthorizationURL, RedirectURI: instructions.RedirectURI})
				return nil
			})
		},
	}
}

func (p providerAccess) source(providerID string) protocol.CredentialSource {
	if p.sources[providerID] == localdiscovery.CredentialSourceEnvironment {
		return protocol.CredentialSourceEnvironment
	}
	return protocol.CredentialSourceStore
}

// changeable checks that the server's credential for providerID is one the
// API may change.
func (p providerAccess) changeable(providerID string) (providerlogin.Provider, error) {
	provider, ok := providerlogin.Find(providerID)
	if !ok {
		return providerlogin.Provider{}, httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "unknown provider", nil)
	}
	if p.source(providerID) == protocol.CredentialSourceEnvironment {
		return providerlogin.Provider{}, httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict,
			fmt.Sprintf("the server's credentials for %s come from its environment; change them there", provider.Name), nil)
	}
	return provider, nil
}

func projectLogin(snapshot providerlogin.Snapshot) protocol.ProviderLogin {
	login := protocol.ProviderLogin{
		ID: snapshot.ID, Provider: snapshot.Provider, Method: protocol.LoginMethod(snapshot.Method),
		State: protocol.LoginState(snapshot.State), Failure: snapshot.Failure,
		ExpiresAt: snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	if device := snapshot.Device; device != nil {
		login.Device = &protocol.DeviceLoginInstructions{
			VerificationURI: device.VerificationURI, UserCode: device.UserCode,
			ExpiresAt: device.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
	}
	if browser := snapshot.Browser; browser != nil {
		login.Browser = &protocol.BrowserLoginInstructions{AuthorizationURL: browser.AuthorizationURL, RedirectURI: browser.RedirectURI}
	}
	return login
}

func loginError(err error) error {
	switch {
	case errors.Is(err, providerlogin.ErrNotFound):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "login not found", nil)
	case errors.Is(err, providerlogin.ErrUnknownProvider):
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "unknown provider", nil)
	case errors.Is(err, providerlogin.ErrUnsupportedMethod):
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, err.Error(), nil)
	case errors.Is(err, providerlogin.ErrCapacity):
		return httpapi.NewAPIError(http.StatusTooManyRequests, httpapi.ErrorCapacityExceeded, err.Error(), nil)
	case errors.Is(err, providerlogin.ErrNotBrowser), errors.Is(err, providerlogin.ErrNotPending), errors.Is(err, providerlogin.ErrSubmitted):
		return httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorConflict, err.Error(), nil)
	case errors.Is(err, providerlogin.ErrClosed):
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, err.Error(), nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The client stopped waiting; nobody reads this response.
		return httpapi.NewAPIError(http.StatusServiceUnavailable, httpapi.ErrorUnavailable, "the request ended", nil)
	}
	return err
}

func validLoginID(id string) error {
	if !identifier.Valid(id, "login_") {
		return httpapi.NewAPIError(http.StatusNotFound, httpapi.ErrorNotFound, "login not found", nil)
	}
	return nil
}

func registerProviderRoutes(mux *http.ServeMux, p providerAccess, logger *slog.Logger) {
	options := httpapi.ServeOptions{MaxRequestBytes: 8 << 10, WriteError: func(writer http.ResponseWriter, err error) {
		var apiError *httpapi.APIError
		if !errors.As(err, &apiError) {
			if logger != nil {
				logger.Error("provider request failed", "error", err)
			}
			apiError = httpapi.NewAPIError(http.StatusInternalServerError, httpapi.ErrorInternal, "internal error", nil)
		}
		httpapi.WriteError(writer, apiError)
	}}
	invalid := func(err error) error {
		return httpapi.NewAPIError(http.StatusBadRequest, httpapi.ErrorInvalidRequest, err.Error(), nil)
	}
	httpapi.Handle(mux, options, httpapi.ListProviders, func(ctx context.Context, _ httpapi.ServerPath, _ httpapi.NoBody) (protocol.ProviderList, error) {
		configured := p.configured(ctx)
		list := protocol.ProviderList{Providers: make([]protocol.ProviderInfo, 0, len(providerlogin.Providers))}
		for _, provider := range providerlogin.Providers {
			methods := make([]protocol.LoginMethod, 0, len(provider.Methods))
			for _, method := range provider.Methods {
				methods = append(methods, protocol.LoginMethod(method))
			}
			list.Providers = append(list.Providers, protocol.ProviderInfo{
				ID: provider.ID, Name: provider.Name, Methods: methods,
				Configured: slices.Contains(configured, provider.ID), Source: p.source(provider.ID),
			})
		}
		return list, nil
	})
	httpapi.Handle(mux, options, httpapi.StartProviderLogin, func(ctx context.Context, _ httpapi.ServerPath, input protocol.StartLoginInput) (protocol.ProviderLogin, error) {
		if err := input.Validate(); err != nil {
			return protocol.ProviderLogin{}, invalid(err)
		}
		if _, err := p.changeable(input.Provider); err != nil {
			return protocol.ProviderLogin{}, err
		}
		snapshot, err := p.logins.Start(ctx, input.Provider, providerlogin.Method(input.Method))
		if err != nil {
			return protocol.ProviderLogin{}, loginError(err)
		}
		return projectLogin(snapshot), nil
	})
	httpapi.Handle(mux, options, httpapi.GetProviderLogin, func(ctx context.Context, path httpapi.WaitLoginPath, _ httpapi.NoBody) (protocol.ProviderLogin, error) {
		if err := validLoginID(path.LoginID); err != nil {
			return protocol.ProviderLogin{}, err
		}
		if path.WaitSeconds < 0 || time.Duration(path.WaitSeconds)*time.Second > providerlogin.MaxWait {
			return protocol.ProviderLogin{}, invalid(fmt.Errorf("waitSeconds must be between 0 and %d", int(providerlogin.MaxWait/time.Second)))
		}
		snapshot, err := p.logins.Wait(ctx, path.LoginID, time.Duration(path.WaitSeconds)*time.Second)
		if err != nil {
			return protocol.ProviderLogin{}, loginError(err)
		}
		return projectLogin(snapshot), nil
	})
	httpapi.Handle(mux, options, httpapi.SubmitProviderLoginAuthorization, func(_ context.Context, path httpapi.LoginPath, input protocol.LoginAuthorizationInput) (httpapi.NoBody, error) {
		if err := validLoginID(path.LoginID); err != nil {
			return httpapi.NoBody{}, err
		}
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, invalid(err)
		}
		return httpapi.NoBody{}, loginError(p.logins.Submit(path.LoginID, input.Authorization))
	})
	httpapi.Handle(mux, options, httpapi.CancelProviderLogin, func(_ context.Context, path httpapi.LoginPath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		if err := validLoginID(path.LoginID); err != nil {
			return httpapi.NoBody{}, err
		}
		return httpapi.NoBody{}, loginError(p.logins.Cancel(path.LoginID))
	})
	httpapi.Handle(mux, options, httpapi.SetProviderAPIKey, func(ctx context.Context, path httpapi.ProviderPath, input protocol.ProviderAPIKeyInput) (httpapi.NoBody, error) {
		provider, err := p.changeable(path.ProviderID)
		if err != nil {
			return httpapi.NoBody{}, err
		}
		if !provider.Supports(providerlogin.MethodAPIKey) {
			return httpapi.NoBody{}, invalid(fmt.Errorf("%s does not accept API keys", provider.Name))
		}
		if err := input.Validate(); err != nil {
			return httpapi.NoBody{}, invalid(err)
		}
		login, err := auth.NewAPIKeyLogin(auth.APIKeyLoginOptions{Store: p.store})
		if err != nil {
			return httpapi.NoBody{}, err
		}
		return httpapi.NoBody{}, login.Login(ctx, provider.ID, input.APIKey)
	})
	httpapi.Handle(mux, options, httpapi.DeleteProviderCredentials, func(ctx context.Context, path httpapi.ProviderPath, _ httpapi.NoBody) (httpapi.NoBody, error) {
		provider, err := p.changeable(path.ProviderID)
		if err != nil {
			return httpapi.NoBody{}, err
		}
		// A login still pending must not store a credential after logout.
		p.logins.CancelProvider(provider.ID)
		return httpapi.NoBody{}, p.store.Delete(ctx, provider.ID)
	})
}
