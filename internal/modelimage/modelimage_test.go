package modelimage_test

import (
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/modelimage"
)

// The protocol declares its attachment bound without depending on Droids; it
// must match the limit the server enforces.
func TestProtocolImageAttachmentBoundMatchesModelImageLimit(t *testing.T) {
	if protocol.MaxImageAttachmentBytes != modelimage.MaxBytes {
		t.Fatalf("protocol.MaxImageAttachmentBytes = %d, want modelimage.MaxBytes = %d", protocol.MaxImageAttachmentBytes, modelimage.MaxBytes)
	}
}
