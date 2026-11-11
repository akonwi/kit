package pluginprotocol

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCommittedSchemasAreCurrent(t *testing.T) {
	for name, generate := range map[string]func() ([]byte, error){
		"protocol.schema.json": Schema,
		"manifest.schema.json": ManifestSchema,
	} {
		generated, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile("../../docs/plugin-protocol/v2/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("docs/plugin-protocol/v2/%s is stale; run go run ./api/pluginprotocol/cmd/emit", name)
		}
	}
}

func TestSchemasAreDeterministic(t *testing.T) {
	first, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := Schema()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("protocol schema differs between runs")
		}
	}
}

func compile(t *testing.T, generate func() ([]byte, error), pointer string) *validator.Schema {
	t.Helper()
	raw, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	const location = "https://kit.invalid/schema.json"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location + pointer)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validate(schema *validator.Schema, message string) error {
	value, err := validator.UnmarshalJSON(strings.NewReader(message))
	if err != nil {
		return err
	}
	return schema.Validate(value)
}

// validMessages has at least one valid message for every method, keyed by
// the side that sends it.
var validMessages = map[string][]string{
	"kit": {
		`{"jsonrpc":"2.0","id":"kit-1","method":"initialize","params":{"protocolVersion":2,"kit":{"version":"0.58.0"},"instance":{"id":"pluginhost_0123:1"},"context":{"project":{"cwd":"/repo","git":{"root":"/repo","branch":"main","dirty":false,"pullRequest":{"number":7,"url":"https://github.com/a/b/pull/7","checks":{"state":"failing","required":"unsatisfied","counts":{"passed":1,"failed":1,"pending":0,"skipped":0,"canceled":0,"unknown":0},"items":[{"name":"test","workflow":"CI","state":"failed","required":true}],"omitted":1}}}},"session":{"id":"session_1","name":null}}}}`,
		`{"jsonrpc":"2.0","id":"kit-2","method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"kit/cancel","params":{"id":"kit-3"}}`,
		// Kit-sent objects are open: a later version 2 Kit may add members.
		`{"jsonrpc":"2.0","method":"kit/cancel","params":{"id":12,"reason":"turn aborted"}}`,
		`{"jsonrpc":"2.0","id":"kit-4","method":"kit/commands/execute","params":{"id":"speech.toggle","args":""}}`,
		`{"jsonrpc":"2.0","id":"kit-5","method":"kit/tools/execute","params":{"id":"speak","toolCallId":"call-1","input":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":"kit-6","method":"kit/tool-calls/before-execute","params":{"toolCall":{"id":"call-1","name":"bash","input":{"command":"ls"}}}}`,
		`{"jsonrpc":"2.0","method":"kit/events/project.changed","params":{"cwd":"/other","git":null}}`,
		`{"jsonrpc":"2.0","method":"kit/events/session.changed","params":{"id":"session_1","name":"Renamed"}}`,
		`{"jsonrpc":"2.0","method":"kit/events/git.changed","params":{"git":{"root":"/repo","branch":null,"dirty":true,"pullRequest":null}}}`,
		`{"jsonrpc":"2.0","method":"kit/events/agent.turn.started","params":{"sessionId":"session_1","turnId":"turn_1"}}`,
		`{"jsonrpc":"2.0","method":"kit/events/agent.turn.completed","params":{"sessionId":"session_1","turn":{"id":"turn_1","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[]}]}}}`,
		`{"jsonrpc":"2.0","id":"plugin-1","result":{"id":"speech.toggle"}}`,
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32006,"message":"Session is busy","data":{"reason":"session_busy"}}}`,
		// Objects Kit sends are open: a newer Kit may add members.
		`{"jsonrpc":"2.0","method":"kit/events/session.changed","params":{"id":"session_1","name":null,"future":true}}`,
		`{"jsonrpc":"2.0","id":"kit-7","method":"initialize","params":{"protocolVersion":2,"kit":{"version":"0.60.0"},"instance":{"id":"pluginhost_0123:1"},"context":{"project":{"cwd":"/repo","git":null},"session":{"id":"session_1","name":null}},"status":{"sessionId":"session_1","model":"openai/gpt-5","thinkingLevel":"","parent":null,"context":null,"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}}}`,
		`{"jsonrpc":"2.0","method":"kit/events/session.status.changed","params":{"sessionId":"session_2","model":"anthropic/claude-sonnet-4-5","thinkingLevel":"medium","parent":{"id":"session_1","name":null},"context":{"tokens":48210,"window":200000},"usage":{"input":812345,"output":40211,"cacheRead":700000,"cacheWrite":52000,"reasoning":9000,"totalTokens":1613556,"cost":{"input":2.44,"output":0.6,"cacheRead":0.21,"cacheWrite":0.2,"total":3.45}}}}`,
		`{"jsonrpc":"2.0","id":"plugin-2","result":{"sessionId":"session_2","model":"anthropic/claude-sonnet-4-5","thinkingLevel":"medium","parent":{"id":"session_1","name":null},"context":{"tokens":48210,"window":200000},"usage":{"input":812345,"output":40211,"cacheRead":700000,"cacheWrite":52000,"reasoning":9000,"totalTokens":1613556,"cost":{"input":2.44,"output":0.6,"cacheRead":0.21,"cacheWrite":0.2,"total":3.45}}}}`,
		`{"jsonrpc":"2.0","method":"kit/events/compaction.started","params":{"sessionId":"session_1","compactionId":"compaction_1","turnId":null}}`,
		`{"jsonrpc":"2.0","method":"kit/events/compaction.completed","params":{"sessionId":"session_1","compactionId":"compaction_1","turnId":"turn_1","failureReason":null}}`,
		`{"jsonrpc":"2.0","method":"kit/events/compaction.completed","params":{"sessionId":"session_1","compactionId":"compaction_1","turnId":null,"failureReason":"provider_error"}}`,
	},
	"plugin": {
		`{"jsonrpc":"2.0","id":"kit-1","result":{"protocolVersion":2}}`,
		`{"jsonrpc":"2.0","id":"kit-2","result":null}`,
		`{"jsonrpc":"2.0","method":"kit/cancel","params":{"id":12}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/commands/register","params":{"id":"settings.open","description":"Open settings","argName":null,"category":"Speech"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"kit/commands/unregister","params":{"id":"settings.open"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"kit/tools/register","params":{"id":"speak_text","label":"Speak","description":"Speak text","inputSchema":{"type":"object","properties":{"text":{"type":"string","minLength":1},"voice":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["text"],"additionalProperties":false},"executionMode":"parallel","promptSnippet":null,"promptGuidelines":["Speak briefly."]}}`,
		`{"jsonrpc":"2.0","id":4,"method":"kit/tools/unregister","params":{"id":"speak_text"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"kit/tool-calls/register-interceptor"}`,
		`{"jsonrpc":"2.0","id":6,"method":"kit/tool-calls/unregister-interceptor"}`,
		`{"jsonrpc":"2.0","id":7,"method":"kit/subagents/register","params":{"id":"reviewer","description":"Reviews","instructions":"Review the change.","model":null}}`,
		`{"jsonrpc":"2.0","id":8,"method":"kit/subagents/unregister","params":{"id":"reviewer"}}`,
		`{"jsonrpc":"2.0","id":9,"method":"kit/footer/set","params":{"id":"status","content":[{"text":"speech "},{"text":"on","style":{"fg":"toolText","bold":true}}]}}`,
		`{"jsonrpc":"2.0","id":10,"method":"kit/footer/clear","params":{"id":"status"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"kit/footer/hide","params":{"id":"kit.footer.location"}}`,
		`{"jsonrpc":"2.0","id":12,"method":"kit/footer/show","params":{"id":"other.status"}}`,
		`{"jsonrpc":"2.0","method":"kit/ui/toast","params":{"title":"Done","subtitle":null,"variant":"info","persistent":false}}`,
		`{"jsonrpc":"2.0","id":13,"method":"kit/ui/confirm","params":{"title":"Run?","message":"` + "```sh\\nls\\n```" + `","confirmLabel":"Run","cancelLabel":null,"defaultValue":false,"parentId":"kit-6"}}`,
		`{"jsonrpc":"2.0","id":14,"method":"kit/ui/input","params":{"title":"Name","placeholder":"name","initialValue":""}}`,
		`{"jsonrpc":"2.0","id":15,"method":"kit/ui/select","params":{"title":"Target","options":[{"label":"File","value":{"kind":"file"}},{"label":"None","value":null,"description":null}],"placeholder":null}}`,
		`{"jsonrpc":"2.0","id":16,"method":"kit/session/submit-message","params":{"text":"Continue.","idempotencyKey":"loop-1"}}`,
		`{"jsonrpc":"2.0","id":17,"method":"kit/session/status"}`,
		`{"jsonrpc":"2.0","id":17,"method":"kit/system/open-url","params":{"url":"https://example.com/docs"}}`,
		`{"jsonrpc":"2.0","id":"kit-5","result":{"content":[{"type":"text","text":"spoke"},{"type":"image","data":"iVBORw0KGgo=","mimeType":"image/png"}],"details":{"ok":true},"terminate":false}}`,
		`{"jsonrpc":"2.0","id":"kit-6","result":{"action":"reject-and-continue","message":"The user rejected this command."}}`,
		`{"jsonrpc":"2.0","id":"kit-4","error":{"code":-32000,"message":"command failed"}}`,
	},
}

// invalidMessages are messages a sender must not write.
var invalidMessages = map[string][]string{
	"kit": {
		`{"jsonrpc":"2.0","method":"kit/events/compaction.completed","params":{"sessionId":"session_1","compactionId":"compaction_1","turnId":null,"failureReason":"exploded"}}`,
		`{"jsonrpc":"2.0","method":"kit/events/session.status.changed","params":{"sessionId":"session_1","model":"openai/gpt-5","thinkingLevel":"","parent":null,"context":{"tokens":1,"window":0},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}}`,
		`{"jsonrpc":"2.0","method":"kit/events/session.status.changed","params":{"sessionId":"session_1","model":"openai/gpt-5","thinkingLevel":""}}`,
	},
	"plugin": {
		`[{"jsonrpc":"2.0","id":1,"method":"kit/tools/unregister","params":{"id":"x"}}]`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/footer/set","params":{"id":"status","content":[{"text":"x"}],"side":"right"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/footer/set","params":{"id":"status","content":[{"text":"x"}],"clickable":false}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/ui/select","params":{"title":"x","options":[{"label":"a","value":1}],"filterable":true}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/session/submit-message","params":{"sessionId":"session_1","text":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/header/set","params":{"id":"x","content":[{"text":"x"}]}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/system-prompt/set","params":{"text":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/footer/click","params":{"id":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/tools/register","params":{"id":"Bad","description":"x","inputSchema":{"type":"object"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/tools/register","params":{"id":"ok","description":"x","inputSchema":{"type":"object","$ref":"#/x"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/tool-calls/register-interceptor","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"kit/session/status","params":{}}`,
		`{"jsonrpc":"2.0","method":"kit/ui/toast","params":{"title":"x","variant":"debug"}}`,
		`{"jsonrpc":"2.0","method":"kit/ui/toast","params":{"title":"x","variant":"info","extra":1}}`,
		`{"jsonrpc":"2.0","method":"kit/cancel","params":{"id":"kit-3","reason":"done"}}`,
	},
}

func TestEveryMethodHasAValidExample(t *testing.T) {
	seen := map[string]bool{}
	for _, messages := range validMessages {
		for _, message := range messages {
			var envelope struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal([]byte(message), &envelope)
			seen[envelope.Method] = true
		}
	}
	for _, method := range Methods() {
		if !seen[method.Name] {
			t.Errorf("no valid example of %s", method.Name)
		}
	}
}

func TestMessagesValidateAgainstTheSendersSchema(t *testing.T) {
	schemas := map[string]*validator.Schema{
		"kit":    compile(t, Schema, "#/$defs/KitMessage"),
		"plugin": compile(t, Schema, "#/$defs/PluginMessage"),
	}
	root := compile(t, Schema, "")
	for sender, messages := range validMessages {
		for _, message := range messages {
			if err := validate(schemas[sender], message); err != nil {
				t.Errorf("%s message should be valid: %s\n%v", sender, message, err)
			}
			if err := validate(root, message); err != nil {
				t.Errorf("message should match the root schema: %s\n%v", message, err)
			}
		}
	}
	for sender, messages := range invalidMessages {
		for _, message := range messages {
			if err := validate(schemas[sender], message); err == nil {
				t.Errorf("%s message should be invalid: %s", sender, message)
			}
		}
	}
}

func TestPluginSentObjectsAreClosedAndKitSentObjectsAreOpen(t *testing.T) {
	raw, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	closed := []string{"InitializeResult", "CommandRegisterParams", "ToolRegisterParams", "ToolExecuteResult", "FooterSetParams", "FooterSegment", "FooterStyle", "ToastParams", "ConfirmParams", "SelectParams", "SelectOption", "SubmitMessageParams", "OpenURLParams", "CancelParams", "CancelNotification", "KitCancelNotification"}
	open := []string{"InitializeParams", "Context", "Project", "Git", "PullRequest", "Session", "ToolExecuteParams", "ToolCall", "CommandExecuteParams", "TurnCompletedParams", "Turn", "Message", "CanonicalIDResult", "ToolRegisterResult", "SubmitMessageResult", "ErrorData", "KitCancelParams"}
	for _, name := range closed {
		if document.Defs[name]["additionalProperties"] != false {
			t.Errorf("%s should be closed", name)
		}
	}
	for _, name := range open {
		if _, present := document.Defs[name]["additionalProperties"]; present {
			t.Errorf("%s should be open", name)
		}
	}
}

func TestManifestSchema(t *testing.T) {
	schema := compile(t, ManifestSchema, "")
	valid := `{"$schema":"https://raw.githubusercontent.com/akonwi/kit/main/docs/plugin-protocol/v2/manifest.schema.json","manifestVersion":1,"protocolVersion":2,"id":"speech","name":"Speech","transport":{"type":"stdio","command":"python3","args":["-u","plugin.py"]}}`
	if err := validate(schema, valid); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	for _, invalid := range []string{
		`{"manifestVersion":1,"id":"speech","transport":{"type":"stdio","command":"python3"}}`,
		`{"manifestVersion":1,"protocolVersion":1,"id":"speech","transport":{"type":"stdio","command":"python3"}}`,
		`{"manifestVersion":1,"protocolVersion":2,"id":"kit-speech","transport":{"type":"stdio","command":"python3"}}`,
		`{"manifestVersion":1,"protocolVersion":2,"id":"speech","transport":{"type":"http","command":"python3"}}`,
		`{"manifestVersion":1,"protocolVersion":2,"id":"speech","transport":{"type":"stdio","command":"python3"},"extra":true}`,
	} {
		if err := validate(schema, invalid); err == nil {
			t.Errorf("manifest should be invalid: %s", invalid)
		}
	}
}

func TestMethodCatalogIsConsistent(t *testing.T) {
	names, titles := map[string]bool{}, map[string]bool{}
	for _, method := range Methods() {
		if names[method.Name] || titles[method.Title] {
			t.Errorf("duplicate method %s or title %s", method.Name, method.Title)
		}
		names[method.Name], titles[method.Title] = true, true
		if method.Notification != (method.Result == nil) {
			t.Errorf("%s: notifications have no result and requests have one", method.Name)
		}
		if method.Summary == "" {
			t.Errorf("%s has no summary", method.Name)
		}
		if found, ok := MethodNamed(method.Name); !ok || found.Title != method.Title {
			t.Errorf("MethodNamed(%s) = %v, %v", method.Name, found, ok)
		}
	}
}

// TestPatternsAvoidEngineDependentShorthand keeps every pattern portable
// between ECMA-262 and RE2 validators, which disagree on what shorthand
// classes such as \s match.
func TestPatternsAvoidEngineDependentShorthand(t *testing.T) {
	for _, generate := range []func() ([]byte, error){Schema, ManifestSchema} {
		raw, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		var walk func(any)
		walk = func(value any) {
			switch value := value.(type) {
			case map[string]any:
				if pattern, ok := value["pattern"].(string); ok {
					for _, shorthand := range []string{`\s`, `\S`, `\d`, `\D`, `\w`, `\W`, `\b`, `\B`, `\p`, `\P`, `\u`} {
						if strings.Contains(strings.ReplaceAll(pattern, `\\`, ""), shorthand) {
							t.Errorf("pattern uses %s: %s", shorthand, pattern)
						}
					}
				}
				for _, child := range value {
					walk(child)
				}
			case []any:
				for _, child := range value {
					walk(child)
				}
			}
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		walk(document)
	}
}

func TestTypeScriptHelperTypesAreCurrent(t *testing.T) {
	source, err := os.ReadFile("../../sdk/plugin/kit-plugin.ts")
	if err != nil {
		t.Fatal(err)
	}
	region, err := TypeScript()
	if err != nil {
		t.Fatal(err)
	}
	want, err := ReplaceTypeScriptRegion(string(source), region)
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != want {
		t.Fatal("sdk/plugin/kit-plugin.ts has stale generated types; run go run ./api/pluginprotocol/cmd/emit")
	}
}
