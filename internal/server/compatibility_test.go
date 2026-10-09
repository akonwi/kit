package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/version"
	"github.com/gofrs/flock"
)

func TestCompatibilityReasonAndDirection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		protocol int
		release  string
		want     CompatibilityReason
	}{
		{"older daemon", version.SessionProtocolVersion - 1, "v1", DaemonProtocolOlder},
		{"older client", version.SessionProtocolVersion + 1, "v2", ClientProtocolOlder},
		{"unverified release skew", version.SessionProtocolVersion, "v2", ReleaseMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := compatibleWithVersion(Registry{ProtocolVersion: tc.protocol, KitVersion: tc.release}, "v1")
			var mismatch *DaemonCompatibilityError
			if !errors.Is(err, ErrIncompatibleDaemon) || !errors.As(err, &mismatch) || mismatch.Reason != tc.want || mismatch.ClientVersion != "v1" || mismatch.DaemonVersion != tc.release || mismatch.ClientProtocol != version.SessionProtocolVersion || mismatch.DaemonProtocol != tc.protocol {
				t.Fatalf("compatibility error = %v (%+v), want %s with both identities", err, mismatch, tc.want)
			}
			if !strings.Contains(err.Error(), "left running") {
				t.Fatalf("mismatch not actionable: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name, client, daemon string
		compatible           bool
	}{
		{"protocol-45 baseline to next stable", "0.42.0", "0.42.1", true},
		{"next stable to protocol-45 baseline", "0.42.1", "0.42.0", true},
		{"future major to protocol-45 baseline", "1.0.0", "0.42.0", true},
		{"same prerelease", "0.39.0-rc.1", "0.39.0-rc.1", true},
		{"same dev label remains local-only", "dev", "dev", true},
		{"rc to stable", "0.39.0-rc.1", "0.39.0", false},
		{"stable to rc", "0.39.0", "0.39.0-rc.1", false},
		{"dev to stable", "dev", "0.39.0", false},
		{"stable to dev", "0.39.0", "dev", false},
		{"prebaseline to stable", "0.38.9", "0.39.0", false},
		{"same prebaseline", "0.38.9", "0.38.9", true},
		{"noncanonical leading zero", "0.039.0", "0.39.0", false},
		{"build metadata", "0.39.0+local", "0.39.0", false},
		{"unverified labels", "release-a", "release-b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := compatibleWithVersion(Registry{ProtocolVersion: version.SessionProtocolVersion, KitVersion: tc.daemon}, tc.client)
			if tc.compatible {
				if err != nil {
					t.Fatalf("compatible releases %q and %q rejected: %v", tc.client, tc.daemon, err)
				}
				return
			}
			var mismatch *DaemonCompatibilityError
			if !errors.As(err, &mismatch) || mismatch.Reason != ReleaseMismatch || mismatch.ClientVersion != tc.client || mismatch.DaemonVersion != tc.daemon {
				t.Fatalf("unverified releases %q and %q = %v, want typed mismatch", tc.client, tc.daemon, err)
			}
		})
	}
}

func TestProtocol42RejectsReleasedProtocol41Daemon(t *testing.T) {
	t.Parallel()
	err := compatibleWithVersion(Registry{ProtocolVersion: 41, KitVersion: "0.38.0"}, "0.39.0")
	var mismatch *DaemonCompatibilityError
	if !errors.As(err, &mismatch) || mismatch.Reason != DaemonProtocolOlder {
		t.Fatalf("protocol-41 daemon must be rejected without replacement: %v", err)
	}
}

func TestStableReleaseScope(t *testing.T) {
	t.Parallel()
	minimum := [3]uint64{0, 42, 0}
	for _, tc := range []struct {
		release string
		want    bool
	}{
		{"0.41.99", false}, {"0.42.0", true}, {"0.42.1", true},
		{"0.43.0", true}, {"1.0.0", true}, {"dev", false},
		{"0.42.0-rc.1", false}, {"0.42.0+local", false},
		{"v0.42.0", false}, {"00.42.0", false}, {"0.042.0", false},
		{"0.42.00", false}, {"0.42", false}, {"0.42.0.0", false},
		{"0.42.-1", false}, {"0.42.9999999999999999999999999999999", false},
	} {
		if got := stableReleaseAtLeast(tc.release, minimum); got != tc.want {
			t.Errorf("stableReleaseAtLeast(%q) = %t, want %t", tc.release, got, tc.want)
		}
	}
	for _, tc := range []struct {
		protocol       int
		client, daemon string
		want           bool
	}{
		{42, "0.39.0", "0.39.1", true},
		{45, "0.42.0", "0.42.1", true},
		{45, "0.41.0", "0.42.0", false},
		{44, "0.42.0", "0.42.1", false},
	} {
		if got := coveredSessionReleasePair(tc.protocol, tc.client, tc.daemon); got != tc.want {
			t.Errorf("protocol %d releases %q/%q = %t, want %t", tc.protocol, tc.client, tc.daemon, got, tc.want)
		}
	}
}

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
	if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
		t.Fatal(err)
	}
	publish := func(id string) {
		t.Helper()
		// The replaced daemon removes its registration before the next one
		// publishes. writeRegistry deliberately refuses to overwrite it.
		if id == "second" {
			if err := os.Remove(paths.ServerRegistry); err != nil {
				t.Fatal(err)
			}
		}
		instance.Store(id)
		if err := writeRegistry(paths, Registry{RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version, PID: os.Getpid(), InstanceID: id, URL: daemon.URL, StartedAt: time.Now().UTC()}); err != nil {
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
		wantReason CompatibilityReason
		wantError  error
	}{
		{name: "older daemon", protocol: version.SessionProtocolVersion - 1, ready: true, wantReason: DaemonProtocolOlder},
		{name: "newer daemon", protocol: version.SessionProtocolVersion + 1, ready: true, wantReason: ClientProtocolOlder},
		{name: "unready database", protocol: version.SessionProtocolVersion - 1, wantError: ErrDaemonNotReady},
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
			registry := Registry{RegistryVersion: version.LocalRegistryVersion, ProtocolVersion: tc.protocol, KitVersion: version.Version, PID: os.Getpid(), InstanceID: instance, URL: daemon.URL, StartedAt: time.Now().UTC()}
			if err := writePrivateFile(paths.ServerToken, []byte(token+"\n")); err != nil {
				t.Fatal(err)
			}
			if err := writeRegistry(paths, registry); err != nil {
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
				var mismatch *DaemonCompatibilityError
				if !errors.As(err, &mismatch) || mismatch.Reason != tc.wantReason {
					t.Fatalf("Ensure() error = %v, want %s", err, tc.wantReason)
				}
			case tc.wantError != nil:
				if !errors.Is(err, tc.wantError) || errors.Is(err, ErrIncompatibleDaemon) {
					t.Fatalf("Ensure() error = %v, want unready only", err)
				}
			default:
				if err == nil || errors.Is(err, ErrIncompatibleDaemon) {
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
			} else if got, err := LoadRegistry(paths); err != nil || got.InstanceID != instance {
				t.Fatalf("live registry replaced: %+v, %v", got, err)
			}
		})
	}
}
