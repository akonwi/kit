package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

var (
	// GetServerSettings reads the settings that govern the server's behavior.
	// A settings file the server cannot read is a conflict the user resolves
	// by fixing it.
	GetServerSettings = Operation[ServerPath, NoBody, protocol.ServerSettings]{ID: "getServerSettings", Tag: "server", Method: http.MethodGet, Path: "/v1/settings", Success: http.StatusOK, Errors: deviceErrors(409), NoProtocol: true}
	// UpdateServerSettings changes the fields it carries and returns the
	// resulting settings.
	UpdateServerSettings = Operation[ServerPath, protocol.UpdateServerSettingsInput, protocol.ServerSettings]{ID: "updateServerSettings", Tag: "server", Method: http.MethodPatch, Path: "/v1/settings", Success: http.StatusOK, Errors: deviceErrors(400, 409, 413), NoProtocol: true}
)
