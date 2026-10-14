// Package clienttransport owns Kit's authenticated HTTP session transport.
package clienttransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/serveridentity"
	"github.com/akonwi/kit/internal/version"
)

const (
	instanceHeader = httpapi.InstanceHeader
	protocolHeader = httpapi.ProtocolHeader
)

// ProtocolError reports a response that violates the declared wire contract.
type ProtocolError = httpapi.ProtocolError

func protocolErrorf(format string, args ...any) error {
	return &ProtocolError{Err: fmt.Errorf(format, args...)}
}

// TransportError reports a failed request exchange with a Kit server.
type TransportError struct {
	Operation string
	Err       error
}

func (e *TransportError) Error() string {
	if e == nil {
		return "Kit server transport failed"
	}
	return fmt.Sprintf("%s: %v", e.Operation, e.Err)
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func transportError(operation string, err error) error {
	return &TransportError{Operation: operation, Err: err}
}

// Connection contains one resolved server identity and its private credential.
type Connection struct {
	URL, Token, InstanceID  string
	PID                     int
	ProtocolVersion         int
	KitVersion              string
	VerifyPublishedIdentity bool
	// ServerID, MachineID, and Home are learned from a compatible server's
	// health; they are empty until ProbeCompatible succeeds.
	ServerID, MachineID, Home string
}

// Resolver returns the current authenticated server connection.
type Resolver func() (Connection, error)

// Health is returned by an authenticated server health check.
type Health = httpapi.Health

// Client performs authenticated session API requests.
type Client struct {
	resolve     Resolver
	endpoint    *endpoint
	http        *http.Client
	sessionHTTP *http.Client
	lifetime    context.Context
	lifetimeErr error
}

// ErrServerChanged indicates that a different server now answers at an
// explicitly addressed endpoint.
var ErrServerChanged = errors.New("a different Kit server answers at this endpoint")

// errInstanceChanged marks a request rejected because it named a server
// instance that is no longer running.
var errInstanceChanged = errors.New("server instance changed")

// endpoint learns the running instance of an explicitly addressed server from
// health, learns it again after the server restarts, and pins the server
// identity from the first handshake so a different server is never adopted.
type endpoint struct {
	url, token string

	mu       sync.Mutex
	current  Connection
	serverID string
}

// connection returns the learned connection, performing a handshake when no
// instance is known.
func (e *endpoint) connection(ctx context.Context, client *Client) (Connection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current.InstanceID != "" {
		return e.current, nil
	}
	learned, health, err := client.ProbeConnection(ctx, Connection{URL: e.url, Token: e.token}, false)
	if err != nil {
		return Connection{}, err
	}
	if err := CheckCompatibility(learned.ProtocolVersion, learned.KitVersion); err != nil {
		return Connection{}, err
	}
	if err := validateIdentity(health); err != nil {
		return Connection{}, err
	}
	if e.serverID != "" && health.ServerID != e.serverID {
		return Connection{}, fmt.Errorf("%w: expected %s, found %s", ErrServerChanged, e.serverID, health.ServerID)
	}
	e.serverID = health.ServerID
	learned.ServerID, learned.MachineID, learned.Home = health.ServerID, health.MachineID, health.Home
	e.current = learned
	return learned, nil
}

// forget discards a learned instance that the server rejected, so the next
// request handshakes again.
func (e *endpoint) forget(instanceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current.InstanceID == instanceID {
		e.current = Connection{}
	}
}

// instanceRejected records that connection's instance is no longer running.
func (c *Client) instanceRejected(connection Connection) {
	if c.endpoint != nil {
		c.endpoint.forget(connection.InstanceID)
	}
}

// rejectedInstance reports whether an error response rejected the request's
// server instance. Such requests were refused before routing, so they had no
// effect and may be retried.
func rejectedInstance(statusCode int, body []byte) bool {
	if statusCode != http.StatusConflict {
		return false
	}
	var apiError *APIError
	return errors.As(DecodeAPIError(statusCode, body), &apiError) && apiError.Code == string(httpapi.ErrorInstanceMismatch)
}

// New creates a transport using resolve for every request.
func New(resolve Resolver) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	noRedirect := func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		resolve: resolve,
		http:    &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: noRedirect},
		// Model calls can legitimately run for minutes. Their caller-provided
		// context owns the deadline; dial timeout remains bounded by transport.
		sessionHTTP: &http.Client{Transport: transport, CheckRedirect: noRedirect},
	}
}

// WithLifetime returns a shallow transport view whose requests are canceled
// when lifetime ends. HTTP connection pools remain shared with the source.
func (c *Client) WithLifetime(lifetime context.Context, lifetimeErr error) *Client {
	clone := *c
	clone.lifetime = lifetime
	clone.lifetimeErr = lifetimeErr
	return &clone
}

