package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	kit "github.com/akonwi/kit/api"
)

// DeviceLoginInstructions tell the user where to enter a device code.
type DeviceLoginInstructions struct {
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
}

// BrowserLoginInstructions tell the user which page authorizes a login.
type BrowserLoginInstructions struct {
	AuthorizationURL string
	RedirectURI      string
	// ReceivesRedirect reports that this client listens for the browser's
	// redirect. When false, the user must paste the redirect URL or code.
	ReceivesRedirect bool
}

// The providers the terminal client's interactive logins are for.
const (
	deviceLoginProvider  = "openai-codex"
	browserLoginProvider = "anthropic"
)

// serverDeviceLogin logs the attached server in to OpenAI Codex with a
// device code. The server runs the login and stores the credential.
type serverDeviceLogin struct{ client *kit.Client }

// serverBrowserLogin logs the attached server in to a Claude subscription.
// The browser redirects to this machine, which relays the redirect to the
// server; the server exchanges it for a credential and stores it.
type serverBrowserLogin struct {
	client *kit.Client
	// listen binds the redirect's address; tests replace it.
	listen func(network, address string) (net.Listener, error)
}

// serverAPIKeyLogin saves API keys on the attached server.
type serverAPIKeyLogin struct{ client *kit.Client }

func (l serverDeviceLogin) Login(ctx context.Context, notify func(DeviceLoginInstructions) error) error {
	login, err := l.client.StartProviderLogin(ctx, deviceLoginProvider, kit.LoginMethodDevice)
	if err != nil {
		return loginError(err)
	}
	if login.State != kit.LoginStatePending || login.Device == nil {
		return loginOutcome(login)
	}
	expires, _ := time.Parse(time.RFC3339Nano, login.Device.ExpiresAt)
	if err := notify(DeviceLoginInstructions{VerificationURI: login.Device.VerificationURI, UserCode: login.Device.UserCode, ExpiresAt: expires}); err != nil {
		cancelLogin(l.client, login.ID)
		return err
	}
	return followLogin(ctx, l.client, login)
}

func (l serverBrowserLogin) Login(ctx context.Context, manual <-chan string, notify func(BrowserLoginInstructions) error) error {
	login, err := l.client.StartProviderLogin(ctx, browserLoginProvider, kit.LoginMethodBrowser)
	if err != nil {
		return loginError(err)
	}
	if login.State != kit.LoginStatePending || login.Browser == nil {
		return loginOutcome(login)
	}
	follow, stopFollowing := context.WithCancel(ctx)
	defer stopFollowing()
	// Without the redirect, the user pastes the redirect URL or the code the
	// page shows.
	relayed := make(chan string, 1)
	listen := l.listen
	if listen == nil {
		listen = net.Listen
	}
	stop, relayErr := relayRedirect(listen, *login.Browser, relayed)
	if relayErr == nil {
		defer stop()
	}
	if err := notify(BrowserLoginInstructions{
		AuthorizationURL: login.Browser.AuthorizationURL, RedirectURI: login.Browser.RedirectURI, ReceivesRedirect: relayErr == nil,
	}); err != nil {
		cancelLogin(l.client, login.ID)
		return err
	}
	submitFailed := make(chan error, 1)
	go func() {
		var authorization string
		select {
		case authorization = <-relayed:
		case authorization = <-manual:
		case <-follow.Done():
			return
		}
		// The server rejecting the authorization fails the login, which
		// followLogin reports. Failing to deliver it ends the login here, so
		// it does not sit pending until it expires.
		if err := l.client.SubmitProviderLoginAuthorization(follow, login.ID, authorization); err != nil && follow.Err() == nil {
			submitFailed <- err
			stopFollowing()
			return
		}
		// The login takes one authorization; discard later pastes so their
		// sender never blocks.
		for {
			select {
			case <-manual:
			case <-follow.Done():
				return
			}
		}
	}()
	err = followLogin(follow, l.client, login)
	select {
	case failure := <-submitFailed:
		return loginError(failure)
	default:
		return err
	}
}

func (l serverAPIKeyLogin) Login(ctx context.Context, providerID, apiKey string) error {
	return loginError(l.client.SetProviderAPIKey(ctx, providerID, apiKey))
}

// serverLogout removes credentials stored on the attached server.
type serverLogout struct{ client *kit.Client }

