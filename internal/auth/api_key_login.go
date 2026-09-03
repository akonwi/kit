package auth

import (
	"context"
	"errors"
	"fmt"
)

// APIKeyLogin installs a provider API key for dynamic provider resolution.
type APIKeyLogin struct {
	store       *Store
	acquireSave func(context.Context, string) (func() error, error)
}

// APIKeyLoginOptions configures API-key installation.
type APIKeyLoginOptions struct {
	Store *Store
	// AcquireSave excludes daemon replacement and rejects environment-owned
	// credentials while the persistent credential is replaced.
	AcquireSave func(context.Context, string) (func() error, error)
}

// NewAPIKeyLogin constructs an API-key login service.
func NewAPIKeyLogin(options APIKeyLoginOptions) (*APIKeyLogin, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("auth: API-key credential store is required")
	}
	return &APIKeyLogin{store: options.Store, acquireSave: options.AcquireSave}, nil
}

// Login saves one supported provider API key without echoing secret material.
func (l *APIKeyLogin) Login(ctx context.Context, providerID, apiKey string) error {
	if l == nil || l.store == nil {
		return fmt.Errorf("auth: API-key login is not configured")
	}
	if !supportedAPIKeyProvider(providerID) {
		return fmt.Errorf("auth: API-key provider is unsupported")
	}
	if err := validateAPIKey(apiKey); err != nil {
		return err
	}
	release := func() error { return nil }
	var err error
	if l.acquireSave != nil {
		release, err = l.acquireSave(ctx, providerID)
		if err != nil {
			return fmt.Errorf("authorize credential save: %w", err)
		}
		if release == nil {
			return fmt.Errorf("auth: API-key login save guard returned no release function")
		}
	}
	saveErr := l.store.ReplaceAPIKey(ctx, providerID, apiKey)
	if saveErr != nil {
		saveErr = fmt.Errorf("save credentials: %w", saveErr)
	}
	return errors.Join(saveErr, release())
}