type lifetimeReadCloser struct {
	io.ReadCloser
	operation string
	cleanup   func()
	once      sync.Once
}

func (r *lifetimeReadCloser) Read(buffer []byte) (int, error) {
	count, err := r.ReadCloser.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return count, transportError(r.operation, err)
	}
	return count, err
}

func (r *lifetimeReadCloser) Close() error {
	r.once.Do(r.cleanup)
	return r.ReadCloser.Close()
}

func (c *Client) operationContext(ctx context.Context) (context.Context, func()) {
	if c.lifetime == nil {
		return ctx, func() {}
	}
	operation, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(c.lifetime, func() { cancel(c.lifetimeErr) })
	if c.lifetime.Err() != nil {
		cancel(c.lifetimeErr)
	}
	return operation, func() {
		stop()
		cancel(context.Canceled)
	}
}

// NewEndpoint creates a transport for an explicitly addressed server. It
// learns the running instance from health and learns it again after the
// server restarts. It never manages the server's lifecycle.
func NewEndpoint(url, token string) *Client {
	client := New(nil)
	client.endpoint = &endpoint{url: url, token: token}
	return client
}

func (c *Client) connection(ctx context.Context) (Connection, error) {
	if c != nil && c.endpoint != nil {
		return c.endpoint.connection(ctx, c)
	}
	if c == nil || c.resolve == nil {
		return Connection{}, errors.New("server connection resolver is unavailable")
	}
	connection, err := c.resolve()
	if err != nil {
		return Connection{}, err
	}
	if connection.URL == "" || connection.Token == "" || connection.InstanceID == "" {
		return Connection{}, errors.New("server connection is incomplete")
	}
	return connection, nil
}

