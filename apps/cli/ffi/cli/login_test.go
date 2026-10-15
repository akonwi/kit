package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/providerlogin"
	kitserver "github.com/akonwi/kit/internal/server"
)

// browserLoginServer runs a server whose browser logins redirect to
// redirectURI and succeed with an authorization in accepted. It returns a
// client of that server and the authorization each login received.
func browserLoginServer(t *testing.T, redirectURI string, accepted ...string) (*kit.Client, <-chan string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	startDaemonWith(t, home, &providerlogin.Flows{
		Browser: func(ctx context.Context, _ string, authorization <-chan string, notify func(providerlogin.BrowserInstructions)) error {
			notify(providerlogin.BrowserInstructions{AuthorizationURL: "https://claude.ai/oauth/authorize?state=s3cret", RedirectURI: redirectURI})
			select {
			case input := <-authorization:
				received <- input
				for _, want := range accepted {
					if input == want {
						return nil
					}
				}
				return errors.New("Anthropic OAuth state mismatch")
			case <-ctx.Done():
				received <- "stopped: " + ctx.Err().Error()
				return ctx.Err()
			}
		},
		Device: func(ctx context.Context, _ string, notify func(providerlogin.DeviceInstructions)) error {
			notify(providerlogin.DeviceInstructions{VerificationURI: "https://auth.openai.com/codex/device", UserCode: "CODE", ExpiresAt: time.Now().Add(time.Minute)})
			<-ctx.Done()
			received <- "stopped: " + ctx.Err().Error()
			return ctx.Err()
		},
	})
	client, err := kit.Connect(t.Context(), kit.Home(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, received
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestTheBrowserRedirectIsRelayedToTheServer(t *testing.T) {
	port := freePort(t)
	redirect := fmt.Sprintf("http://localhost:%d/callback", port)
	relayed := redirect + "?code=abc&state=s3cret"
	client, received := browserLoginServer(t, redirect, relayed)

	browser := func(query string) int {
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?%s", port, query))
		if err != nil {
			t.Errorf("browser redirect: %v", err)
			return 0
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	err := serverBrowserLogin{client: client}.Login(t.Context(), nil, func(instructions BrowserLoginInstructions) error {
		if instructions.RedirectURI != redirect || instructions.AuthorizationURL != "https://claude.ai/oauth/authorize?state=s3cret" ||
			!instructions.ReceivesRedirect {
			t.Errorf("instructions = %+v", instructions)
		}
		// Only a redirect carrying the login's state is relayed.
		if status := browser("code=abc&state=forged"); status != http.StatusBadRequest {
			t.Errorf("forged redirect status = %d", status)
		}
		if status := browser("code=abc&state=s3cret"); status != http.StatusOK {
			t.Errorf("redirect status = %d", status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if got := <-received; got != relayed {
		t.Fatalf("server received %q, want %q", got, relayed)
	}
}

func TestAPastedCodeCompletesABrowserLoginWithoutTheRedirect(t *testing.T) {
	client, received := browserLoginServer(t, "http://localhost:53692/callback", "abc#s3cret")
	manual := make(chan string, 1)
	unavailable := func(string, string) (net.Listener, error) { return nil, errors.New("address already in use") }
	err := serverBrowserLogin{client: client, listen: unavailable}.Login(t.Context(), manual, func(instructions BrowserLoginInstructions) error {
		if instructions.ReceivesRedirect {
			t.Error("instructions claim the redirect is received without a listener")
		}
		manual <- "abc#s3cret"
		return nil
	})
	if err != nil || <-received != "abc#s3cret" {
		t.Fatalf("Login() = %v", err)
	}

	// A rejected code fails the login with the server's reason.
	err = serverBrowserLogin{client: client, listen: unavailable}.Login(t.Context(), manual, func(BrowserLoginInstructions) error {
		manual <- "wrong"
		return nil
	})
	<-received
	if err == nil || err.Error() != "Anthropic OAuth state mismatch" {
		t.Fatalf("Login(wrong code) = %v", err)
	}
}

// An authorization that cannot be delivered ends the login at once, rather
// than leaving it pending on the server until it expires.
func TestAnUndeliverableAuthorizationEndsTheLogin(t *testing.T) {
	client, received := browserLoginServer(t, "http://localhost:53692/callback")
	manual := make(chan string, 1)
	unavailable := func(string, string) (net.Listener, error) { return nil, errors.New("address already in use") }
	done := make(chan error, 1)
	go func() {
		done <- serverBrowserLogin{client: client, listen: unavailable}.Login(t.Context(), manual, func(BrowserLoginInstructions) error {
			manual <- "nul\x00byte"
			return nil
		})
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != "authorization must be non-empty text of at most 4096 bytes" {
			t.Fatalf("Login() = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the login waited on after its authorization failed to send")
	}
	if got := <-received; got != "stopped: context canceled" {
		t.Fatalf("server flow = %q", got)
	}
}

// A localhost redirect is received on both loopback addresses, since the
// browser may resolve localhost to either.
func TestTheRedirectIsReceivedOnBothLoopbackAddresses(t *testing.T) {
	port := freePort(t)
	var addresses []string
	listen := func(network, address string) (net.Listener, error) {
		addresses = append(addresses, address)
		return net.Listen(network, address)
	}
	stop, err := relayRedirect(listen, kit.BrowserLoginInstructions{
		AuthorizationURL: "https://claude.ai/oauth/authorize?state=s3cret",
		RedirectURI:      fmt.Sprintf("http://localhost:%d/callback", port),
	}, make(chan string, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	want := []string{fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("[::1]:%d", port)}
	if fmt.Sprint(addresses) != fmt.Sprint(want) {
		t.Fatalf("listened on %v, want %v", addresses, want)
	}
}

func TestAbandoningALoginCancelsItOnTheServer(t *testing.T) {
	client, received := browserLoginServer(t, "http://localhost:53692/callback")
	ctx, cancel := context.WithCancel(t.Context())
	err := serverDeviceLogin{client: client}.Login(ctx, func(DeviceLoginInstructions) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() = %v", err)
	}
	if got := <-received; got != "stopped: context canceled" {
		t.Fatalf("server flow = %q", got)
	}
}

// A client following a pending login does not hold the server open: stopping
// fails the login, and the server stops cleanly within its shutdown window.
func TestAPendingLoginDoesNotStallShutdown(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	result := make(chan error, 1)
	go func() {
		result <- kitserver.Run(ctx, kitserver.RunOptions{
			Paths: apphome.FromHome(home), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			LoginFlows: &providerlogin.Flows{Device: func(ctx context.Context, _ string, notify func(providerlogin.DeviceInstructions)) error {
				notify(providerlogin.DeviceInstructions{VerificationURI: "https://auth.openai.com/codex/device", UserCode: "CODE", ExpiresAt: time.Now().Add(time.Minute)})
				<-ctx.Done()
				return ctx.Err()
			}},
		})
	}()
	manager := daemonctl.NewManager(apphome.FromHome(home))
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, _, err := manager.Status(context.Background()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not become ready")
		}
	}
	client, err := kit.Connect(t.Context(), kit.Home(home))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	login, err := client.StartProviderLogin(t.Context(), deviceLoginProvider, kit.LoginMethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	followed := make(chan kit.ProviderLogin, 1)
	go func() {
		next, _ := client.WaitProviderLogin(context.Background(), login.ID, kit.MaxLoginWait)
		followed <- next
	}()
	time.Sleep(50 * time.Millisecond) // let the long poll reach the server
	stopServer()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("a pending login stalled server shutdown")
	}
	if next := <-followed; next.State != kit.LoginStateFailed || next.Failure != "the server stopped" {
		t.Fatalf("follower saw %+v", next)
	}
}
