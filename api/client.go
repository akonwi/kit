package kit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/apphome"
	kitclient "github.com/akonwi/kit/internal/client"
	kitserver "github.com/akonwi/kit/internal/server"
	"github.com/akonwi/kit/internal/sessionclient"
)

// ErrClientClosed indicates an operation attempted after Client.Close.
var ErrClientClosed = errors.New("kit client is closed")

// ErrIncompatibleServer indicates that the target server cannot serve this client.
var ErrIncompatibleServer = kitserver.ErrIncompatibleDaemon

// CompatibilityError describes an authenticated server incompatibility.
type CompatibilityError = kitserver.DaemonCompatibilityError

// Credential is an opaque server access credential.
type Credential struct {
	token, instanceID string
}

// AccessToken creates credentials for an explicitly addressed Kit server.
func AccessToken(token, instanceID string) Credential {
	return Credential{token: token, instanceID: instanceID}
}

type targetKind uint8

const (
	targetInvalid targetKind = iota
	targetLocal
	targetEndpoint
)

// Target identifies one Kit server without exposing transport request details.
type Target struct {
	kind       targetKind
	address    string
	credential Credential
}

// Local discovers the registered daemon under the effective Kit home.
func Local() Target { return Target{kind: targetLocal} }

// Endpoint addresses one explicit Kit server.
func Endpoint(address string, credential Credential) Target {
	return Target{kind: targetEndpoint, address: address, credential: credential}
}

// Option configures a Client. No options are currently defined.
type Option interface{ apply(*clientOptions) error }

type clientOptions struct{}

type clientBackend interface {
	sessionclient.Server
}

// Client is a concurrent, stateful client for one Kit server.
type Client struct {
	mu             sync.RWMutex
	backend        clientBackend
	lifetime       context.Context
	cancel         context.CancelFunc
	closed         bool
	closeTransport func()
}

// Connect authenticates and verifies a compatible server without managing its lifecycle.
func Connect(ctx context.Context, target Target, options ...Option) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("connect context is nil")
	}
	configuration := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("client option is nil")
		}
		if err := option.apply(&configuration); err != nil {
			return nil, err
		}
	}

	var transport *kitserver.Client
	switch target.kind {
	case targetLocal:
		paths, err := apphome.Resolve("")
		if err != nil {
			return nil, fmt.Errorf("resolve Kit home: %w", err)
		}
		transport = kitserver.NewClient(paths)
	case targetEndpoint:
		address, err := validateEndpoint(target.address)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(target.credential.token) == "" || strings.TrimSpace(target.credential.instanceID) == "" {
			return nil, errors.New("endpoint token and instance id are required")
		}
		transport = kitserver.NewEndpointClient(address, target.credential.token, target.credential.instanceID)
	default:
		return nil, errors.New("Kit server target is invalid")
	}

	if _, err := transport.ProbeCompatible(ctx); err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return newClient(kitclient.NewServer(transport), transport.CloseIdleConnections), nil
}

func newClient(backend clientBackend, closeTransport func()) *Client {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Client{backend: backend, lifetime: lifetime, cancel: cancel, closeTransport: closeTransport}
}

func validateEndpoint(address string) (string, error) {
	address = strings.TrimSpace(address)
	parsed, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("parse Kit server endpoint: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Kit server endpoint must be an absolute HTTP(S) origin")
	}
	parsed.Path = ""
	return parsed.String(), nil
}

// Close detaches client-owned work and releases idle transport resources.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	closeTransport := c.closeTransport
	c.mu.Unlock()
	cancel()
	if closeTransport != nil {
		closeTransport()
	}
	return nil
}

func (c *Client) operationContext(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("operation context is nil")
	}
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, nil, ErrClientClosed
	}
	lifetime := c.lifetime
	c.mu.RUnlock()

	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	cleanup := func() {
		stop()
		cancel()
	}
	if err := operation.Err(); err != nil && lifetime.Err() != nil {
		cleanup()
		return nil, nil, ErrClientClosed
	}
	return operation, cleanup, nil
}

// ListSessionsOptions filters session discovery.
type ListSessionsOptions struct {
	CWD string
}

// ListSessions returns authoritative session metadata.
func (c *Client) ListSessions(ctx context.Context, options ListSessionsOptions) ([]SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return c.backend.ListSessions(operation, options.CWD)
}

// CreateSession creates one persisted or temporary session.
func (c *Client) CreateSession(ctx context.Context, input CreateSessionInput) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return c.backend.CreateSession(operation, input)
}

// ForkSession creates a linked child session.
func (c *Client) ForkSession(ctx context.Context, sourceSessionID string, input ForkSessionInput) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return c.backend.ForkSession(operation, sourceSessionID, input)
}

// RenameSession changes a persisted session's display name.
func (c *Client) RenameSession(ctx context.Context, sessionID, name string) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return c.backend.RenameSession(operation, sessionID, name)
}

// DeleteSession permanently deletes one persisted session.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return c.backend.DeleteSession(operation, sessionID)
}

// DisposeTemporarySession removes one temporary session.
func (c *Client) DisposeTemporarySession(ctx context.Context, sessionID string) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return c.backend.DisposeTemporarySession(operation, sessionID)
}

// Models returns the server's model catalog.
func (c *Client) Models(ctx context.Context) (ModelCatalog, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ModelCatalog{}, err
	}
	defer cancel()
	return c.backend.Models(operation)
}

// RefreshModels refreshes and returns the server's model catalog.
func (c *Client) RefreshModels(ctx context.Context) (ModelCatalog, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ModelCatalog{}, err
	}
	defer cancel()
	refresher, ok := c.backend.(sessionclient.ModelCatalogRefresher)
	if !ok {
		return ModelCatalog{}, errors.New("server client does not support model refresh")
	}
	return refresher.RefreshModels(operation)
}

// ResolveSession resolves an exact or uniquely prefixed saved session selector.
func (c *Client) ResolveSession(ctx context.Context, selector string) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return sessionclient.ResolveSession(operation, c.backend, selector)
}

// Attach binds one stateful Session handle to an immutable session identity.
func (c *Client) Attach(ctx context.Context, sessionID string) (*Session, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	bound, err := c.backend.Attach(operation, sessionID)
	if err != nil {
		return nil, err
	}
	return &Session{client: c, bound: bound, id: bound.ID()}, nil
}

// Session is a stateful handle bound to one immutable session identity.
type Session struct {
	client *Client
	bound  sessionclient.Session
	id     string
}

// ID returns the bound session identity.
func (s *Session) ID() string {
	if s == nil {
		return ""
	}
	return s.id
}

// Snapshot refreshes and returns the authoritative session snapshot.
func (s *Session) Snapshot(ctx context.Context) (SessionSnapshot, error) {
	if s == nil || s.client == nil || s.bound == nil {
		return SessionSnapshot{}, errors.New("session handle is nil")
	}
	operation, cancel, err := s.client.operationContext(ctx)
	if err != nil {
		return SessionSnapshot{}, err
	}
	defer cancel()
	return s.bound.Snapshot(operation)
}
