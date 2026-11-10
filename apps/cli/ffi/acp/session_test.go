package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

// clientCapabilities a client like Zed advertises.
var richClient = map[string]any{
	"fs": map[string]any{"readTextFile": true, "writeTextFile": true}, "terminal": true,
	"session":     map[string]any{"compaction": map[string]any{}, "notices": map[string]any{}},
	"elicitation": map[string]any{"form": map[string]any{}},
}

func TestAPromptStreamsTheReplyAndEndsTheTurn(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	response, _ := p.prompt(sessionID, "hello")
	if got := stopReason(t, response); got != "end_turn" {
		t.Fatalf("stopReason = %q, want end_turn", got)
	}
	p.golden("prompt-turn")
}

func TestToolCallsReportTheirKindLocationAndDiff(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newPeer(t, cwd)
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	response, _ := p.prompt(sessionID, "edit notes.txt")
	if got := stopReason(t, response); got != "end_turn" {
		t.Fatalf("stopReason = %q", got)
	}
	content, err := os.ReadFile(filepath.Join(cwd, "notes.txt"))
	if err != nil || string(content) != "new line\n" {
		t.Fatalf("notes.txt = %q, %v; want the edit applied on disk", content, err)
	}
	p.golden("tool-edit")
}

func TestAReadToolPointsAtTheFile(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newPeer(t, cwd)
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	response, _ := p.prompt(sessionID, "read notes.txt")
	if got := stopReason(t, response); got != "end_turn" {
		t.Fatalf("stopReason = %q", got)
	}
	p.golden("tool-read")
}

func TestAConfirmationIsAPermissionRequestOnItsToolCall(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		option string
	}{{"allow", "allow"}, {"reject", "reject"}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p := newPeer(t, t.TempDir())
			p.answer = func(method string, _ json.RawMessage) any {
				if method != "session/request_permission" {
					t.Errorf("kit acp asked %s", method)
					return nil
				}
				return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": test.option}}
			}
			p.initialize(map[string]any{})
			sessionID := p.newSession()
			response, _ := p.prompt(sessionID, "confirm")
			if got := stopReason(t, response); got != "end_turn" {
				t.Fatalf("stopReason = %q", got)
			}
			p.golden("permission-"+test.name, interactionExchange)
		})
	}
}

func TestAChoiceIsAnElicitationFormWhenTheClientSupportsForms(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.answer = func(method string, params json.RawMessage) any {
		if method != "elicitation/create" {
			t.Errorf("kit acp asked %s", method)
			return nil
		}
		var request elicitationParams
		if err := json.Unmarshal(params, &request); err != nil {
			t.Error(err)
			return nil
		}
		choices := request.RequestedSchema.Properties[formChoice].OneOf
		return map[string]any{"action": "accept", "content": map[string]any{formChoice: choices[1].Const}}
	}
	p.initialize(richClient)
	sessionID := p.newSession()
	response, updates := p.prompt(sessionID, "choose")
	if got := stopReason(t, response); got != "end_turn" {
		t.Fatalf("stopReason = %q", got)
	}
	if !anyUpdateContains(updates, `\"label\":\"Blue\"`) {
		t.Fatalf("the tool result does not report the chosen option; updates:\n%s", rawUpdates(updates))
	}
	p.golden("elicitation-select", interactionExchange)
}

func TestAChoiceWaitsForAnotherClientWhenTheClientHasNoForms(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	id := p.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": "choose"}}})
	p.until(func(received message) bool { return strings.Contains(received.raw, `"status":"in_progress"`) })
	waitFor(t, func() bool { return strings.Contains(p.stderr.String(), "interaction left for another Kit client") })
	p.notify("session/cancel", map[string]any{"sessionId": sessionID})
	response, _ := p.await(id)
	if got := stopReason(t, response); got != "cancelled" {
		t.Fatalf("stopReason = %q, want cancelled", got)
	}
}

func TestSessionCancelEndsThePromptAsCancelled(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	id := p.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": "slow"}}})
	p.notify("session/cancel", map[string]any{"sessionId": sessionID})
	response, _ := p.await(id)
	if got := stopReason(t, response); got != "cancelled" {
		t.Fatalf("stopReason = %q, want cancelled", got)
	}
	response, _ = p.prompt(sessionID, "hello")
	if got := stopReason(t, response); got != "end_turn" {
		t.Fatalf("next prompt stopReason = %q, want end_turn", got)
	}
}

