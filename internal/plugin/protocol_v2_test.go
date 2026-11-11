package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/api/pluginprotocol"
	"github.com/akonwi/kit/internal/apphome"
	kitversion "github.com/akonwi/kit/internal/version"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// protocolSchema compiles one definition of the committed version 2 schema.
func protocolSchema(t *testing.T, definition string) *validator.Schema {
	t.Helper()
	raw, err := os.ReadFile("../../docs/plugin-protocol/v2/protocol.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	const location = "https://kit.invalid/protocol.schema.json"
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location + "#/$defs/" + definition)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestVersion2InitializeIdentifiesKitAndTheInstance(t *testing.T) {
	instance, root := testInstanceVersion(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines(), 2)
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(pluginprotocol.InitializeParams{
		ProtocolVersion: 2,
		Kit:             pluginprotocol.KitInfo{Version: kitversion.Version},
		Instance:        pluginprotocol.InstanceInfo{ID: "pluginhost_0123456789abcdef0123456789abcdef:1"},
		Context:         PluginContext{Project: ProjectContext{Cwd: root}, Session: SessionContext{ID: "session-one"}},
	})
	if string(raw) != string(expected) {
		t.Fatalf("initialize params = %s\nwant %s", raw, expected)
	}
}

func TestVersion1InitializeIsUnchanged(t *testing.T) {
	instance, root := testInstance(t, t.Context(), "normal", RPCHandlers{}, testInstanceDeadlines())
	if err := instance.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"protocolVersion":1,"context":{"project":{"cwd":` + mustJSONString(t, root) + `,"git":null},"session":{"id":"session-one","name":null}}}`; string(raw) != want {
		t.Fatalf("initialize params = %s\nwant %s", raw, want)
	}
}

func TestVersion2PluginMustAcceptVersion2(t *testing.T) {
	// bad-version answers protocol version 2 whatever Kit asked for; a
	// version 2 plugin answering 2 is accepted, and the same answer from a
	// version 1 plugin is not.
	accepted, _ := testInstanceVersion(t, t.Context(), "bad-version", RPCHandlers{}, testInstanceDeadlines(), 2)
	if err := accepted.WaitReady(t.Context()); err != nil {
		t.Fatalf("version 2 answer to version 2: %v", err)
	}
	rejected, _ := testInstance(t, t.Context(), "bad-version", RPCHandlers{}, testInstanceDeadlines())
	if err := rejected.WaitReady(t.Context()); err == nil || !strings.Contains(err.Error(), "unsupported protocol version") {
		t.Fatalf("version 2 answer to version 1 = %v", err)
	}
}

func TestManifestProtocolVersion(t *testing.T) {
	base := `"manifestVersion":1,"id":"demo","transport":{"type":"stdio","command":"demo"}`
	manifest, err := ParseManifest([]byte(`{`+base+`,"protocolVersion":2}`), nil)
	if err != nil || manifest.ProtocolVersion != 2 || (Installation{Manifest: manifest}).ProtocolVersion() != 2 {
		t.Fatalf("version 2 manifest = %#v, %v", manifest, err)
	}
	manifest, err = ParseManifest([]byte(`{`+base+`}`), nil)
	if err != nil || (Installation{Manifest: manifest}).ProtocolVersion() != 1 {
		t.Fatalf("manifest without protocolVersion = %#v, %v", manifest, err)
	}
	for _, value := range []string{"1", "3", `"2"`, "null", "2.5"} {
		if _, err := ParseManifest([]byte(`{`+base+`,"protocolVersion":`+value+`}`), nil); err == nil {
			t.Errorf("accepted protocolVersion %s", value)
		}
	}
}

func TestVersion2RejectsBatchesAndKeepsServing(t *testing.T) {
	handlers := RPCHandlers{Request: func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		return json.Marshal(method)
	}}
	limits := defaultRPCLimits
	limits.protocolVersion = 2
	endpoint, peer, reader := rpcTestPeer(t, handlers, limits)
	rpcWritePeer(t, peer, `[{"jsonrpc":"2.0","id":1,"method":"first"},{"jsonrpc":"2.0","id":2,"method":"second"}]`)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Batches are not supported"}}` + "\n"; string(line) != want {
		t.Fatalf("batch answer = %s", line)
	}
	rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":3,"method":"after"}`)
	line, err = reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"jsonrpc":"2.0","id":3,"result":"after"}` + "\n"; string(line) != want {
		t.Fatalf("request after batch = %s", line)
	}
	if endpoint.Err() != nil {
		t.Fatalf("endpoint closed: %v", endpoint.Err())
	}
}

func TestBusyErrorsCarryAReasonInVersion2Only(t *testing.T) {
	for _, test := range []struct {
		version int
		want    string
	}{
		{1, `{"jsonrpc":"2.0","id":2,"error":{"code":-32006,"message":"Endpoint is busy"}}`},
		{2, `{"jsonrpc":"2.0","id":2,"error":{"code":-32006,"message":"Endpoint is busy","data":{"reason":"busy"}}}`},
	} {
		t.Run(fmt.Sprintf("version %d", test.version), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			handlers := RPCHandlers{Request: func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return nil, nil
			}}
			limits := defaultRPCLimits
			limits.incoming = 1
			limits.protocolVersion = test.version
			_, peer, reader := rpcTestPeer(t, handlers, limits)
			rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":1,"method":"wait"}`)
			<-started
			rpcWritePeer(t, peer, `{"jsonrpc":"2.0","id":2,"method":"busy"}`)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatal(err)
			}
			if string(line) != test.want+"\n" {
				t.Fatalf("busy answer = %s, want %s", line, test.want)
			}
		})
	}
}

