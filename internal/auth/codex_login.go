package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/akonwi/kit/internal/droids"
	codexauth "github.com/akonwi/kit/internal/droids/openaicodex"
)

// OpenAICodexDeviceInstructions are safe, non-secret values an application can
// render while a device login is pending.
type OpenAICodexDeviceInstructions struct {
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
}

// OpenAICodexDeviceLogin performs one application-facing Codex device flow.
type OpenAICodexDeviceLogin struct {
	oauth       openAICodexDeviceOAuth
	installer   openAICodexCredentialInstaller
	acquireSave func(context.Context) (func() error, error)
}

// OpenAICodexDeviceLoginOptions configures a device login service.
type OpenAICodexDeviceLoginOptions struct {
	Store      *Store
	HTTPClient *http.Client
	// AcquireSave is called immediately before a completed login replaces the
	// stored generation. Its release function runs after the atomic write. Kit
	// uses this to exclude daemon restarts and reject a conflicting auth source.
	AcquireSave func(context.Context) (func() error, error)
}

type openAICodexDeviceOAuth interface {
	BeginDeviceAuthorization(context.Context) (*codexauth.DeviceAuthorization, error)
	PollDeviceAuthorization(context.Context, *codexauth.DeviceAuthorization) (codexauth.Credentials, error)
}

type openAICodexCredentialInstaller interface {
	ReplaceOpenAICodexCredentials(context.Context, droids.OpenAICodexCredentials) error
}

// NewOpenAICodexDeviceLogin constructs a login service using OpenAI's fixed
// OAuth origin.
func NewOpenAICodexDeviceLogin(options OpenAICodexDeviceLoginOptions) (*OpenAICodexDeviceLogin, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("auth: OpenAI Codex credential store is required")
	}
	oauthClient, err := codexauth.NewClient(options.HTTPClient, "kit")
	if err != nil {
		return nil, err
	}
	return newOpenAICodexDeviceLogin(oauthClient, options.Store, options.AcquireSave), nil
}

func newOpenAICodexDeviceLogin(
	oauthClient openAICodexDeviceOAuth,
	installer openAICodexCredentialInstaller,
	acquireSave func(context.Context) (func() error, error),
) *OpenAICodexDeviceLogin {
	return &OpenAICodexDeviceLogin{
		oauth: oauthClient, installer: installer, acquireSave: acquireSave,
	}
}

// Login runs a device flow, reports its safe display instructions, then saves
// the resulting credential as a new generation.
func (l *OpenAICodexDeviceLogin) Login(
	ctx context.Context,
	notify func(OpenAICodexDeviceInstructions) error,
) error {
	if l == nil || l.oauth == nil || l.installer == nil {
		return fmt.Errorf("auth: OpenAI Codex login is not configured")
	}
	if notify == nil {
		return fmt.Errorf("auth: OpenAI Codex login notifier is required")
	}
	device, err := l.oauth.BeginDeviceAuthorization(ctx)
	if err != nil {
		return fmt.Errorf("start device authorization: %w", err)
	}
	if device == nil || device.VerificationURI == "" || device.UserCode == "" || device.ExpiresAt.IsZero() {
		return fmt.Errorf("auth: OpenAI Codex returned incomplete device instructions")
	}
	if err := notify(OpenAICodexDeviceInstructions{
		VerificationURI: device.VerificationURI,
		UserCode:        device.UserCode,
		ExpiresAt:       device.ExpiresAt,
	}); err != nil {
		return err
	}
	credentials, err := l.oauth.PollDeviceAuthorization(ctx, device)
	if err != nil {
		return fmt.Errorf("complete device authorization: %w", err)
	}
	release := func() error { return nil }
	if l.acquireSave != nil {
		release, err = l.acquireSave(ctx)
		if err != nil {
			return fmt.Errorf("authorize credential save: %w", err)
		}
		if release == nil {
			return fmt.Errorf("auth: OpenAI Codex login save guard returned no release function")
		}
	}
	saveErr := l.installer.ReplaceOpenAICodexCredentials(ctx, credentials)
	if saveErr != nil {
		saveErr = fmt.Errorf("save credentials: %w", saveErr)
	}
	releaseErr := release()
	return errors.Join(saveErr, releaseErr)
}
