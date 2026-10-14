package clienttransport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/httpapi"
)

// CreatePairing issues a pairing code for another device.
func (c *Client) CreatePairing(ctx context.Context) (protocol.PairingCode, error) {
	output, err := httpapi.Call(ctx, c, httpapi.CreatePairing, httpapi.ServerPath{}, httpapi.NoBody{})
	if err != nil {
		return protocol.PairingCode{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.PairingCode{}, protocolErrorf("validate pairing code: %w", err)
	}
	return output, nil
}

// ListDevices lists the server's paired devices.
func (c *Client) ListDevices(ctx context.Context) (protocol.DeviceList, error) {
	output, err := httpapi.Call(ctx, c, httpapi.ListDevices, httpapi.ServerPath{}, httpapi.NoBody{})
	if err != nil {
		return protocol.DeviceList{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.DeviceList{}, protocolErrorf("validate device list: %w", err)
	}
	return output, nil
}

// RevokeDevice revokes one paired device.
func (c *Client) RevokeDevice(ctx context.Context, deviceID string) error {
	_, err := httpapi.Call(ctx, c, httpapi.RevokeDevice, httpapi.DevicePath{DeviceID: deviceID}, httpapi.NoBody{})
	return err
}

// redeemTimeout bounds one redemption when the caller sets no deadline.
const redeemTimeout = 30 * time.Second

// anonymous sends requests to an explicit server without a credential or an
// instance, as redeeming a pairing code requires.
type anonymous struct {
	url  string
	http *http.Client
}

func (a anonymous) DoSessionRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, a.url+path, body)
	if err != nil {
		return nil, fmt.Errorf("create pairing request: %w", err)
	}
	if hasJSONBody {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := a.http.Do(request)
	if err != nil {
		return nil, transportError("redeem pairing code", err)
	}
	return response, nil
}

// RedeemPairing exchanges a pairing code at the server at url, an HTTP(S)
// origin, for a device token.
func RedeemPairing(ctx context.Context, url string, input protocol.RedeemPairingInput) (protocol.RedeemPairingResult, error) {
	if err := input.Validate(); err != nil {
		return protocol.RedeemPairingResult{}, fmt.Errorf("validate pairing redemption: %w", err)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, redeemTimeout)
		defer cancel()
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	output, err := httpapi.Call(ctx, anonymous{url: url, http: client}, httpapi.RedeemPairing, httpapi.ServerPath{}, input)
	if err != nil {
		return protocol.RedeemPairingResult{}, err
	}
	if err := output.Validate(); err != nil {
		return protocol.RedeemPairingResult{}, protocolErrorf("validate pairing redemption: %w", err)
	}
	if output.Device.Name != input.DeviceName || !devices.IsToken(output.Token) {
		return protocol.RedeemPairingResult{}, protocolErrorf("pairing redemption does not match the request")
	}
	return output, nil
}
