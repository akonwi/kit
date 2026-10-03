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
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/httpapi"
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
}

// Resolver returns the current authenticated server connection.
type Resolver func() (Connection, error)

// Health is returned by an authenticated server health check.
type Health = httpapi.Health

// Client performs authenticated session API requests.
type Client struct {
	resolve     Resolver
	http        *http.Client
	sessionHTTP *http.Client
	lifetime    context.Context
	lifetimeErr error
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

// NewEndpoint creates a transport for one fixed explicit endpoint.
func NewEndpoint(endpoint, token, instanceID string) *Client {
	connection := Connection{
		URL: endpoint, Token: token, InstanceID: instanceID,
		ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version,
	}
	return New(func() (Connection, error) { return connection, nil })
}

func (c *Client) connection() (Connection, error) {
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
	connection, err := c.connection()
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

// Probe verifies server identity, authentication, compatibility, and readiness.
func (c *Client) Probe(ctx context.Context, requireDatabase bool) (Connection, Health, error) {
	connection, err := c.connection()
	if err != nil {
		return Connection{}, Health{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.URL+"/v1/health", nil)
	if err != nil {
		return Connection{}, Health{}, fmt.Errorf("create health request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(httpapi.InstanceHeader, connection.InstanceID)
	response, err := c.http.Do(request)
	if err != nil {
		return Connection{}, Health{}, transportError("contact server", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return Connection{}, Health{}, fmt.Errorf("server health returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var health Health
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&health); err != nil {
		return Connection{}, Health{}, protocolErrorf("decode server health: %w", err)
	}
	if health.InstanceID != connection.InstanceID {
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
	connection, _, err := c.Probe(ctx, true)
	if err != nil {
		return Connection{}, err
	}
	if err := CheckCompatibility(connection); err != nil {
		return Connection{}, err
	}
	return connection, nil
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
	ReleaseMismatch     CompatibilityReason = "release_mismatch"
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
	action := "Use a matching client; restart the daemon with the intended binary only when interrupting active work is acceptable."
	if e.Reason == ClientProtocolOlder {
		action = "Update this client; do not replace the newer daemon with an older binary."
	}
	return fmt.Sprintf("%s (%s): client %s (protocol %d), daemon %s (protocol %d); the daemon was left running. %s",
		ErrIncompatibleServer, e.Reason, e.ClientVersion, e.ClientProtocol, e.ServerVersion, e.ServerProtocol, action)
}
func (e *CompatibilityError) Unwrap() error            { return ErrIncompatibleServer }
func (e *CompatibilityError) IncompatibleDaemon() bool { return true }

// CheckCompatibility compares a verified connection with this client.
func CheckCompatibility(connection Connection) error {
	return CheckCompatibilityWithVersion(connection, version.Version)
}

// CheckCompatibilityWithVersion compares a connection with a supplied client version.
func CheckCompatibilityWithVersion(connection Connection, clientVersion string) error {
	failure := CompatibilityError{
		ClientVersion: clientVersion, ServerVersion: connection.KitVersion,
		ClientProtocol: version.SessionProtocolVersion, ServerProtocol: connection.ProtocolVersion,
	}
	switch {
	case connection.ProtocolVersion < version.SessionProtocolVersion:
		failure.Reason = ServerProtocolOlder
	case connection.ProtocolVersion > version.SessionProtocolVersion:
		failure.Reason = ClientProtocolOlder
	case connection.KitVersion == clientVersion:
		return nil
	case coveredReleasePair(version.SessionProtocolVersion, clientVersion, connection.KitVersion):
		return nil
	default:
		failure.Reason = ReleaseMismatch
	}
	return &failure
}

func compatible(connection Connection) error { return CheckCompatibility(connection) }

func coveredReleasePair(protocol int, clientVersion, serverVersion string) bool {
	return protocol == 42 && protocol42Release(clientVersion) && protocol42Release(serverVersion)
}

func protocol42Release(release string) bool {
	if len(release) == 0 || len(release) > 64 {
		return false
	}
	var major, minor, patch uint64
	if _, err := fmt.Sscanf(release, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	canonical := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	return canonical == release && (major > 0 || minor >= 39)
}
