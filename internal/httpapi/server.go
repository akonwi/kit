package httpapi

import (
	"net/http"

	"github.com/akonwi/kit/internal/protocol"
)

// Health is the authenticated daemon lifecycle status.
type Health struct {
	InstanceID      string   `json:"instanceId"`
	PID             int      `json:"pid"`
	KitVersion      string   `json:"kitVersion"`
	ProtocolVersion int      `json:"protocolVersion"`
	DatabaseReady   bool     `json:"databaseReady"`
	Providers       []string `json:"providers"`
}

type ShutdownResult struct {
	Stopping bool `json:"stopping"`
}

type ServerPath struct{}

func serverErrors() []ErrorResponse {
	return []ErrorResponse{{Status: http.StatusInternalServerError, Codes: []ErrorCode{ErrorInternal}}}
}

var (
	GetHealth     = Operation[ServerPath, NoBody, Health]{ID: "getHealth", Tag: "server", Method: http.MethodGet, Path: "/v1/health", Success: http.StatusOK, Errors: serverErrors(), NoProtocol: true}
	Shutdown      = Operation[ServerPath, NoBody, ShutdownResult]{ID: "shutdown", Tag: "server", Method: http.MethodPost, Path: "/v1/shutdown", Success: http.StatusAccepted, Errors: serverErrors(), NoProtocol: true}
	ListModels    = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "listModels", Tag: "server", Method: http.MethodGet, Path: "/v1/models", Success: http.StatusOK, Errors: serverErrors()}
	RefreshModels = Operation[ServerPath, NoBody, protocol.ModelCatalog]{ID: "refreshModels", Tag: "server", Method: http.MethodPost, Path: "/v1/models/refresh", Success: http.StatusOK, Errors: serverErrors()}
)
