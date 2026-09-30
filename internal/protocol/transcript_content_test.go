package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranscriptContentWireCompatibility(t *testing.T) {
	t.Parallel()
	cases := []struct {
		content TranscriptContent
		want    string
	}{
		{TextBlock("hello"), `{"kind":"text","text":"hello"}`},
		{NewTranscriptContent(ThinkingContent{Text: "reason"}), `{"kind":"thinking","text":"reason"}`},
		{NewTranscriptContent(ToolCallContent{ToolCallID: "call", ToolName: "read", Arguments: `{}`}), `{"kind":"toolCall","toolCallId":"call","toolName":"read","arguments":"{}"}`},
		{NewTranscriptContent(ImageContent{Filename: "a.png", MediaType: "image/png", AttachmentID: "attachment_1"}), `{"kind":"image","filename":"a.png","mediaType":"image/png","attachmentId":"attachment_1"}`},
		{NewTranscriptContent(FileContent{Filename: "a.txt", MediaType: "text/plain"}), `{"kind":"file","filename":"a.txt","mediaType":"text/plain"}`},
	}
	for _, test := range cases {
		got, err := json.Marshal(test.content)
		if err != nil || string(got) != test.want {
			t.Fatalf("MarshalJSON() = %s, %v; want %s", got, err, test.want)
		}
		var decoded TranscriptContent
		if err := json.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("UnmarshalJSON(%s): %v", got, err)
		}
		if decoded.Kind() != test.content.Kind() {
			t.Fatalf("decoded kind = %q; want %q", decoded.Kind(), test.content.Kind())
		}
	}
}

func TestTranscriptContentRejectsInvalidUnionRecords(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"kind":"unknown"}`,
		`{"kind":"text","toolName":"read"}`,
		`{"kind":"toolCall","toolCallId":"call"}`,
	} {
		var content TranscriptContent
		if err := json.Unmarshal([]byte(raw), &content); err == nil {
			t.Fatalf("UnmarshalJSON(%s) succeeded", raw)
		}
	}
	var typedNil *TextContent
	content := NewTranscriptContent(typedNil)
	if content.Kind() != "" {
		t.Fatalf("typed nil Kind() = %q; want empty", content.Kind())
	}
	if _, err := json.Marshal(content); err == nil || !strings.Contains(err.Error(), "no payload") {
		t.Fatalf("typed nil marshal error = %v", err)
	}
}

func TestTranscriptContentUnionVariantsAreCopied(t *testing.T) {
	variants := (TranscriptContent{}).UnionVariants()
	variants[0].Kind = "mutated"
	if (TranscriptContent{}).UnionVariants()[0].Kind != string(TranscriptContentText) {
		t.Fatal("UnionVariants exposed mutable declaration")
	}
}
