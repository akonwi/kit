package httpapi

import (
	"net/http"

	protocol "github.com/akonwi/kit/api/contract"
)

// ProviderPath addresses one provider.
type ProviderPath struct {
	ProviderID string `path:"providerID"`
}

// LoginPath addresses one provider login.
type LoginPath struct {
	LoginID string `path:"loginID"`
}

// WaitLoginPath addresses one provider login and how long to wait for it to
// finish.
type WaitLoginPath struct {
	LoginID string `path:"loginID"`
	// WaitSeconds is how long to wait for a pending login to finish, at
	// most 25; zero returns at once.
	WaitSeconds int `query:"waitSeconds,omitempty"`
}

var (
	// ListProviders lists the server's model providers and their credentials.
	ListProviders = Operation[ServerPath, NoBody, protocol.ProviderList]{ID: "listProviders", Tag: "server", Method: http.MethodGet, Path: "/v1/providers", Success: http.StatusOK, Errors: serverErrors(), NoProtocol: true}
	// StartProviderLogin starts a device or browser login, superseding a
	// pending login to the same provider.
	StartProviderLogin = Operation[ServerPath, protocol.StartLoginInput, protocol.ProviderLogin]{ID: "startProviderLogin", Tag: "server", Method: http.MethodPost, Path: "/v1/logins", Success: http.StatusCreated, Errors: append(deviceErrors(400, 404, 409, 413, 503), ErrorResponse{Status: http.StatusTooManyRequests, Codes: []ErrorCode{ErrorCapacityExceeded}}), NoProtocol: true}
	// GetProviderLogin returns a login, waiting up to waitSeconds for a
	// pending login to finish.
	GetProviderLogin = Operation[WaitLoginPath, NoBody, protocol.ProviderLogin]{ID: "getProviderLogin", Tag: "server", Method: http.MethodGet, Path: "/v1/logins/{loginID}", Success: http.StatusOK, Errors: deviceErrors(400, 404, 503), NoProtocol: true}
	// SubmitProviderLoginAuthorization completes a pending browser login.
	SubmitProviderLoginAuthorization = Operation[LoginPath, protocol.LoginAuthorizationInput, NoBody]{ID: "submitProviderLoginAuthorization", Tag: "server", Method: http.MethodPost, Path: "/v1/logins/{loginID}/authorization", Success: http.StatusNoContent, Errors: deviceErrors(400, 404, 409, 413), NoProtocol: true}
	// CancelProviderLogin cancels a pending login.
	CancelProviderLogin = Operation[LoginPath, NoBody, NoBody]{ID: "cancelProviderLogin", Tag: "server", Method: http.MethodDelete, Path: "/v1/logins/{loginID}", Success: http.StatusNoContent, Errors: deviceErrors(404), NoProtocol: true}
	// SetProviderAPIKey saves an API key for a provider.
	SetProviderAPIKey = Operation[ProviderPath, protocol.ProviderAPIKeyInput, NoBody]{ID: "setProviderAPIKey", Tag: "server", Method: http.MethodPut, Path: "/v1/providers/{providerID}/api-key", Success: http.StatusNoContent, Errors: deviceErrors(400, 404, 409, 413), NoProtocol: true}
	// DeleteProviderCredentials logs the server out of a provider.
	DeleteProviderCredentials = Operation[ProviderPath, NoBody, NoBody]{ID: "deleteProviderCredentials", Tag: "server", Method: http.MethodDelete, Path: "/v1/providers/{providerID}/credentials", Success: http.StatusNoContent, Errors: deviceErrors(404, 409), NoProtocol: true}
)
