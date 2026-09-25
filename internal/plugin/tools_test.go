package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registerTestTool(t *testing.T, h *Host, owner InstanceID, id string) Tool {
	t.Helper()
	params := json.RawMessage(`{"id":"` + id + `","description":"Echo exact input","inputSchema":{"type":"object","properties":{"value":{"type":"integer","const":9007199254740993}},"required":["value"],"additionalProperties":false}}`)
	result, err := h.handleRequest(t.Context(), owner, "kit/tools/register", params)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"id":"demo.`+id+`","modelName":"demo__`+id+`"}` {
		t.Fatalf("registration = %s", result)
	}
	for _, tool := range h.Tools() {
		if tool.LocalID == id {
			return tool
		}
	}
	t.Fatal("tool missing from catalog")
	return Tool{}
}
func TestPluginToolExecutionPreservesInputAndRegistrationOwnership(t *testing.T) {
	host, root := commandHost(t)
	owner := findCommand(t, host, "demo.ok").Owner
	tool := registerTestTool(t, host, owner, "echo_value")
	if tool.ExecutionMode != "sequential" || tool.Label != "echo_value" {
		t.Fatalf("tool defaults = %#v", tool)
	}
	input := json.RawMessage(`{"value":9007199254740993}`)
	result, err := host.ExecuteTool(t.Context(), tool, "call_exact", input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Terminate || len(result.Content) != 1 || result.Content[0].Text != string(input) || string(result.Details) != string(input) {
		t.Fatalf("tool result = %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "tool.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		ID, ToolCallID string
		Input          json.RawMessage
	}
	if err := json.Unmarshal(data, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.ID != "echo_value" || sent.ToolCallID != "call_exact" || string(sent.Input) != string(input) {
		t.Fatalf("execute payload = %s", data)
	}
	if _, err := host.ExecuteTool(t.Context(), tool, "bad", json.RawMessage(`{"value":9007199254740992}`)); err == nil {
		t.Fatal("rounded input passed exact schema")
	}
	if _, err := host.handleRequest(t.Context(), owner, "kit/tools/unregister", json.RawMessage(`{"id":"echo_value"}`)); err != nil {
		t.Fatal(err)
	}
	replacement := registerTestTool(t, host, owner, "echo_value")
	if replacement.Registration == tool.Registration {
		t.Fatal("registration identity reused")
	}
	if _, err := host.ExecuteTool(t.Context(), tool, "stale", input); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("stale registration = %v", err)
	}
	host.Reload()
	if _, err := host.ExecuteTool(t.Context(), replacement, "revoked", input); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("revoked tool = %v", err)
	}
}
func TestPluginToolCancellationAndMalformedResult(t *testing.T) {
	host, root := commandHost(t)
	owner := findCommand(t, host, "demo.ok").Owner
	wait := registerTestTool(t, host, owner, "wait")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTool(ctx, wait, "call_wait", json.RawMessage(`{"value":9007199254740993}`))
		done <- err
	}()
	eventuallyHost(t, func() bool { _, err := os.Stat(filepath.Join(root, "tool.json")); return err == nil })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tool cancellation blocked")
	}
	bad := registerTestTool(t, host, owner, "invalid")
	if _, err := host.ExecuteTool(t.Context(), bad, "call_bad", json.RawMessage(`{"value":9007199254740993}`)); err == nil {
		t.Fatal("malformed result accepted")
	}
	eventuallyHost(t, func() bool { return len(host.Tools()) == 0 })
}
func TestPluginToolSchemaProfileAndStrictJSON(t *testing.T) {
	for _, raw := range []string{
		`{"type":"array"}`, `{"type":"object","$ref":"https://example.com"}`, `{"type":"object","properties":{"x":{"format":"date"}}}`,
		`{"type":"object","additionalProperties":{}}`, `{"type":"object","type":"object"}`, `{"type":"object","properties":{"x":{"type":["string","null"]}}}`,
		`{"type":"object","properties":{"x":{"type":"string","pattern":"["}}}`, `{"type":"object","required":[1]}`,
	} {
		if _, err := compilePluginToolSchema(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid schema %s", raw)
		}
	}
	schema, err := compilePluginToolSchema(json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"null"},{"type":"array","minItems":1,"maxItems":2,"items":{"type":"string","minLength":2,"pattern":"^a"}}]}},"required":["x"],"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"x":null}`, `{"x":["ab"]}`} {
		value, err := decodeToolJSON([]byte(raw))
		if err != nil || schema.Validate(value) != nil {
			t.Fatalf("valid input rejected: %s, %v", raw, err)
		}
	}
	for _, raw := range []string{`{"x":["b"]}`, `{}`, `{"x":null,"extra":true}`} {
		value, _ := decodeToolJSON([]byte(raw))
		if schema.Validate(value) == nil {
			t.Errorf("invalid input accepted: %s", raw)
		}
	}
	if _, err := decodeToolJSON([]byte(`{"x":1,"x":2}`)); err == nil {
		t.Fatal("duplicate input accepted")
	}
	if _, err := compilePluginToolSchema(json.RawMessage(`{"type":"object","properties":{"x":` + strings.Repeat(`{"items":`, 18) + `{}` + strings.Repeat(`}`, 18) + `}}`)); err == nil {
		t.Fatal("deep schema accepted")
	}
}
func TestToolJSONNumberBudgets(t *testing.T) {
	for _, raw := range []string{`1e1000000000`, `1e-1000000000`, strings.Repeat("9", 1025)} {
		if _, err := decodeToolJSON([]byte(raw)); err == nil {
			t.Fatalf("oversized JSON number accepted: %.40s", raw)
		}
	}
	if _, err := decodeToolJSON([]byte(`9007199254740993`)); err != nil {
		t.Fatal(err)
	}
}

