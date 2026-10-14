package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/pairing"
	"github.com/akonwi/kit/internal/serveraccess"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/version"
)

type deviceHarness struct {
	t *testing.T
	// address is the remote address of requests; 100.64.0.7 by default.
	address string
	handler http.Handler
	devices *devices.Service
	now     time.Time
}

func newDeviceHarness(t *testing.T) *deviceHarness {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	harness := &deviceHarness{t: t, now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	harness.devices, err = devices.NewService(t.Context(), store, devices.Options{Now: func() time.Time { return harness.now }})
	if err != nil {
		t.Fatal(err)
	}
	record := protocol.Scratchpad{OwnerSessionID: "session_0123456789abcdef0123456789abcdef", Content: "shared", Revision: 1, UpdatedAt: "2026-03-23T12:34:56Z"}
	next := newHandler(localHandlerOptions{
		access: serveraccess.NewPolicy([]string{"kit.example.ts.net"}, nil), token: "test",
		registry: localdiscovery.Registry{InstanceID: "instance_test"}, sessions: scratchpadWireTestService{record: record},
		devices: harness.devices, pairing: pairing.NewRegistry(func() time.Time { return harness.now }),
	})
	harness.handler = contractConformanceMiddleware(t, next, func(err error) { t.Errorf("contract violation: %v", err) })
	return harness
}

// do sends a request as credential, the local token "test" by default, and
// decodes a JSON response into out when it is not nil.
func (h *deviceHarness) do(method, path, credential string, body any, out any) (int, string) {
	h.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, "http://kit.example.ts.net"+path, reader)
	request.RemoteAddr = "100.64.0.7:51000"
	if h.address != "" {
		request.RemoteAddr = h.address + ":51000"
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	// "-" sends no credential on an operation that requires one.
	if credential != "" {
		if credential != "-" {
			request.Header.Set("Authorization", "Bearer "+credential)
		}
		request.Header.Set(instanceHeader, "instance_test")
		request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	if out != nil && response.Code < 300 {
		if err := json.Unmarshal(response.Body.Bytes(), out); err != nil {
			h.t.Fatalf("decode %s %s: %v; body = %s", method, path, err, response.Body.String())
		}
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	return response.Code, envelope.Error.Code
}

func (h *deviceHarness) pair(name string) protocol.RedeemPairingResult {
	h.t.Helper()
	var code protocol.PairingCode
	if status, _ := h.do(http.MethodPost, "/v1/pairing", "test", nil, &code); status != http.StatusCreated || code.Validate() != nil {
		h.t.Fatalf("create pairing = %d %+v", status, code)
	}
	var redeemed protocol.RedeemPairingResult
	if status, failure := h.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: code.Code, DeviceName: name}, &redeemed); status != http.StatusCreated {
		h.t.Fatalf("redeem = %d %s", status, failure)
	}
	return redeemed
}

func TestPairingIssuesADeviceTokenThatAuthenticates(t *testing.T) {
	harness := newDeviceHarness(t)
	var code protocol.PairingCode
	harness.do(http.MethodPost, "/v1/pairing", "test", nil, &code)
	if expires, _ := time.Parse(time.RFC3339Nano, code.ExpiresAt); !expires.Equal(harness.now.Add(pairing.CommandLifetime)) {
		t.Fatalf("code expires %s, want five minutes from now", code.ExpiresAt)
	}

	var redeemed protocol.RedeemPairingResult
	status, _ := harness.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: code.Code, DeviceName: "laptop"}, &redeemed)
	if status != http.StatusCreated || redeemed.Validate() != nil || redeemed.Device.Name != "laptop" ||
		redeemed.Device.LastAddress != "100.64.0.7" || !redeemed.Device.Current {
		t.Fatalf("redeem = %d %+v", status, redeemed)
	}
	if status, failure := harness.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: code.Code, DeviceName: "again"}, nil); status != http.StatusUnauthorized || failure != "unauthorized" {
		t.Fatalf("reused code = %d %s", status, failure)
	}

	// The device token reaches session routes and sees itself as current.
	if status, _ := harness.do(http.MethodGet, "/v1/sessions/session_test/scratchpad", redeemed.Token, nil, nil); status != http.StatusOK {
		t.Fatalf("session route with device token = %d", status)
	}
	other := harness.pair("phone")
	var list protocol.DeviceList
	if status, _ := harness.do(http.MethodGet, "/v1/devices", redeemed.Token, nil, &list); status != http.StatusOK || list.Validate() != nil {
		t.Fatalf("list = %d %+v", status, list)
	}
	names := map[string]bool{}
	for _, device := range list.Devices {
		names[device.Name] = device.Current
	}
	if len(names) != 2 || !names["laptop"] || names["phone"] {
		t.Fatalf("devices (name: current) = %v", names)
	}

	// A device can revoke another; the revoked token stops working at once.
	if status, _ := harness.do(http.MethodDelete, "/v1/devices/"+other.Device.ID, redeemed.Token, nil, nil); status != http.StatusNoContent {
		t.Fatalf("revoke = %d", status)
	}
	if status, failure := harness.do(http.MethodGet, "/v1/devices", other.Token, nil, nil); status != http.StatusUnauthorized || failure != "unauthorized" {
		t.Fatalf("revoked token = %d %s", status, failure)
	}
	if status, failure := harness.do(http.MethodDelete, "/v1/devices/"+other.Device.ID, "test", nil, nil); status != http.StatusNotFound || failure != "not_found" {
		t.Fatalf("revoke again = %d %s", status, failure)
	}
}

