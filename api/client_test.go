package kit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
)

type fakeClientBackend struct {
	list func(context.Context, string) ([]protocol.SessionInfo, error)
}

func (f *fakeClientBackend) CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{}, errors.New("unexpected CreateSession")
}
func (f *fakeClientBackend) ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.ForkSessionResult, error) {
	return protocol.ForkSessionResult{}, errors.New("unexpected ForkSession")
}
func (f *fakeClientBackend) RenameSession(context.Context, string, string) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{}, errors.New("unexpected RenameSession")
}
func (f *fakeClientBackend) DeleteSession(context.Context, string) error {
	return errors.New("unexpected DeleteSession")
}
func (f *fakeClientBackend) DisposeTemporarySession(context.Context, string) error {
	return errors.New("unexpected DisposeTemporarySession")
}
func (f *fakeClientBackend) ListSessions(ctx context.Context, cwd string) ([]protocol.SessionInfo, error) {
	return f.list(ctx, cwd)
}
func (f *fakeClientBackend) ListModels(context.Context) (protocol.ModelCatalog, error) {
	return protocol.ModelCatalog{}, errors.New("unexpected ListModels")
}
func (f *fakeClientBackend) RefreshModels(context.Context) (protocol.ModelCatalog, error) {
	return protocol.ModelCatalog{}, errors.New("unexpected RefreshModels")
}

func TestClientListSessionsProjectsOptions(t *testing.T) {
	backend := &fakeClientBackend{list: func(_ context.Context, cwd string) ([]protocol.SessionInfo, error) {
		if cwd != "/workspace" {
			t.Fatalf("cwd = %q", cwd)
		}
		return []protocol.SessionInfo{{ID: "session_test"}}, nil
	}}
	client := newClient(backend, nil)
	t.Cleanup(func() { _ = client.Close() })

	sessions, err := client.ListSessions(t.Context(), ListSessionsOptions{CWD: "/workspace"})
	if err != nil || len(sessions) != 1 || sessions[0].ID != "session_test" {
		t.Fatalf("sessions = %+v, err = %v", sessions, err)
	}
}

func TestResolveSessionSelectorUsesExactThenUniquePrefix(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "session_0123456789abcdef0123456789abcdef"},
		{ID: "session_01239999999999999999999999999999"},
	}
	exact, err := resolveSessionSelector(sessions, sessions[0].ID)
	if err != nil || exact.ID != sessions[0].ID {
		t.Fatalf("exact = %+v, err = %v", exact, err)
	}
	short, err := resolveSessionSelector(sessions, "012345")
	if err != nil || short.ID != sessions[0].ID {
		t.Fatalf("short = %+v, err = %v", short, err)
	}
	if _, err := resolveSessionSelector(sessions, "0123"); !errors.Is(err, ErrSessionAmbiguous) {
		t.Fatalf("ambiguous error = %v", err)
	}
}

