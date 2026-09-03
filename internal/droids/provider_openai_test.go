package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func TestOpenAIResponsesStreamsTextAndBuildsRequest(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newResponsesServer(t, requests,
		`{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello"}`,
		`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":" there"}`,
		`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","object":"response","created_at":1,"model":"gpt-test-2026","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello there","annotations":[]}]}],"usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":3},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18}}}`,
	)
	defer server.Close()

	report, err := NewFileURL("report.pdf", "application/pdf", "https://files.example/report.pdf?signature=abc")
	if err != nil {
		t.Fatal(err)
	}
	namedImage, err := NewFileURL("photo.png", "image/png", "https://files.example/photo.png")
	if err != nil {
		t.Fatal(err)
	}

	providers, model := testOpenAIProvider(t, server.URL)
	temperature := 0.25
	stream := providers.Stream(context.Background(), model, Request{
		SystemPrompt: "Be concise.",
		Messages: []Message{UserMessage{Content: []Content{
			TextContent{Text: "Say hello"},
			NewImageData("image/png", []byte("hello")),
			report,
			NewFileData("notes.txt", "text/plain", []byte("notes")),
			namedImage,
		}}},
		Tools: []ToolSchema{{
			Name:        "lookup",
			Description: "Look something up.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string"}},
			},
		}},
		Reasoning:   "low",
		Temperature: &temperature,
		MaxTokens:   128,
	})

	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	message := stream.Result()

	if len(events) != 5 {
		t.Fatalf("events = %#v, want start + text start + 2 deltas + done", events)
	}
	if _, ok := events[0].(StreamStart); !ok {
		t.Fatalf("first event = %T, want StreamStart", events[0])
	}
	if delta, ok := events[2].(StreamTextDelta); !ok || delta.Delta != "Hello" {
		t.Fatalf("first text delta = %#v", events[2])
	}
	if _, ok := events[4].(StreamDone); !ok {
		t.Fatalf("last event = %T, want StreamDone", events[4])
	}
	if message.Text() != "Hello there" || message.ResponseID != "resp_1" || message.ResponseModel != "gpt-test-2026" {
		t.Fatalf("message = %#v", message)
	}
	if message.StopReason != StopReasonStop {
		t.Fatalf("stop reason = %q", message.StopReason)
	}
	if message.Usage.Input != 11 || message.Usage.Output != 7 || message.Usage.CacheRead != 3 || message.Usage.Reasoning != 2 || message.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %#v", message.Usage)
	}
	text := message.Content[0].(TextContent)
	if !strings.Contains(text.Signature, `"id":"msg_1"`) {
		t.Fatalf("text signature did not retain output item: %q", text.Signature)
	}

	request := <-requests
	if request["model"] != "gpt-5-mini" || request["instructions"] != "Be concise." || request["store"] != false || request["stream"] != true {
		t.Fatalf("request controls = %#v", request)
	}
	if request["max_output_tokens"] != float64(128) || request["temperature"] != 0.25 {
		t.Fatalf("request limits = %#v", request)
	}
	reasoning := request["reasoning"].(map[string]any)
	if reasoning["effort"] != "low" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	include := request["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", include)
	}
	input := request["input"].([]any)
	user := input[0].(map[string]any)
	content := user["content"].([]any)
	if got := content[0].(map[string]any); got["type"] != "input_text" || got["text"] != "Say hello" {
		t.Fatalf("text input = %#v", got)
	}
	if got := content[1].(map[string]any); got["type"] != "input_image" || got["image_url"] != "data:image/png;base64,aGVsbG8=" || got["detail"] != "auto" {
		t.Fatalf("image input = %#v", got)
	}
	if got := content[2].(map[string]any); got["type"] != "input_file" || got["file_url"] != "https://files.example/report.pdf?signature=abc" || got["filename"] != nil {
		t.Fatalf("URL file input = %#v", got)
	}
	if got := content[3].(map[string]any); got["type"] != "input_file" || got["file_data"] != "data:text/plain;base64,bm90ZXM=" || got["filename"] != "notes.txt" {
		t.Fatalf("data file input = %#v", got)
	}
	if got := content[4].(map[string]any); got["type"] != "input_image" || got["image_url"] != "https://files.example/photo.png" {
		t.Fatalf("named image input = %#v", got)
	}
	tools := request["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "lookup" || tool["strict"] != false {
		t.Fatalf("tool = %#v", tool)
	}
}

func TestOpenAIResponsesRejectsInvalidContentWithoutRequest(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newResponsesServer(t, requests)
	defer server.Close()

	providers, model := testOpenAIProvider(t, server.URL)
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []Content{FileContent{
			Filename: "report.pdf", MediaType: "application/pdf", URL: "http://files.example/report.pdf",
		}}},
	}})
	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want start + error", events)
	}
	if _, ok := events[0].(StreamStart); !ok {
		t.Fatalf("first event = %T, want StreamStart", events[0])
	}
	if _, ok := events[1].(StreamError); !ok {
		t.Fatalf("last event = %T, want StreamError", events[1])
	}
	message := stream.Result()
	if message.StopReason != StopReasonError || !strings.Contains(message.ErrorMessage, "unsupported user content") || !strings.Contains(message.ErrorMessage, "https or data") {
		t.Fatalf("message = %#v", message)
	}
	select {
	case request := <-requests:
		t.Fatalf("invalid content reached provider: %#v", request)
	default:
	}
}