func TestCancelRequestEndsThePromptAsCancelled(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	id := p.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": "slow"}}})
	p.notify("$/cancel_request", map[string]any{"requestId": id})
	response, _ := p.await(id)
	if got := stopReason(t, response); got != "cancelled" {
		t.Fatalf("stopReason = %q, want cancelled", got)
	}
}

func TestCancelRequestForAnotherRequestAnswersItCancelled(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	p := newPeer(t, t.TempDir(), withConnect(func(ctx context.Context) (Backend, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return connectTestServer(context.Background())
	}))
	p.initialize(map[string]any{})
	id := p.request("session/list", map[string]any{})
	p.notify("$/cancel_request", map[string]any{"requestId": id})
	response, _ := p.await(id)
	if response.Error == nil || response.Error.Code != codeCancelled {
		t.Fatalf("response = %s, want -32800", response.raw)
	}
	close(block)
}

func TestAFailedTurnAnswersThePromptWithItsError(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	response, _ := p.prompt(sessionID, "fail auth")
	if response.Error == nil || response.Error.Code != codeInternalError || response.Error.Message != "Provider authentication failed" {
		t.Fatalf("response = %s, want the turn's error", response.raw)
	}
}

func TestTurnOutcomesBecomeStopReasonsOrErrors(t *testing.T) {
	t.Parallel()
	s := &session{}
	for _, test := range []struct {
		outcome turnOutcome
		reason  string
		code    int
	}{
		{turnOutcome{status: "completed"}, "end_turn", 0},
		{turnOutcome{status: "aborted"}, "cancelled", 0},
		{turnOutcome{status: "interrupted"}, "cancelled", 0},
		{turnOutcome{status: "failed", kind: "authentication", message: "Provider authentication failed"}, "", codeAuthRequired},
		{turnOutcome{status: "failed", kind: "rate_limit", message: "Provider request was rate limited"}, "", codeInternalError},
	} {
		result, err := s.promptOutcome(&promptState{}, test.outcome)
		if test.code != 0 {
			if failure := asRPCError(err); err == nil || failure.Code != test.code {
				t.Errorf("%+v answered %v, %v; want error %d", test.outcome, result, err, test.code)
			}
			continue
		}
		if err != nil || result.(promptResult).StopReason != test.reason {
			t.Errorf("%+v answered %v, %v; want %s", test.outcome, result, err, test.reason)
		}
	}
}

func TestLoadingASessionReplaysItsTranscript(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := newPeer(t, cwd)
	first.initialize(map[string]any{})
	sessionID := first.newSession()
	if response, _ := first.prompt(sessionID, "read notes.txt"); stopReason(t, response) != "end_turn" {
		t.Fatal("prompt did not end")
	}
	_ = first.close()

	second := newPeer(t, cwd)
	second.initialize(map[string]any{})
	var listed listSessionsResult
	if err := json.Unmarshal(second.call("session/list", map[string]any{"cwd": cwd}), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != sessionID || listed.Sessions[0].CWD != cwd {
		t.Fatalf("session/list = %+v, want the session in %s", listed, cwd)
	}
	second.call("session/load", map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}})
	second.until(func(received message) bool { return updateKind(received) == "available_commands_update" })
	second.golden("load-replay")
}

func TestLoadingInAnotherDirectoryIsRejected(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	p := newPeer(t, cwd)
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	other := newPeer(t, t.TempDir())
	other.initialize(map[string]any{})
	failure := other.callError("session/load", map[string]any{"sessionId": sessionID, "cwd": other.cwd, "mcpServers": []any{}})
	if failure.Code != codeInvalidParams {
		t.Fatalf("session/load error = %+v, want -32602", failure)
	}
	missing := other.callError("session/resume", map[string]any{"sessionId": "session_00000000000000000000000000000000", "cwd": other.cwd, "mcpServers": []any{}})
	if missing.Code != codeNotFound {
		t.Fatalf("session/resume error = %+v, want -32002", missing)
	}
}

