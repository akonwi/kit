package kit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serveridentity"
	"github.com/akonwi/kit/internal/sshtunnel"
)

// ErrClientClosed indicates an operation attempted after Client.Close.
var ErrClientClosed = errors.New("kit client is closed")

// ErrIncompatibleServer indicates that the target server cannot serve this client.
var ErrIncompatibleServer = clienttransport.ErrIncompatibleServer

// CompatibilityError describes an authenticated server incompatibility.
type CompatibilityError = clienttransport.CompatibilityError

// ErrServerChanged indicates that a different Kit server now answers at an
// explicitly addressed endpoint than the one the client first connected to.
var ErrServerChanged = clienttransport.ErrServerChanged

// Credential is an opaque server access credential.
type Credential struct {
	token string
}

// AccessToken creates credentials for an explicitly addressed Kit server.
func AccessToken(token string) Credential {
	return Credential{token: token}
}

type targetKind uint8

const (
	targetInvalid targetKind = iota
	targetLocal
	targetHome
	targetEndpoint
	targetSSH
)

// Target identifies one Kit server without exposing transport request details.
type Target struct {
	kind       targetKind
	address    string
	credential Credential
	// prompts receives ssh's diagnostics while an SSH target connects
	// interactively; nil connects without prompting.
	prompts io.Writer
}

// Local discovers the registered daemon under the effective Kit home.
func Local() Target { return Target{kind: targetLocal} }

// Home discovers the daemon registered under another Kit home, such as an
// isolated development home. Like every target, it never starts, replaces,
// or stops that daemon, and it follows the daemon across restarts.
func Home(path string) Target { return Target{kind: targetHome, address: path} }

// Endpoint addresses one explicit Kit server. The client learns the running
// server instance, follows it across restarts, and fails with
// ErrServerChanged if a different server answers at the address.
func Endpoint(address string, credential Credential) Target {
	return Target{kind: targetEndpoint, address: address, credential: credential}
}

// SSH reaches the Kit server on another host through the system ssh
// executable, so the user's SSH configuration, agent, and jump hosts apply.
// Destination is [user@]host, an alias from the SSH configuration, or an
// ssh:// URL. Kit must be on the host's PATH for commands run over SSH.
//
// Unlike other targets, SSH starts the host's server when none is running.
// The client forwards a loopback port to the server for its lifetime and,
// after a disconnect or a server restart, forwards one again without
// prompting, which requires authentication that needs no prompt, such as an
// SSH agent.
func SSH(destination string) Target { return Target{kind: targetSSH, address: destination} }

// WithSSHPrompts lets an SSH target ask for passwords, passphrases, and host
// key confirmation on the terminal while it first connects, writing ssh's
// diagnostics to stderr. It has no effect on other targets.
func (t Target) WithSSHPrompts(stderr io.Writer) Target {
	if stderr == nil {
		stderr = io.Discard
	}
	t.prompts = stderr
	return t
}

// Option configures a Client. No options are currently defined.
type Option interface{ apply(*clientOptions) error }

type clientOptions struct{}

type compatibilityTransport interface {
	ProbeCompatible(context.Context) (clienttransport.Connection, error)
}

type operationTransport interface {
	CreateSession(context.Context, CreateSessionInput) (SessionInfo, error)
	ForkSession(context.Context, string, ForkSessionInput) (ForkSessionResult, error)
	RenameSession(context.Context, string, string) (SessionInfo, error)
	DeleteSession(context.Context, string) error
	DisposeTemporarySession(context.Context, string) error
	ListSessions(context.Context, string) ([]SessionInfo, error)
	GetSessionActivity(context.Context) ([]SessionActivityEntry, error)
	ListModels(context.Context) (ModelCatalog, error)
	RefreshModels(context.Context) (ModelCatalog, error)
}

