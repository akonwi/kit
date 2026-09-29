package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
)

// ErrorCode is a stable session API failure identity.
type ErrorCode string

const (
	ErrorInvalidRequest   ErrorCode = "invalid_request"
	ErrorUnauthorized     ErrorCode = "unauthorized"
	ErrorForbidden        ErrorCode = "forbidden"
	ErrorNotFound         ErrorCode = "not_found"
	ErrorConflict         ErrorCode = "conflict"
	ErrorInstanceMismatch ErrorCode = "instance_mismatch"
	ErrorLimitExceeded    ErrorCode = "limit_exceeded"
	ErrorInvalidHost      ErrorCode = "invalid_host"
	ErrorUnprocessable    ErrorCode = "unprocessable"
	ErrorProtocolMismatch ErrorCode = "protocol_mismatch"
	ErrorCapacityExceeded ErrorCode = "capacity_exceeded"
	ErrorInternal         ErrorCode = "internal"
	ErrorUnavailable      ErrorCode = "unavailable"
)

// CommonErrorResponses are rejections that can happen before routing any
// catalogued operation.
var CommonErrorResponses = []ErrorResponse{
	{Status: http.StatusUnauthorized, Codes: []ErrorCode{ErrorUnauthorized}},
	{Status: http.StatusForbidden, Codes: []ErrorCode{ErrorForbidden}},
	{Status: http.StatusConflict, Codes: []ErrorCode{ErrorInstanceMismatch}},
	{Status: http.StatusMisdirectedRequest, Codes: []ErrorCode{ErrorInvalidHost}},
	{Status: http.StatusUpgradeRequired, Codes: []ErrorCode{ErrorProtocolMismatch}},
}

// StringErrorEnvelope is the legacy plain error response.
type StringErrorEnvelope struct {
	Error string `json:"error"`
}

// TypedErrorEnvelope is the common typed protocol error response.
type TypedErrorEnvelope struct {
	Error TypedError `json:"error"`
}

// TypedError carries a stable code, message, and optional domain details.
type TypedError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// ScratchpadTypedErrorEnvelope is retained for source compatibility with the
// protocol-41 decoder. New contracts use TypedErrorEnvelope exclusively.
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
	StatusCode        int
	Code              string
	Message           string
	Details           map[string]string
	TypedDetails      any
	CurrentScratchpad *protocol.Scratchpad

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

// NewAPIError constructs a server-side error using the common response shape.
func NewAPIError(status int, code ErrorCode, message string, details any) *APIError {
	return &APIError{StatusCode: status, Code: string(code), Message: message, TypedDetails: details}
}