func TestServerErrorsBecomeACPErrors(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	const missing = "session_0123456789abcdef0123456789abcdef"
	for _, method := range []string{"session/load", "session/resume"} {
		failure := p.callError(method, map[string]any{"sessionId": missing, "cwd": p.cwd, "mcpServers": []any{}})
		data, _ := json.Marshal(failure.Data)
		if failure.Code != codeNotFound || string(data) != `{"sessionId":"`+missing+`"}` {
			t.Fatalf("%s of a session the server lacks = %+v, want -32002 naming it", method, failure)
		}
	}
	// The server rejects a cwd that does not exist as an invalid request.
	failure := p.callError("session/new", map[string]any{"cwd": filepath.Join(p.cwd, "missing"), "mcpServers": []any{}})
	if failure.Code != codeInvalidParams || failure.Message != "invalid request" {
		t.Fatalf("session/new in a missing directory = %+v, want -32602 with the server's message", failure)
	}
}

func TestResumingASessionDoesNotReplayAndSharesItsTurns(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	owner := newPeer(t, cwd)
	owner.initialize(map[string]any{})
	sessionID := owner.newSession()

	watcher := newPeer(t, cwd)
	watcher.initialize(map[string]any{})
	_, before := watcher.await(watcher.request("session/resume", map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}}))
	if len(before) != 0 {
		t.Fatalf("session/resume sent %d updates before answering, want none", len(before))
	}
	watcher.until(func(received message) bool { return updateKind(received) == "available_commands_update" })

	id := owner.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": "slow"}}})
	watcher.until(func(received message) bool {
		return updateKind(received) == "user_message_chunk" && strings.Contains(received.raw, `"text":"slow"`)
	})
	busy, _ := watcher.prompt(sessionID, "hello")
	if busy.Error == nil || !strings.Contains(busy.Error.Message, "busy") {
		t.Fatalf("prompt during another client's turn = %s, want a busy error", busy.raw)
	}
	owner.notify("session/cancel", map[string]any{"sessionId": sessionID})
	if response, _ := owner.await(id); stopReason(t, response) != "cancelled" {
		t.Fatal("the owner's prompt was not cancelled")
	}
}

func TestConfigOptionsSelectTheModelAndThinkingLevel(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	var created newSessionResult
	if err := json.Unmarshal(p.call("session/new", map[string]any{"cwd": p.cwd, "mcpServers": []any{}}), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.ConfigOptions) != 2 || created.ConfigOptions[0].ID != "model" || created.ConfigOptions[0].CurrentValue != "test/script" || created.ConfigOptions[1].ID != "thinking" {
		t.Fatalf("configOptions = %+v, want the model and thinking options", created.ConfigOptions)
	}
	var thinking setConfigOptionResult
	if err := json.Unmarshal(p.call("session/set_config_option", map[string]any{"sessionId": created.SessionID, "configId": "thinking", "value": "high"}), &thinking); err != nil {
		t.Fatal(err)
	}
	if thinking.ConfigOptions[1].CurrentValue != "high" {
		t.Fatalf("thinking = %+v, want high", thinking.ConfigOptions[1])
	}
	// A model without thinking levels keeps the thinking option, offering
	// only "off", so the client's list of options does not change shape.
	const withoutThinking = `{"configOptions":[` +
		`{"id":"model","name":"Model","category":"model","type":"select","currentValue":"test/other","options":[{"group":"test","name":"test","options":[{"value":"test/other","name":"Other"},{"value":"test/script","name":"Script"}]}]},` +
		`{"id":"thinking","name":"Thinking","category":"thought_level","type":"select","currentValue":"off","options":[{"value":"off","name":"Off"}]}]}`
	if model := p.call("session/set_config_option", map[string]any{"sessionId": created.SessionID, "configId": "model", "value": "test/other"}); string(model) != withoutThinking {
		t.Fatalf("after choosing test/other:\n%s\nwant\n%s", model, withoutThinking)
	}
	if off := p.call("session/set_config_option", map[string]any{"sessionId": created.SessionID, "configId": "thinking", "value": "off"}); string(off) != withoutThinking {
		t.Fatalf("after choosing off:\n%s\nwant\n%s", off, withoutThinking)
	}
	failure := p.callError("session/set_config_option", map[string]any{"sessionId": created.SessionID, "configId": "model", "value": "test/missing"})
	if failure.Code != codeInvalidParams {
		t.Fatalf("unknown model error = %+v, want -32602", failure)
	}
}

