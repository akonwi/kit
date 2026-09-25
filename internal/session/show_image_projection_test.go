package session

import (
	"encoding/json"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/showimage"
)

func TestShowImageCompletionPromotesOnlyExplicitMatchingImage(t *testing.T) {
	t.Parallel()
	details, err := json.Marshal(showimage.Details{
		Presentation: showimage.Presentation,
		AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename:     "sample.png", MediaType: "image/png", Width: 3, Height: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	file := droids.NewFileData("sample.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: showimage.ToolName,
		Result: droids.ToolResult{
			Content: []droids.ResultContent{
				droids.TextContent{Text: "Displayed sample.png (3x2)."}, file,
			},
			Details: details,
		},
	})
	if len(events) != 1 || len(events[0].Content) != 2 {
		t.Fatalf("events = %#v", events)
	}
	image := events[0].Content[1]
	if image.Kind != TranscriptContentImage || image.AttachmentID != "attachment_0123456789abcdef0123456789abcdef" {
		t.Fatalf("promoted image = %#v", image)
	}
}

func TestShowImageSnapshotRetainsPromotedAttachmentIdentity(t *testing.T) {
	t.Parallel()
	details, _ := json.Marshal(showimage.Details{
		Presentation: showimage.Presentation,
		AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename:     "sample.png", MediaType: "image/png", Width: 3, Height: 2,
	})
	file := droids.NewFileData("sample.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	message, err := projectTranscriptMessage(droids.MessageEnvelope{
		ID: "message_1", TurnID: "turn_1",
		Message: droids.ToolResultMessage{
			ToolCallID: "call_1", ToolName: showimage.ToolName, Details: details,
			Content: []droids.ResultContent{file},
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 1 || message.Content[0].AttachmentID != "attachment_0123456789abcdef0123456789abcdef" {
		t.Fatalf("snapshot message = %#v", message)
	}
}

func TestUnmarkedToolImageIsNotPromoted(t *testing.T) {
	t.Parallel()
	file := droids.NewFileData("sample.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "other_tool",
		Result: droids.ToolResult{Content: []droids.ResultContent{file}},
	})
	if len(events) != 1 || len(events[0].Content) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if image := events[0].Content[0]; image.AttachmentID != "" {
		t.Fatalf("unmarked image was promoted: %#v", image)
	}
}
