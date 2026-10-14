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

var (
	GetHealth     = Operation[ServerPath, NoBody, Health]{ID: "getHealth", Tag: "server", Method: http.MethodGet, Path: "/v1/health", Success: http.StatusOK, Errors: serverErrors(), NoProtocol: true, OptionalInstance: true}
	Shutdown      = Operation[ServerPath, NoBody, ShutdownResult]{ID: "shutdown", Tag: "server", Method: http.MethodPost, Path: "/v1/shutdown", Success: http.StatusAccepted, Errors: serverErrors(), NoProtocol: true}
	ListModels    = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "listModels", Tag: "server", Method: http.MethodGet, Path: "/v1/models", Success: http.StatusOK, Errors: serverErrors()}
	RefreshModels = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "refreshModels", Tag: "server", Method: http.MethodPost, Path: "/v1/models/refresh", Success: http.StatusOK, Errors: serverErrors()}
)
