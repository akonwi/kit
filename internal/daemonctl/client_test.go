package daemonctl

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
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

func TestProbeCompatibleSeesNewDaemonIdentityWithoutRestarting(t *testing.T) {
	t.Parallel()
	const token = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	var instance atomic.Value
	instance.Store("first")
	var requests atomic.Int32
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/health" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get(instanceHeader) != instance.Load().(string) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, Health{InstanceID: instance.Load().(string), PID: os.Getpid(), KitVersion: version.Version, ProtocolVersion: version.SessionProtocolVersion, DatabaseReady: true})
	}))
	defer daemon.Close()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	publish := func(id string) {
		t.Helper()
		// The replaced daemon withdraws its publication before the next one
		// publishes; Publish deliberately refuses to overwrite it.
		if id == "second" {
			if err := localdiscovery.Withdraw(paths, "first"); err != nil {
				t.Fatal(err)
			}
		}
		instance.Store(id)
		if err := localdiscovery.Publish(paths, token, localdiscovery.Registry{RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version, PID: os.Getpid(), InstanceID: id, URL: daemon.URL, StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	publish("first")
	client := NewClient(paths)
	for _, id := range []string{"first", "second"} {
		if id == "second" {
			publish(id)
		}
		got, err := client.ProbeCompatible(t.Context())
		if err != nil || got.InstanceID != id {
			t.Fatalf("ProbeCompatible() = %+v, %v, want instance %s", got, err, id)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("probe requests = %d, want exactly two authenticated health GETs", got)
	}
}

func TestManagerEnsureLeavesLiveUnusableDaemonUntouched(t *testing.T) {
	t.Parallel()
	const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name       string
		protocol   int
		ready      bool
		identity   bool
		versionID  bool
		malformed  bool
		wantReason clienttransport.CompatibilityReason
		wantError  error
	}{
		{name: "older daemon", protocol: version.SessionProtocolVersion - 1, ready: true, wantReason: clienttransport.ServerProtocolOlder},
		{name: "newer daemon", protocol: version.SessionProtocolVersion + 1, ready: true, wantReason: clienttransport.ClientProtocolOlder},
		{name: "unready database", protocol: version.SessionProtocolVersion - 1, wantError: clienttransport.ErrServerNotReady},
		{name: "identity disagreement", protocol: version.SessionProtocolVersion, ready: true, identity: true},
		{name: "health version disagreement", protocol: version.SessionProtocolVersion, ready: true, versionID: true},
		{name: "malformed registry", protocol: version.SessionProtocolVersion, ready: true, malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const instance = "live-daemon"
			var shutdown atomic.Int32
			daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get(instanceHeader) != instance {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				if r.URL.Path == "/v1/shutdown" {
					shutdown.Add(1)
					w.WriteHeader(http.StatusAccepted)
					return
				}
				id := instance
				if tc.identity {
					id = "other-daemon"
				}
				kitVersion := version.Version
				if tc.versionID {
					kitVersion = "different release"
				}
				writeJSON(w, http.StatusOK, Health{InstanceID: id, PID: os.Getpid(), KitVersion: kitVersion, ProtocolVersion: tc.protocol, DatabaseReady: tc.ready})
			}))
			defer daemon.Close()
			paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
			if err := paths.Ensure(); err != nil {
				t.Fatal(err)
			}
			registry := localdiscovery.Registry{RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: tc.protocol, KitVersion: version.Version, PID: os.Getpid(), InstanceID: instance, URL: daemon.URL, StartedAt: time.Now().UTC()}
			if err := localdiscovery.Publish(paths, token, registry); err != nil {
				t.Fatal(err)
			}
			if tc.malformed {
				if err := os.WriteFile(paths.ServerRegistry, []byte("malformed registry"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lifetime := flock.New(paths.ServerLock)
			if ok, err := lifetime.TryLock(); err != nil || !ok {
				t.Fatalf("hold daemon lifetime: %v, %v", ok, err)
			}
			defer lifetime.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
			defer cancel()
			_, err := NewManager(paths).Ensure(ctx)
			switch {
			case tc.wantReason != "":
				var mismatch *clienttransport.CompatibilityError
				if !errors.As(err, &mismatch) || mismatch.Reason != tc.wantReason {
					t.Fatalf("Ensure() error = %v, want %s", err, tc.wantReason)
				}
			case tc.wantError != nil:
				if !errors.Is(err, tc.wantError) || errors.Is(err, clienttransport.ErrIncompatibleServer) {
					t.Fatalf("Ensure() error = %v, want unready only", err)
				}
			default:
				if err == nil || errors.Is(err, clienttransport.ErrIncompatibleServer) {
					t.Fatalf("Ensure() identity error = %v, want non-compatibility error", err)
				}
			}
			if shutdown.Load() != 0 {
				t.Fatalf("shut down a live daemon %d times", shutdown.Load())
			}
			if tc.malformed {
				content, err := os.ReadFile(paths.ServerRegistry)
				if err != nil || string(content) != "malformed registry" {
					t.Fatalf("malformed live registry replaced: %q, %v", content, err)
				}
			} else if got, err := localdiscovery.Load(paths); err != nil || got.InstanceID != instance {
				t.Fatalf("live registry replaced: %+v, %v", got, err)
			}
		})
	}
}
