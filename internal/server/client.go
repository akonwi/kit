package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

const (
	instanceHeader = "X-Kit-Instance-ID"
	protocolHeader = "X-Kit-Protocol-Version"
)

// Health is returned by an authenticated local daemon health check.
type Health struct {
	InstanceID      string   `json:"instanceId"`
	PID             int      `json:"pid"`
	KitVersion      string   `json:"kitVersion"`
	ProtocolVersion int      `json:"protocolVersion"`
	DatabaseReady   bool     `json:"databaseReady"`
	Providers       []string `json:"providers"`
}

// Client performs authenticated local daemon lifecycle requests.
type Client struct {
	paths       apphome.Paths
	http        *http.Client
	sessionHTTP *http.Client
}

// NewClient creates a local daemon client that never uses an HTTP proxy.
func NewClient(paths apphome.Paths) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second}).DialContext
	return &Client{
		paths: paths,
		http: &http.Client{
			Transport: transport,
			Timeout:   3 * time.Second,
		},
		// Model calls can legitimately run for minutes. Their caller-provided
		// context owns the deadline; dial timeout remains bounded by transport.
		sessionHTTP: &http.Client{Transport: transport},
	}
}

// Probe verifies the registered daemon's identity, authentication, and full readiness.
func (c *Client) Probe(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, true)
}

// ProbeLifecycle verifies enough stable state to manage a daemon even when its
// database or newer session protocol is unhealthy.
func (c *Client) ProbeLifecycle(ctx context.Context) (Registry, Health, error) {
	return c.probe(ctx, false)
}

func (c *Client) probe(ctx context.Context, requireDatabase bool) (Registry, Health, error) {
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return Registry{}, Health{}, err
	}
	token, err := loadToken(c.paths)
	if err != nil {
		return Registry{}, Health{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, registry.URL+"/v1/health", nil)
	if err != nil {
		return Registry{}, Health{}, fmt.Errorf("create health request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(instanceHeader, registry.InstanceID)

	response, err := c.http.Do(request)
	if err != nil {
		return Registry{}, Health{}, fmt.Errorf("contact daemon: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return Registry{}, Health{}, fmt.Errorf(
			"daemon health returned %s: %s",
			response.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var health Health
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&health); err != nil {
		return Registry{}, Health{}, fmt.Errorf("decode daemon health: %w", err)
	}
	if health.InstanceID != registry.InstanceID {
		return Registry{}, Health{}, errors.New("daemon instance does not match registry")
	}
	if health.PID != registry.PID {
		return Registry{}, Health{}, errors.New("daemon pid does not match registry")
	}
	if health.ProtocolVersion != registry.ProtocolVersion {
		return Registry{}, Health{}, errors.New("daemon protocol does not match registry")
	}
	if requireDatabase && !health.DatabaseReady {
		return Registry{}, Health{}, errors.New("daemon database is not ready")
	}
	seenProviders := map[string]bool{}
	for _, providerID := range health.Providers {
		if providerID == "" || providerID != strings.TrimSpace(providerID) || len(providerID) > 128 || strings.ContainsAny(providerID, "/\\\r\n\t") {
			return Registry{}, Health{}, fmt.Errorf("daemon returned invalid provider id %q", providerID)
		}
		if seenProviders[providerID] {
			return Registry{}, Health{}, fmt.Errorf("daemon returned duplicate provider id %q", providerID)
		}
		seenProviders[providerID] = true
	}
	return registry, health, nil
}

// Stop requests graceful shutdown from the registered daemon.
func (c *Client) Stop(ctx context.Context) error {
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return err
	}
	token, err := loadToken(c.paths)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, registry.URL+"/v1/shutdown", nil)
	if err != nil {
		return fmt.Errorf("create shutdown request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(instanceHeader, registry.InstanceID)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("request daemon shutdown: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("daemon shutdown returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
