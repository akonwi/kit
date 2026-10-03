package kit

import (
	"errors"
	"fmt"

	"github.com/akonwi/kit/internal/clienttransport"
)

var (
	// ErrSessionNotFound indicates that no saved session matches a selector.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionAmbiguous indicates that a selector matches several sessions.
	ErrSessionAmbiguous = errors.New("session selector is ambiguous")
)

// ErrorCode identifies one server-declared failure independently of transport.
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

// ServerError is one failure declared by a Kit server operation.
type ServerError struct {
	Code    ErrorCode
	Message string
	Details any
	cause   error
}

func (e *ServerError) Error() string {
	if e == nil {
		return "Kit server request failed"
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("Kit server request failed (%s): %s", e.Code, e.Message)
}

// Unwrap exposes a typed domain error when the declared failure provides one.
func (e *ServerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is recognizes compatibility failures without exposing HTTP status codes.
func (e *ServerError) Is(target error) bool {
	return target == ErrIncompatibleServer && e != nil && e.Code == ErrorProtocolMismatch
}

// TransportError reports failure to exchange a request or stream with a server.
type TransportError = clienttransport.TransportError

func projectResult[T any](value T, err error) (T, error) {
	return value, projectError(err)
}

func projectError(err error) error {
	if err == nil {
		return nil
	}
	var failure *clienttransport.APIError
	if !errors.As(err, &failure) {
		return err
	}
	details := failure.TypedDetails
	if details == nil && len(failure.Details) > 0 {
		details = failure.Details
	}
	return &ServerError{
		Code: ErrorCode(failure.Code), Message: failure.Message,
		Details: details, cause: failure.Unwrap(),
	}
}
