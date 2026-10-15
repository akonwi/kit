package clienttransport

import (
	"context"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// ServerSettings reads the server's settings.
func (c *Client) ServerSettings(ctx context.Context) (protocol.ServerSettings, error) {
	return validSettings(httpapi.Call(ctx, c, httpapi.GetServerSettings, httpapi.ServerPath{}, httpapi.NoBody{}))
}

// UpdateServerSettings changes the server's settings and returns the result.
func (c *Client) UpdateServerSettings(ctx context.Context, input protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error) {
	return validSettings(httpapi.Call(ctx, c, httpapi.UpdateServerSettings, httpapi.ServerPath{}, input))
}

func validSettings(output protocol.ServerSettings, err error) (protocol.ServerSettings, error) {
	if err != nil {
		return protocol.ServerSettings{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.ServerSettings{}, protocolErrorf("validate server settings: %w", err)
	}
	return output, nil
}
