package auth

import (
	"context"
	"fmt"
)

// APIKeyLogin installs a provider API key for dynamic provider resolution.
type APIKeyLogin struct {
	store *Store
}

// APIKeyLoginOptions configures API-key installation.
type APIKeyLoginOptions struct {
	Store *Store
}

// NewAPIKeyLogin constructs an API-key login service.
func NewAPIKeyLogin(options APIKeyLoginOptions) (*APIKeyLogin, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("auth: API-key credential store is required")
	}
	return &APIKeyLogin{store: options.Store}, nil
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
	if err := l.store.ReplaceAPIKey(ctx, providerID, apiKey); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}
