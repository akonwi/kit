package plugin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/api/pluginprotocol"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// fixtureSession speaks protocol 2 to one repository fixture over its real
// stdio. It answers every plugin request plausibly and checks every line the
// plugin writes against the published schema: requests and notifications
// against their method's message, and results against their method's result.
type fixtureSession struct {
	t        *testing.T
	stdin    io.WriteCloser
	lines    chan []byte
	schema   map[string]*validator.Schema
	next     int
	pending  map[string]string // Kit request id -> method
	commands []string
	tools    []string
	written  []string        // plugin lines, for failure messages
	observed map[string]bool // plugin-sent methods, shared across fixtures
}

func compileDefinitions(t *testing.T) map[string]*validator.Schema {
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
	schemas := map[string]*validator.Schema{}
	for _, name := range []string{"PluginMessage"} {
		schemas[name] = mustCompile(t, compiler, location+"#/$defs/"+name)
	}
	for _, method := range pluginprotocol.Methods() {
		if method.Result != nil {
			schemas["result:"+method.Name] = mustCompile(t, compiler, location+"#/$defs/"+method.Result.Name())
		}
	}
	return schemas
}

func mustCompile(t *testing.T, compiler *validator.Compiler, location string) *validator.Schema {
	t.Helper()
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func startFixture(t *testing.T, directory string, transport StdioTransport, schemas map[string]*validator.Schema) *fixtureSession {
	t.Helper()
	command := exec.Command(transport.Command, transport.Args...)
	command.Dir = directory
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	session := &fixtureSession{t: t, stdin: stdin, lines: make(chan []byte, 64), schema: schemas, pending: map[string]string{}}
	go func() {
		defer close(session.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 16<<20)
		for scanner.Scan() {
			session.lines <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("plugin stderr:\n%s", stderr.String())
		}
	})
	return session
}

func (s *fixtureSession) send(value any) {
	s.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		s.t.Fatal(err)
	}
}

// request sends a Kit request and serves the plugin until it answers,
// returning the result.
func (s *fixtureSession) request(method string, params any) json.RawMessage {
	s.t.Helper()
	s.next++
	id := fmt.Sprintf("kit-%d", s.next)
	s.pending[id] = method
	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	s.send(message)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				s.t.Fatalf("plugin exited before answering %s; it wrote:\n%s", method, strings.Join(s.written, "\n"))
			}
			if result, answered := s.handle(line, id); answered {
				return result
			}
		case <-deadline:
			s.t.Fatalf("plugin did not answer %s; it wrote:\n%s", method, strings.Join(s.written, "\n"))
		}
	}
}

// settle serves the plugin until it has been quiet for a moment, for work it
// does after answering.
func (s *fixtureSession) settle() {
	s.t.Helper()
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return
			}
			s.handle(line, "")
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}

func (s *fixtureSession) validate(schema *validator.Schema, data []byte, what string) {
	s.t.Helper()
	value, err := validator.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		s.t.Fatalf("%s is not JSON: %s", what, data)
	}
	if err := schema.Validate(value); err != nil {
		s.t.Errorf("%s does not match the schema: %s\n%v", what, data, err)
	}
}

// handle checks and answers one plugin line. It reports the result when the
// line answers the Kit request awaited.
func (s *fixtureSession) handle(line []byte, awaited string) (json.RawMessage, bool) {
	s.t.Helper()
	s.written = append(s.written, string(line))
	s.validate(s.schema["PluginMessage"], line, "plugin message")
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &message); err != nil {
		s.t.Fatal(err)
	}
	if message.Method != "" && s.observed != nil {
		s.observed[message.Method] = true
	}
	if message.Method == "" {
		var id string
		_ = json.Unmarshal(message.ID, &id)
		method, known := s.pending[id]
		if !known {
			s.t.Fatalf("plugin answered an unknown request: %s", line)
		}
		delete(s.pending, id)
		if message.Error == nil {
			s.validate(s.schema["result:"+method], message.Result, method+" result")
		}
		return message.Result, id == awaited
	}
	if len(message.ID) == 0 {
		return nil, false // A notification: kit/ui/toast or kit/cancel.
	}
	var params map[string]json.RawMessage
	_ = json.Unmarshal(message.Params, &params)
	if parent, ok := params["parentId"]; ok {
		var id string
		if json.Unmarshal(parent, &id) != nil || s.pending[id] == "" {
			s.t.Errorf("dialog names a parent that is not a pending Kit request: %s", line)
		}
	}
	var local struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(message.Params, &local)
	plugin := "fixture"
	var result any
	switch message.Method {
	case "kit/commands/register":
		s.commands = append(s.commands, local.ID)
		result = pluginprotocol.CanonicalIDResult{ID: plugin + "." + local.ID}
	case "kit/subagents/register", "kit/footer/set":
		result = pluginprotocol.CanonicalIDResult{ID: plugin + "." + local.ID}
	case "kit/tools/register":
		s.tools = append(s.tools, local.ID)
		result = pluginprotocol.ToolRegisterResult{ID: plugin + "." + local.ID, ModelName: plugin + "__" + local.ID}
	case "kit/ui/confirm":
		result = true
	case "kit/ui/input":
		result = "typed"
	case "kit/ui/select":
		var options struct {
			Options []struct {
				Value json.RawMessage `json:"value"`
			} `json:"options"`
		}
		_ = json.Unmarshal(message.Params, &options)
		result = pluginprotocol.SelectResult{Value: options.Options[0].Value}
	case "kit/session/submit-message":
		result = pluginprotocol.SubmitMessageResult{MessageID: "message_1", TurnID: "turn_1"}
	default:
		result = nil
	}
	s.send(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result})
	return nil, false
}

