package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/securefs"
	"github.com/gofrs/flock"
)

// ErrEnvironmentCredentialsActive indicates that a running daemon would ignore
// a mutation to the persisted credential store.
var ErrEnvironmentCredentialsActive = errors.New("running daemon uses environment credentials")

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
		Paths:        m.paths,
		Probe:        m.client.ProbeCompatible,
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

// AcquireCredentialStoreMutation prevents a daemon restart while a client
// verifies that providerID is backed by the persistent store. The returned
// release function must be called after the auth-file mutation completes.
func (m *Manager) AcquireCredentialStoreMutation(
	ctx context.Context,
	providerID string,
) (func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.paths.Ensure(); err != nil {
		return nil, err
	}
	guardContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	startupLock := flock.New(m.paths.StartupLock)
	locked, err := startupLock.TryLockContext(guardContext, 50*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("wait for daemon startup lock before changing credentials: %w", err)
	}
	if !locked {
		return nil, errors.New("daemon startup lock was not acquired")
	}
	lifetimeLock := flock.New(m.paths.ServerLock)
	lifetimeHeld := false
	var releaseOnce sync.Once
	var releaseErr error
	release := func() error {
		releaseOnce.Do(func() {
			if lifetimeHeld {
				if err := lifetimeLock.Unlock(); err != nil {
					releaseErr = errors.Join(
						releaseErr,
						fmt.Errorf("release daemon lifetime lock after changing credentials: %w", err),
					)
				}
				lifetimeHeld = false
			}
			if err := startupLock.Unlock(); err != nil {
				releaseErr = errors.Join(
					releaseErr,
					fmt.Errorf("release daemon startup lock after changing credentials: %w", err),
				)
			}
		})
		return releaseErr
	}
	fail := func(err error) (func() error, error) {
		return nil, errors.Join(err, release())
	}

	var lastProbeErr error
	for {
		available, err := lifetimeLock.TryLock()
		if err != nil {
			return fail(fmt.Errorf("inspect daemon lifetime before changing credentials: %w", err))
		}
		if available {
			lifetimeHeld = true
			// With both startup and lifetime ownership, any publication belongs to
			// a dead process and cannot race a normal replacement daemon.
			if err := clearRegistration(m.paths); err != nil {
				return fail(fmt.Errorf("clear stale daemon registration: %w", err))
			}
			return release, nil
		}

		registry, registryErr := LoadRegistry(m.paths)
		if registryErr == nil {
			probed, _, probeErr := m.client.ProbeLifecycle(guardContext)
			if probeErr == nil {
				if probed.InstanceID != registry.InstanceID {
					return fail(errors.New("daemon changed while checking credential source"))
				}
				source, ok := registry.CredentialSources[providerID]
				if !ok {
					return fail(fmt.Errorf("running daemon does not report a credential source for %q; use a matching client or explicitly restart with the intended binary when interrupting active work is acceptable", providerID))
				}
				if source == CredentialSourceEnvironment {
					return fail(fmt.Errorf("%w for %q; restart it without provider environment credentials", ErrEnvironmentCredentialsActive, providerID))
				}
				if source != CredentialSourceStore {
					return fail(fmt.Errorf("running daemon reports unsupported credential source %q for %q", source, providerID))
				}
				return release, nil
			}
			lastProbeErr = probeErr
		} else if !errors.Is(registryErr, os.ErrNotExist) {
			return fail(fmt.Errorf("inspect live daemon credential source: %w", registryErr))
		}

		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-guardContext.Done():
			timer.Stop()
			message := fmt.Errorf("wait for daemon credential source: %w", guardContext.Err())
			if lastProbeErr != nil {
				message = fmt.Errorf("%w (last probe: %v)", message, lastProbeErr)
			}
			return fail(message)
		case <-timer.C:
		}
	}
}

// Restart reloads daemon-owned provider configuration from its durable sources.
func (m *Manager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	_, err := m.Ensure(ctx)
	return err
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
	daemonExecutable, err := stageDaemonExecutable(executable, m.paths.ServerExecutable)
	if err != nil {
		return fmt.Errorf("stage daemon executable: %w", err)
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

	command := exec.Command(daemonExecutable, "__server", "--home", m.paths.Home)
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

func stageDaemonExecutable(source, destination string) (string, error) {
	if filepath.Clean(source) == filepath.Clean(destination) {
		return destination, nil
	}
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source %q is not a regular file", source)
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".kit-daemon-*")
	if err != nil {
		return "", fmt.Errorf("create temporary executable: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if _, err := io.Copy(temporary, input); err != nil {
		return "", fmt.Errorf("copy executable: %w", err)
	}
	if err := temporary.Chmod(0o700); err != nil {
		return "", fmt.Errorf("protect executable: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return "", fmt.Errorf("close executable: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", fmt.Errorf("publish executable: %w", err)
	}
	return destination, nil
}