// RequestShutdown asks the connected server to stop.
func (c *Client) RequestShutdown(ctx context.Context) error {
	connection, err := c.connection(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.URL+"/v1/shutdown", nil)
	if err != nil {
		return fmt.Errorf("create shutdown request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(httpapi.InstanceHeader, connection.InstanceID)
	response, err := c.http.Do(request)
	if err != nil {
		return transportError("request server shutdown", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("server shutdown returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// CloseIdleConnections closes idle HTTP connections owned by the transport.
func (c *Client) CloseIdleConnections() {
	if c == nil {
		return
	}
	c.http.CloseIdleConnections()
	c.sessionHTTP.CloseIdleConnections()
}

// Probe verifies the resolved server's identity, authentication, and readiness.
func (c *Client) Probe(ctx context.Context, requireDatabase bool) (Connection, Health, error) {
	for attempt := 0; ; attempt++ {
		connection, err := c.connection(ctx)
		if err != nil {
			return Connection{}, Health{}, err
		}
		probed, health, err := c.ProbeConnection(ctx, connection, requireDatabase)
		if errors.Is(err, errInstanceChanged) && attempt == 0 {
			c.instanceRejected(connection)
			continue
		}
		return probed, health, err
	}
}

// ProbeConnection verifies identity, authentication, and readiness for a
// connection the caller already resolved, so the result describes exactly that
// publication rather than one resolved again. A connection without an instance
// learns the running instance from the response.
func (c *Client) ProbeConnection(ctx context.Context, connection Connection, requireDatabase bool) (Connection, Health, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.URL+"/v1/health", nil)
	if err != nil {
		return Connection{}, Health{}, fmt.Errorf("create health request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	if connection.InstanceID != "" {
		request.Header.Set(httpapi.InstanceHeader, connection.InstanceID)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Connection{}, Health{}, transportError("contact server", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		if rejectedInstance(response.StatusCode, body) {
			return Connection{}, Health{}, errInstanceChanged
		}
		// Declared failures, such as an expired device token, keep their
		// stable codes; anything else is reported with the raw status.
		var declared *APIError
		if err := httpapi.DecodeOperationError(httpapi.GetHealth, response.StatusCode, body); errors.As(err, &declared) {
			return Connection{}, Health{}, err
		}
		return Connection{}, Health{}, fmt.Errorf("server health returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var health Health
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&health); err != nil {
		return Connection{}, Health{}, protocolErrorf("decode server health: %w", err)
	}
	if connection.InstanceID == "" {
		if health.InstanceID == "" || len(health.InstanceID) > 128 {
			return Connection{}, Health{}, protocolErrorf("server returned invalid instance id %q", health.InstanceID)
		}
		connection.InstanceID = health.InstanceID
	} else if health.InstanceID != connection.InstanceID {
		return Connection{}, Health{}, errors.New("server instance does not match connection")
	}
	if connection.VerifyPublishedIdentity {
		if health.PID != connection.PID {
			return Connection{}, Health{}, errors.New("server pid does not match registry")
		}
		if health.ProtocolVersion != connection.ProtocolVersion {
			return Connection{}, Health{}, errors.New("server protocol does not match registry")
		}
		if health.KitVersion != connection.KitVersion {
			return Connection{}, Health{}, errors.New("server Kit version does not match registry")
		}
	} else {
		connection.PID = health.PID
		connection.ProtocolVersion = health.ProtocolVersion
		connection.KitVersion = health.KitVersion
	}
	if requireDatabase && !health.DatabaseReady {
		return Connection{}, Health{}, ErrServerNotReady
	}
	seenProviders := map[string]bool{}
	for _, providerID := range health.Providers {
		if providerID == "" || providerID != strings.TrimSpace(providerID) || len(providerID) > 128 || strings.ContainsAny(providerID, "/\\\r\n\t") {
			return Connection{}, Health{}, protocolErrorf("server returned invalid provider id %q", providerID)
		}
		if seenProviders[providerID] {
			return Connection{}, Health{}, protocolErrorf("server returned duplicate provider id %q", providerID)
		}
		seenProviders[providerID] = true
	}
	return connection, health, nil
}

// ProbeCompatible verifies a ready, compatible server.
func (c *Client) ProbeCompatible(ctx context.Context) (Connection, error) {
	connection, health, err := c.Probe(ctx, true)
	if err != nil {
		return Connection{}, err
	}
	if err := CheckCompatibility(connection.ProtocolVersion, connection.KitVersion); err != nil {
		return Connection{}, err
	}
	// Identity is validated only after compatibility, so a server from another
	// protocol is reported as incompatible rather than malformed.
	if err := validateIdentity(health); err != nil {
		return Connection{}, err
	}
	connection.ServerID = health.ServerID
	connection.MachineID = health.MachineID
	connection.Home = health.Home
	return connection, nil
}

func validateIdentity(health Health) error {
	if !serveridentity.ValidServerID(health.ServerID) {
		return protocolErrorf("server returned invalid server id %q", health.ServerID)
	}
	if !serveridentity.ValidMachineID(health.MachineID) {
		return protocolErrorf("server returned invalid machine id %q", health.MachineID)
	}
	if !validHome(health.Home) {
		return protocolErrorf("server returned invalid home directory %q", health.Home)
	}
	return nil
}

func validHome(home string) bool {
	if home == "" || len(home) > 4096 || !path.IsAbs(home) || path.Clean(home) != home {
		return false
	}
	return !strings.ContainsFunc(home, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// ErrIncompatibleServer indicates an authenticated incompatible server.
var ErrIncompatibleServer = errors.New("local daemon is incompatible")

// ErrServerNotReady indicates an authenticated server whose database is not ready.
var ErrServerNotReady = errors.New("local daemon database is not ready")

// CompatibilityReason identifies why a verified server cannot serve this client.
type CompatibilityReason string

const (
	ServerProtocolOlder CompatibilityReason = "server_protocol_older"
	ClientProtocolOlder CompatibilityReason = "client_protocol_older"
)

// CompatibilityError preserves the versions needed to explain a mismatch.
type CompatibilityError struct {
	Reason         CompatibilityReason
	ClientVersion  string
	ServerVersion  string
	ClientProtocol int
	ServerProtocol int
}

func (e *CompatibilityError) Error() string {
	if e == nil {
		return ErrIncompatibleServer.Error()
	}
	action := "Restart the daemon with a newer binary only when interrupting active work is acceptable."
	if e.Reason == ClientProtocolOlder {
		action = "Update this client; do not replace the newer daemon with an older binary."
	}
	return fmt.Sprintf("%s (%s): client %s (protocol %d), daemon %s (protocol %d); the daemon was left running. %s",
		ErrIncompatibleServer, e.Reason, e.ClientVersion, e.ClientProtocol, e.ServerVersion, e.ServerProtocol, action)
}
func (e *CompatibilityError) Unwrap() error            { return ErrIncompatibleServer }
func (e *CompatibilityError) IncompatibleDaemon() bool { return true }

// CheckCompatibility compares a verified server's identity with this client.
// Callers must authenticate the server and verify its published identity first.
// A client and server are compatible exactly when they speak the same session
// protocol; the server's release label is reported for diagnosis only.
func CheckCompatibility(serverProtocol int, serverVersion string) error {
	if serverProtocol == version.SessionProtocolVersion {
		return nil
	}
	failure := CompatibilityError{
		Reason:        ServerProtocolOlder,
		ClientVersion: version.Version, ServerVersion: serverVersion,
		ClientProtocol: version.SessionProtocolVersion, ServerProtocol: serverProtocol,
	}
	if serverProtocol > version.SessionProtocolVersion {
		failure.Reason = ClientProtocolOlder
	}
	return &failure
}
