package droids

import (
	"errors"
	"strings"
)

const maxDurableErrorBytes = 8 << 10

var (
	ErrClosed                 = errors.New("droid closed")
	ErrBusy                   = errors.New("droid busy")
	ErrNoActiveExecution      = errors.New("no active execution")
	ErrTurnNotFound           = errors.New("turn not found")
	ErrUnsafeContinuation     = errors.New("unsafe continuation")
	ErrConflict               = errors.New("store revision conflict")
	ErrStoreUninitialized     = errors.New("store is not initialized")
	ErrForkAlreadyInitialized = errors.New("fork destination is already initialized")
	ErrForkDestinationExists  = errors.New("fork destination already contains another conversation")
	ErrSubscriberLagged       = errors.New("subscriber lagged")
)

// ForkAlreadyInitializedError identifies a durable fork destination without
// implicitly attaching another live Droid to its Store.
type ForkAlreadyInitializedError struct {
	Point ForkPoint
}

func (e *ForkAlreadyInitializedError) Error() string {
	return "droids: fork destination is already initialized"
}

func (e *ForkAlreadyInitializedError) Unwrap() error {
	return ErrForkAlreadyInitialized
}

func safeRuntimeError(kind DroidErrorKind, err error) string {
	switch kind {
	case DroidErrorProvider:
		return "Provider request failed"
	case DroidErrorPersistence:
		return "Droid persistence failed"
	case DroidErrorCompaction:
		return "Context compaction failed"
	case DroidErrorTool:
		return "Tool processing failed"
	case DroidErrorUnsafe:
		return "Droid continuation is unsafe"
	case DroidErrorInternal:
		return "Droid execution was interrupted"
	default:
		return boundedErrorText(err)
	}
}

func boundedErrorText(err error) string {
	if err == nil {
		return ""
	}
	return boundedDiagnostic(err.Error())
}

func boundedDiagnostic(message string) string {
	message = strings.ToValidUTF8(message, "�")
	if len(message) <= maxDurableErrorBytes {
		return message
	}
	end := maxDurableErrorBytes
	for end > 0 && end < len(message) && !utf8Start(message[end]) {
		end--
	}
	return message[:end] + "…"
}

func utf8Start(value byte) bool {
	return value&0xC0 != 0x80
}
