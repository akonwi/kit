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
