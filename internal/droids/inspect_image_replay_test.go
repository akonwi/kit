package droids

import (
	"testing"
	"time"
)

func TestInspectImageToolResultRoundTripsModelVisibleImage(t *testing.T) {
	t.Parallel()
	file := NewFileData("sample.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	envelope := MessageEnvelope{
		ID: "message_image", ConversationID: "conversation_image", TurnID: "turn_image", CreatedAt: time.Unix(1, 0).UTC(),
		Message: ToolResultMessage{
			ToolCallID: "call_image", ToolName: "inspect_image",
			Content: []ResultContent{TextContent{Text: "Image loaded for inspection."}, file},
		},
	}
	encoded, err := encodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessageEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.Message.(ToolResultMessage)
	if !ok || result.ToolName != "inspect_image" || len(result.Content) != 2 {
		t.Fatalf("decoded result = %#v", decoded.Message)
	}
	image, ok := result.Content[1].(FileContent)
	if !ok || image.Filename != file.Filename || image.MediaType != file.MediaType || image.URL != file.URL || image.AttachmentID != file.AttachmentID {
		t.Fatalf("decoded image = %#v", result.Content[1])
	}
}