func TestRedeemRejectsInvalidInputAndCodes(t *testing.T) {
	harness := newDeviceHarness(t)
	var code protocol.PairingCode
	harness.do(http.MethodPost, "/v1/pairing", "test", nil, &code)
	for _, test := range []struct {
		name   string
		input  protocol.RedeemPairingInput
		status int
		code   string
	}{
		{"malformed code", protocol.RedeemPairingInput{Code: "nope", DeviceName: "laptop"}, http.StatusBadRequest, "invalid_request"},
		{"blank name", protocol.RedeemPairingInput{Code: code.Code, DeviceName: " "}, http.StatusBadRequest, "invalid_request"},
		{"unknown code", protocol.RedeemPairingInput{Code: strings.Repeat("a", 26), DeviceName: "laptop"}, http.StatusUnauthorized, "unauthorized"},
	} {
		if status, failure := harness.do(http.MethodPost, "/v1/pairing/redeem", "", test.input, nil); status != test.status || failure != test.code {
			t.Errorf("%s: %d %s, want %d %s", test.name, status, failure, test.status, test.code)
		}
	}
	harness.now = harness.now.Add(pairing.CommandLifetime)
	if status, _ := harness.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: code.Code, DeviceName: "laptop"}, nil); status != http.StatusUnauthorized {
		t.Fatalf("expired code = %d", status)
	}
}

func TestExpiredDeviceTokensReportCredentialExpired(t *testing.T) {
	harness := newDeviceHarness(t)
	device := harness.pair("laptop")
	harness.now = harness.now.Add(devices.IdleLifetime)
	status, failure := harness.do(http.MethodGet, "/v1/devices", device.Token, nil, nil)
	if status != http.StatusUnauthorized || failure != "credential_expired" {
		t.Fatalf("expired token = %d %s", status, failure)
	}
}

func TestOnlyRedeemSkipsTheCredential(t *testing.T) {
	harness := newDeviceHarness(t)
	for _, path := range []string{"/v1/pairing", "/v1/devices"} {
		method := http.MethodPost
		if path == "/v1/devices" {
			method = http.MethodGet
		}
		if status, failure := harness.do(method, path, "-", nil, nil); status != http.StatusUnauthorized || failure != "unauthorized" {
			t.Errorf("%s without a credential = %d %s", path, status, failure)
		}
	}
}

func TestFailuresAreLimitedPerAddressWithoutRefusingValidCredentials(t *testing.T) {
	harness := newDeviceHarness(t)
	device := harness.pair("laptop")
	guess := "kit_device_" + strings.Repeat("A", 43)
	limited := false
	for range failureBurst + 5 {
		if status, failure := harness.do(http.MethodGet, "/v1/devices", guess, nil, nil); status == http.StatusTooManyRequests && failure == string(httpapi.ErrorRateLimited) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("repeated failed authentications were never rate limited")
	}
	// Valid credentials from the limited address still pass.
	for _, credential := range []string{"test", device.Token} {
		if status, _ := harness.do(http.MethodGet, "/v1/devices", credential, nil, nil); status != http.StatusOK {
			t.Fatalf("valid credential from a limited address = %d", status)
		}
	}
	// So does a valid pairing code, while a wrong one is refused as limited.
	var code protocol.PairingCode
	harness.do(http.MethodPost, "/v1/pairing", "test", nil, &code)
	if status, failure := harness.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: strings.Repeat("a", 26), DeviceName: "x"}, nil); status != http.StatusTooManyRequests || failure != "rate_limited" {
		t.Fatalf("wrong code from a limited address = %d %s", status, failure)
	}
	if status, _ := harness.do(http.MethodPost, "/v1/pairing/redeem", "", protocol.RedeemPairingInput{Code: code.Code, DeviceName: "phone"}, nil); status != http.StatusCreated {
		t.Fatalf("valid code from a limited address = %d", status)
	}
	// Another address is unaffected.
	harness.address = "100.64.0.8"
	if status, failure := harness.do(http.MethodGet, "/v1/devices", guess, nil, nil); status != http.StatusUnauthorized || failure != "unauthorized" {
		t.Fatalf("failure from another address = %d %s", status, failure)
	}
}

