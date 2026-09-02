package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/securefs"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

// ErrIncompatibleDaemon indicates a healthy daemon that this client must not use.
var ErrIncompatibleDaemon = errors.New("local daemon is incompatible")

// Manager discovers, starts, inspects, and stops the local daemon.
type Manager struct {
	paths  apphome.Paths
	client *Client
}

// NewManager creates a daemon lifecycle manager.
func NewManager(paths apphome.Paths) *Manager {
	return &Manager{paths: paths, client: NewClient(paths)}
}

// Ensure returns a compatible local daemon, starting one if needed.
func (m *Manager) Ensure(ctx context.Context) (Registry, error) {
	coordinator := Coordinator{
		Paths: m.paths,
		Probe: func(ctx context.Context) (Registry, error) {
			registry, _, err := m.client.Probe(ctx)
			if err != nil {
				return Registry{}, err
			}
			if err := compatible(registry); err != nil {
				return Registry{}, err
			}
			return registry, nil
		},
		Launch:       m.launch,
		PollInterval: 50 * time.Millisecond,
		StartTimeout: 10 * time.Second,
	}
	return coordinator.Ensure(ctx)
}

// Status returns the currently registered daemon, including an older version or
// one whose database is not ready.
func (m *Manager) Status(ctx context.Context) (Registry, Health, error) {
	return m.client.ProbeLifecycle(ctx)
}

// Stop asks the local daemon to shut down and waits until all daemon-owned
// resources and its lifetime lock have been released. It is idempotent when no
// daemon is registered or the registered process exits during the request.
func (m *Manager) Stop(ctx context.Context) error {
	if err := m.paths.Ensure(); err != nil {
		return err
	}
	startupLock := flock.New(m.paths.StartupLock)
	locked, err := startupLock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("wait for daemon startup lock before stopping: %w", err)
	}
	if !locked {
		return errors.New("daemon startup lock was not acquired")
	}
	defer startupLock.Unlock()

	registry, registryErr := LoadRegistry(m.paths)
	var lifecycleErr error
	if registryErr == nil {
		_, _, healthErr := m.client.ProbeLifecycle(ctx)
		shutdownErr := m.client.Stop(ctx)
		if shutdownErr != nil {
			lifecycleErr = shutdownErr
			if healthErr != nil {
				lifecycleErr = fmt.Errorf("health check failed (%v); shutdown request failed: %w", healthErr, shutdownErr)
			}
		}
	}

	// Whether shutdown was acknowledged or the process died independently, this
	// lock becomes available only after daemon-owned resources are closed.
	lifetimeLock := flock.New(m.paths.ServerLock)
	locked, waitErr := lifetimeLock.TryLockContext(ctx, 50*time.Millisecond)
	if waitErr != nil {
		if lifecycleErr != nil {
			return fmt.Errorf("stop daemon (%v), then wait for resources: %w", lifecycleErr, waitErr)
		}
		return fmt.Errorf("wait for daemon resources to close: %w", waitErr)
	}
	if !locked {
		return errors.New("daemon lifetime lock was not acquired")
	}
	defer lifetimeLock.Unlock()

	if registryErr == nil {
		current, err := LoadRegistry(m.paths)
		if err == nil && current.InstanceID != registry.InstanceID {
			return fmt.Errorf("another daemon replaced instance %s while stopping", registry.InstanceID)
		}
	}
	// The startup and lifetime locks prove no process can own any remaining files.
	return clearRegistration(m.paths)
}

func (m *Manager) launch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve Kit executable: %w", err)
	}
	if err := m.paths.Ensure(); err != nil {
		return err
	}
	logFile, err := os.OpenFile(m.paths.ServerLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()
	if err := securefs.ProtectFile(m.paths.ServerLog); err != nil {
		return fmt.Errorf("protect daemon log: %w", err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open null input: %w", err)
	}
	defer input.Close()

	command := exec.Command(executable, "__daemon", "--home", m.paths.Home)
	command.Dir = m.paths.Home
	command.Stdin = input
	command.Stdout = logFile
	command.Stderr = logFile
	configureDetached(command)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}
	// Reap an early daemon exit while this client remains alive. If the client
	// exits first, the detached session is adopted by the platform's process reaper.
	go func() { _ = command.Wait() }()
	return nil
}

func compatible(registry Registry) error {
	if registry.ProtocolVersion != version.SessionProtocolVersion {
		return fmt.Errorf(
			"%w: daemon protocol %d does not match client protocol %d",
			ErrIncompatibleDaemon,
			registry.ProtocolVersion,
			version.SessionProtocolVersion,
		)
	}
	if version.Version != "dev" && registry.KitVersion != version.Version {
		return fmt.Errorf("%w: daemon version %q does not match client version %q", ErrIncompatibleDaemon, registry.KitVersion, version.Version)
	}
	return nil
}
