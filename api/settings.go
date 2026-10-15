package kit

import (
	"context"
	"errors"

	protocol "github.com/akonwi/kit/api/contract"
)

type settingsTransport interface {
	ServerSettings(context.Context) (protocol.ServerSettings, error)
	UpdateServerSettings(context.Context, protocol.UpdateServerSettingsInput) (protocol.ServerSettings, error)
}

var errNoSettingsTransport = errors.New("settings transport is unavailable")

// ServerSettings reads the settings that govern the server's behavior,
// including the model and thinking level a new session would start with.
func (c *Client) ServerSettings(ctx context.Context) (ServerSettings, error) {
	var settings ServerSettings
	err := c.withSettings(ctx, func(ctx context.Context, transport settingsTransport) (err error) {
		settings, err = transport.ServerSettings(ctx)
		return err
	})
	return settings, err
}

// UpdateServerSettings changes the settings input carries and returns the
// resulting settings. Models must be in the server's catalog.
func (c *Client) UpdateServerSettings(ctx context.Context, input UpdateServerSettingsInput) (ServerSettings, error) {
	if err := input.Validate(); err != nil {
		return ServerSettings{}, err
	}
	var settings ServerSettings
	err := c.withSettings(ctx, func(ctx context.Context, transport settingsTransport) (err error) {
		settings, err = transport.UpdateServerSettings(ctx, input)
		return err
	})
	return settings, err
}

// SetModelContextWindow sets the server's context-window override for an
// exact provider/model, or removes it when contextWindow is zero.
func (c *Client) SetModelContextWindow(ctx context.Context, model string, contextWindow int) error {
	_, err := c.UpdateServerSettings(ctx, UpdateServerSettingsInput{ModelOverrides: []ModelOverride{{Model: model, ContextWindow: contextWindow}}})
	return err
}

func (c *Client) withSettings(ctx context.Context, operation func(context.Context, settingsTransport) error) error {
	operationContext, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.settings == nil {
		return errNoSettingsTransport
	}
	return projectError(operation(operationContext, c.settings))
}
