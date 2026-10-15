package daemonctl

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serverlisten"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

func TestClientStreamsAuthenticatedSessionEvents(t *testing.T) {
	t.Parallel()

	const (
		token      = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		instanceID = "sse-instance"
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get(instanceHeader) != instanceID {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.Header.Get(protocolHeader) != strconv.Itoa(version.SessionProtocolVersion) || request.Header.Get("Accept") != "text/event-stream" {
			http.Error(writer, "invalid stream headers", http.StatusBadRequest)
			return
		}
		if request.URL.Path != "/v1/sessions/session_test/events/stream" || request.URL.Query().Get("stream") != "stream_test" || request.URL.Query().Get("after") != "7" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(": connected\n\n"))
	}))
	defer server.Close()

	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := localdiscovery.Publish(paths, token, localdiscovery.Registry{
		RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
		KitVersion: version.Version, PID: os.Getpid(), InstanceID: instanceID, URL: server.URL,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	body, err := NewClient(paths).transport.StreamSessionEvents(t.Context(), "session_test", "stream_test", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
}

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
	registry := localdiscovery.Registry{
		RegistryVersion: version.LocalRegistryVersion,
		ProtocolVersion: version.SessionProtocolVersion,
		KitVersion:      "older",
		Commit:          "test",
		PID:             os.Getpid(),
		InstanceID:      instanceID,
		URL:             server.URL,
		StartedAt:       time.Now().UTC(),
	}
	if err := localdiscovery.Publish(paths, token, registry); err != nil {
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

func TestStageDaemonExecutablePublishesPrivateStableCopy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "run", "kit-daemon")
	if err := os.Mkdir(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("first executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged, err := stageDaemonExecutable(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if staged != destination {
		t.Fatalf("staged path = %q, want %q", staged, destination)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first executable" {
		t.Fatalf("staged content = %q", content)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o700 {
		t.Fatalf("staged permissions = %o, want 700", permissions)
	}

	if err := os.WriteFile(source, []byte("replacement executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageDaemonExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "replacement executable" {
		t.Fatalf("replacement content = %q", content)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".kit-daemon-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary executables = %v, error %v", matches, err)
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

func TestEnsureRequiresARunningDaemonToServeTheRequestedAddress(t *testing.T) {
	t.Parallel()
	const (
		token      = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		instanceID = "listen-instance"
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
	port := server.Listener.Addr().(*net.TCPAddr).Port

	for _, test := range []struct {
		listen  string
		wantErr error
	}{
		{listen: "127.0.0.1:0"},
		{listen: "127.0.0.1:" + strconv.Itoa(port)},
		{listen: "0.0.0.0:" + strconv.Itoa(port)},
		{listen: "127.0.0.1:" + strconv.Itoa(port+1), wantErr: ErrListenMismatch},
	} {
		paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
		if err := paths.Ensure(); err != nil {
			t.Fatal(err)
		}
		lifetime := flock.New(paths.ServerLock)
		if locked, err := lifetime.TryLock(); err != nil || !locked {
			t.Fatalf("hold daemon lifetime = %v, %v", locked, err)
		}
		registry := localdiscovery.Registry{
			RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion,
			KitVersion: version.Version, PID: os.Getpid(), InstanceID: instanceID,
			URL: server.URL, StartedAt: time.Now().UTC(),
		}
		if err := localdiscovery.Publish(paths, token, registry); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := NewManager(paths, WithListen(serverlisten.MustParse(test.listen))).Ensure(ctx)
		cancel()
		_ = lifetime.Unlock()
		if !errors.Is(err, test.wantErr) {
			t.Fatalf("Ensure() with listen %s error = %v, want %v", test.listen, err, test.wantErr)
		}
	}
}
