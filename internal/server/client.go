package server

import (
	"context"
	"fmt"

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

// Client owns local daemon lifecycle operations. Session transport is private
// to the public API and low-level adapter tests.
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
func (c *Client) Probe(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, true)
}

// ProbeLifecycle verifies enough state to manage a local daemon.
func (c *Client) ProbeLifecycle(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, false)
}

func (c *Client) probe(ctx context.Context, requireDatabase bool) (Registry, Health, error) {
	_, health, err := c.transport.Probe(ctx, requireDatabase)
	if err != nil {
		return Registry{}, Health{}, err
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
	return c.transport.RequestShutdown(ctx)
}