func (l serverLogout) Logout(ctx context.Context, providerID string) error {
	return loginError(l.client.Logout(ctx, providerID))
}

// ReceiveRedirect listens at redirectURI, which must be on this machine's
// loopback interface, for the browser's redirect after it opens
// authorizationURL, and sends the redirect's URL to relayed. It returns how
// to stop listening.
func ReceiveRedirect(authorizationURL, redirectURI string, relayed chan<- string) (func(), error) {
	redirect, err := url.Parse(redirectURI)
	if err != nil {
		return nil, err
	}
	if host := redirect.Hostname(); host != "localhost" {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return nil, errors.New("the redirect is not a loopback HTTP address")
		}
	}
	return relayRedirect(net.Listen, kit.BrowserLoginInstructions{AuthorizationURL: authorizationURL, RedirectURI: redirectURI}, relayed)
}

// relayRedirect receives the browser's redirect at the address instructions
// name and relays its URL. It accepts only a redirect carrying the login's
// state, so other local pages cannot complete the login. A localhost redirect
// is received on both loopback addresses, since browsers may resolve
// localhost to either; the IPv4 one is required.
func relayRedirect(listen func(string, string) (net.Listener, error), instructions kit.BrowserLoginInstructions, relayed chan<- string) (func(), error) {
	redirect, err := url.Parse(instructions.RedirectURI)
	if err != nil || redirect.Scheme != "http" || redirect.Port() == "" {
		return nil, errors.New("the redirect is not a loopback HTTP address")
	}
	authorization, err := url.Parse(instructions.AuthorizationURL)
	if err != nil {
		return nil, err
	}
	state := authorization.Query().Get("state")
	if state == "" {
		return nil, errors.New("the authorization URL carries no state")
	}
	host := redirect.Hostname()
	optional := ""
	if host == "localhost" {
		host, optional = "127.0.0.1", "::1"
	}
	listener, err := listen("tcp", net.JoinHostPort(host, redirect.Port()))
	if err != nil {
		return nil, err
	}
	listeners := []net.Listener{listener}
	if optional != "" {
		// A host without IPv6 loopback cannot have the browser use it.
		if extra, err := listen("tcp", net.JoinHostPort(optional, redirect.Port())); err == nil {
			listeners = append(listeners, extra)
		}
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if request.URL.Path != redirect.Path || query.Get("state") != state {
			http.Error(writer, "This is not the redirect Kit is waiting for.", http.StatusBadRequest)
			return
		}
		received := *redirect
		received.RawQuery = request.URL.RawQuery
		select {
		case relayed <- received.String():
		default:
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<!doctype html><title>Kit</title><p>Kit received the authorization. You can close this window.</p>"))
	})}
	for _, listener := range listeners {
		go func() { _ = server.Serve(listener) }()
	}
	return func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}, nil
}

// followLogin waits for a login to finish, cancelling it on the server when
// ctx ends first.
func followLogin(ctx context.Context, client *kit.Client, login kit.ProviderLogin) error {
	for login.State == kit.LoginStatePending {
		next, err := client.WaitProviderLogin(ctx, login.ID, kit.MaxLoginWait)
		if err != nil {
			cancelLogin(client, login.ID)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return loginError(err)
		}
		login = next
	}
	return loginOutcome(login)
}

// cancelLogin cancels a login the client is abandoning.
func cancelLogin(client *kit.Client, loginID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.CancelProviderLogin(ctx, loginID)
}

// loginOutcome explains how a finished login ended; success is nil.
func loginOutcome(login kit.ProviderLogin) error {
	switch login.State {
	case kit.LoginStateSucceeded:
		return nil
	case kit.LoginStateFailed:
		return errors.New(login.Failure)
	case kit.LoginStateExpired:
		return errors.New("the login expired; start it again")
	case kit.LoginStateCancelled:
		return errors.New("the login was cancelled")
	case kit.LoginStateSuperseded:
		return errors.New("another login to this provider started and replaced this one")
	default:
		return fmt.Errorf("the login ended in an unknown state %q", login.State)
	}
}

// loginError reports a server's refusal by its message alone.
func loginError(err error) error {
	var failure *kit.ServerError
	if errors.As(err, &failure) && failure.Message != "" {
		return errors.New(failure.Message)
	}
	return err
}