// WriteError writes an APIError using the common response shape.
func WriteError(w http.ResponseWriter, apiError *APIError) {
	body := map[string]any{"code": apiError.Code, "message": apiError.Message}
	if apiError.TypedDetails != nil {
		body["details"] = apiError.TypedDetails
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(apiError.StatusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": body})
}

// DecodeOperationError decodes only codes declared by op for statusCode.
func DecodeOperationError[Params, In, Out any](op Operation[Params, In, Out], statusCode int, body []byte) error {
	responses := mergeErrorResponses(op.Errors, CommonErrorResponses)
	var declaration *ErrorResponse
	for i := range responses {
		if responses[i].Status == statusCode {
			declaration = &responses[i]
			break
		}
	}
	if declaration == nil {
		return fmt.Errorf("daemon returned undeclared HTTP status %d", statusCode)
	}
	var envelope struct {
		Error struct {
			Code    ErrorCode       `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := decodeStrictJSONObject(body, &envelope); err != nil || envelope.Error.Code == "" || !validErrorMessage(envelope.Error.Message) {
		return fmt.Errorf("daemon returned malformed operation error")
	}
	if !containsCode(declaration.Codes, envelope.Error.Code) {
		return fmt.Errorf("daemon returned undeclared error code %q for HTTP %d", envelope.Error.Code, statusCode)
	}
	detailsType, requiresDetails := declaration.Details[envelope.Error.Code]
	if !requiresDetails && len(envelope.Error.Details) != 0 {
		return fmt.Errorf("daemon returned unexpected details for error code %q", envelope.Error.Code)
	}
	apiError := &APIError{StatusCode: statusCode, Code: string(envelope.Error.Code), Message: envelope.Error.Message}
	if requiresDetails {
		if len(envelope.Error.Details) == 0 {
			return fmt.Errorf("daemon omitted details for error code %q", envelope.Error.Code)
		}
		details := reflect.New(detailsType)
		if err := decodeStrictJSONObject(envelope.Error.Details, details.Interface()); err != nil {
			return fmt.Errorf("daemon returned malformed details for error code %q: %w", envelope.Error.Code, err)
		}
		apiError.TypedDetails = details.Elem().Interface()
	}
	if code := protocol.ScratchpadErrorCode(envelope.Error.Code); code.Valid() {
		var current *protocol.Scratchpad
		if details, ok := apiError.TypedDetails.(protocol.ScratchpadErrorDetails); ok {
			current = details.Scratchpad
		}
		typed := &protocol.ScratchpadError{Code: code, Message: envelope.Error.Message, Current: current}
		if err := typed.Validate(); err != nil {
			return fmt.Errorf("daemon returned malformed scratchpad error: %w", err)
		}
		apiError.CurrentScratchpad, apiError.scratchpadError = current, typed
	}
	return apiError
}

// DecodeError decodes the session protocol's legacy envelopes and also accepts
// common pre-routing errors introduced by ADR 0034.
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
			if code := ErrorCode(typed.Code); genericStatus(code) == statusCode && len(typed.Details) == 0 && validErrorMessage(typed.Message) {
				apiError.Code, apiError.Message = typed.Code, typed.Message
				return apiError
			}
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

func validErrorMessage(message string) bool {
	if strings.TrimSpace(message) == "" || len(message) > 1024 || !utf8.ValidString(message) {
		return false
	}
	for _, character := range message {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func genericStatus(code ErrorCode) int {
	return map[ErrorCode]int{
		ErrorInvalidRequest: http.StatusBadRequest, ErrorUnauthorized: http.StatusUnauthorized,
		ErrorForbidden: http.StatusForbidden, ErrorNotFound: http.StatusNotFound, ErrorConflict: http.StatusConflict,
		ErrorInstanceMismatch: http.StatusConflict, ErrorLimitExceeded: http.StatusRequestEntityTooLarge,
		ErrorInvalidHost: http.StatusMisdirectedRequest, ErrorUnprocessable: http.StatusUnprocessableEntity,
		ErrorProtocolMismatch: http.StatusUpgradeRequired, ErrorCapacityExceeded: http.StatusTooManyRequests,
		ErrorInternal: http.StatusInternalServerError, ErrorUnavailable: http.StatusServiceUnavailable,
	}[code]
}

func mergeErrorResponses(groups ...[]ErrorResponse) []ErrorResponse {
	var merged []ErrorResponse
	for _, group := range groups {
		for _, response := range group {
			index := -1
			for i := range merged {
				if merged[i].Status == response.Status {
					index = i
					break
				}
			}
			if index < 0 {
				copy := ErrorResponse{Status: response.Status, Codes: append([]ErrorCode(nil), response.Codes...)}
				if len(response.Details) > 0 {
					copy.Details = make(map[ErrorCode]reflect.Type, len(response.Details))
					for code, typ := range response.Details {
						copy.Details[code] = typ
					}
				}
				merged = append(merged, copy)
				continue
			}
			for _, code := range response.Codes {
				if !containsCode(merged[index].Codes, code) {
					merged[index].Codes = append(merged[index].Codes, code)
				}
			}
			if len(response.Details) > 0 && merged[index].Details == nil {
				merged[index].Details = make(map[ErrorCode]reflect.Type)
			}
			for code, typ := range response.Details {
				merged[index].Details[code] = typ
			}
		}
	}
	return merged
}

func containsCode(codes []ErrorCode, code ErrorCode) bool {
	for _, candidate := range codes {
		if candidate == code {
			return true
		}
	}
	return false
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
