package clienttransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

// fixedEndpoint addresses one known server instance without a handshake, for
// tests of individual operations.
func fixedEndpoint(url, token, instanceID string) *Client {
	connection := Connection{
		URL: url, Token: token, InstanceID: instanceID,
		ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version,
	}
	return New(func() (Connection, error) { return connection, nil })
}

const (
	endpointServerID  = "server_0123456789abcdef0123456789abcdef"
	endpointMachineID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// restartableServer is a fake Kit server whose instance and identity tests
// change to simulate restarts and replacements.
type restartableServer struct {
	mu       sync.Mutex
	instance string
	serverID string
	protocol int
	// sessionRequests records the instance header of every session request.
	sessionRequests []string
	healthRequests  atomic.Int32
}

func newRestartableServer(t *testing.T) (*restartableServer, *httptest.Server) {
	t.Helper()
	fake := &restartableServer{instance: "instance_one", serverID: endpointServerID, protocol: version.SessionProtocolVersion}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusUnauthorized, httpapi.ErrorUnauthorized, "unauthorized", nil))
			return
		}
		fake.mu.Lock()
		instance, serverID, protocolVersion := fake.instance, fake.serverID, fake.protocol
		header, present := request.Header.Get(httpapi.InstanceHeader), len(request.Header.Values(httpapi.InstanceHeader)) > 0
		if request.URL.Path != "/v1/health" {
			fake.sessionRequests = append(fake.sessionRequests, header)
		}
		fake.mu.Unlock()
		learning := request.URL.Path == "/v1/health" && !present
		if !learning && header != instance {
			httpapi.WriteError(writer, httpapi.NewAPIError(http.StatusConflict, httpapi.ErrorInstanceMismatch, "daemon instance mismatch", nil))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/health":
			fake.healthRequests.Add(1)
			_ = json.NewEncoder(writer).Encode(Health{
				InstanceID: instance, PID: 42, KitVersion: version.Version, ProtocolVersion: protocolVersion,
				DatabaseReady: true, ServerID: serverID, MachineID: endpointMachineID, Home: "/home/kit",
			})
		case "/v1/sessions":
			_ = json.NewEncoder(writer).Encode(protocol.SessionList{Sessions: []protocol.SessionInfo{}})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func (s *restartableServer) restart(instance string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instance = instance
}

func (s *restartableServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sessionRequests...)
}

func TestEndpointLearnsTheRunningInstance(t *testing.T) {
	_, server := newRestartableServer(t)
	client := NewEndpoint(server.URL, "secret")

	connection, err := client.ProbeCompatible(t.Context())
	if err != nil {
		t.Fatalf("ProbeCompatible() error = %v", err)
	}
	if connection.InstanceID != "instance_one" || connection.ServerID != endpointServerID || connection.Home != "/home/kit" {
		t.Fatalf("connection = %+v, want the learned instance and identity", connection)
	}
}

func TestEndpointFollowsARestartWithoutFailingTheRequest(t *testing.T) {
	fake, server := newRestartableServer(t)
	client := NewEndpoint(server.URL, "secret")
	if _, err := client.ListSessions(t.Context(), ""); err != nil {
		t.Fatalf("first ListSessions() error = %v", err)
	}

	fake.restart("instance_two")
	if _, err := client.ListSessions(t.Context(), ""); err != nil {
		t.Fatalf("ListSessions() after restart error = %v", err)
	}
	want := []string{"instance_one", "instance_one", "instance_two"}
	if got := fake.requests(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("session request instances = %q, want %q", got, want)
	}
}

func TestEndpointRefusesADifferentServer(t *testing.T) {
	fake, server := newRestartableServer(t)
	client := NewEndpoint(server.URL, "secret")
	if _, err := client.ProbeCompatible(t.Context()); err != nil {
		t.Fatal(err)
	}

	fake.mu.Lock()
	fake.instance, fake.serverID = "instance_other", "server_ffffffffffffffffffffffffffffffff"
	fake.mu.Unlock()
	_, err := client.ListSessions(t.Context(), "")
	if !errors.Is(err, ErrServerChanged) {
		t.Fatalf("ListSessions() against another server error = %v, want ErrServerChanged", err)
	}
	if got := fake.requests(); got[len(got)-1] != "instance_one" {
		t.Fatalf("session requests = %q, want none sent to the other server", got)
	}
}

func TestEndpointReportsAnIncompatibleServer(t *testing.T) {
	fake, server := newRestartableServer(t)
	fake.protocol = version.SessionProtocolVersion - 1
	_, err := NewEndpoint(server.URL, "secret").ListSessions(t.Context(), "")
	var mismatch *CompatibilityError
	if !errors.As(err, &mismatch) || mismatch.Reason != ServerProtocolOlder {
		t.Fatalf("ListSessions() error = %v, want a server-protocol-older CompatibilityError", err)
	}
	if got := fake.requests(); len(got) != 0 {
		t.Fatalf("session requests = %q, want none sent to an incompatible server", got)
	}
}

func TestEndpointHandshakesOnceForConcurrentRequests(t *testing.T) {
	fake, server := newRestartableServer(t)
	client := NewEndpoint(server.URL, "secret")
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := client.ListSessions(t.Context(), ""); err != nil {
				t.Errorf("ListSessions() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := fake.healthRequests.Load(); got != 1 {
		t.Fatalf("health requests = %d, want one handshake", got)
	}
}

// A forwarded server that restarted refuses the old token. A resolver that
// can be invalidated resolves it again, and the request is sent once more.
func TestInvalidatingResolverRecoversFromARefusedCredential(t *testing.T) {
	_, server := newRestartableServer(t)
	var mu sync.Mutex
	token := "stale"
	var invalidated []string
	resolve := func() (Connection, error) {
		mu.Lock()
		defer mu.Unlock()
		return Connection{URL: server.URL, Token: token, InstanceID: "instance_one", ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version}, nil
	}
	client := NewInvalidating(resolve, func(connection Connection) {
		mu.Lock()
		defer mu.Unlock()
		invalidated = append(invalidated, connection.Token)
		token = "secret"
	})
	if _, err := client.ListSessions(t.Context(), ""); err != nil {
		t.Fatalf("ListSessions() = %v", err)
	}
	if strings.Join(invalidated, ",") != "stale" {
		t.Fatalf("invalidated = %q", invalidated)
	}

	mu.Lock()
	token = "stale"
	mu.Unlock()
	_, err := client.StreamSessionEvents(t.Context(), "session_1", "", 0)
	var transport *TransportError
	if !errors.As(err, &transport) || !errors.Is(err, errStaleConnection) {
		t.Fatalf("a refused stream = %v, want a transient failure", err)
	}

	// Without invalidation, a refused credential stays a server error.
	plain := New(func() (Connection, error) {
		return Connection{URL: server.URL, Token: "stale", InstanceID: "instance_one", ProtocolVersion: version.SessionProtocolVersion, KitVersion: version.Version}, nil
	})
	var refused *APIError
	if _, err := plain.ListSessions(t.Context(), ""); !errors.As(err, &refused) || refused.StatusCode != http.StatusUnauthorized {
		t.Fatalf("plain ListSessions() = %v", err)
	}
}