func TestEncodedRedeemPathsNeedACredential(t *testing.T) {
	harness := newDeviceHarness(t)
	for _, path := range []string{"/v1/pairing%2Fredeem", "/v1/pairing/%72edeem"} {
		// The contract middleware knows only canonical paths, so check the
		// access decision directly.
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+path, strings.NewReader(`{}`))
		if newAccess("test", harness.devices, pairing.NewRegistry(nil)).skipsCredential(request) {
			t.Errorf("%s skips the credential", path)
		}
	}
	if !newAccess("test", harness.devices, pairing.NewRegistry(nil)).skipsCredential(httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/pairing/redeem", nil)) {
		t.Error("the canonical redeem path needs a credential")
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	credentials := newAccess("test", nil, pairing.NewRegistry(nil))
	for _, header := range []string{"Bearer test", "bearer test", "BEARER test"} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/health", nil)
		request.Header.Set("Authorization", header)
		if _, _, ok := credentials.authenticate(httptest.NewRecorder(), request); !ok {
			t.Errorf("%q was rejected", header)
		}
	}
	empty := newAccess("", nil, pairing.NewRegistry(nil))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/health", nil)
	request.Header.Set("Authorization", "Bearer ")
	if _, _, ok := empty.authenticate(httptest.NewRecorder(), request); ok {
		t.Fatal("an empty local token authenticated")
	}
}

func TestRequestsEndWhenTheDeviceTokenExpires(t *testing.T) {
	harness := newDeviceHarness(t)
	device := harness.pair("laptop")
	credentials := newAccess("test", harness.devices, pairing.NewRegistry(nil))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions/s/events", nil)
	request.Header.Set("Authorization", "Bearer "+device.Token)
	authenticated, release, ok := credentials.authenticate(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("device token was rejected")
	}
	defer release()
	deadline, ok := authenticated.Context().Deadline()
	if want, _ := time.Parse(time.RFC3339Nano, device.Device.ExpiresAt); !ok || !deadline.Equal(want) {
		t.Fatalf("request deadline = %v, %v; want the token's expiry %v", deadline, ok, want)
	}
}

func TestRevocationEndsTheDevicesOpenRequests(t *testing.T) {
	harness := newDeviceHarness(t)
	device := harness.pair("laptop")
	credentials := newAccess("test", harness.devices, pairing.NewRegistry(nil))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/sessions/s/events", nil)
	request.Header.Set("Authorization", "Bearer "+device.Token)
	authenticated, release, ok := credentials.authenticate(httptest.NewRecorder(), request)
	if !ok {
		t.Fatal("device token was rejected")
	}
	defer release()
	if requestDevice(authenticated.Context()) != device.Device.ID {
		t.Fatalf("request device = %q", requestDevice(authenticated.Context()))
	}
	if err := harness.devices.Revoke(context.Background(), device.Device.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-authenticated.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("revocation did not end the open request")
	}
}

func TestStartupPairingIsAnnouncedOnlyWhileServingWithoutDevices(t *testing.T) {
	harness := newDeviceHarness(t)
	var logs bytes.Buffer
	logger := newTextLogger(&logs)
	allowed := accessSettings{hosts: []string{"kit.example.ts.net"}}
	registry := pairing.NewRegistry(func() time.Time { return harness.now })

	if err := announcePairing(logger, harness.devices, registry, false, allowed, "http://127.0.0.1:7777"); err != nil || logs.Len() != 0 {
		t.Fatalf("loopback-only daemon announced: %v %q", err, logs.String())
	}
	if err := announcePairing(logger, harness.devices, registry, true, allowed, "http://127.0.0.1:7777"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "link=https://kit.example.ts.net/pair#") || !strings.Contains(lines[1], "link=http://127.0.0.1:7777/pair#") {
		t.Fatalf("startup links:\n%s", logs.String())
	}
	_, code, err := pairing.ParseLink(strings.Fields(lines[0][strings.Index(lines[0], "link="):])[0][len("link="):])
	if err != nil {
		t.Fatal(err)
	}
	harness.now = harness.now.Add(pairing.StartupLifetime - time.Minute)
	if err := registry.Redeem(code); err != nil {
		t.Fatalf("startup code within an hour: %v", err)
	}

	harness.pair("laptop")
	logs.Reset()
	if err := announcePairing(logger, harness.devices, registry, true, allowed, "http://127.0.0.1:7777"); err != nil || logs.Len() != 0 {
		t.Fatalf("paired daemon announced: %v %q", err, logs.String())
	}
}

func newTextLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
