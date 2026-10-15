package clienttransport

import (
	"context"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// ListProviders lists the server's providers and their credentials.
func (c *Client) ListProviders(ctx context.Context) (protocol.ProviderList, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ListProviders, httpapi.ServerPath{}, httpapi.NoBody{})
	if err != nil {
		return protocol.ProviderList{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ProviderList{}, protocolErrorf("validate provider list: %w", err)
	}
	return output, nil
}

// StartProviderLogin starts a device or browser login on the server.
func (c *Client) StartProviderLogin(ctx context.Context, input protocol.StartLoginInput) (protocol.ProviderLogin, error) {
	return validLogin(httpapi.Call(ctx, c, httpapi.StartProviderLogin, httpapi.ServerPath{}, input))
}

// GetProviderLogin returns a login, waiting up to waitSeconds for a pending
// login to finish.
func (c *Client) GetProviderLogin(ctx context.Context, loginID string, waitSeconds int) (protocol.ProviderLogin, error) {
	return validLogin(httpapi.Call(ctx, c, httpapi.GetProviderLogin, httpapi.WaitLoginPath{LoginID: loginID, WaitSeconds: waitSeconds}, httpapi.NoBody{}))
}

// SubmitProviderLoginAuthorization completes a pending browser login.
func (c *Client) SubmitProviderLoginAuthorization(ctx context.Context, loginID, authorization string) error {
	_, err := httpapi.Call(ctx, c, httpapi.SubmitProviderLoginAuthorization, httpapi.LoginPath{LoginID: loginID}, protocol.LoginAuthorizationInput{Authorization: authorization})
	return err
}

// CancelProviderLogin cancels a pending login.
func (c *Client) CancelProviderLogin(ctx context.Context, loginID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.CancelProviderLogin, httpapi.LoginPath{LoginID: loginID}, httpapi.NoBody{})
	return err
}

// SetProviderAPIKey saves an API key for a provider on the server.
func (c *Client) SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	_, err := httpapi.Call(ctx, c, httpapi.SetProviderAPIKey, httpapi.ProviderPath{ProviderID: providerID}, protocol.ProviderAPIKeyInput{APIKey: apiKey})
	return err
}

// DeleteProviderCredentials logs the server out of a provider.
func (c *Client) DeleteProviderCredentials(ctx context.Context, providerID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.DeleteProviderCredentials, httpapi.ProviderPath{ProviderID: providerID}, httpapi.NoBody{})
	return err
}

func validLogin(login protocol.ProviderLogin, err error) (protocol.ProviderLogin, error) {
	if err != nil {
		return protocol.ProviderLogin{}, err
	}
	if err := login.Validate(); err != nil {
		return protocol.ProviderLogin{}, protocolErrorf("validate provider login: %w", err)
	}
	return login, nil
}