// TestVersion2FixturesConformToTheSchema runs every repository fixture that
// declares protocol 2, and testdata/lifecycle-v2, through initialization,
// each command and tool it registers, and shutdown, checking everything it
// writes. Together they must send every plugin request and notification.
func TestVersion2FixturesConformToTheSchema(t *testing.T) {
	schemas := compileDefinitions(t)
	toolInputs := map[string]any{"echo": map[string]any{"text": "hello"}}
	// Commands take "3" (a count for the session-message demo's loop) unless
	// they need something else; open-url-demo's open needs an address.
	commandArgs := map[string]string{"open": "https://example.com/docs"}
	directories, err := filepath.Glob("../../.kit/plugins/*/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	// testdata/lifecycle-v2 sends the requests the repository's fixtures
	// have no use for.
	directories = append(directories, "testdata/lifecycle-v2/plugin.json")
	tested, skipped := 0, 0
	observed := map[string]bool{}
	for _, manifestPath := range directories {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ParseManifest(data, nil)
		if err != nil {
			t.Fatalf("%s: %v", manifestPath, err)
		}
		if manifest.ProtocolVersion != 2 {
			continue
		}
		if _, err := exec.LookPath(manifest.Transport.Command); err != nil {
			t.Logf("skipping %s: %s is not installed", manifest.ID, manifest.Transport.Command)
			skipped++
			continue
		}
		tested++
		directory := filepath.Dir(manifestPath)
		t.Run(manifest.ID, func(t *testing.T) {
			session := startFixture(t, directory, manifest.Transport, schemas)
			session.observed = observed
			cwd := t.TempDir()
			name := "Fixture session"
			result := session.request("initialize", pluginprotocol.InitializeParams{
				ProtocolVersion: 2, Kit: pluginprotocol.KitInfo{Version: "test"}, Instance: pluginprotocol.InstanceInfo{ID: "pluginhost_test:1"},
				Context: PluginContext{Project: ProjectContext{Cwd: cwd}, Session: SessionContext{ID: "session_1", Name: &name}},
			})
			var compact bytes.Buffer
			if json.Compact(&compact, result) != nil || compact.String() != `{"protocolVersion":2}` {
				t.Fatalf("initialize result = %s", result)
			}
			session.settle()
			if len(session.commands)+len(session.tools) == 0 {
				t.Fatal("the fixture registered no commands or tools to exercise")
			}
			for _, command := range session.commands {
				args, ok := commandArgs[command]
				if !ok {
					args = "3"
				}
				session.request("kit/commands/execute", pluginprotocol.CommandExecuteParams{ID: command, Args: args})
				session.settle()
			}
			for index, tool := range session.tools {
				input, ok := toolInputs[tool]
				if !ok {
					input = map[string]any{}
				}
				session.request("kit/tools/execute", map[string]any{"id": tool, "toolCallId": fmt.Sprintf("call-%d", index), "input": input})
			}
			session.send(map[string]any{"jsonrpc": "2.0", "method": "kit/events/agent.turn.completed", "params": pluginprotocol.TurnCompletedParams{SessionID: "session_1", Turn: pluginprotocol.Turn{ID: "turn_1", Messages: []pluginprotocol.Message{}}}})
			session.settle()
			if result := session.request("shutdown", nil); string(result) != "null" {
				t.Fatalf("shutdown result = %s", result)
			}
		})
	}
	if tested+skipped < 8 {
		t.Fatalf("found %d version 2 fixtures, want at least 8", tested+skipped)
	}
	if skipped > 0 {
		t.Skipf("skipped %d fixtures whose command is not installed", skipped)
	}
	var missing []string
	for _, method := range pluginprotocol.Methods() {
		if method.Sends(pluginprotocol.PluginToKit) && !observed[method.Name] {
			missing = append(missing, method.Name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("no fixture sent %v", missing)
	}
}

// installRepositoryPlugin copies the repository fixture name to root,
// following links such as a fixture's link to the TypeScript helper. It
// skips the test when the fixture's command is not installed.
func installRepositoryPlugin(t *testing.T, name, root string) {
	t.Helper()
	source := filepath.Join("..", "..", ".kit", "plugins", name)
	data, err := os.ReadFile(filepath.Join(source, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(manifest.Transport.Command); err != nil {
		t.Skipf("%s requires %s", name, manifest.Transport.Command)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, entry.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
