package kit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/sessionclient"
	"github.com/akonwi/kit/internal/version"
)

type fakeClientBackend struct {
	list func(context.Context, string) ([]protocol.SessionInfo, error)
}

func (f *fakeClientBackend) CreateSession(context.Context, protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{}, errors.New("unexpected CreateSession")
}
func (f *fakeClientBackend) ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{}, errors.New("unexpected ForkSession")
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
func (f *fakeClientBackend) Models(context.Context) (protocol.ModelCatalog, error) {
	return protocol.ModelCatalog{}, errors.New("unexpected Models")
}
func (f *fakeClientBackend) Attach(context.Context, string) (sessionclient.Session, error) {
	return nil, errors.New("unexpected Attach")
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
			_ = json.NewEncoder(response).Encode(httpapi.Health{
				InstanceID: instanceID, PID: 42, KitVersion: version.Version,
				ProtocolVersion: version.SessionProtocolVersion, DatabaseReady: true,
			})
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
	if sessions, err := client.ListSessions(t.Context(), ListSessionsOptions{}); err != nil || len(sessions) != 0 {
		t.Fatalf("sessions = %+v, err = %v", sessions, err)
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
