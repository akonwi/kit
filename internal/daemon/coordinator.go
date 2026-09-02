package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/gofrs/flock"
)

// ProbeFunc returns a healthy local daemon registry.
type ProbeFunc func(context.Context) (Registry, error)

// LaunchFunc starts a daemon process without waiting for its lifetime.
type LaunchFunc func(context.Context) error

// Coordinator serializes discovery and startup across concurrent Kit clients.
type Coordinator struct {
	Paths        apphome.Paths
	Probe        ProbeFunc
	Launch       LaunchFunc
	PollInterval time.Duration
	StartTimeout time.Duration
}

// Ensure returns a healthy daemon, launching exactly one when none is reachable.
func (c Coordinator) Ensure(ctx context.Context) (Registry, error) {
	if c.Probe == nil {
		return Registry{}, errors.New("daemon coordinator probe is nil")
	}
	if c.Launch == nil {
		return Registry{}, errors.New("daemon coordinator launcher is nil")
	}
	if registry, err := c.Probe(ctx); err == nil {
		return registry, nil
	} else if errors.Is(err, ErrIncompatibleDaemon) {
		return Registry{}, err
	}
	if err := c.Paths.Ensure(); err != nil {
		return Registry{}, err
	}

	pollInterval := c.PollInterval
	if pollInterval <= 0 {
		pollInterval = 50 * time.Millisecond
	}
	startTimeout := c.StartTimeout
	if startTimeout <= 0 {
		startTimeout = 10 * time.Second
	}

	startupLock := flock.New(c.Paths.StartupLock)
	locked, err := startupLock.TryLockContext(ctx, pollInterval)
	if err != nil {
		return Registry{}, fmt.Errorf("wait for daemon startup lock: %w", err)
	}
	if !locked {
		return Registry{}, errors.New("daemon startup lock was not acquired")
	}
	defer startupLock.Unlock()

	// Another client may have completed startup while this caller waited.
	if registry, err := c.Probe(ctx); err == nil {
		return registry, nil
	} else if errors.Is(err, ErrIncompatibleDaemon) {
		return Registry{}, err
	}
	// A previous daemon may have removed its registry while still flushing the
	// database and holding the lifetime lock. Wait for complete teardown before
	// launching, otherwise the one child attempt can lose the lock and exit.
	lifetimeLock := flock.New(c.Paths.ServerLock)
	available, err := lifetimeLock.TryLockContext(ctx, pollInterval)
	if err != nil {
		return Registry{}, fmt.Errorf("wait for previous daemon resources: %w", err)
	}
	if !available {
		return Registry{}, errors.New("daemon lifetime lock was not acquired")
	}
	if err := lifetimeLock.Unlock(); err != nil {
		return Registry{}, fmt.Errorf("release daemon lifetime probe: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Registry{}, fmt.Errorf("launch local daemon: %w", err)
	}
	if err := c.Launch(ctx); err != nil {
		return Registry{}, fmt.Errorf("launch local daemon: %w", err)
	}

	waitContext, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		registry, err := c.Probe(waitContext)
		if err == nil {
			return registry, nil
		}
		if errors.Is(err, ErrIncompatibleDaemon) {
			return Registry{}, err
		}
		lastErr = err
		select {
		case <-waitContext.Done():
			return Registry{}, fmt.Errorf("wait for local daemon readiness: %w (last probe: %v)", waitContext.Err(), lastErr)
		case <-ticker.C:
		}
	}
}
