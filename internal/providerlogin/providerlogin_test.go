package providerlogin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeFlows run logins whose outcome a test decides.
type fakeFlows struct {
	// startErr fails a flow before it reports instructions.
	startErr error
	// outcome receives each device login's result.
	outcome chan error
	// stopped receives each flow's ctx error when it ends early.
	stopped chan error
	// exchanging, when set, holds a browser flow after it reads its
	// authorization until the channel closes.
	exchanging chan struct{}
}

func newFakeFlows() *fakeFlows {
	return &fakeFlows{outcome: make(chan error, 1), stopped: make(chan error, 4)}
}

func (f *fakeFlows) flows() Flows {
	return Flows{
		Device: func(ctx context.Context, provider string, notify func(DeviceInstructions)) error {
			if f.startErr != nil {
				return f.startErr
			}
			notify(DeviceInstructions{VerificationURI: "https://auth.example.com/device", UserCode: "ABCD-1234", ExpiresAt: time.Unix(1_900_000_000, 0)})
			select {
			case err := <-f.outcome:
				return err
			case <-ctx.Done():
				f.stopped <- ctx.Err()
				return ctx.Err()
			}
		},
		Browser: func(ctx context.Context, provider string, authorization <-chan string, notify func(BrowserInstructions)) error {
			notify(BrowserInstructions{AuthorizationURL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://localhost:53692/callback"})
			select {
			case input := <-authorization:
				if f.exchanging != nil {
					<-f.exchanging
				}
				if input != "code#s" {
					return errors.New("Anthropic OAuth state mismatch")
				}
				return nil
			case <-ctx.Done():
				f.stopped <- ctx.Err()
				return ctx.Err()
			}
		},
	}
}

func waitFor(t *testing.T, registry *Registry, id string, want State) Snapshot {
	t.Helper()
	snapshot, err := registry.Wait(t.Context(), id, 5*time.Second)
	if err != nil || snapshot.State != want {
		t.Fatalf("Wait(%s) = %+v, %v; want state %s", id, snapshot, err, want)
	}
	return snapshot
}

func TestADeviceLoginReportsInstructionsThenSucceeds(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	started, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != StatePending || started.Device == nil || started.Device.UserCode != "ABCD-1234" || started.Browser != nil ||
		!strings.HasPrefix(started.ID, "login_") {
		t.Fatalf("started = %+v", started)
	}
	if pending, err := registry.Wait(t.Context(), started.ID, 10*time.Millisecond); err != nil || pending.State != StatePending {
		t.Fatalf("short wait = %+v, %v", pending, err)
	}
	fake.outcome <- nil
	waitFor(t, registry, started.ID, StateSucceeded)
}

func TestAFailedLoginExplainsWhy(t *testing.T) {
	fake := newFakeFlows()
	fake.startErr = errors.New("start device authorization: 503\nService\tUnavailable")
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	started, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != StateFailed || started.Failure != "start device authorization: 503 Service Unavailable" || started.Device != nil {
		t.Fatalf("started = %+v", started)
	}
}

func TestANewLoginSupersedesThePendingOne(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	first, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil || second.State != StatePending {
		t.Fatalf("second = %+v, %v", second, err)
	}
	waitFor(t, registry, first.ID, StateSuperseded)
	if err := <-fake.stopped; !errors.Is(err, context.Canceled) {
		t.Fatalf("superseded flow stopped with %v", err)
	}
	fake.outcome <- nil
	waitFor(t, registry, second.ID, StateSucceeded)
}

func TestCancellingEndsTheFlow(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	started, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Cancel(started.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, registry, started.ID, StateCancelled)
	if err := <-fake.stopped; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled flow stopped with %v", err)
	}
	// Cancelling a finished login changes nothing.
	if err := registry.Cancel(started.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, registry, started.ID, StateCancelled)
	if err := registry.Cancel("login_00000000000000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel(unknown) = %v", err)
	}
}

func TestALoginExpires(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	registry.lifetime = 50 * time.Millisecond
	defer registry.Close()
	started, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, registry, started.ID, StateExpired)
}

