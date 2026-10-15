package kit

import (
	"context"
	"errors"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/identifier"
)

type providerTransport interface {
	ListProviders(context.Context) (protocol.ProviderList, error)
	StartProviderLogin(context.Context, protocol.StartLoginInput) (protocol.ProviderLogin, error)
	GetProviderLogin(context.Context, string, int) (protocol.ProviderLogin, error)
	SubmitProviderLoginAuthorization(context.Context, string, string) error
	CancelProviderLogin(context.Context, string) error
	SetProviderAPIKey(context.Context, string, string) error
	DeleteProviderCredentials(context.Context, string) error
}

// MaxLoginWait bounds how long one WaitProviderLogin call waits.
const MaxLoginWait = 25 * time.Second

var errNoProviderTransport = errors.New("provider transport is unavailable")

// withProviders runs operation with the client's provider transport.
func (c *Client) withProviders(ctx context.Context, operation func(context.Context, providerTransport) error) error {
	operationContext, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.providers == nil {
		return errNoProviderTransport
	}
	return projectError(operation(operationContext, c.providers))
}

// Providers lists the server's model providers, how each is logged in to, and
// whether the server has a credential for it.
func (c *Client) Providers(ctx context.Context) ([]ProviderInfo, error) {
	var list protocol.ProviderList
	err := c.withProviders(ctx, func(ctx context.Context, transport providerTransport) (err error) {
		list, err = transport.ListProviders(ctx)
		return err
	})
	return list.Providers, err
}

// StartProviderLogin starts a device or browser login on the server and
// returns it with its instructions. A pending login to the same provider is
// superseded. The server stores the credential when the login succeeds.
func (c *Client) StartProviderLogin(ctx context.Context, providerID string, method LoginMethod) (ProviderLogin, error) {
	input := protocol.StartLoginInput{Provider: providerID, Method: method}
	if err := input.Validate(); err != nil {
		return ProviderLogin{}, err
	}
	var login ProviderLogin
	err := c.withProviders(ctx, func(ctx context.Context, transport providerTransport) (err error) {
		login, err = transport.StartProviderLogin(ctx, input)
		return err
	})
	return login, err
}

// WaitProviderLogin returns a login once it finishes or wait, at most
// MaxLoginWait, elapses. Its state is pending when wait elapsed first.
func (c *Client) WaitProviderLogin(ctx context.Context, loginID string, wait time.Duration) (ProviderLogin, error) {
	if !identifier.Valid(loginID, "login_") {
		return ProviderLogin{}, errors.New("login id is not canonical")
	}
	seconds := int(min(max(wait, 0), MaxLoginWait) / time.Second)
	var login ProviderLogin
	err := c.withProviders(ctx, func(ctx context.Context, transport providerTransport) (err error) {
		login, err = transport.GetProviderLogin(ctx, loginID, seconds)
		return err
	})
	if err == nil && login.ID != loginID {
		return ProviderLogin{}, &ProtocolError{Err: errors.New("server returned a different login")}
	}
	return login, err
}

// SubmitProviderLoginAuthorization completes a pending browser login with the
// redirect URL the browser was sent to, or the code the authorization page
// showed.
func (c *Client) SubmitProviderLoginAuthorization(ctx context.Context, loginID, authorization string) error {
	if !identifier.Valid(loginID, "login_") {
		return errors.New("login id is not canonical")
	}
	if err := (protocol.LoginAuthorizationInput{Authorization: authorization}).Validate(); err != nil {
		return err
	}
	return c.withProviders(ctx, func(ctx context.Context, transport providerTransport) error {
		return transport.SubmitProviderLoginAuthorization(ctx, loginID, authorization)
	})
}

// CancelProviderLogin cancels a pending login. Cancelling a finished login
// has no effect.
func (c *Client) CancelProviderLogin(ctx context.Context, loginID string) error {
	if !identifier.Valid(loginID, "login_") {
		return errors.New("login id is not canonical")
	}
	return c.withProviders(ctx, func(ctx context.Context, transport providerTransport) error {
		return transport.CancelProviderLogin(ctx, loginID)
	})
}

// SetProviderAPIKey saves an API key for a provider on the server, replacing
// its earlier credential. The server never returns it.
func (c *Client) SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	if err := (protocol.ProviderAPIKeyInput{APIKey: apiKey}).Validate(); err != nil {
		return err
	}
	return c.withProviders(ctx, func(ctx context.Context, transport providerTransport) error {
		return transport.SetProviderAPIKey(ctx, providerID, apiKey)
	})
}

// Logout removes the server's stored credential for a provider. Credentials
// from the server's environment cannot be removed and fail with
// ErrorConflict.
func (c *Client) Logout(ctx context.Context, providerID string) error {
	return c.withProviders(ctx, func(ctx context.Context, transport providerTransport) error {
		return transport.DeleteProviderCredentials(ctx, providerID)
	})
}