func TestPluginToolResultContract(t *testing.T) {
	for _, raw := range []string{`null`, `{"content":null}`, `{"content":[],"unknown":true}`, `{"content":[],"terminate":null}`, `{"content":[{"type":"text","text":"x","extra":1}]}`, `{"content":[{"type":"image","data":"aGVsbG8=","mimeType":"text/plain"}]}`} {
		if _, err := parseToolResult(json.RawMessage(raw)); err == nil {
			t.Errorf("invalid tool result accepted: %s", raw)
		}
	}
	result, err := parseToolResult(json.RawMessage(`{"content":[{"type":"text","text":"ok"},{"type":"image","data":"aGVsbG8=","mimeType":"image/png"}],"details":{"n":9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 2 || result.Content[1].MIMEType != "image/png" || string(result.Details) != `{"n":9007199254740993}` {
		t.Fatalf("result = %#v", result)
	}
}

func TestPluginToolResultAggregateLimit(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": strings.Repeat("x", MaxToolResultBytes)}, {"type": "text", "text": "overflow"}}})
	if _, err := parseToolResult(raw); err == nil {
		t.Fatal("aggregate tool result limit ignored")
	}
}

func TestPluginToolRegistrationRejectsMalformedOptionsAndLongModelNames(t *testing.T) {
	for _, extra := range []string{`,"executionMode":null`, `,"executionMode":""`, `,"executionMode":"background"`, `,"promptGuidelines":null`, `,"promptGuidelines":[1]`, `,"inputSchema":{}`} {
		raw := json.RawMessage(`{"id":"echo","description":"Echo","inputSchema":{"type":"object"}` + extra + `}`)
		if _, err := parseTool(raw); err == nil {
			t.Errorf("invalid registration accepted: %s", raw)
		}
	}
	host, _ := commandHost(t)
	owner := findCommand(t, host, "demo.ok").Owner
	raw := json.RawMessage(`{"id":"` + strings.Repeat("a", 64) + `","description":"Too long","inputSchema":{"type":"object"}}`)
	if _, err := host.handleRequest(t.Context(), owner, "kit/tools/register", raw); err == nil {
		t.Fatal("oversized model-facing name accepted")
	}
}
