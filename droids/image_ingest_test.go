package droids

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"reflect"
	"strings"
	"testing"
)

// pngHeader returns a PNG signature and IHDR chunk declaring w×h pixels. It
// carries no image data, which suffices for header-only inspection.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 0, 0, 0, 0}) // 8-bit grayscale, default methods
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4))
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

func TestIngestToolResultImagesAppliesProviderNeutralLimits(t *testing.T) {
	screenshot := testPNG(t, 64, 48)
	metadataImage := NewImageData(ImagePNG, screenshot)
	metadataImage.URL = strings.Replace(metadataImage.URL, "data:image/png;base64,", "data:image/png;name=shot;base64,", 1)
	remote, err := NewImageURL(ImagePNG, "https://example.com/shot.png")
	if err != nil {
		t.Fatal(err)
	}
	pdf := FileContent{Filename: "report.pdf", MediaType: "application/pdf", URL: "data:application/pdf;base64,JVBERi0="}
	for _, test := range []struct {
		name  string
		block ResultContent
		want  ResultContent
	}{
		{"within limits", NewImageData(ImagePNG, screenshot), NewImageData(ImagePNG, screenshot)},
		{"canonicalizes metadata", metadataImage, NewImageData(ImagePNG, screenshot)},
		{"mislabeled", NewImageData(ImageJPEG, screenshot), NewImageData(ImagePNG, screenshot)},
		{"remote", remote, remote},
		{"non-image file", pdf, pdf},
		{"invalid data", FileContent{MediaType: ImagePNG, URL: "data:image/png;base64,%%%"}, TextContent{Text: "[Image omitted: the image data is invalid.]"}},
		{"unsupported format", NewImageData(ImagePNG, []byte("BM not a png")), TextContent{Text: "[Image omitted: the image format is not supported.]"}},
		{"truncated", NewImageData(ImagePNG, screenshot[:20]), TextContent{Text: "[Image omitted: the image could not be decoded.]"}},
		{"too many bytes", NewImageData(ImagePNG, append(screenshot, make([]byte, MaxImageBytes)...)), TextContent{Text: "[Image omitted: the image exceeds the 10 MiB image size limit.]"}},
		{"too wide", NewImageData(ImagePNG, pngHeader(8193, 1)), TextContent{Text: "[Image omitted: the image's 8193×1 px exceed the 8192 px, 12 MP image limit.]"}},
		{"too many pixels", NewImageData(ImagePNG, pngHeader(4000, 3001)), TextContent{Text: "[Image omitted: the image's 4000×3001 px exceed the 8192 px, 12 MP image limit.]"}},
		{"named", FileContent{Filename: "shot.png", MediaType: ImagePNG, URL: imageDataURL(ImagePNG, []byte("nope"))}, TextContent{Text: `[Image "shot.png" omitted: the image format is not supported.]`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []ResultContent{TextContent{Text: "captured"}, test.block}
			got := ingestToolResultImages(content)
			want := []ResultContent{TextContent{Text: "captured"}, test.want}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ingestToolResultImages() = %#v, want %#v", got, want)
			}
			if !reflect.DeepEqual(content[1], test.block) {
				t.Fatalf("input content was modified: %#v", content[1])
			}
		})
	}
}

func TestToolResultImagesAreIngestedBeforeHistory(t *testing.T) {
	screenshot := testPNG(t, 64, 48)
	var requests []Request
	stream := func(_ context.Context, model Model, request Request) Stream {
		requests = append(requests, request)
		message := AssistantMessage{Provider: model.Provider, Model: model.ID, StopReason: StopReasonStop, Content: []AssistantContent{TextContent{Text: "done"}}}
		if len(requests) == 1 {
			message.StopReason = StopReasonToolUse
			message.Content = []AssistantContent{ToolCall{ID: "call", Name: "screenshot", Arguments: []byte(`{}`)}}
		}
		return reconfigureStream{message: message}
	}
	model := Model{Provider: "test", ID: "vision", Input: []string{"text", "image"}, ContextWindow: 200_000, MaxOutputTokens: 8_000}
	bound, err := BindModel(AdaptProviderWithImagePolicy("test", []Model{model}, stream, func(Model) ImagePolicy { return testImagePolicy() }), model)
	if err != nil {
		t.Fatal(err)
	}
	tool := MustTool(Tool[struct{}]{Name: "screenshot", Execute: func(context.Context, ToolContext, struct{}, ToolUpdate) (ToolResult, error) {
		return ToolResult{Content: []ResultContent{
			TextContent{Text: "captured"},
			NewImageData(ImageJPEG, screenshot),           // mislabeled PNG
			NewImageData(ImagePNG, pngHeader(9000, 9000)), // over the pixel limit
		}}, nil
	}})
	d, err := Spawn(t.Context(), "image_ingest", Config{Store: NewMemoryStore(), Model: bound, Tools: []AnyTool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	execution, err := d.Prompt(t.Context(), Input{Content: []InputContent{TextInput{Text: "take a screenshot"}}}, PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := execution.Wait(t.Context()); err != nil || outcome.Status != ExecutionCompleted {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}

	want := []ResultContent{
		TextContent{Text: "captured"},
		NewImageData(ImagePNG, screenshot),
		TextContent{Text: "[Image omitted: the image's 9000×9000 px exceed the 8192 px, 12 MP image limit.]"},
	}
	d.sdk.mu.Lock()
	envelopes, err := runtimeMessageEnvelopes(d.sdk.state)
	d.sdk.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var stored ToolResultMessage
	for _, envelope := range envelopes {
		if result, ok := envelope.Message.(ToolResultMessage); ok {
			stored = result
		}
	}
	if !reflect.DeepEqual(stored.Content, want) {
		t.Fatalf("stored tool result = %#v, want %#v", stored.Content, want)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	sent := requests[1].Messages[len(requests[1].Messages)-1].(ToolResultMessage)
	if !reflect.DeepEqual(sent.Content, want) {
		t.Fatalf("sent tool result = %#v, want %#v", sent.Content, want)
	}
}
