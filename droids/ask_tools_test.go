package droids_test

import (
	"encoding/json"
	"testing"

	"github.com/akonwi/kit/droids"
)

// askToolOutcome runs one standard question tool call, answers its question
// with answer (or steers when steer is set), and returns the result text and
// details the model's conversation recorded.
func askToolOutcome(t *testing.T, tool droids.AnyTool, arguments string, answer func(droids.Interaction) droids.Answer, steer bool) (string, string) {
	t.Helper()
	providers := newInteractionProviders(callTools(toolCall("call_ask", tool.Name(), arguments)))
	store := droids.NewMemoryStore()
	droid := spawnInteractionDroid(t, droids.ConversationID("ask_"+tool.Name()), store, providers, tool)
	handle, err := droid.Prompt(t.Context(), textInput("ask"), droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pending := waitPending(t, droid, 1)
	if pending[0].Live {
		t.Fatalf("a standard question tool asked a live question: %+v", pending[0])
	}
	if steer {
		if _, err := droid.Prompt(t.Context(), textInput("never mind"), droids.PromptOptions{Steer: true}); err != nil {
			t.Fatal(err)
		}
	} else if err := droid.Respond(t.Context(), pending[0].ID, answer(pending[0])); err != nil {
		t.Fatal(err)
	}
	settle(t, handle)
	page, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, envelope := range page.Messages {
		if result, ok := envelope.Message.(droids.ToolResultMessage); ok && result.ProviderCallID == "call_ask" {
			return result.Content[0].(droids.TextContent).Text, string(result.Details)
		}
	}
	t.Fatal("the tool result is missing")
	return "", ""
}

func TestStandardQuestionToolsReturnTheAnswer(t *testing.T) {
	text := "blue"
	cases := []struct {
		name, arguments, result, details string
		tool                             droids.AnyTool
		answer                           func(droids.Interaction) droids.Answer
		steer                            bool
	}{
		{name: "confirm yes", tool: droids.ConfirmTool("confirm_from_user"), arguments: `{"title":"Ship it?"}`,
			answer: func(droids.Interaction) droids.Answer { return droids.Answer{Confirmed: yes()} },
			result: `{"confirmed":true}`, details: `{"resolution":"answered"}`},
		{name: "confirm no", tool: droids.ConfirmTool("confirm_from_user"), arguments: `{"title":"Ship it?"}`,
			answer: func(droids.Interaction) droids.Answer { return droids.Answer{Confirmed: no()} },
			result: `{"confirmed":false}`, details: `{"resolution":"negative_confirmation"}`},
		{name: "confirm steered", tool: droids.ConfirmTool("confirm_from_user"), arguments: `{"title":"Ship it?"}`, steer: true,
			result: `{"confirmed":false,"reason":"the user sent a message instead of answering"}`, details: `{"resolution":"user_cancelled"}`},
		{name: "input", tool: droids.InputTool("input_from_user"), arguments: `{"title":"Favorite color?"}`,
			answer: func(droids.Interaction) droids.Answer { return droids.Answer{Value: &text} },
			result: `{"cancelled":false,"value":"blue"}`, details: `{"resolution":"answered"}`},
		{name: "input declined", tool: droids.InputTool("input_from_user"), arguments: `{"title":"Favorite color?"}`,
			answer: func(droids.Interaction) droids.Answer { return droids.Answer{Declined: true} },
			result: `{"cancelled":true,"reason":"the user declined","value":null}`, details: `{"resolution":"user_cancelled"}`},
		{name: "select", tool: droids.SelectTool("select_from_user"), arguments: `{"title":"Branch?","options":[{"label":"Main","value":"main"},{"label":"Dev","value":"dev"}]}`,
			answer: func(i droids.Interaction) droids.Answer { return droids.Answer{OptionID: i.Question.Options[1].ID} },
			result: `{"cancelled":false,"label":"Dev","value":"dev"}`, details: `{"resolution":"answered"}`},
		{name: "guided", tool: droids.GuidedTool("guided_questions"),
			arguments: `{"title":"Setup","questions":[{"id":"name","prompt":"Name?","kind":"text"},{"id":"tags","prompt":"Tags?","kind":"multiselect","options":[{"label":"A","value":"a"},{"label":"B","value":"b"}]},{"id":"skip","prompt":"Optional?","kind":"boolean","required":false}]}`,
			answer: func(i droids.Interaction) droids.Answer {
				name := "Kit"
				return droids.Answer{Answers: map[string]droids.GuidedAnswer{
					"name": {Text: &name},
					"tags": {OptionIDs: []string{i.Question.Questions[1].Options[1].ID}},
					"skip": {Skipped: true},
				}}
			},
			result: `{"answeredCount":2,"answers":{"name":"Kit","tags":["b"]},"cancelled":false,"completed":true,"totalQuestions":3}`, details: `{"resolution":"answered"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, details := askToolOutcome(t, test.tool, test.arguments, test.answer, test.steer)
			if !jsonEqual(result, test.result) || !jsonEqual(details, test.details) {
				t.Fatalf("result = %s, details = %s; want %s, %s", result, details, test.result, test.details)
			}
		})
	}
}

func jsonEqual(left, right string) bool {
	var a, b any
	if json.Unmarshal([]byte(left), &a) != nil || json.Unmarshal([]byte(right), &b) != nil {
		return false
	}
	encodedA, _ := json.Marshal(a)
	encodedB, _ := json.Marshal(b)
	return string(encodedA) == string(encodedB)
}