func TestClientMCPServersAreReportedNotConnected(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(richClient)
	p.call("session/new", map[string]any{"cwd": p.cwd, "mcpServers": []any{map[string]any{
		"name": "filesystem", "command": "/usr/bin/false", "args": []any{}, "env": []any{map[string]any{"name": "TOKEN", "value": "secret-value"}},
	}}})
	notice := p.until(func(received message) bool { return updateKind(received) == "notice" })
	if !strings.Contains(notice.raw, "filesystem") || strings.Contains(notice.raw, "secret-value") {
		t.Fatalf("notice = %s, want the server named without its env", notice.raw)
	}
	stderr := p.stderr.String()
	if strings.Count(stderr, "does not connect to MCP servers") != 1 || !strings.Contains(stderr, "filesystem") || strings.Contains(stderr, "secret-value") {
		t.Fatalf("stderr = %q, want one diagnostic naming the server without its env", stderr)
	}
}

func TestTemporarySessionsAreNotListedAndAreDisposedOnClose(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir(), withTemporary())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	var listed listSessionsResult
	if err := json.Unmarshal(p.call("session/list", map[string]any{"cwd": p.cwd}), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 0 {
		t.Fatalf("session/list = %+v, want no temporary session", listed)
	}
	p.call("session/close", map[string]any{"sessionId": sessionID})
	p.call("session/close", map[string]any{"sessionId": sessionID})
	failure := p.callError("session/resume", map[string]any{"sessionId": sessionID, "cwd": p.cwd, "mcpServers": []any{}})
	if failure.Code != codeNotFound {
		t.Fatalf("resuming a disposed temporary session = %+v, want -32002", failure)
	}
}

func TestEmbeddedContextAndLinksBecomePromptText(t *testing.T) {
	t.Parallel()
	text, attachments, err := convertPrompt([]contentBlock{
		textBlock("Compare "),
		{Type: "resource_link", URI: "file:///work/a.go", Name: "a.go"},
		textBlock(" with "),
		{Type: "resource", Resource: &embeddedResource{URI: "file:///work/b.go", Text: ptr("package b\n")}},
		textBlock(" and see "),
		{Type: "resource_link", URI: "https://example.com/spec", Name: "the spec"},
	}, true)
	if err != nil || len(attachments) != 0 {
		t.Fatalf("convertPrompt() = %v, %v", attachments, err)
	}
	want := "Compare /work/a.go with /work/b.go and see [the spec](https://example.com/spec)\n\nContents of /work/b.go:\n\n```\npackage b\n```"
	if text != want {
		t.Fatalf("prompt text =\n%s\nwant\n%s", text, want)
	}
}

func TestAnImageIsANoteForAModelWithoutImageInput(t *testing.T) {
	t.Parallel()
	blocks := []contentBlock{textBlock("Describe "), {Type: "image", MimeType: "image/png", Data: "iVBORw0KGgo="}, textBlock(" please")}
	text, attachments, err := convertPrompt(blocks, false)
	if err != nil || len(attachments) != 0 {
		t.Fatalf("convertPrompt() = %v, %v", attachments, err)
	}
	if want := `Describe [Image "attachment-2.png" omitted: this model does not accept image input.] please`; text != want {
		t.Fatalf("prompt text = %q, want %q", text, want)
	}

	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	sessionID := p.newSession()
	response, _ := p.await(p.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": blocks}))
	if reason := stopReason(t, response); reason != "end_turn" {
		t.Fatalf("stopReason = %q, want end_turn", reason)
	}
}

func TestPromptCommandsAreRecognizedByName(t *testing.T) {
	t.Parallel()
	commands := []protocol.PromptCommand{{Name: "review"}}
	for _, test := range []struct{ text, name, args string }{
		{"/review src/main.go", "review", "src/main.go"},
		{"  /review\nfocus on errors", "review", "focus on errors"},
		{"/compact", "compact", ""},
	} {
		name, args, ok := promptCommand(test.text, commands)
		if !ok || name != test.name || args != test.args {
			t.Errorf("promptCommand(%q) = %q, %q, %v", test.text, name, args, ok)
		}
	}
	if _, _, ok := promptCommand("/unknown thing", commands); ok {
		t.Error("an unknown command was recognized")
	}
}

func anyUpdateContains(updates []message, text string) bool {
	for _, update := range updates {
		if strings.Contains(update.raw, text) {
			return true
		}
	}
	return false
}

func rawUpdates(updates []message) string {
	lines := make([]string, 0, len(updates))
	for _, update := range updates {
		lines = append(lines, strings.TrimSpace(update.raw))
	}
	return strings.Join(lines, "\n")
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(peerTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ptr[T any](value T) *T { return &value }
