package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
)

// StringErrorEnvelope is the legacy plain error response.
type StringErrorEnvelope struct {
	Error string `json:"error"`
}

// TypedErrorEnvelope is a typed protocol error response.
type TypedErrorEnvelope struct {
	Error TypedError `json:"error"`
}

// TypedError carries a stable code, message, and domain-specific details.
type TypedError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details"`
}

// ScratchpadTypedErrorEnvelope describes a scratchpad typed error response.
type ScratchpadTypedErrorEnvelope struct {
	Error ScratchpadTypedError `json:"error"`
}

// ScratchpadTypedError carries scratchpad-specific details.
type ScratchpadTypedError struct {
	Code    protocol.ScratchpadErrorCode    `json:"code"`
	Message string                          `json:"message"`
	Details protocol.ScratchpadErrorDetails `json:"details"`
}

// APIError is a non-success response from the local session protocol.
type APIError struct {
	StatusCode         int
	Code               string
	Message            string
	Details            map[string]string
	CurrentScratchpad  *protocol.Scratchpad
	scratchpadError    *protocol.ScratchpadError
	pluginCommandError *protocol.PluginCommandError
}

func (e *APIError) Error() string {
	return fmt.Sprintf("daemon returned HTTP %d: %s", e.StatusCode, e.Message)
}
func (e *APIError) IncompatibleDaemon() bool {
	return e != nil && e.StatusCode == http.StatusUpgradeRequired
}
func (e *APIError) UserMessage() string { return e.Message }
func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.pluginCommandError != nil {
		return e.pluginCommandError
	}
	if e.scratchpadError != nil {
		return e.scratchpadError
	}
	return nil
}

// DecodeError decodes the session protocol's current legacy and typed envelopes.
func DecodeError(statusCode int, body []byte) error {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	message := strings.TrimSpace(string(body))
	apiError := &APIError{StatusCode: statusCode}
	if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 {
		var plain string
		if json.Unmarshal(envelope.Error, &plain) == nil {
			apiError.Message = plain
			return apiError
		}
		var typed TypedError
		if json.Unmarshal(envelope.Error, &typed) == nil && typed.Message != "" {
			var stringDetails map[string]string
			_ = json.Unmarshal(typed.Details, &stringDetails)
			workspaceError := protocol.WorkspaceError{Code: protocol.WorkspaceErrorCode(typed.Code), Message: typed.Message, Details: stringDetails}
			diffError := protocol.DiffError{Code: protocol.DiffErrorCode(typed.Code), Message: typed.Message, Details: stringDetails}
			annotationError := protocol.AnnotationEvidenceError{Code: protocol.AnnotationEvidenceErrorCode(typed.Code), Message: typed.Message}
			annotationValid := annotationError.Validate() == nil && len(stringDetails) == 0
			code := protocol.ScratchpadErrorCode(typed.Code)
			var details protocol.ScratchpadErrorDetails
			scratchValid := decodeStrictJSONObject(typed.Details, &details) == nil
			typedScratch := &protocol.ScratchpadError{Code: code, Message: typed.Message, Current: details.Scratchpad}
			scratchValid = scratchValid && typedScratch.Validate() == nil && scratchpadStatusMatches(code, statusCode)
			pluginError := protocol.PluginCommandError{Code: typed.Code, Message: typed.Message}
			pluginValid := pluginError.Validate() == nil && (len(typed.Details) == 0 || bytes.Equal(bytes.TrimSpace(typed.Details), []byte("{}"))) && ((typed.Code == protocol.PluginCommandUnavailable && statusCode == http.StatusConflict) || (typed.Code == protocol.PluginCommandFailed && statusCode == http.StatusUnprocessableEntity))
			if workspaceError.Validate() != nil && diffError.Validate() != nil && !annotationValid && !scratchValid && !pluginValid {
				return fmt.Errorf("daemon returned malformed typed error")
			}
			apiError.Code, apiError.Message, apiError.Details = typed.Code, typed.Message, stringDetails
			if pluginValid {
				apiError.pluginCommandError = &pluginError
			}
			if scratchValid {
				apiError.CurrentScratchpad, apiError.scratchpadError = details.Scratchpad, typedScratch
			}
			return apiError
		}
	}
	apiError.Message = message
	return apiError
}

func scratchpadStatusMatches(code protocol.ScratchpadErrorCode, status int) bool {
	switch code {
	case protocol.ScratchpadInvalidContent:
		return status == http.StatusBadRequest
	case protocol.ScratchpadTooLarge:
		return status == http.StatusRequestEntityTooLarge
	case protocol.ScratchpadRevisionConflict, protocol.ScratchpadRevisionExhausted, protocol.ScratchpadMigrationRequired, protocol.ScratchpadUnsupported:
		return status == http.StatusConflict
	case protocol.ScratchpadUnavailable:
		return status == http.StatusServiceUnavailable
	default:
		return false
	}
}

func decodeStrictJSONObject(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return fmt.Errorf("expected one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}
