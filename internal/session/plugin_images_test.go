package session

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
)

func pluginTestPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 4, 3))
	picture.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestPluginToolImagesAreStoredAsSessionAttachments(t *testing.T) {
	t.Parallel()
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	data := pluginTestPNG(t)
	images := pluginImageStore{sessionID: "session_1", store: store}
	content := images.content(context.Background(), "browser__screenshot", "image/png", data)
	if len(content) != 2 {
		t.Fatalf("content = %#v, want the image and its reference", content)
	}
	file, ok := content[0].(droids.FileContent)
	if !ok || file.AttachmentID == "" || file.Filename != "browser__screenshot.png" || file.MediaType != "image/png" || file.URL != droids.NewImageData("image/png", data).URL {
		t.Fatalf("file = %#v, want the image stored and kept inline for the model", content[0])
	}
	record, reader, err := store.Open(context.Background(), "session_1", file.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if record.Width != 4 || record.Height != 3 {
		t.Fatalf("record = %#v", record)
	}
	want := "Attachment " + file.AttachmentID + ": image/png, 4×3, " + strconv.FormatInt(record.Size, 10) + " bytes. save_attachment copies it to a file."
	if reference, ok := content[1].(droids.TextContent); !ok || reference.Text != want {
		t.Fatalf("reference = %#v, want %q", content[1], want)
	}
	if _, _, err := store.Open(context.Background(), "session_2", file.AttachmentID); err == nil {
		t.Fatal("another session opened the plugin image")
	}
}

func TestPluginToolImagesStayInlineWhenTheyCannotBeStored(t *testing.T) {
	t.Parallel()
	data := pluginTestPNG(t)
	inline := droids.NewImageData("image/png", data)
	// Without a store there is nothing to save, so the image stands alone.
	if content := (pluginImageStore{}).content(context.Background(), "browser__screenshot", "image/png", data); len(content) != 1 || content[0] != droids.ResultContent(inline) {
		t.Fatalf("without a store: %#v", content)
	}
	store, err := attachment.NewFilesystem(filepath.Join(t.TempDir(), "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	images := pluginImageStore{sessionID: "session_1", store: store}
	// Bytes that don't match the declared type stay with the model only, and
	// the model is told the image cannot be saved.
	content := images.content(context.Background(), "browser__screenshot", "image/jpeg", data)
	want := []droids.ResultContent{
		droids.NewImageData("image/jpeg", data),
		droids.TextContent{Text: "This image was not kept as an attachment, so save_attachment cannot copy it."},
	}
	if len(content) != 2 || content[0] != want[0] || content[1] != want[1] {
		t.Fatalf("mismatched type: %#v, want %#v", content, want)
	}
}

func TestPluginToolImagesKeepTheirAttachmentForClients(t *testing.T) {
	t.Parallel()
	file := droids.NewFileData("browser__screenshot.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	result := droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: "Screenshot"}, file}}
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "browser__screenshot", Result: result,
	})
	if len(events) != 1 || len(events[0].Content) != 2 || events[0].Content[1].AttachmentID != file.AttachmentID ||
		events[0].Content[1].Kind != TranscriptContentImage {
		t.Fatalf("live events = %#v", events)
	}
	message, err := projectTranscriptMessage(droids.MessageEnvelope{
		ID: "message_1", TurnID: "turn_1",
		Message: droids.ToolResultMessage{ToolCallID: "call_1", ToolName: "browser__screenshot", Content: result.Content},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 || message.Content[1].AttachmentID != file.AttachmentID {
		t.Fatalf("snapshot message = %#v", message)
	}
}
