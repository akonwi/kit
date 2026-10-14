package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// Health is the authenticated daemon lifecycle status.
type Health struct {
	InstanceID      string   `json:"instanceId"`
	PID             int      `json:"pid"`
	KitVersion      string   `json:"kitVersion"`
	ProtocolVersion int      `json:"protocolVersion"`
	DatabaseReady   bool     `json:"databaseReady"`
	Providers       []string `json:"providers"`
	// ServerID identifies the server's Kit home across process restarts.
	ServerID string `json:"serverId"`
	// MachineID is the Kit-scoped identifier of the server's machine. A client
	// whose own machine identifier matches runs on the same machine.
	MachineID string `json:"machineId"`
	// Home is the server user's home directory, used to present server paths.
	Home string `json:"home"`
}

type ShutdownResult struct {
	Stopping bool `json:"stopping"`
}

type ServerPath struct{}

func serverErrors() []ErrorResponse {
	return []ErrorResponse{{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}}}
}

// DevicePath addresses one paired device.
type DevicePath struct {
	DeviceID string `path:"deviceID"`
}

func deviceErrors(statuses ...int) []ErrorResponse {
	return append(lifecycleErrors(statuses...), serverErrors()...)
}

var (
	// RedeemPairing exchanges a pairing code for a device token. It is the
	// only operation that accepts requests without a credential.
	RedeemPairing = Operation[ServerPath, protocol.RedeemPairingInput, protocol.RedeemPairingResult]{ID: "redeemPairing", Tag: "server", Method: http.MethodPost, Path: "/v1/pairing/redeem", Success: http.StatusCreated, Errors: append(deviceErrors(400, 413), ErrorResponse{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorRateLimited, ErrorCapacityExceeded}}), NoProtocol: true, Unauthenticated: true}
	// CreatePairing issues a pairing code for another device.
	CreatePairing = Operation[ServerPath, NoBody, protocol.PairingCode]{ID: "createPairing", Tag: "server", Method: http.MethodPost, Path: "/v1/pairing", Success: http.StatusCreated, Errors: serverErrors(), NoProtocol: true}
	// ListDevices lists paired devices.
	ListDevices = Operation[ServerPath, NoBody, protocol.DeviceList]{ID: "listDevices", Tag: "server", Method: http.MethodGet, Path: "/v1/devices", Success: http.StatusOK, Errors: serverErrors(), NoProtocol: true}
	// RevokeDevice revokes a device's token and ends its open requests.
	RevokeDevice = Operation[DevicePath, NoBody, NoBody]{ID: "revokeDevice", Tag: "server", Method: http.MethodDelete, Path: "/v1/devices/{deviceID}", Success: http.StatusNoContent, Errors: deviceErrors(404), NoProtocol: true}

	GetHealth     = Operation[ServerPath, NoBody, Health]{ID: "getHealth", Tag: "server", Method: http.MethodGet, Path: "/v1/health", Success: http.StatusOK, Errors: serverErrors(), NoProtocol: true, OptionalInstance: true}
	Shutdown      = Operation[ServerPath, NoBody, ShutdownResult]{ID: "shutdown", Tag: "server", Method: http.MethodPost, Path: "/v1/shutdown", Success: http.StatusAccepted, Errors: serverErrors(), NoProtocol: true}
	ListModels    = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "listModels", Tag: "server", Method: http.MethodGet, Path: "/v1/models", Success: http.StatusOK, Errors: serverErrors()}
	RefreshModels = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "refreshModels", Tag: "server", Method: http.MethodPost, Path: "/v1/models/refresh", Success: http.StatusOK, Errors: serverErrors()}
)
