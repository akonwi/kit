// Package daemonctl discovers, starts, inspects, and stops the local Kit
// daemon on behalf of clients. It relies only on the publication contract in
// localdiscovery and the session transport, never on daemon internals.
package daemonctl

import (
	"context"
	"fmt"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
)

// Health is returned by an authenticated local daemon health check.
type Health = httpapi.Health

// Client probes and stops the published local daemon.
type Client struct {
	transport *clienttransport.Client
	paths     apphome.Paths
}

// NewClient creates an authenticated local daemon client.
func NewClient(paths apphome.Paths) *Client {
	transport := clienttransport.New(func() (clienttransport.Connection, error) {
		return localdiscovery.Resolve(paths)
	})
	return &Client{transport: transport, paths: paths}
}

// Probe verifies identity, authentication, and full readiness.
func (c *Client) Probe(ctx context.Context) (localdiscovery.Registry, Health, error) {
	return c.probe(ctx, true)
}

// ProbeLifecycle verifies enough state to manage a local daemon.
func (c *Client) ProbeLifecycle(ctx context.Context) (localdiscovery.Registry, Health, error) {
	return c.probe(ctx, false)
}

// probe reads the publication once and verifies the daemon behind exactly
// that registry, so the returned registry and health describe one instance.
func (c *Client) probe(ctx context.Context, requireDatabase bool) (localdiscovery.Registry, Health, error) {
	registry, connection, err := localdiscovery.Discover(c.paths)
	if err != nil {
		return localdiscovery.Registry{}, Health{}, err
	}
	_, health, err := c.transport.ProbeConnection(ctx, connection, requireDatabase)
	if err != nil {
		return localdiscovery.Registry{}, Health{}, err
	}
	return registry, health, nil
}

// ProbeCompatible authenticates a ready daemon and checks protocol compatibility.
// Readiness is checked first: an unready daemon is never reported as
// incompatible.
func (c *Client) ProbeCompatible(ctx context.Context) (localdiscovery.Registry, error) {
	registry, health, err := c.ProbeLifecycle(ctx)
	if err != nil {
		return localdiscovery.Registry{}, err
	}
	if !health.DatabaseReady {
		return localdiscovery.Registry{}, fmt.Errorf("%w: daemon %s (protocol %d)", clienttransport.ErrServerNotReady, registry.KitVersion, registry.ProtocolVersion)
	}
	return registry, clienttransport.CheckCompatibility(registry.ProtocolVersion, registry.KitVersion)
}

// Stop requests graceful shutdown from the published local daemon.
func (c *Client) Stop(ctx context.Context) error {
	return c.transport.RequestShutdown(ctx)
}
