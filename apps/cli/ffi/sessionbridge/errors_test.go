package sessionbridge

import (
	"errors"
	"fmt"
	"testing"

	kit "github.com/akonwi/kit/api"
)

func TestIncompatibleRecognizesWrappedCompatibilityFailures(t *testing.T) {
	t.Parallel()

	if !Incompatible(fmt.Errorf("watch session: %w", kit.ErrIncompatibleServer)) {
		t.Fatal("Incompatible() = false for a wrapped compatibility failure")
	}
	if Incompatible(errors.New("connection refused")) {
		t.Fatal("Incompatible() = true for a transport failure")
	}
}

func TestConflictRecognizesDeclaredConflicts(t *testing.T) {
	t.Parallel()

	if !Conflict(fmt.Errorf("submit prompt: %w", &kit.ServerError{Code: kit.ErrorConflict, Message: "operation conflicts with session state"})) {
		t.Fatal("Conflict() = false for a declared conflict")
	}
	if Conflict(&kit.ServerError{Code: kit.ErrorInvalidRequest, Message: "invalid request"}) || Conflict(errors.New("connection refused")) {
		t.Fatal("Conflict() = true for another failure")
	}
}
