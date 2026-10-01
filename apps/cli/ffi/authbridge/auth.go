// Package authbridge adapts callback-shaped CLI authentication services for Ard.
package authbridge

import (
	"context"
	"fmt"

	"github.com/akonwi/kit/apps/cli/ffi/cli"
	"github.com/akonwi/kit/internal/auth"
)

// DeviceInstructions is the display-safe state for an OpenAI Codex device login.
type DeviceInstructions struct {
	VerificationURI string
	UserCode        string
	ExpiresAtUnixMS int64
}

// BrowserInstructions is the display-safe state for a Claude browser login.
type BrowserInstructions struct {
	AuthorizationURL string
	RedirectURI      string
}

// RunDevice runs an injected Codex device login and projects its instructions
// into Ard-friendly primitive fields.
func RunDevice(ctx context.Context, login cli.DeviceLogin, notify func(DeviceInstructions)) error {
	if login == nil {
		return fmt.Errorf("OpenAI Codex login is unavailable")
	}
	if notify == nil {
		return fmt.Errorf("device-login instruction handler is unavailable")
	}
	return login.Login(ctx, func(instructions auth.OpenAICodexDeviceInstructions) error {
		notify(DeviceInstructions{
			VerificationURI: instructions.VerificationURI,
			UserCode:        instructions.UserCode,
			ExpiresAtUnixMS: instructions.ExpiresAt.UnixMilli(),
		})
		return nil
	})
}

// RunBrowser runs an injected Claude browser login with its optional manual
// approval-code channel and projects its instructions into primitive fields.
func RunBrowser(ctx context.Context, login cli.BrowserLogin, manual <-chan string, notify func(BrowserInstructions)) error {
	if login == nil {
		return fmt.Errorf("Claude subscription login is unavailable")
	}
	if notify == nil {
		return fmt.Errorf("browser-login instruction handler is unavailable")
	}
	return login.Login(ctx, manual, func(instructions auth.AnthropicLoginInstructions) error {
		notify(BrowserInstructions{
			AuthorizationURL: instructions.AuthorizationURL,
			RedirectURI:      instructions.RedirectURI,
		})
		return nil
	})
}

// RunAPIKey installs an API-key credential through an injected CLI service.
func RunAPIKey(ctx context.Context, login cli.APIKeyLogin, providerID, apiKey string) error {
	if login == nil {
		return fmt.Errorf("API-key login is unavailable")
	}
	return login.Login(ctx, providerID, apiKey)
}