func TestVersion2ParamsHaveExactlyTheDeclaredMembers(t *testing.T) {
	for _, test := range []struct {
		name    string
		members []string
		want    []string
	}{
		{"v1 footer set", versionMembers(1, footerSetMembers, v1FooterSetExtras...), []string{"id", "content", "side", "clickable", "action"}},
		{"v2 footer set", versionMembers(2, footerSetMembers, v1FooterSetExtras...), []string{"id", "content"}},
		{"v1 select", versionMembers(1, selectMembers, v1SelectExtras...), []string{"title", "message", "options", "placeholder", "filterable"}},
		{"v2 select", versionMembers(2, selectMembers, v1SelectExtras...), []string{"title", "message", "options", "placeholder", "parentId"}},
		{"v1 confirm", versionMembers(1, confirmMembers), []string{"title", "message", "confirmLabel", "cancelLabel", "defaultValue"}},
		{"v2 confirm", versionMembers(2, confirmMembers), []string{"title", "message", "confirmLabel", "cancelLabel", "defaultValue", "parentId"}},
		{"v2 input", versionMembers(2, inputMembers), []string{"title", "message", "placeholder", "initialValue", "parentId"}},
		{"command", commandRegisterMembers, []string{"id", "description", "argName", "category"}},
		{"tool", toolRegisterMembers, []string{"id", "label", "description", "inputSchema", "executionMode", "promptSnippet", "promptGuidelines"}},
		{"subagent", subagentRegisterMembers, []string{"id", "description", "instructions", "model"}},
		{"toast", toastMembers, []string{"title", "subtitle", "variant", "persistent"}},
		{"submit message", submitMessageMembers, []string{"text", "idempotencyKey"}},
		{"tool result", toolResultMembers, []string{"content", "details", "terminate"}},
		{"decision", decisionMembers, []string{"action", "message"}},
	} {
		if !slices.Equal(test.members, test.want) {
			t.Errorf("%s members = %v, want %v", test.name, test.members, test.want)
		}
	}
}

func TestVersion2RejectsVersion1OnlyMembers(t *testing.T) {
	var failure *RPCError
	if _, err := parseFooterItem(json.RawMessage(`{"id":"status","content":[{"text":"x"}],"side":"right"}`), 2); !errors.As(err, &failure) || failure.Code != -32602 {
		t.Fatalf("v2 side = %v", err)
	}
	if _, err := parseFooterItem(json.RawMessage(`{"id":"status","content":[{"text":"x"}],"side":"right","clickable":false}`), 1); err != nil {
		t.Fatalf("v1 side = %v", err)
	}
	if _, _, err := parseInteraction("kit/ui/select", json.RawMessage(`{"title":"x","options":[{"label":"a","value":1}],"filterable":true}`), 2); !errors.As(err, &failure) || failure.Code != -32602 {
		t.Fatalf("v2 filterable = %v", err)
	}
	if _, _, err := parseInteraction("kit/ui/confirm", json.RawMessage(`{"title":"x","parentId":"kit-1"}`), 1); !errors.As(err, &failure) || failure.Code != -32602 {
		t.Fatalf("v1 parentId = %v", err)
	}
	for _, parent := range []string{`null`, `1.5`, `{}`, `true`} {
		if _, _, err := parseInteraction("kit/ui/confirm", json.RawMessage(`{"title":"x","parentId":`+parent+`}`), 2); !errors.As(err, &failure) || failure.Code != -32602 {
			t.Errorf("v2 parentId %s = %v", parent, err)
		}
	}
	_, parent, err := parseInteraction("kit/ui/input", json.RawMessage(`{"title":"x","parentId":42}`), 2)
	if err != nil || string(parent) != "42" {
		t.Fatalf("v2 integer parentId = %s, %v", parent, err)
	}
}

