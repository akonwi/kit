// Package authbridge adapts callback-shaped CLI authentication services for Ard.
package authbridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/akonwi/kit/apps/cli/ffi/cli"
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
	// ReceivesRedirect reports that the client listens for the browser's
	// redirect; otherwise the user pastes it.
	ReceivesRedirect bool
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
	return login.Login(ctx, func(instructions cli.DeviceLoginInstructions) error {
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
	return login.Login(ctx, manual, func(instructions cli.BrowserLoginInstructions) error {
		notify(BrowserInstructions{
			AuthorizationURL: instructions.AuthorizationURL,
			RedirectURI:      instructions.RedirectURI,
			ReceivesRedirect: instructions.ReceivesRedirect,
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

// RunLogout removes a provider's credential through an injected CLI service.
func RunLogout(ctx context.Context, logout cli.ProviderLogout, providerID string) error {
	if logout == nil {
		return fmt.Errorf("logout is unavailable")
	}
	return logout.Logout(ctx, providerID)
}

// RedirectReceiver listens for an authorization redirect until Stop.
type RedirectReceiver struct {
	Stop func()
}

// ReceiveRedirect listens at redirectURI for the browser's redirect after it
// opens authorizationURL, and passes the first redirect's URL to receive.
// Receive runs on its own goroutine, so it must dispatch to the UI.
func ReceiveRedirect(authorizationURL, redirectURI string, receive func(string)) (RedirectReceiver, error) {
	if receive == nil {
		return RedirectReceiver{}, fmt.Errorf("redirect handler is unavailable")
	}
	relayed := make(chan string, 1)
	stopListening, err := cli.ReceiveRedirect(authorizationURL, redirectURI, relayed)
	if err != nil {
		return RedirectReceiver{}, err
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case redirect := <-relayed:
			receive(redirect)
		case <-done:
		}
	}()
	return RedirectReceiver{Stop: func() {
		once.Do(func() {
			close(done)
			stopListening()
		})
	}}, nil
}
