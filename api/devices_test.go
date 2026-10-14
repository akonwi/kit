package kit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

type fakeDevices struct {
	code    protocol.PairingCode
	list    protocol.DeviceList
	revoked string
	err     error
}

func (f *fakeDevices) CreatePairing(context.Context) (protocol.PairingCode, error) {
	return f.code, f.err
}
func (f *fakeDevices) ListDevices(context.Context) (protocol.DeviceList, error) { return f.list, f.err }
func (f *fakeDevices) RevokeDevice(_ context.Context, id string) error {
	f.revoked = id
	return f.err
}

const testDeviceID = "device_0123456789abcdef0123456789abcdef"

func TestClientCreatesPairingsAndManagesDevices(t *testing.T) {
	fake := &fakeDevices{
		code: protocol.PairingCode{Code: strings.Repeat("a", 26), ExpiresAt: "2026-03-01T12:05:00Z"},
		list: protocol.DeviceList{Devices: []protocol.DeviceInfo{{ID: testDeviceID, Name: "laptop"}}},
	}
	client := newClient(&fakeClientBackend{}, nil)
	client.devices = fake
	t.Cleanup(func() { _ = client.Close() })

	pairing, err := client.CreatePairing(t.Context())
	if err != nil || !pairing.ExpiresAt.Equal(time.Date(2026, 3, 1, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("CreatePairing() = %+v, %v", pairing, err)
	}
	if link := pairing.Link("https://kit.example.ts.net"); link != "https://kit.example.ts.net/pair#"+strings.Repeat("a", 26) {
		t.Fatalf("Link() = %q", link)
	}
	if devices, err := client.Devices(t.Context()); err != nil || len(devices) != 1 || devices[0].Name != "laptop" {
		t.Fatalf("Devices() = %+v, %v", devices, err)
	}
	if err := client.RevokeDevice(t.Context(), testDeviceID); err != nil || fake.revoked != testDeviceID {
		t.Fatalf("RevokeDevice() = %v, revoked %q", err, fake.revoked)
	}
	if err := client.RevokeDevice(t.Context(), "laptop"); err == nil {
		t.Fatal("RevokeDevice accepted a non-canonical ID")
	}
}

func TestPairRedeemsALinkWithoutACredential(t *testing.T) {
	code := strings.Repeat("b", 26)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/pairing/redeem" || request.Method != http.MethodPost ||
			request.Header.Get("Authorization") != "" || request.Header.Get("X-Kit-Instance-ID") != "" {
			t.Errorf("request = %s %s %v", request.Method, request.URL.Path, request.Header)
		}
		var input protocol.RedeemPairingInput
		_ = json.NewDecoder(request.Body).Decode(&input)
		if input.Code != code || input.DeviceName != "laptop" {
			t.Errorf("input = %+v", input)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(protocol.RedeemPairingResult{
			Token: "kit_device_" + strings.Repeat("A", 43),
			Device: protocol.DeviceInfo{ID: testDeviceID, Name: "laptop", CreatedAt: "2026-03-01T12:00:00Z",
				LastUsedAt: "2026-03-01T12:00:00Z", ExpiresAt: "2026-03-31T12:00:00Z", Current: true},
		})
	}))
	defer server.Close()

	paired, err := Pair(t.Context(), server.URL+"/pair#"+code, " laptop ")
	if err != nil {
		t.Fatalf("Pair() error = %v", err)
	}
	if paired.ServerURL != server.URL || paired.Token != "kit_device_"+strings.Repeat("A", 43) || paired.Device.ID != testDeviceID {
		t.Fatalf("Pair() = %+v", paired)
	}
	if _, err := Pair(t.Context(), server.URL+"/pair", "laptop"); err == nil {
		t.Fatal("Pair accepted a link without a code")
	}
}

func TestPairReportsARejectedCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"code":"unauthorized","message":"pairing code is invalid or expired"}}`))
	}))
	defer server.Close()
	_, err := Pair(t.Context(), server.URL+"/pair#"+strings.Repeat("c", 26), "laptop")
	var failure *ServerError
	if !errors.As(err, &failure) || failure.Code != ErrorUnauthorized {
		t.Fatalf("Pair() error = %v, want an unauthorized ServerError", err)
	}
}

func TestConnectingWithAnExpiredDeviceTokenAsksToPairAgain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"code":"credential_expired","message":"device token expired; pair this device again"}}`))
	}))
	defer server.Close()
	_, err := Connect(t.Context(), Endpoint(server.URL, AccessToken("kit_device_"+strings.Repeat("A", 43))))
	var failure *ServerError
	if !errors.As(err, &failure) || failure.Code != ErrorCredentialExpired {
		t.Fatalf("Connect() error = %v, want credential_expired", err)
	}
}
