package session

import (
	"encoding/json"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/inspectimage"
	"github.com/akonwi/kit/internal/showimage"
)

func TestShowImageCompletionProjectsPresentationFromDetails(t *testing.T) {
	t.Parallel()
	details, err := json.Marshal(showimage.Details{
		Presentation: showimage.Presentation,
		AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename:     "sample.png", MediaType: "image/png", Width: 3, Height: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: showimage.ToolName,
		Result: droids.ToolResult{
			Content: []droids.ResultContent{droids.TextContent{Text: "Image displayed successfully."}},
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

func TestShowImageSnapshotProjectsPresentationFromDetails(t *testing.T) {
	t.Parallel()
	details, _ := json.Marshal(showimage.Details{
		Presentation: showimage.Presentation,
		AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename:     "sample.png", MediaType: "image/png", Width: 3, Height: 2,
	})
	message, err := projectTranscriptMessage(droids.MessageEnvelope{
		ID: "message_1", TurnID: "turn_1",
		Message: droids.ToolResultMessage{
			ToolCallID: "call_1", ToolName: showimage.ToolName, Details: details,
			Content: []droids.ResultContent{droids.TextContent{Text: "Image displayed successfully."}},
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 || message.Content[1].AttachmentID != "attachment_0123456789abcdef0123456789abcdef" {
		t.Fatalf("snapshot message = %#v", message)
	}
}

func TestInspectImageIsNotPromoted(t *testing.T) {
	t.Parallel()
	file := droids.NewFileData("sample.png", "image/png", []byte("image"))
	file.AttachmentID = "attachment_0123456789abcdef0123456789abcdef"
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: inspectimage.ToolName,
		Result: droids.ToolResult{Content: []droids.ResultContent{file}},
	})
	if len(events) != 1 || len(events[0].Content) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if image := events[0].Content[0]; image.AttachmentID != "" {
		t.Fatalf("unmarked image was promoted: %#v", image)
	}
}

func TestShowImageCompletionReservesBoundedLivePresentationBlock(t *testing.T) {
	t.Parallel()
	details, _ := json.Marshal(showimage.Details{
		Presentation: showimage.Presentation,
		AttachmentID: "attachment_0123456789abcdef0123456789abcdef",
		Filename:     "sample.png", MediaType: "image/png", Width: 3, Height: 2,
	})
	content := make([]droids.ResultContent, maxLiveEventContentBlocks)
	for index := range content {
		content[index] = droids.TextContent{Text: "result"}
	}
	events := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: showimage.ToolName,
		Result: droids.ToolResult{Content: content, Details: details},
	})
	if len(events) != 1 || len(events[0].Content) != maxLiveEventContentBlocks || !events[0].ContentTruncated {
		t.Fatalf("events = %#v", events)
	}
	image := events[0].Content[len(events[0].Content)-1]
	if image.Kind != TranscriptContentImage || image.AttachmentID != "attachment_0123456789abcdef0123456789abcdef" {
		t.Fatalf("last content = %#v", image)
	}
}