func TestClientProjectsProtocolErrors(t *testing.T) {
	backend := &fakeClientBackend{list: func(context.Context, string) ([]protocol.SessionInfo, error) {
		return nil, &clienttransport.ProtocolError{Err: errors.New("unknown response field")}
	}}
	client := newClient(backend, nil)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.ListSessions(t.Context(), ListSessionsOptions{})
	var failure *ProtocolError
	if !errors.As(err, &failure) || !strings.Contains(failure.Error(), "unknown response field") {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientProjectsDeclaredServerErrors(t *testing.T) {
	backend := &fakeClientBackend{list: func(context.Context, string) ([]protocol.SessionInfo, error) {
		return nil, &clienttransport.APIError{Code: "not_found", Message: "session not found"}
	}}
	client := newClient(backend, nil)
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.ListSessions(t.Context(), ListSessionsOptions{})
	var failure *ServerError
	if !errors.As(err, &failure) || failure.Code != ErrorNotFound || failure.Message != "session not found" {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientCloseCancelsOwnedOperationsAndIsIdempotent(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	backend := &fakeClientBackend{list: func(ctx context.Context, _ string) ([]protocol.SessionInfo, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	closedTransport := 0
	client := newClient(backend, func() { closedTransport++ })

	result := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(context.Background(), ListSessionsOptions{})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("operation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not cancel operation")
	}
	if closedTransport != 1 {
		t.Fatalf("transport closes = %d", closedTransport)
	}
	if _, err := client.ListSessions(t.Context(), ListSessionsOptions{}); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("closed operation error = %v", err)
	}
}

func TestConnectEndpointAuthenticatesAndChecksCompatibility(t *testing.T) {
	const instanceID = "instance_test"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get(httpapi.InstanceHeader) != instanceID {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(response).Encode(compatibleHealth(instanceID))
		case "/v1/sessions":
			if request.Header.Get(httpapi.ProtocolHeader) == "" {
				http.Error(response, "missing protocol", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(response).Encode(protocol.SessionList{Sessions: []protocol.SessionInfo{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := Connect(t.Context(), Endpoint(server.URL+"/", AccessToken("secret", instanceID)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.ProbeCompatibility(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sessions, err := client.ListSessions(t.Context(), ListSessionsOptions{}); err != nil || len(sessions) != 0 {
		t.Fatalf("sessions = %+v, err = %v", sessions, err)
	}
}

func TestConnectedClientsHaveIndependentLifetimes(t *testing.T) {
	const instanceID = "instance_test"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(response).Encode(compatibleHealth(instanceID))
		case "/v1/sessions":
			_ = json.NewEncoder(response).Encode(protocol.SessionList{Sessions: []protocol.SessionInfo{}})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	target := Endpoint(server.URL, AccessToken("secret", instanceID))
	first, err := Connect(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Connect(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ListSessions(t.Context(), ListSessionsOptions{}); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("closed first client error = %v", err)
	}
	if _, err := second.ListSessions(t.Context(), ListSessionsOptions{}); err != nil {
		t.Fatalf("second client after first close: %v", err)
	}
}

func TestAttachedSessionOperationsReturnClientClosedAfterClose(t *testing.T) {
	const instanceID = "instance_test"
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	var snapshotRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(response).Encode(compatibleHealth(instanceID))
		case "/v1/sessions/" + sessionID:
			snapshotRequests.Add(1)
			_ = json.NewEncoder(response).Encode(validWatchSnapshot(sessionID, "stream_test", 0))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := Connect(t.Context(), Endpoint(server.URL, AccessToken("secret", instanceID)))
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Attach(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Snapshot(t.Context()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshotRequests.Load() != 1 {
		t.Fatalf("snapshot requests = %d, want attach only", snapshotRequests.Load())
	}
}

func TestConnectEndpointDoesNotFollowRedirects(t *testing.T) {
	contacted := make(chan struct{}, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case contacted <- struct{}{}:
		default:
		}
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := Connect(t.Context(), Endpoint(origin.URL, AccessToken("secret", "instance_test")))
	if client != nil || err == nil {
		t.Fatalf("client = %v, err = %v", client, err)
	}
	select {
	case <-contacted:
		t.Fatal("redirect destination was contacted")
	default:
	}
}

const (
	testServerID  = "server_0123456789abcdef0123456789abcdef"
	testMachineID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// compatibleHealth is the health of a ready server speaking this client's protocol.
func compatibleHealth(instanceID string) httpapi.Health {
	return httpapi.Health{
		InstanceID: instanceID, PID: 42, KitVersion: version.Version,
		ProtocolVersion: version.SessionProtocolVersion, DatabaseReady: true,
		ServerID: testServerID, MachineID: testMachineID, Home: "/home/kit",
	}
}

func healthServer(t *testing.T, health func() httpapi.Health) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/health" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(health())
	}))
	t.Cleanup(server.Close)
	return server
}

func TestServerReportsIdentityAndMachineLocality(t *testing.T) {
	const instanceID = "instance_test"
	server := healthServer(t, func() httpapi.Health { return compatibleHealth(instanceID) })
	client, err := Connect(t.Context(), Endpoint(server.URL, AccessToken("secret", instanceID)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	cases := []struct {
		name  string
		local func() (string, error)
		same  bool
	}{
		{name: "matching machine", local: func() (string, error) { return testMachineID, nil }, same: true},
		{name: "other machine", local: func() (string, error) { return strings.Repeat("f", 64), nil }, same: false},
		{name: "unknown local machine", local: func() (string, error) { return "", errors.New("unavailable") }, same: false},
	}
	for _, test := range cases {
		client.localMachineID = test.local
		info, err := client.Server(t.Context())
		if err != nil {
			t.Fatalf("%s: Server() error = %v", test.name, err)
		}
		want := ServerInfo{
			ID: testServerID, KitVersion: version.Version, ProtocolVersion: version.SessionProtocolVersion,
			Home: "/home/kit", SameMachine: test.same,
		}
		if info != want {
			t.Fatalf("%s: Server() = %+v, want %+v", test.name, info, want)
		}
	}
}

func TestConnectRejectsMalformedServerIdentity(t *testing.T) {
	const instanceID = "instance_test"
	cases := map[string]func(*httpapi.Health){
		"server id":  func(health *httpapi.Health) { health.ServerID = "server_bad" },
		"machine id": func(health *httpapi.Health) { health.MachineID = "ABC" },
		"home":       func(health *httpapi.Health) { health.Home = "relative/home" },
	}
	for name, corrupt := range cases {
		server := healthServer(t, func() httpapi.Health {
			health := compatibleHealth(instanceID)
			corrupt(&health)
			return health
		})
		_, err := Connect(t.Context(), Endpoint(server.URL, AccessToken("secret", instanceID)))
		var protocolErr *ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("%s: Connect() error = %v, want ProtocolError", name, err)
		}
	}
}

func TestConnectReportsOtherProtocolsAsIncompatibleBeforeIdentity(t *testing.T) {
	const instanceID = "instance_test"
	server := healthServer(t, func() httpapi.Health {
		return httpapi.Health{
			InstanceID: instanceID, PID: 42, KitVersion: "0.0.1",
			ProtocolVersion: version.SessionProtocolVersion - 1, DatabaseReady: true,
		}
	})
	_, err := Connect(t.Context(), Endpoint(server.URL, AccessToken("secret", instanceID)))
	if !errors.Is(err, ErrIncompatibleServer) {
		t.Fatalf("Connect() error = %v, want ErrIncompatibleServer", err)
	}
}