// Client is a concurrent, stateful client for one Kit server.
type Client struct {
	mu             sync.RWMutex
	transport      operationTransport
	compatibility  compatibilityTransport
	sessions       sessionTransport
	devices        deviceTransport
	providers      providerTransport
	settings       settingsTransport
	lifetime       context.Context
	cancel         context.CancelFunc
	closed         bool
	closeTransport func()
	// localMachineID derives this client's machine identifier. Tests replace it.
	localMachineID func() (string, error)
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
	closeTunnel := func() {}
	switch target.kind {
	case targetLocal:
		paths, err := apphome.Resolve("")
		if err != nil {
			return nil, fmt.Errorf("resolve Kit home: %w", err)
		}
		transport = clienttransport.New(func() (clienttransport.Connection, error) {
			return localdiscovery.Resolve(paths)
		})
	case targetHome:
		if strings.TrimSpace(target.address) == "" {
			return nil, errors.New("Kit home path is required")
		}
		paths, err := apphome.Resolve(target.address)
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
		if strings.TrimSpace(target.credential.token) == "" {
			return nil, errors.New("endpoint token is required")
		}
		transport = clienttransport.NewEndpoint(address, target.credential.token)
	case targetSSH:
		tunnel, err := sshtunnel.Open(ctx, sshtunnel.Options{
			Destination: target.address, Start: true, Interactive: target.prompts != nil, Stderr: target.prompts,
		})
		if err != nil {
			return nil, projectError(err)
		}
		transport = clienttransport.NewInvalidating(tunnel.Resolve, tunnel.Invalidate)
		closeTunnel = func() { _ = tunnel.Close() }
	default:
		return nil, errors.New("Kit server target is invalid")
	}

	closeTransport := func() {
		transport.CloseIdleConnections()
		closeTunnel()
	}
	if _, err := transport.ProbeCompatible(ctx); err != nil {
		closeTransport()
		return nil, projectError(err)
	}
	client := newClient(transport, closeTransport)
	client.compatibility = transport
	client.sessions = transport
	client.devices = transport
	client.providers = transport
	client.settings = transport
	return client, nil
}

func newClient(transport operationTransport, closeTransport func()) *Client {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Client{transport: transport, lifetime: lifetime, cancel: cancel, closeTransport: closeTransport, localMachineID: localMachineID}
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

// SessionActivity reports the saved sessions that are working or waiting on
// their user, ordered by session ID. It is a point-in-time snapshot taken
// separately from ListSessions; a session it omits is idle.
func (c *Client) SessionActivity(ctx context.Context) ([]SessionActivityEntry, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return projectResult(c.transport.GetSessionActivity(operation))
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

// ForkSession creates a linked child session. A first prompt that could not
// start is reported in the result's FirstTurnError; the fork still succeeded.
func (c *Client) ForkSession(ctx context.Context, sourceSessionID string, input ForkSessionInput) (ForkSessionResult, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ForkSessionResult{}, err
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

// ServerInfo describes the server a Client is connected to.
type ServerInfo struct {
	// ID identifies the server's Kit home across server restarts.
	ID string
	// KitVersion is the server's Kit release label.
	KitVersion string
	// ProtocolVersion is the server's session protocol version.
	ProtocolVersion int
	// Home is the server user's home directory, for presenting server paths.
	Home string
	// SameMachine reports whether the server runs on this client's machine.
	// It is false when this client's machine identifier is unavailable.
	SameMachine bool
	// Providers are the model providers the server has credentials for.
	Providers []string
}

// Server verifies that the connected server remains ready and compatible and
// returns its identity.
func (c *Client) Server(ctx context.Context) (ServerInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	defer cancel()
	if c.compatibility == nil {
		return ServerInfo{}, errors.New("compatibility transport is unavailable")
	}
	connection, err := c.compatibility.ProbeCompatible(operation)
	if err != nil {
		return ServerInfo{}, projectError(err)
	}
	return ServerInfo{
		ID: connection.ServerID, KitVersion: connection.KitVersion, ProtocolVersion: connection.ProtocolVersion,
		Home: connection.Home, SameMachine: c.isLocalMachine(connection.MachineID), Providers: connection.Providers,
	}, nil
}

func (c *Client) isLocalMachine(serverMachineID string) bool {
	local, err := c.localMachineID()
	return err == nil && local != "" && local == serverMachineID
}

// localMachineID derives this client's machine identifier from the effective
// Kit home, matching how a server on this machine derives its own.
func localMachineID() (string, error) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return "", err
	}
	return serveridentity.MachineID(paths)
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