func TestOpenAIResponsesStreamsToolCall(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newResponsesServer(t, requests,
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"weather","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","sequence_number":2,"item_id":"fc_1","output_index":0,"delta":"{\"city\":"}`,
		`{"type":"response.function_call_arguments.delta","sequence_number":3,"item_id":"fc_1","output_index":0,"delta":"\"Paris\"}"}`,
		`{"type":"response.completed","sequence_number":4,"response":{"id":"resp_tool","object":"response","created_at":1,"model":"gpt-test","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Paris\"}","status":"completed"}],"usage":{"input_tokens":5,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":9}}}`,
	)
	defer server.Close()

	providers, model := testOpenAIProvider(t, server.URL)
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []Content{TextContent{Text: "Weather?"}}},
	}})

	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	message := stream.Result()

	if len(events) != 5 {
		t.Fatalf("events = %#v, want start + tool start + 2 deltas + done", events)
	}
	start, ok := events[1].(StreamToolCallStart)
	if !ok || start.ID != "call_1" || start.Name != "weather" || start.ContentIndex != 0 {
		t.Fatalf("tool start = %#v", events[1])
	}
	if message.StopReason != StopReasonToolUse {
		t.Fatalf("stop reason = %q", message.StopReason)
	}
	calls := message.ToolCalls()
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "weather" || string(calls[0].Arguments) != `{"city":"Paris"}` || calls[0].Signature != "fc_1" {
		t.Fatalf("tool calls = %#v", calls)
	}
	<-requests
}

func TestOpenAIResponsesRejectsFunctionCallWithoutCallID(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newResponsesServer(t, requests,
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"fc_1","type":"function_call","name":"weather","arguments":"","status":"in_progress"}}`,
	)
	defer server.Close()

	providers, model := testOpenAIProvider(t, server.URL)
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []Content{TextContent{Text: "Weather?"}}},
	}})
	var last StreamEvent
	for event := range stream.Events() {
		last = event
	}
	if _, ok := last.(StreamError); !ok {
		t.Fatalf("last event = %T, want StreamError", last)
	}
	message := stream.Result()
	if message.StopReason != StopReasonError || !strings.Contains(message.ErrorMessage, "missing call_id") {
		t.Fatalf("message = %#v", message)
	}
	<-requests
}

func TestOpenAIResponsesStreamsRefusalAsText(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := newResponsesServer(t, requests,
		`{"type":"response.refusal.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Cannot comply"}`,
		`{"type":"response.completed","sequence_number":2,"response":{"id":"resp_refusal","model":"gpt-test","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"Cannot comply"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":3}}}`,
	)
	defer server.Close()

	providers, model := testOpenAIProvider(t, server.URL)
	stream := providers.Stream(context.Background(), model, Request{Messages: []Message{
		UserMessage{Content: []Content{TextContent{Text: "Request"}}},
	}})
	var events []StreamEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want start + text start + refusal delta + done", events)
	}
	if delta, ok := events[2].(StreamTextDelta); !ok || delta.Delta != "Cannot comply" {
		t.Fatalf("refusal delta = %#v", events[2])
	}
	if got := stream.Result().Text(); got != "Cannot comply" {
		t.Fatalf("result text = %q", got)
	}
	<-requests
}

func TestOpenAIResponsesReplaysOutputAndReasoningItems(t *testing.T) {
	var response responses.Response
	err := json.Unmarshal([]byte(`{
		"id":"resp_1",
		"model":"gpt-test",
		"status":"completed",
		"output":[
			{"id":"rs_1","type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"Checked the inputs."}],"encrypted_content":"encrypted"},
			{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Calling the tool.","annotations":[]}]},
			{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}","status":"completed"}
		],
		"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}
	}`), &response)
	if err != nil {
		t.Fatal(err)
	}

	message := assembleResponse(Model{ID: "gpt-test", Provider: "openai"}, response)
	if len(message.Content) != 3 {
		t.Fatalf("content = %#v", message.Content)
	}
	thinking := message.Content[0].(ThinkingContent)
	if thinking.Thinking != "Checked the inputs." || !strings.Contains(thinking.Signature, `"encrypted_content":"encrypted"`) {
		t.Fatalf("thinking = %#v", thinking)
	}

	input, err := toOpenAIInput([]Message{
		UserMessage{Content: []Content{TextContent{Text: "Look it up"}}},
		message,
		ToolResultMessage{ToolCallID: "call_1", ToolName: "lookup", Content: []Content{
			TextContent{Text: "found"},
			NewImageData("image/png", []byte("image")),
			NewFileData("details.txt", "text/plain", []byte("details")),
		}, IsError: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, want := range []string{
		`"type":"reasoning"`,
		`"id":"rs_1"`,
		`"encrypted_content":"encrypted"`,
		`"type":"message"`,
		`"id":"msg_1"`,
		`"type":"function_call"`,
		`"call_id":"call_1"`,
		`"type":"function_call_output"`,
		`"text":"found","type":"input_text"`,
		`"type":"input_image"`,
		`"image_url":"data:image/png;base64,aW1hZ2U="`,
		`"file_data":"data:text/plain;base64,ZGV0YWlscw=="`,
		`"filename":"details.txt"`,
		`"type":"input_file"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("replayed input missing %s: %s", want, body)
		}
	}
}

