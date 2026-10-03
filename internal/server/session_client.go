package server

import (
	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/httpapi"
)

// APIError is a non-success response from the session protocol.
type APIError = httpapi.APIError

func validateDiffTargetCatalogResponse(sessionID string, input protocol.ListDiffTargetsInput, output protocol.DiffTargetCatalog) error {
	return clienttransport.ValidateDiffTargetCatalogResponse(sessionID, input, output)
}

func validateObserveDiffResponse(sessionID string, input protocol.ObserveDiffInput, output protocol.DiffPage) error {
	return clienttransport.ValidateObserveDiffResponse(sessionID, input, output)
}

func validateFileDiffResponse(sessionID string, input protocol.ReadFileDiffInput, output protocol.FileDiffPage) error {
	return clienttransport.ValidateFileDiffResponse(sessionID, input, output)
}

func decodeAPIError(statusCode int, body []byte) error {
	return clienttransport.DecodeAPIError(statusCode, body)
}

func decodeStrictJSONObject(data []byte, target any) error {
	return clienttransport.DecodeStrictJSONObject(data, target)
}