// parentDialog starts a Kit request on a test endpoint and opens a version 2
// dialog naming it as parent. It returns the dialog's outcome, a way to
// finish the parent, and the endpoint's peer.
func parentDialog(t *testing.T, parentID string) (<-chan error, context.CancelFunc, *RPCEndpoint, *bufio.Reader, func(string)) {
	t.Helper()
	entered := make(chan struct{})
	host, owner := interactionHost(t, func(ctx context.Context, _ InteractionRequest) (InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		return InteractionResponse{}, ctx.Err()
	})
	endpoint, peer, reader := rpcTestPeer(t, RPCHandlers{}, defaultRPCLimits)
	callContext, cancelCall := context.WithCancel(t.Context())
	go func() { _, _ = endpoint.Call(callContext, "kit/tool-calls/before-execute", json.RawMessage(`{}`)) }()
	messages, _ := rpcRead(t, reader)
	if string(messages[0].ID) != `"kit-1"` {
		t.Fatalf("parent request = %#v", messages[0])
	}
	handlerContext := context.WithValue(withProtocolVersion(t.Context(), 2), endpointContextKey{}, endpoint)
	outcome := make(chan error, 1)
	go func() {
		_, err := host.handleRequest(handlerContext, owner, "kit/ui/confirm", json.RawMessage(`{"title":"Run?","parentId":`+parentID+`}`))
		outcome <- err
	}()
	if parentID == `"kit-1"` {
		select {
		case <-entered:
		case err := <-outcome:
			t.Fatalf("dialog ended before it was shown: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("dialog was not shown")
		}
	}
	write := func(frame string) { rpcWritePeer(t, peer, frame) }
	return outcome, cancelCall, endpoint, reader, write
}

func requireCancelled(t *testing.T, outcome <-chan error) {
	t.Helper()
	select {
	case err := <-outcome:
		var failure *RPCError
		if !errors.Is(err, context.Canceled) && !(errors.As(err, &failure) && failure.Code == -32001) {
			t.Fatalf("dialog outcome = %v, want cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dialog was not cancelled")
	}
}

func TestDialogClosesWhenItsParentIsAnswered(t *testing.T) {
	outcome, _, _, _, write := parentDialog(t, `"kit-1"`)
	write(`{"jsonrpc":"2.0","id":"kit-1","result":{"action":"allow"}}`)
	requireCancelled(t, outcome)
}

func TestDialogClosesWhenItsParentIsCancelled(t *testing.T) {
	outcome, cancelCall, _, reader, _ := parentDialog(t, `"kit-1"`)
	cancelCall()
	messages, _ := rpcRead(t, reader)
	if messages[0].Method == nil || *messages[0].Method != "kit/cancel" || string(messages[0].Params) != `{"id":"kit-1"}` {
		t.Fatalf("cancel = %#v", messages[0])
	}
	requireCancelled(t, outcome)
}

func TestDialogClosesWhenItsParentsInstanceStops(t *testing.T) {
	outcome, _, endpoint, _, _ := parentDialog(t, `"kit-1"`)
	endpoint.Close(nil)
	requireCancelled(t, outcome)
}

func TestDialogWithAnUnknownParentIsCancelledAtOnce(t *testing.T) {
	outcome, _, _, _, _ := parentDialog(t, `"kit-99"`)
	select {
	case err := <-outcome:
		var failure *RPCError
		if !errors.As(err, &failure) || failure.Code != -32001 {
			t.Fatalf("unknown parent = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dialog with an unknown parent stayed open")
	}
}

func TestVersion2MessagesKitSendsMatchTheSchema(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	cwd, other := t.TempDir(), t.TempDir()
	root := hostInstallationVersion(t, home.Plugins, "commands", "demo", "commands", 2)
	branch := "main"
	git := &GitContext{Root: cwd, Branch: &branch, PullRequest: &PullRequestContext{Number: 7, URL: "https://github.com/a/b/pull/7"}}
	project := func(_ context.Context, dir string) (ProjectContext, error) {
		if dir != cwd {
			return ProjectContext{Cwd: dir}, nil
		}
		return ProjectContext{Cwd: dir, Git: cloneGitContext(git)}, nil
	}
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: cwd, Session: SessionContext{ID: "session-one"}, Project: project})
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
	owner := findCommand(t, host, "demo.ok").Owner

	if err := host.ExecuteCommand(t.Context(), findCommand(t, host, "demo.ok"), "go"); err != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_ = host.ExecuteCommand(waiting, findCommand(t, host, "demo.wait"), "")
	cancel()
	if _, err := host.handleRequest(t.Context(), owner, "kit/tools/register", json.RawMessage(`{"id":"echo","description":"Echo","inputSchema":{"type":"object"}}`)); err != nil {
		t.Fatal(err)
	}
	tools := host.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	if _, err := host.ExecuteTool(t.Context(), tools[0], ToolCall{ID: "call_1", TurnID: "turn_1"}, json.RawMessage(`{"value":1}`)); err != nil {
		t.Fatal(err)
	}
	registerInterceptor(t, host, owner)
	if _, err := host.InterceptTool(t.Context(), host.InterceptorIdentity(), "call_2", "bash", json.RawMessage(`{"command":"ls"}`)); err != nil {
		t.Fatal(err)
	}
	host.TurnStarted("turn_1")
	host.TurnCompleted(PublicTurn{ID: "turn_1", Messages: []PublicMessage{{Role: pluginprotocol.RoleUser, Content: []PublicTextContent{{Type: "text", Text: "hi"}}}}}, "")
	host.SetStatus(testStatus(1000, 0.5))
	host.CompactionChanged(Compaction{ID: "compaction_1"})
	host.CompactionChanged(Compaction{ID: "compaction_1", Completed: true, FailureReason: contract.CompactionFailureProviderError})
	host.Rename("Renamed")
	git.Dirty = true
	host.refreshGit()
	host.ChangeCWD(other)
	eventuallyHost(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(root, "received.jsonl"))
		return bytes.Contains(data, []byte(`"kit/events/project.changed"`)) && bytes.Contains(data, []byte(`"kit/events/session.changed"`)) &&
			bytes.Contains(data, []byte(`"kit/events/git.changed"`)) && bytes.Contains(data, []byte(`"kit/events/agent.turn.completed"`)) &&
			bytes.Contains(data, []byte(`"kit/events/session.status.changed"`)) && bytes.Contains(data, []byte(`"kit/events/compaction.completed"`))
	})
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "received.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	schema := protocolSchema(t, "KitMessage")
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		value, err := validator.UnmarshalJSON(bytes.NewReader(line))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if err := schema.Validate(value); err != nil {
			t.Errorf("Kit sent a message outside the schema: %s\n%v", line, err)
		}
		var envelope struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(line, &envelope)
		seen[envelope.Method] = true
	}
	for _, method := range pluginprotocol.Methods() {
		if method.Sends(pluginprotocol.KitToPlugin) && !seen[method.Name] {
			t.Errorf("the test did not exercise %s", method.Name)
		}
	}
}

