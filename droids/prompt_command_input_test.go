package droids

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func promptCommandInputForTest() PromptCommandInput {
	return PromptCommandInput{Name: "review", Arguments: `"auth module" carefully`, Source: "claude_project", Text: "Review auth module carefully."}
}

func TestPromptCommandInputCanonicalRoundTrip(t *testing.T) {
	want := promptCommandInputForTest()
	envelope := MessageEnvelope{
		ID: "message_command", ConversationID: "conversation_command", TurnID: "turn_command", CreatedAt: time.Unix(1, 0).UTC(),
		Message: UserMessage{Content: []InputContent{want}},
	}
	encoded, err := encodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessageEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Message.(UserMessage).Content; !reflect.DeepEqual(got, []InputContent{want}) {
		t.Fatalf("decoded content = %#v, want %#v", got, []InputContent{want})
	}

	// Pending runtime input uses its own wire form.
	wire, err := inputToWire([]InputContent{want})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := inputFromWire(wire)
	if err != nil || !reflect.DeepEqual(restored, []InputContent{want}) {
		t.Fatalf("pending input = %#v, %v; want %#v", restored, err, []InputContent{want})
	}
}

func TestPromptCommandInputValidation(t *testing.T) {
	valid := promptCommandInputForTest()
	if err := validatePromptCommandInput(valid); err != nil {
		t.Fatal(err)
	}
	if err := validatePromptCommandInput(PromptCommandInput{Name: "review", Source: "user", Text: "Review."}); err != nil {
		t.Fatalf("command without arguments: %v", err)
	}
	for name, mutate := range map[string]func(*PromptCommandInput){
		"blank name":       func(input *PromptCommandInput) { input.Name = "" },
		"spaced name":      func(input *PromptCommandInput) { input.Name = "code review" },
		"slashed name":     func(input *PromptCommandInput) { input.Name = "a/b" },
		"long name":        func(input *PromptCommandInput) { input.Name = strings.Repeat("a", 129) },
		"NUL arguments":    func(input *PromptCommandInput) { input.Arguments = "a\x00b" },
		"invalid source":   func(input *PromptCommandInput) { input.Source = "Claude Project" },
		"missing source":   func(input *PromptCommandInput) { input.Source = "" },
		"blank expansion":  func(input *PromptCommandInput) { input.Text = " \n" },
		"invalid encoding": func(input *PromptCommandInput) { input.Text = "\xff" },
	} {
		input := valid
		mutate(&input)
		if err := validatePromptCommandInput(input); err == nil {
			t.Errorf("%s: validatePromptCommandInput(%#v) accepted", name, input)
		}
		if _, err := inputToMessage(Input{Content: []InputContent{input}}); err == nil {
			t.Errorf("%s: inputToMessage accepted %#v", name, input)
		}
	}
	if _, err := promptCommandFromWire("Review.", nil); err == nil {
		t.Fatal("prompt command wire block without an invocation was accepted")
	}
}

// TestPromptCommandInputSendsOnlyItsExpansion checks that every provider sends
// the expanded text, and nothing about the invocation, to the model.
func TestPromptCommandInputSendsOnlyItsExpansion(t *testing.T) {
	messages := []Message{UserMessage{Content: []InputContent{promptCommandInputForTest()}}}
	encodedText := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	anthropic, err := toAnthropicMessages(Model{Provider: "anthropic", ID: "claude-opus-5-5", Input: []string{"text"}}, messages)
	if err != nil {
		t.Fatal(err)
	}
	openAI, err := toOpenAIInput(messages)
	if err != nil {
		t.Fatal(err)
	}
	openCode, err := openCodeChatMessages("", messages)
	if err != nil {
		t.Fatal(err)
	}
	for provider, request := range map[string]string{"anthropic": encodedText(anthropic), "openai": encodedText(openAI), "opencode": encodedText(openCode)} {
		if !strings.Contains(request, `"Review auth module carefully."`) {
			t.Errorf("%s request does not carry the expansion: %s", provider, request)
		}
		for _, invocation := range []string{"claude_project", `auth module\" carefully`, `"review"`} {
			if strings.Contains(request, invocation) {
				t.Errorf("%s request leaks invocation %q: %s", provider, invocation, request)
			}
		}
	}
	if got := compactionInputText([]InputContent{promptCommandInputForTest()}); got != "Review auth module carefully." {
		t.Fatalf("compaction text = %q", got)
	}
}