func TestABrowserLoginCompletesWithItsAuthorization(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	started, err := registry.Start(t.Context(), "anthropic", MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if started.Browser == nil || started.Browser.RedirectURI != "http://localhost:53692/callback" || started.Device != nil {
		t.Fatalf("started = %+v", started)
	}
	if err := registry.Submit(started.ID, "code#s"); err != nil {
		t.Fatal(err)
	}
	if succeeded := waitFor(t, registry, started.ID, StateSucceeded); succeeded.Browser != nil {
		t.Fatalf("a finished login kept its instructions: %+v", succeeded.Browser)
	}
	if err := registry.Submit(started.ID, "code#s"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("Submit after success = %v", err)
	}

	rejected, err := registry.Start(t.Context(), "anthropic", MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Submit(rejected.ID, "code#forged"); err != nil {
		t.Fatal(err)
	}
	if failed := waitFor(t, registry, rejected.ID, StateFailed); failed.Failure != "Anthropic OAuth state mismatch" {
		t.Fatalf("failure = %q", failed.Failure)
	}
}

func TestOnlyPendingBrowserLoginsTakeAnAuthorization(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	device, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Submit(device.ID, "code"); !errors.Is(err, ErrNotBrowser) {
		t.Fatalf("Submit(device) = %v", err)
	}
	if err := registry.Submit("login_00000000000000000000000000000000", "code"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Submit(unknown) = %v", err)
	}
	if err := registry.Submit(device.ID, strings.Repeat("a", MaxAuthorizationBytes+1)); err == nil {
		t.Fatal("an oversized authorization was accepted")
	}
}

func TestStartRejectsUnknownProvidersAndMethods(t *testing.T) {
	registry := NewRegistry(newFakeFlows().flows(), nil)
	defer registry.Close()
	for _, test := range []struct {
		provider string
		method   Method
		want     error
	}{
		{provider: "nope", method: MethodDevice, want: ErrUnknownProvider},
		{provider: "anthropic", method: MethodDevice, want: ErrUnsupportedMethod},
		{provider: "openai", method: MethodAPIKey, want: ErrUnsupportedMethod},
	} {
		if _, err := registry.Start(t.Context(), test.provider, test.method); !errors.Is(err, test.want) {
			t.Errorf("Start(%s, %s) = %v, want %v", test.provider, test.method, err, test.want)
		}
	}
}

// A login takes one authorization; a second, sent while the first is being
// exchanged, is refused rather than silently dropped.
func TestABrowserLoginTakesOneAuthorization(t *testing.T) {
	fake := newFakeFlows()
	fake.exchanging = make(chan struct{})
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	started, err := registry.Start(t.Context(), "anthropic", MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Submit(started.ID, "code#s"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Submit(started.ID, "code#s"); !errors.Is(err, ErrSubmitted) {
		t.Fatalf("second Submit = %v, want ErrSubmitted", err)
	}
	close(fake.exchanging)
	waitFor(t, registry, started.ID, StateSucceeded)
}

func TestCancellingAProviderEndsItsPendingLogin(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	defer registry.Close()
	codex, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	claude, err := registry.Start(t.Context(), "anthropic", MethodBrowser)
	if err != nil {
		t.Fatal(err)
	}
	registry.CancelProvider("openai-codex")
	if cancelled := waitFor(t, registry, codex.ID, StateCancelled); cancelled.Device != nil {
		t.Fatalf("a cancelled login kept its instructions: %+v", cancelled.Device)
	}
	if other, err := registry.Get(claude.ID); err != nil || other.State != StatePending {
		t.Fatalf("other provider's login = %+v, %v", other, err)
	}
}

func TestClosingFailsPendingLoginsAndWaitsForTheirFlows(t *testing.T) {
	fake := newFakeFlows()
	registry := NewRegistry(fake.flows(), nil)
	started, err := registry.Start(t.Context(), "openai-codex", MethodDevice)
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan Snapshot, 1)
	go func() {
		snapshot, _ := registry.Wait(context.Background(), started.ID, MaxWait)
		waited <- snapshot
	}()
	registry.Close()
	// Close returned, so the flow has already ended.
	select {
	case err := <-fake.stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("flow stopped with %v", err)
		}
	default:
		t.Fatal("Close returned before the login's flow ended")
	}
	select {
	case failed := <-waited:
		if failed.State != StateFailed || failed.Failure != "the server stopped" {
			t.Fatalf("waiter saw %+v", failed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a waiting follower was not released by Close")
	}
	if _, err := registry.Start(t.Context(), "openai-codex", MethodDevice); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close = %v, want ErrClosed", err)
	}
	registry.Close()
}

func TestFailureTextIsBoundedAndPrintable(t *testing.T) {
	long := strings.Repeat("é", maxFailureBytes)
	text := failureText(errors.New(long))
	if len(text) > maxFailureBytes || !strings.HasPrefix(long, text) || text == "" {
		t.Fatalf("failureText kept %d bytes: %q", len(text), text)
	}
	if got := failureText(errors.New("\x1b[31m\n")); got != "[31m" {
		t.Fatalf("failureText(escape) = %q", got)
	}
}
