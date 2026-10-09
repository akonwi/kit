package contract

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

func TestPromptCommandContentValidation(t *testing.T) {
	t.Parallel()
	valid := []PromptCommandContent{
		{Name: "claude-fix", Arguments: "123 high", Source: PromptCommandSourceClaudeProject, Text: "Fix issue #123 at high priority."},
		// A command run without arguments.
		{Name: "claude-ping", Source: PromptCommandSourceClaudeProject, Text: "Reply with pong."},
	}
	for _, content := range valid {
		if err := NewTranscriptContent(content).validate(); err != nil {
			t.Errorf("validate(%+v) = %v", content, err)
		}
	}
	invalid := map[string]PromptCommandContent{
		"missing name":    {Source: PromptCommandSourceUser, Text: "Review."},
		"spaced name":     {Name: "code review", Source: PromptCommandSourceUser, Text: "Review."},
		"unknown source":  {Name: "review", Source: "claude", Text: "Review."},
		"NUL arguments":   {Name: "review", Arguments: "a\x00b", Source: PromptCommandSourceUser, Text: "Review."},
		"blank expansion": {Name: "review", Source: PromptCommandSourceUser, Text: " "},
	}
	for name, content := range invalid {
		if err := NewTranscriptContent(content).validate(); err == nil {
			t.Errorf("%s: validate(%+v) accepted", name, content)
		}
	}
}

func TestTranscriptContentUnionVariantsAreCopied(t *testing.T) {
	variants := (TranscriptContent{}).UnionVariants()
	variants[0].Kind = "mutated"
	if (TranscriptContent{}).UnionVariants()[0].Kind != string(TranscriptContentText) {
		t.Fatal("UnionVariants exposed mutable declaration")
	}
}
