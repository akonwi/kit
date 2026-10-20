package contract

import (
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/pairing"
)

// MaxDevices bounds a device list crossing a transport boundary; a server
// keeps at most this many devices.
const MaxDevices = devices.MaxDevices

// DeviceInfo describes one paired device. Its token is never returned after
// pairing.
type DeviceInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
	// LastAddress is the network address of the device's latest recorded
	// request as the server saw it; a proxy's address when one forwards it.
	LastAddress string `json:"lastAddress,omitempty"`
	// ExpiresAt is when the token expires unless it is used again: the earlier
	// of 30 days after its last use and one year after pairing.
	ExpiresAt string `json:"expiresAt"`
	// Current marks the device whose token made the request.
	Current bool `json:"current"`
}

// DeviceList lists paired devices, most recently used first.
type DeviceList struct {
	Devices []DeviceInfo `json:"devices"`
}

// MaxPairingURLs bounds the served URLs a pairing code lists.
const MaxPairingURLs = 32

// PairingCode is a single-use code another device redeems for a device token.
type PairingCode struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
	// URLs are the HTTP(S) origins the server is served at, each of which
	// joins the code into a pairing link: allowed hosts first, then its
	// loopback URL, which only its own machine can reach.
	URLs []string `json:"urls,omitempty"`
}

// RedeemPairingInput exchanges a pairing code for a device token for a device
// named DeviceName.
type RedeemPairingInput struct {
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
}

// RedeemPairingResult is a newly paired device and its token, which the server
// returns only once.
type RedeemPairingResult struct {
	Token  string     `json:"token"`
	Device DeviceInfo `json:"device"`
}

func validDeviceName(name string) bool {
	return devices.ValidateName(name) == nil && validRendererText(name, devices.MaxNameBytes)
}

// Validate checks a device crossing a transport boundary.
func (device DeviceInfo) Validate() error {
	if !identifier.Valid(device.ID, devices.IDPrefix) {
		return fmt.Errorf("device id %q is not canonical", device.ID)
	}
	if !validDeviceName(device.Name) {
		return fmt.Errorf("device name must be non-empty trimmed renderer-safe text of at most %d bytes", devices.MaxNameBytes)
	}
	for name, value := range map[string]string{"createdAt": device.CreatedAt, "lastUsedAt": device.LastUsedAt, "expiresAt": device.ExpiresAt} {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return fmt.Errorf("device %s is not an RFC 3339 timestamp", name)
		}
	}
	if device.LastAddress != "" && (!validRendererText(device.LastAddress, 256) || strings.ContainsAny(device.LastAddress, " \t")) {
		return fmt.Errorf("device last address is invalid")
	}
	return nil
}

// Validate checks a device list crossing a transport boundary.
func (list DeviceList) Validate() error {
	if len(list.Devices) > MaxDevices {
		return fmt.Errorf("device list exceeds %d devices", MaxDevices)
	}
	seen := make(map[string]bool, len(list.Devices))
	current := 0
	for _, device := range list.Devices {
		if err := device.Validate(); err != nil {
			return err
		}
		if seen[device.ID] {
			return fmt.Errorf("device %s is listed twice", device.ID)
		}
		seen[device.ID] = true
		if device.Current {
			current++
		}
	}
	if current > 1 {
		return fmt.Errorf("device list marks more than one current device")
	}
	return nil
}

// Validate checks a pairing code crossing a transport boundary.
func (code PairingCode) Validate() error {
	if !pairing.ValidCode(code.Code) {
		return fmt.Errorf("pairing code is malformed")
	}
	if _, err := time.Parse(time.RFC3339Nano, code.ExpiresAt); err != nil {
		return fmt.Errorf("pairing code expiry is not an RFC 3339 timestamp")
	}
	if len(code.URLs) > MaxPairingURLs {
		return fmt.Errorf("pairing code lists more than %d URLs", MaxPairingURLs)
	}
	seen := make(map[string]bool, len(code.URLs))
	for _, served := range code.URLs {
		if !pairing.ValidServedURL(served) || seen[served] {
			return fmt.Errorf("pairing code URL %q is not a distinct HTTP(S) origin", served)
		}
		seen[served] = true
	}
	return nil
}

// Validate checks a redemption request crossing a transport boundary.
func (input RedeemPairingInput) Validate() error {
	if !pairing.ValidCode(input.Code) {
		return fmt.Errorf("pairing code is malformed")
	}
	if !validDeviceName(input.DeviceName) {
		return fmt.Errorf("device name must be non-empty trimmed renderer-safe text of at most %d bytes", devices.MaxNameBytes)
	}
	return nil
}

// Validate checks a redemption result crossing a transport boundary.
func (result RedeemPairingResult) Validate() error {
	if !devices.IsToken(result.Token) {
		return fmt.Errorf("device token is malformed")
	}
	return result.Device.Validate()
}
