package kit

import (
	"context"
	"errors"
	"strings"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/pairing"
)

type deviceTransport interface {
	CreatePairing(context.Context) (protocol.PairingCode, error)
	ListDevices(context.Context) (protocol.DeviceList, error)
	RevokeDevice(context.Context, string) error
}

// Pairing is a single-use code another device redeems, with Link, for a
// device token.
type Pairing struct {
	// Code is the pairing code.
	Code string
	// ExpiresAt is when the code stops being accepted.
	ExpiresAt time.Time
	// URLs are the HTTP(S) origins the server reports it is served at, each
	// usable with Link: allowed hosts first, then its loopback URL, which only
	// the server's machine can reach. Older servers report none.
	URLs []string
}

// Link returns the pairing link that reaches the server at serverURL, an
// HTTP(S) origin, as `https://host/pair#CODE`.
func (p Pairing) Link(serverURL string) string { return pairing.Link(serverURL, p.Code) }

// CreatePairing issues a pairing code that another device redeems with Pair.
// The code is valid for five minutes.
func (c *Client) CreatePairing(ctx context.Context) (Pairing, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return Pairing{}, err
	}
	defer cancel()
	if c.devices == nil {
		return Pairing{}, errors.New("device transport is unavailable")
	}
	code, err := c.devices.CreatePairing(operation)
	if err != nil {
		return Pairing{}, projectError(err)
	}
	expires, err := time.Parse(time.RFC3339Nano, code.ExpiresAt)
	if err != nil {
		return Pairing{}, &ProtocolError{Err: err}
	}
	return Pairing{Code: code.Code, ExpiresAt: expires, URLs: code.URLs}, nil
}

// Devices lists the server's paired devices, most recently used first.
func (c *Client) Devices(ctx context.Context) ([]DeviceInfo, error) {
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if c.devices == nil {
		return nil, errors.New("device transport is unavailable")
	}
	list, err := c.devices.ListDevices(operation)
	if err != nil {
		return nil, projectError(err)
	}
	return list.Devices, nil
}

// RevokeDevice revokes a paired device by ID; its token stops working and its
// open requests end. Revoking an unknown device fails with ErrorNotFound.
func (c *Client) RevokeDevice(ctx context.Context, deviceID string) error {
	if !identifier.Valid(deviceID, "device_") {
		return errors.New("device id is not canonical")
	}
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.devices == nil {
		return errors.New("device transport is unavailable")
	}
	return projectError(c.devices.RevokeDevice(operation, deviceID))
}

// PairedDevice is the result of pairing this device with a server.
type PairedDevice struct {
	// ServerURL is the server's HTTP(S) origin from the pairing link.
	ServerURL string
	// Token is the device token. The server returns it only once; store it
	// securely and connect with Endpoint(ServerURL, AccessToken(Token)).
	Token string
	// Device describes the new device.
	Device DeviceInfo
}

// Pair redeems a pairing link, `https://host/pair#CODE`, for a device token
// named deviceName. It needs no credential; the code is single-use.
func Pair(ctx context.Context, link, deviceName string) (PairedDevice, error) {
	if ctx == nil {
		return PairedDevice{}, errors.New("pair context is nil")
	}
	serverURL, code, err := pairing.ParseLink(link)
	if err != nil {
		return PairedDevice{}, err
	}
	result, err := clienttransport.RedeemPairing(ctx, serverURL, protocol.RedeemPairingInput{Code: code, DeviceName: strings.TrimSpace(deviceName)})
	if err != nil {
		return PairedDevice{}, projectError(err)
	}
	return PairedDevice{ServerURL: serverURL, Token: result.Token, Device: result.Device}, nil
}
