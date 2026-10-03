package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
)

const (
	instanceHeader = httpapi.InstanceHeader
	protocolHeader = httpapi.ProtocolHeader
)

// Health is returned by an authenticated local daemon health check.
type Health = httpapi.Health

// Client retains daemon lifecycle access while delegating session transport to
// the transport-owned client. Session methods are promoted from the embedded
// transport during the public-client migration.
type Client struct {
	*clienttransport.Client
	paths    apphome.Paths
	endpoint bool
}

// NewClient creates an authenticated local daemon client.
func NewClient(paths apphome.Paths) *Client {
	transport := clienttransport.New(func() (clienttransport.Connection, error) {
		return localdiscovery.Resolve(paths)
	})
	return &Client{Client: transport, paths: paths}
}

// NewEndpointClient creates a client for one explicitly configured server.
func NewEndpointClient(endpoint, token, instanceID string) *Client {
	return &Client{Client: clienttransport.NewEndpoint(strings.TrimRight(endpoint, "/"), token, instanceID), endpoint: true}
}

// Probe verifies identity, authentication, and full readiness.
func (c *Client) Probe(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, true)
}

// ProbeLifecycle verifies enough state to manage a local daemon.
func (c *Client) ProbeLifecycle(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, false)
}

func (c *Client) probe(ctx context.Context, requireDatabase bool) (Registry, Health, error) {
	connection, health, err := c.Client.Probe(ctx, requireDatabase)
	if err != nil {
		return Registry{}, Health{}, err
	}
	if c.endpoint {
		return Registry{
			ProtocolVersion: connection.ProtocolVersion, KitVersion: connection.KitVersion,
			PID: connection.PID, InstanceID: connection.InstanceID, URL: connection.URL,
		}, health, nil
	}
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return Registry{}, Health{}, err
	}
	return registry, health, nil
}

// ProbeCompatible authenticates a ready server and checks release compatibility.
func (c *Client) ProbeCompatible(ctx context.Context) (Registry, error) {
	registry, health, err := c.ProbeLifecycle(ctx)
	if err != nil {
		return Registry{}, err
	}
	if !health.DatabaseReady {
		return Registry{}, fmt.Errorf("%w: daemon %s (protocol %d)", ErrDaemonNotReady, registry.KitVersion, registry.ProtocolVersion)
	}
	return registry, CheckCompatibility(registry)
}

// Stop requests graceful shutdown from the registered local daemon.
func (c *Client) Stop(ctx context.Context) error {
	if c.endpoint {
		return fmt.Errorf("explicit endpoint lifecycle is not managed by this client")
	}
	return c.Client.RequestShutdown(ctx)
}
