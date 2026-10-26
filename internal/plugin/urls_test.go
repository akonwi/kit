package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func TestOpenURLParamsAcceptWebAddressesInCanonicalForm(t *testing.T) {
	for raw, want := range map[string]string{
		`{"url":"https://example.com/docs?q=1#top"}`: "https://example.com/docs?q=1#top",
		`{"url":"HTTP://localhost:3000/"}`:           "http://localhost:3000/",
		`{"url":"https://[::1]:8443/a"}`:             "https://[::1]:8443/a",
	} {
		got, err := parseOpenURL(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("parseOpenURL(%s) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestOpenURLParamsRejectInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		raw     string
		code    int64
		message string
	}{
		{`{}`, -32602, "Missing required parameter: url"},
		{`{"url":null}`, -32602, "Missing required parameter: url"},
		{`{"url":1}`, -32602, "Invalid parameter: url"},
		{`{"url":""}`, -32602, "Invalid parameter: url is required"},
		{`{"url":"file:///etc/passwd"}`, -32602, "Invalid parameter: url must use http or https"},
		{`{"url":"javascript:alert(1)"}`, -32602, "Invalid parameter: url must use http or https"},
		{`{"url":"https:///path"}`, -32602, "Invalid parameter: url must include a host"},
		{`{"url":"https://user:secret@example.com/"}`, -32602, "Invalid parameter: url must not include credentials"},
		{`{"url":"https://example.com/a b"}`, -32602, "Invalid parameter: url must not contain whitespace or control characters"},
		{`{"url":"https://example.com/` + strings.Repeat("a", 2048) + `"}`, -32602, "Invalid parameter: url is too long"},
		{`{"url":"https://example.com/","sessionId":"session_other"}`, -32602, "Unknown parameter"},
		{`["https://example.com/"]`, -32602, "Params must be an object"},
		{`{"url":"https://example.com/` + strings.Repeat("a", maxOpenURLParamsBytes) + `"}`, -32005, "Contribution parameters exceed protocol limits"},
	} {
		_, err := parseOpenURL(json.RawMessage(test.raw))
		var failure *RPCError
		if !errors.As(err, &failure) || failure.Code != test.code || failure.Message != test.message {
			t.Errorf("parseOpenURL(%.48s) = %v; want %d %q", test.raw, err, test.code, test.message)
		}
	}
}

func openURLHost(t *testing.T, open func(context.Context, URLRequest) error) (*Host, InstanceID) {
	t.Helper()
	home := apphome.FromHome(t.TempDir())
	hostInstallation(t, home.Plugins, "demo", "demo", "commands")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "one"}, OpenURL: open})
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
	return host, host.Commands()[0].Owner
}

func TestOpenURLForwardsTheCanonicalAddressOfTheCurrentGeneration(t *testing.T) {
	var mu sync.Mutex
	var requests []URLRequest
	host, owner := openURLHost(t, func(_ context.Context, request URLRequest) error {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, request)
		return nil
	})
	result, err := host.handleRequest(t.Context(), owner, "kit/system/open-url", json.RawMessage(`{"url":"HTTPS://example.com/docs"}`))
	if err != nil || string(result) != "null" {
		t.Fatalf("open = %s, %v; want null", result, err)
	}
	host.Reload()
	_, err = host.handleRequest(t.Context(), owner, "kit/system/open-url", json.RawMessage(`{"url":"https://example.com/old"}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32002 || failure.Message != "Plugin generation is unavailable" {
		t.Fatalf("revoked generation = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []URLRequest{{Owner: owner, URL: "https://example.com/docs"}}; !reflect.DeepEqual(requests, want) {
		t.Fatalf("session requests = %+v, want %+v", requests, want)
	}
}

func TestOpenURLReportsSessionErrorsAndMissingPort(t *testing.T) {
	closed := &RPCError{Code: -32002, Message: "Plugin generation is unavailable"}
	host, owner := openURLHost(t, func(context.Context, URLRequest) error { return closed })
	// Rejected requests publish nothing and so never count toward the limit.
	for range MaxOpenURLRequests + 1 {
		if _, err := host.handleRequest(t.Context(), owner, "kit/system/open-url", json.RawMessage(`{"url":"https://example.com/"}`)); !errors.Is(err, closed) {
			t.Fatalf("session error = %v, want %v", err, closed)
		}
	}
	unavailable, unavailableOwner := openURLHost(t, nil)
	_, err := unavailable.handleRequest(t.Context(), unavailableOwner, "kit/system/open-url", json.RawMessage(`{"url":"https://example.com/"}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32601 || failure.Message != "Opening URLs is unavailable" {
		t.Fatalf("missing port = %v", err)
	}
}

func TestOpenURLLimitsRequestsPerSessionWindow(t *testing.T) {
	var mu sync.Mutex
	var opened []string
	host, owner := openURLHost(t, func(_ context.Context, request URLRequest) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, request.URL)
		return nil
	})
	start := time.Unix(1000, 0)
	now := start
	host.mu.Lock()
	host.now = func() time.Time { return now }
	host.mu.Unlock()
	open := func(path string) error {
		_, err := host.handleRequest(t.Context(), owner, "kit/system/open-url", json.RawMessage(`{"url":"https://example.com/`+path+`"}`))
		return err
	}
	for index := range MaxOpenURLRequests {
		if err := open(string(rune('a' + index))); err != nil {
			t.Fatalf("request %d = %v", index, err)
		}
	}
	now = start.Add(OpenURLWindow - time.Millisecond)
	err := open("limited")
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32006 || failure.Message != "Too many URL requests; try again later" || string(failure.Data) != `{"reason":"rate_limited"}` {
		t.Fatalf("request over the limit = %v", err)
	}
	now = start.Add(OpenURLWindow)
	if err := open("later"); err != nil {
		t.Fatalf("request after the window = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"https://example.com/a", "https://example.com/b", "https://example.com/c", "https://example.com/d", "https://example.com/e", "https://example.com/later"}
	if !reflect.DeepEqual(opened, want) {
		t.Fatalf("opened = %v, want %v", opened, want)
	}
}
