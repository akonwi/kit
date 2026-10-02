package modelimage_test

import (
	"testing"

	"github.com/akonwi/kit/internal/modelimage"
	"github.com/akonwi/kit/internal/protocol"
)

// The protocol declares its attachment bound without depending on Droids; it
// must match the limit the server enforces.
func TestProtocolImageAttachmentBoundMatchesModelImageLimit(t *testing.T) {
	if protocol.MaxImageAttachmentBytes != modelimage.MaxBytes {
		t.Fatalf("protocol.MaxImageAttachmentBytes = %d, want modelimage.MaxBytes = %d", protocol.MaxImageAttachmentBytes, modelimage.MaxBytes)
	}
}