// TestSchemaAndHostAgreeOnPluginText checks representative text and address
// rules: each case's params are accepted by both the host's parser and the
// published schema, or rejected by both.
func TestSchemaAndHostAgreeOnPluginText(t *testing.T) {
	schema := compileDefinitions(t)["PluginMessage"]
	parsers := map[string]func(json.RawMessage) error{
		"kit/commands/register":      func(raw json.RawMessage) error { _, err := parseCommand(raw); return err },
		"kit/tools/register":         func(raw json.RawMessage) error { _, err := parseTool(raw); return err },
		"kit/subagents/register":     func(raw json.RawMessage) error { _, err := parseSubagent(raw); return err },
		"kit/footer/set":             func(raw json.RawMessage) error { _, err := parseFooterItem(raw, 2); return err },
		"kit/ui/toast":               func(raw json.RawMessage) error { _, err := parseToast(raw); return err },
		"kit/ui/select":              func(raw json.RawMessage) error { _, _, err := parseInteraction("kit/ui/select", raw, 2); return err },
		"kit/ui/input":               func(raw json.RawMessage) error { _, _, err := parseInteraction("kit/ui/input", raw, 2); return err },
		"kit/session/submit-message": func(raw json.RawMessage) error { _, err := parseMessage(raw); return err },
		"kit/system/open-url":        func(raw json.RawMessage) error { _, err := parseOpenURL(raw); return err },
	}
	long := func(n int) string { return strings.Repeat("a", n) }
	tool := func(member string) string {
		return `{"id":"echo","description":"Echo","inputSchema":{"type":"object"}` + member + `}`
	}
	for _, test := range []struct {
		name, method, params string
		valid                bool
	}{
		{"command description", "kit/commands/register", `{"id":"go","description":"Go somewhere"}`, true},
		{"command description at its bound", "kit/commands/register", `{"id":"go","description":"` + long(1024) + `"}`, true},
		{"command description beyond its bound", "kit/commands/register", `{"id":"go","description":"` + long(1025) + `"}`, false},
		{"blank command description", "kit/commands/register", `{"id":"go","description":"  "}`, false},
		{"multiline command description", "kit/commands/register", `{"id":"go","description":"a\nb"}`, false},
		{"format character in a category", "kit/commands/register", `{"id":"go","description":"Go","category":"a\u200bb"}`, false},
		{"multiline tool description", "kit/tools/register", `{"id":"echo","description":"a\n\tb","inputSchema":{"type":"object"}}`, true},
		{"carriage return in a tool description", "kit/tools/register", `{"id":"echo","description":"a\rb","inputSchema":{"type":"object"}}`, false},
		{"prompt guideline beyond its bound", "kit/tools/register", tool(`,"promptGuidelines":["` + long(1025) + `"]`), false},
		{"control character in a prompt guideline", "kit/tools/register", tool(`,"promptGuidelines":["a\u0007"]`), false},
		{"blank subagent instructions", "kit/subagents/register", `{"id":"r","description":"Review","instructions":"\n\t "}`, false},
		{"multiline footer text", "kit/footer/set", `{"id":"s","content":[{"text":"a\nb"}]}`, false},
		{"empty footer text", "kit/footer/set", `{"id":"s","content":[{"text":""}]}`, true},
		{"multiline toast subtitle", "kit/ui/toast", `{"title":"Done","subtitle":"a\nb","variant":"info"}`, true},
		{"C1 control in a toast title", "kit/ui/toast", `{"title":"a\u0085","variant":"info"}`, false},
		{"blank option label", "kit/ui/select", `{"title":"Pick","options":[{"label":" ","value":1}]}`, false},
		{"byte order mark in an initial value", "kit/ui/input", `{"title":"Name","initialValue":"\ufeffx"}`, false},
		{"message with tabs and newlines", "kit/session/submit-message", `{"text":"a\n\tb"}`, true},
		{"message with NUL", "kit/session/submit-message", `{"text":"a\u0000b"}`, false},
		{"blank message", "kit/session/submit-message", `{"text":" \n"}`, false},
		{"message of a byte order mark", "kit/session/submit-message", `{"text":"\ufeff"}`, true},
		{"message of no-break spaces", "kit/session/submit-message", `{"text":"\u00a0\u3000"}`, false},
		{"https address", "kit/system/open-url", `{"url":"https://example.com/a?b=c#d"}`, true},
		{"address with @ in its path", "kit/system/open-url", `{"url":"https://example.com/@me"}`, true},
		{"ftp address", "kit/system/open-url", `{"url":"ftp://example.com/"}`, false},
		{"address with credentials", "kit/system/open-url", `{"url":"https://user:pw@example.com/"}`, false},
		{"address with white space", "kit/system/open-url", `{"url":"https://example.com/a b"}`, false},
		{"address with a no-break space", "kit/system/open-url", `{"url":"https://example.com/a\u00a0b"}`, false},
		{"address with a byte order mark", "kit/system/open-url", `{"url":"https://example.com/\ufeff"}`, true},
		{"address with a trailing newline", "kit/system/open-url", `{"url":"https://example.com/\n"}`, false},
		{"address without a host", "kit/system/open-url", `{"url":"https:///a"}`, false},
		{"relative address", "kit/system/open-url", `{"url":"/docs"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			hostErr := parsers[test.method](json.RawMessage(test.params))
			message := `{"jsonrpc":"2.0","id":1,"method":"` + test.method + `","params":` + test.params + `}`
			if test.method == "kit/ui/toast" {
				message = `{"jsonrpc":"2.0","method":"kit/ui/toast","params":` + test.params + `}`
			}
			value, err := validator.UnmarshalJSON(strings.NewReader(message))
			if err != nil {
				t.Fatal(err)
			}
			schemaErr := schema.Validate(value)
			if (hostErr == nil) != test.valid {
				t.Errorf("host accepted = %t, want %t (%v)", hostErr == nil, test.valid, hostErr)
			}
			if (schemaErr == nil) != test.valid {
				t.Errorf("schema accepted = %t, want %t (%v)", schemaErr == nil, test.valid, schemaErr)
			}
		})
	}
}

// TestVersion2RejectsPrunedSurfaceAndKeepsServing sends each method and member
// version 2 removed through an endpoint served by a host's request handler,
// as an instance serves a version 2 plugin, and checks the exact error and
// that the next request still succeeds.
func TestVersion2RejectsPrunedSurfaceAndKeepsServing(t *testing.T) {
	host, owner := interactionHost(t, func(ctx context.Context, _ InteractionRequest) (InteractionResponse, error) {
		<-ctx.Done()
		return InteractionResponse{}, ctx.Err()
	})
	handlers := RPCHandlers{Request: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		return host.handleRequest(withProtocolVersion(ctx, 2), owner, method, params)
	}}
	limits := defaultRPCLimits
	limits.protocolVersion = 2
	endpoint, peer, reader := rpcTestPeer(t, handlers, limits)
	for index, test := range []struct {
		method, params string
		code           int64
	}{
		{"kit/header/set", `{"id":"status","content":[{"text":"x"}]}`, -32601},
		{"kit/header/clear", `{"id":"status"}`, -32601},
		{"kit/header/hide", `{"id":"kit.header.title"}`, -32601},
		{"kit/footer/click", `{"id":"status"}`, -32601},
		{"kit/system-prompt/set", `{"id":"rules","text":"x"}`, -32601},
		{"kit/system-prompt/clear", `{"id":"rules"}`, -32601},
		{"kit/footer/set", `{"id":"status","content":[{"text":"x"}],"side":"right"}`, -32602},
		{"kit/footer/set", `{"id":"status","content":[{"text":"x"}],"clickable":false}`, -32602},
		{"kit/footer/set", `{"id":"status","content":[{"text":"x"}],"action":{"type":"url","url":"https://example.com"}}`, -32602},
		{"kit/ui/select", `{"title":"Pick","options":[{"label":"a","value":1}],"filterable":true}`, -32602},
		{"kit/session/submit-message", `{"sessionId":"one","text":"Continue."}`, -32602},
	} {
		t.Run(test.method+" "+test.params, func(t *testing.T) {
			id := 2 * index
			rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, test.method, test.params))
			messages, _ := rpcRead(t, reader)
			if string(messages[0].ID) != fmt.Sprint(id) || messages[0].Error == nil || messages[0].Error.Code != test.code {
				t.Fatalf("answer = %#v, want error %d", messages[0], test.code)
			}
			rpcWritePeer(t, peer, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"kit/footer/clear","params":{"id":"status"}}`, id+1))
			messages, _ = rpcRead(t, reader)
			if string(messages[0].ID) != fmt.Sprint(id+1) || messages[0].Error != nil || string(messages[0].Result) != "null" {
				t.Fatalf("request after the rejection = %#v", messages[0])
			}
			if endpoint.Err() != nil {
				t.Fatalf("endpoint closed: %v", endpoint.Err())
			}
		})
	}
}

func TestOversizedParamsExceedTheLimitAndTheCatalogSaysSo(t *testing.T) {
	host, owner := interactionHost(t, nil)
	padding := `"` + strings.Repeat(" ", MaxContributionParamsBytes) + `"`
	for _, method := range []string{"kit/commands/unregister", "kit/tools/unregister", "kit/subagents/unregister", "kit/footer/clear", "kit/footer/show"} {
		_, err := host.handleRequest(withProtocolVersion(t.Context(), 2), owner, method, json.RawMessage(`{"id":`+padding+`}`))
		var failure *RPCError
		if !errors.As(err, &failure) || failure.Code != -32005 {
			t.Errorf("%s = %v, want -32005", method, err)
		}
		declared, _ := pluginprotocol.MethodNamed(method)
		if !slices.Contains(declared.Errors, pluginprotocol.CodeLimitExceeded) {
			t.Errorf("%s does not declare -32005", method)
		}
	}
}
