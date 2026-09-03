package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

func TestManagerStopsDaemonWhoseDatabaseIsUnhealthy(t *testing.T) {
	t.Parallel()

	const (
		token      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		instanceID = "unhealthy-instance"
	)
	var shutdownRequested atomic.Bool
	var malformedHealth atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get(instanceHeader) != instanceID {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/health":
			if malformedHealth.Load() {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("{"))
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{
				"instanceId":      instanceID,
				"pid":             os.Getpid(),
				"kitVersion":      "older",
				"protocolVersion": version.SessionProtocolVersion,
				"databaseReady":   false,
				"futureField":     "ignored by lifecycle clients",
			})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/shutdown":
			shutdownRequested.Store(true)
			writeJSON(writer, http.StatusAccepted, map[string]bool{"stopping": true})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	registry := Registry{
		RegistryVersion: version.LocalRegistryVersion,
		ProtocolVersion: version.SessionProtocolVersion,
		KitVersion:      "older",
		Commit:          "test",
		PID:             os.Getpid(),
		InstanceID:      instanceID,
		URL:             server.URL,
		StartedAt:       time.Now().UTC(),
	}
	if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := writeRegistry(paths, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	manager := NewManager(paths)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, health, err := manager.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if health.DatabaseReady {
		t.Fatal("Status() reported unhealthy database as ready")
	}
	malformedHealth.Store(true)
	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !shutdownRequested.Load() {
		t.Fatal("Stop() did not call the shutdown endpoint")
	}
	if _, err := os.Stat(paths.ServerRegistry); !os.IsNotExist(err) {
		t.Fatalf("registry remains after stop: %v", err)
	}
}

func TestAcquireCredentialStoreMutationChecksRunningDaemonSource(t *testing.T) {
	t.Parallel()
	const (
		token      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		instanceID = "credential-source-instance"
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get(instanceHeader) != instanceID {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(writer, http.StatusOK, Health{
			InstanceID: instanceID, PID: os.Getpid(), KitVersion: version.Version,
			ProtocolVersion: version.SessionProtocolVersion, DatabaseReady: true,
		})
	}))
	defer server.Close()

	for _, test := range []struct {
		name    string
		source  CredentialSource
		wantErr error
	}{
		{name: "store", source: CredentialSourceStore},
		{name: "environment", source: CredentialSourceEnvironment, wantErr: ErrEnvironmentCredentialsActive},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
			if err := paths.Ensure(); err != nil {
				t.Fatal(err)
			}
			lifetime := flock.New(paths.ServerLock)
			if locked, err := lifetime.TryLock(); err != nil || !locked {
				t.Fatalf("hold daemon lifetime = %v, %v", locked, err)
			}
			defer lifetime.Unlock()
			registry := Registry{
				RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
				KitVersion: version.Version, PID: os.Getpid(), InstanceID: instanceID,
				URL: server.URL, StartedAt: time.Now().UTC(),
				CredentialSources: map[string]CredentialSource{"openai-codex": test.source},
			}
			if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
				t.Fatal(err)
			}
			if err := writeRegistry(paths, registry); err != nil {
				t.Fatal(err)
			}
			release, err := NewManager(paths).AcquireCredentialStoreMutation(context.Background(), "openai-codex")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("AcquireCredentialStoreMutation() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				return
			}
			contender := flock.New(paths.StartupLock)
			if locked, err := contender.TryLock(); err != nil || locked {
				t.Fatalf("startup lock while guarded = %v, %v", locked, err)
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
			if locked, err := contender.TryLock(); err != nil || !locked {
				t.Fatalf("startup lock after release = %v, %v", locked, err)
			}
			_ = contender.Unlock()
		})
	}
}

func TestAcquireCredentialStoreMutationClearsStaleRegistry(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(paths.ServerToken, []byte("stale")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(paths.ServerRegistry, []byte("stale")); err != nil {
		t.Fatal(err)
	}
	release, err := NewManager(paths).AcquireCredentialStoreMutation(context.Background(), "openai-codex")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, path := range []string{paths.ServerToken, paths.ServerRegistry} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale publication %q remains: %v", path, err)
		}
	}
}

func TestAcquireCredentialStoreMutationWaitsForUnpublishedDaemon(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	lifetime := flock.New(paths.ServerLock)
	if locked, err := lifetime.TryLock(); err != nil || !locked {
		t.Fatalf("hold daemon lifetime = %v, %v", locked, err)
	}
	defer lifetime.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := NewManager(paths).AcquireCredentialStoreMutation(ctx, "openai-codex"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcquireCredentialStoreMutation() error = %v", err)
	}
}

func TestAcquireCredentialStoreMutationWithoutDaemon(t *testing.T) {
	t.Parallel()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	release, err := NewManager(paths).AcquireCredentialStoreMutation(context.Background(), "openai-codex")
	if err != nil {
		t.Fatal(err)
	}
	startupContender := flock.New(paths.StartupLock)
	if locked, err := startupContender.TryLock(); err != nil || locked {
		t.Fatalf("startup lock while guarded = %v, %v", locked, err)
	}
	lifetimeContender := flock.New(paths.ServerLock)
	if locked, err := lifetimeContender.TryLock(); err != nil || locked {
		t.Fatalf("lifetime lock while guarded = %v, %v", locked, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if locked, err := startupContender.TryLock(); err != nil || !locked {
		t.Fatalf("startup lock after release = %v, %v", locked, err)
	}
	_ = startupContender.Unlock()
	if locked, err := lifetimeContender.TryLock(); err != nil || !locked {
		t.Fatalf("lifetime lock after release = %v, %v", locked, err)
	}
	_ = lifetimeContender.Unlock()
}

func TestManagerLaunchHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewManager(paths).launch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("launch() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(paths.ServerRegistry); !os.IsNotExist(err) {
		t.Fatalf("canceled launch published a daemon: %v", err)
	}
}

func TestCompatibleWrapsSentinel(t *testing.T) {
	t.Parallel()

	err := compatible(Registry{ProtocolVersion: version.SessionProtocolVersion + 1})
	if !errors.Is(err, ErrIncompatibleDaemon) {
		t.Fatalf("compatible() error = %v", err)
	}
}
