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
