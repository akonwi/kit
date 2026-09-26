package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/gofrs/flock"
)

func TestCoordinatorSerializesConcurrentStartup(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	var launches atomic.Int32
	var ready atomic.Bool
	registry := Registry{InstanceID: "ready"}

	newCoordinator := func() Coordinator {
		return Coordinator{
			Paths: paths,
			Probe: func(context.Context) (Registry, error) {
				if !ready.Load() {
					return Registry{}, errors.New("not ready")
				}
				return registry, nil
			},
			Launch: func(context.Context) error {
				launches.Add(1)
				time.Sleep(40 * time.Millisecond)
				ready.Store(true)
				return nil
			},
			PollInterval: 5 * time.Millisecond,
			StartTimeout: time.Second,
		}
	}

	start := make(chan struct{})
	results := make(chan Registry, 2)
	errorsFound := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := newCoordinator().Ensure(context.Background())
			results <- result
			errorsFound <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)

	for err := range errorsFound {
		if err != nil {
			t.Errorf("Ensure() error = %v", err)
		}
	}
	for result := range results {
		if result.InstanceID != registry.InstanceID {
			t.Errorf("InstanceID = %q, want %q", result.InstanceID, registry.InstanceID)
		}
	}
	if got := launches.Load(); got != 1 {
		t.Fatalf("launch count = %d, want 1", got)
	}
}

func TestCoordinatorWaitsForPreviousDaemonLifetimeBeforeLaunch(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	lifetime := flock.New(paths.ServerLock)
	locked, err := lifetime.TryLock()
	if err != nil || !locked {
		t.Fatalf("hold lifetime lock: locked=%v err=%v", locked, err)
	}

	var launches atomic.Int32
	var ready atomic.Bool
	coordinator := Coordinator{
		Paths: paths,
		Probe: func(context.Context) (Registry, error) {
			if ready.Load() {
				return Registry{InstanceID: "new"}, nil
			}
			return Registry{}, errors.New("not ready")
		},
		Launch: func(context.Context) error {
			launches.Add(1)
			ready.Store(true)
			return nil
		},
		PollInterval: 5 * time.Millisecond,
		StartTimeout: time.Second,
	}
	result := make(chan error, 1)
	go func() {
		_, err := coordinator.Ensure(context.Background())
		result <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if got := launches.Load(); got != 0 {
		t.Fatalf("launch count while old lifetime held = %d, want 0", got)
	}
	if err := lifetime.Unlock(); err != nil {
		t.Fatalf("release lifetime lock: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ensure() did not launch after lifetime lock release")
	}
	if got := launches.Load(); got != 1 {
		t.Fatalf("launch count = %d, want 1", got)
	}
}

func TestCoordinatorDoesNotReplaceIncompatibleDaemon(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	var launches atomic.Int32
	coordinator := Coordinator{
		Paths: paths,
		Probe: func(context.Context) (Registry, error) {
			return Registry{}, fmt.Errorf("%w: old protocol", ErrIncompatibleDaemon)
		},
		Launch: func(context.Context) error {
			launches.Add(1)
			return nil
		},
	}
	if _, err := coordinator.Ensure(context.Background()); !errors.Is(err, ErrIncompatibleDaemon) {
		t.Fatalf("Ensure() error = %v, want ErrIncompatibleDaemon", err)
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("launch count = %d, want 0", got)
	}
}

func TestCoordinatorDoesNotReplaceDaemonDiscoveredUnderStartupLock(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	startup := flock.New(paths.StartupLock)
	if locked, err := startup.TryLock(); err != nil || !locked {
		t.Fatalf("hold startup lock: %t, %v", locked, err)
	}
	defer startup.Unlock()

	var probes, launches atomic.Int32
	result := make(chan error, 1)
	coordinator := Coordinator{
		Paths: paths,
		Probe: func(context.Context) (Registry, error) {
			if probes.Add(1) == 1 {
				return Registry{}, errors.New("no daemon yet")
			}
			return Registry{ProtocolVersion: 5, InstanceID: "old"}, fmt.Errorf("%w: old protocol", ErrIncompatibleDaemon)
		},
		Launch: func(context.Context) error {
			launches.Add(1)
			return nil
		},
		PollInterval: 5 * time.Millisecond,
	}
	go func() { _, err := coordinator.Ensure(context.Background()); result <- err }()
	for probes.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if err := startup.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrIncompatibleDaemon) {
			t.Fatalf("Ensure() error = %v, want incompatible", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ensure() did not finish")
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("launched %d daemons after detecting an incompatible owner", got)
	}
}

func TestCoordinatorHonorsCancellationWhileWaitingForLock(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	held := flock.New(paths.StartupLock)
	locked, err := held.TryLock()
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	if !locked {
		t.Fatal("failed to hold startup lock")
	}
	defer held.Unlock()

	var launches atomic.Int32
	coordinator := Coordinator{
		Paths: paths,
		Probe: func(context.Context) (Registry, error) {
			return Registry{}, errors.New("not ready")
		},
		Launch: func(context.Context) error {
			launches.Add(1)
			return nil
		},
		PollInterval: 5 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := coordinator.Ensure(ctx); err == nil {
		t.Fatal("Ensure() succeeded while startup lock was held")
	}
	if got := launches.Load(); got != 0 {
		t.Fatalf("launch count = %d, want 0", got)
	}
}