func TestOpenAIToolURLFileOmitsFilename(t *testing.T) {
	file, err := NewFileURL("report.pdf", "application/pdf", "https://files.example/report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	input, err := openAIToolFileParam(file)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["file_url"] != "https://files.example/report.pdf" || got["filename"] != nil {
		t.Fatalf("tool URL file input = %#v", got)
	}
}

func TestOpenAIResponsesIncompleteAndErrorEvents(t *testing.T) {
	t.Run("max output tokens is a successful length stop", func(t *testing.T) {
		requests := make(chan map[string]any, 1)
		server := newResponsesServer(t, requests,
			`{"type":"response.incomplete","sequence_number":1,"response":{"id":"resp_short","model":"gpt-test","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"id":"fc_partial","type":"function_call","call_id":"call_partial","name":"dangerous","arguments":"{\"path\":","status":"incomplete"}],"usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":5}}}`,
		)
		defer server.Close()
		providers, model := testOpenAIProvider(t, server.URL)
		stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []Content{TextContent{Text: "hi"}}}}})
		var last StreamEvent
		for event := range stream.Events() {
			last = event
		}
		if _, ok := last.(StreamDone); !ok {
			t.Fatalf("last event = %T, want StreamDone", last)
		}
		message := stream.Result()
		if got := message.StopReason; got != StopReasonLength {
			t.Fatalf("stop reason = %q", got)
		}
		if len(message.ToolCalls()) != 1 {
			t.Fatalf("partial tool call should be retained for observability: %#v", message.Content)
		}
		replayInput, err := toOpenAIInput([]Message{
			message,
			UserMessage{Content: []Content{TextContent{Text: "Try something else"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := json.Marshal(replayInput)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(replayed), `"type":"function_call"`) {
			t.Fatalf("partial tool call was replayed: %s", replayed)
		}
		<-requests
	})

	t.Run("provider cancellation is aborted", func(t *testing.T) {
		requests := make(chan map[string]any, 1)
		server := newResponsesServer(t, requests,
			`{"type":"response.failed","sequence_number":1,"response":{"id":"resp_cancelled","model":"gpt-test","status":"cancelled","error":{"message":"cancelled upstream","code":"server_error"},"output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":1}}}`,
		)
		defer server.Close()
		providers, model := testOpenAIProvider(t, server.URL)
		stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []Content{TextContent{Text: "hi"}}}}})
		var last StreamEvent
		for event := range stream.Events() {
			last = event
		}
		if _, ok := last.(StreamError); !ok {
			t.Fatalf("last event = %T, want StreamError", last)
		}
		message := stream.Result()
		if message.StopReason != StopReasonAborted || message.ErrorMessage != "cancelled upstream" {
			t.Fatalf("message = %#v", message)
		}
		<-requests
	})

	t.Run("error event uses stream error", func(t *testing.T) {
		requests := make(chan map[string]any, 1)
		server := newResponsesServer(t, requests,
			`{"type":"error","sequence_number":1,"code":"server_error","message":"upstream failed","param":""}`,
		)
		defer server.Close()
		providers, model := testOpenAIProvider(t, server.URL)
		stream := providers.Stream(context.Background(), model, Request{Messages: []Message{UserMessage{Content: []Content{TextContent{Text: "hi"}}}}})
		var last StreamEvent
		for event := range stream.Events() {
			last = event
		}
		if _, ok := last.(StreamError); !ok {
			t.Fatalf("last event = %T, want StreamError", last)
		}
		message := stream.Result()
		if message.StopReason != StopReasonError || message.ErrorMessage != "upstream failed" {
			t.Fatalf("message = %#v", message)
		}
		<-requests
	})
}

func testOpenAIProvider(t *testing.T, baseURL string) (Providers, Model) {
	t.Helper()
	providers, err := NewProviders(OpenAI{
		APIKey:  "test-key",
		BaseURL: baseURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := providers.Model("gpt-5-mini")
	if !ok {
		t.Fatal("test model did not resolve")
	}
	return providers, model
}

func newResponsesServer(t *testing.T, requests chan<- map[string]any, events ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/responses" {
			t.Errorf("request = %s %s, want POST /responses", request.Method, request.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer request.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range events {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				return
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}
