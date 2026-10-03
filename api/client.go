package kit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/localdiscovery"
)

// ErrClientClosed indicates an operation attempted after Client.Close.
var ErrClientClosed = errors.New("kit client is closed")

// ErrIncompatibleServer indicates that the target server cannot serve this client.
var ErrIncompatibleServer = clienttransport.ErrIncompatibleServer

// CompatibilityError describes an authenticated server incompatibility.
type CompatibilityError = clienttransport.CompatibilityError

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

type compatibilityTransport interface {
	ProbeCompatible(context.Context) (clienttransport.Connection, error)
}

type operationTransport interface {
	CreateSession(context.Context, CreateSessionInput) (SessionInfo, error)
	ForkSession(context.Context, string, ForkSessionInput) (SessionInfo, error)
	RenameSession(context.Context, string, string) (SessionInfo, error)
	DeleteSession(context.Context, string) error
	DisposeTemporarySession(context.Context, string) error
	ListSessions(context.Context, string) ([]SessionInfo, error)
	ListModels(context.Context) (ModelCatalog, error)
	RefreshModels(context.Context) (ModelCatalog, error)
}

// Client is a concurrent, stateful client for one Kit server.
type Client struct {
	mu             sync.RWMutex
	transport      operationTransport
	compatibility  compatibilityTransport
	sessions       sessionTransport
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

	var transport *clienttransport.Client
	switch target.kind {
	case targetLocal:
		paths, err := apphome.Resolve("")
		if err != nil {
			return nil, fmt.Errorf("resolve Kit home: %w", err)
		}
		transport = clienttransport.New(func() (clienttransport.Connection, error) {
			return localdiscovery.Resolve(paths)
		})
	case targetEndpoint:
		address, err := validateEndpoint(target.address)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(target.credential.token) == "" || strings.TrimSpace(target.credential.instanceID) == "" {
			return nil, errors.New("endpoint token and instance id are required")
		}
		transport = clienttransport.NewEndpoint(address, target.credential.token, target.credential.instanceID)
	default:
		return nil, errors.New("Kit server target is invalid")
	}

	if _, err := transport.ProbeCompatible(ctx); err != nil {
		transport.CloseIdleConnections()
		return nil, projectError(err)
	}
	client := newClient(transport, transport.CloseIdleConnections)
	client.compatibility = transport
	client.sessions = transport
	return client, nil
}

func newClient(transport operationTransport, closeTransport func()) *Client {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Client{transport: transport, lifetime: lifetime, cancel: cancel, closeTransport: closeTransport}
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

func (c *Client) preflight(ctx context.Context) error {
	if ctx == nil {
		return errors.New("operation context is nil")
	}
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrClientClosed
	}
	return ctx.Err()
}

func (c *Client) operationContext(ctx context.Context) (context.Context, func(), error) {
	if err := c.preflight(ctx); err != nil {
		return nil, nil, err
	}
	c.mu.RLock()
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
	return projectResult(c.transport.ListSessions(operation, options.CWD))
}

// CreateSession creates one persisted or temporary session.
func (c *Client) CreateSession(ctx context.Context, input CreateSessionInput) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return projectResult(c.transport.CreateSession(operation, input))
}

// ForkSession creates a linked child session.
func (c *Client) ForkSession(ctx context.Context, sourceSessionID string, input ForkSessionInput) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return projectResult(c.transport.ForkSession(operation, sourceSessionID, input))
}

// RenameSession changes a persisted session's display name.
func (c *Client) RenameSession(ctx context.Context, sessionID, name string) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	return projectResult(c.transport.RenameSession(operation, sessionID, name))
}

// DeleteSession permanently deletes one persisted session.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return projectError(c.transport.DeleteSession(operation, sessionID))
}

// DisposeTemporarySession removes one temporary session.
func (c *Client) DisposeTemporarySession(ctx context.Context, sessionID string) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return projectError(c.transport.DisposeTemporarySession(operation, sessionID))
}

// ProbeCompatibility verifies that the connected server remains ready and
// compatible without managing its lifecycle.
func (c *Client) ProbeCompatibility(ctx context.Context) error {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.compatibility == nil {
		return errors.New("compatibility transport is unavailable")
	}
	_, err = c.compatibility.ProbeCompatible(operation)
	return projectError(err)
}

// Models returns the server's model catalog.
func (c *Client) Models(ctx context.Context) (ModelCatalog, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ModelCatalog{}, err
	}
	defer cancel()
	return projectResult(c.transport.ListModels(operation))
}

// RefreshModels refreshes and returns the server's model catalog.
func (c *Client) RefreshModels(ctx context.Context) (ModelCatalog, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ModelCatalog{}, err
	}
	defer cancel()
	return projectResult(c.transport.RefreshModels(operation))
}

// ResolveSession resolves an exact or uniquely prefixed saved session selector.
func (c *Client) ResolveSession(ctx context.Context, selector string) (SessionInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return SessionInfo{}, err
	}
	defer cancel()
	sessions, err := c.transport.ListSessions(operation, "")
	if err != nil {
		return SessionInfo{}, projectError(err)
	}
	return resolveSessionSelector(sessions, selector)
}

func resolveSessionSelector(sessions []SessionInfo, selector string) (SessionInfo, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return SessionInfo{}, fmt.Errorf("%w: selector is empty", ErrSessionNotFound)
	}
	for _, candidate := range sessions {
		if candidate.ID == selector {
			return candidate, nil
		}
	}
	canonicalPrefix := selector
	if !strings.HasPrefix(canonicalPrefix, "session_") {
		canonicalPrefix = "session_" + canonicalPrefix
	}
	var match SessionInfo
	matches := 0
	for _, candidate := range sessions {
		if strings.HasPrefix(candidate.ID, canonicalPrefix) {
			match = candidate
			matches++
		}
	}
	switch matches {
	case 0:
		return SessionInfo{}, fmt.Errorf("%w: %s", ErrSessionNotFound, selector)
	case 1:
		return match, nil
	default:
		return SessionInfo{}, fmt.Errorf("%w: %s matches %d sessions", ErrSessionAmbiguous, selector, matches)
	}
}

// Attach binds one stateful Session handle to an immutable session identity.
func (c *Client) Attach(ctx context.Context, sessionID string) (*Session, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if c.sessions == nil {
		return nil, errors.New("session transport is unavailable")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id is empty")
	}
	snapshot, err := c.sessions.GetSessionSnapshot(operation, sessionID)
	if err != nil {
		return nil, projectError(err)
	}
	return newSession(c, c.sessions, sessionID, snapshot), nil
}
