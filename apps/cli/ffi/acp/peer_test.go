package acp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const peerTimeout = 20 * time.Second

// peer drives one kit acp connection as an ACP client would, over in-memory
// pipes. It records the transcript and checks every message kit acp writes
// against the ACP v1 schema.
type peer struct {
	t      *testing.T
	cwd    string
	stdin  *io.PipeWriter
	lines  chan string
	served chan error
	stderr *syncBuffer
	nextID int

	// answer, when set, answers requests kit acp makes; the result is sent
	// as the response. A nil result leaves the request unanswered.
	answer func(method string, params json.RawMessage) any

	mu         sync.Mutex
	transcript []string
}

type peerOption func(*Options)

func withConnect(connect func(context.Context) (Backend, error)) peerOption {
	return func(options *Options) { options.Connect = connect }
}

func withTemporary() peerOption {
	return func(options *Options) { options.Temporary = true }
}

func newPeer(t *testing.T, cwd string, options ...peerOption) *peer {
	t.Helper()
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	p := &peer{
		t: t, cwd: cwd, stdin: inWriter, lines: make(chan string, 256),
		served: make(chan error, 1), stderr: &syncBuffer{}, nextID: 1,
	}
	serveOptions := Options{
		In: inReader, Out: outWriter, Logger: slog.New(slog.NewTextHandler(p.stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Connect: connectTestServer, Version: "test",
		NewSessionID: func() (string, error) { return newTestSessionID(), nil },
	}
	for _, option := range options {
		option(&serveOptions)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		p.served <- Serve(ctx, serveOptions)
		_ = outWriter.Close()
	}()
	go func() {
		defer close(p.lines)
		reader := bufio.NewReaderSize(outReader, 1<<20)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				p.lines <- line
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = inWriter.Close()
		select {
		case <-p.served:
		case <-time.After(peerTimeout):
			t.Errorf("kit acp did not stop after stdin closed")
		}
		cancel()
	})
	return p
}

// close ends the connection by closing stdin and returns Serve's error.
func (p *peer) close() error {
	p.t.Helper()
	_ = p.stdin.Close()
	for range p.lines {
	}
	select {
	case err := <-p.served:
		p.served <- err
		return err
	case <-time.After(peerTimeout):
		p.t.Fatal("kit acp did not stop after stdin closed")
		return nil
	}
}

func (p *peer) record(direction, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.transcript = append(p.transcript, direction+" "+strings.TrimRight(line, "\n"))
}

// send writes one raw line to kit acp.
func (p *peer) send(line string) {
	p.t.Helper()
	p.record("->", line)
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		p.t.Fatalf("write to kit acp: %v", err)
	}
}

func (p *peer) sendJSON(message any) {
	p.t.Helper()
	raw, err := json.Marshal(message)
	if err != nil {
		p.t.Fatal(err)
	}
	p.send(string(raw))
}

// request sends a request and returns its id without waiting.
func (p *peer) request(method string, params any) int {
	p.t.Helper()
	id := p.nextID
	p.nextID++
	p.sendJSON(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return id
}

func (p *peer) notify(method string, params any) {
	p.t.Helper()
	p.sendJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// message is one decoded message from kit acp.
type message struct {
	raw    string
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// next returns kit acp's next message, answering requests it makes.
func (p *peer) next() message {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			p.t.Fatalf("kit acp closed stdout; stderr:\n%s", p.stderr.String())
		}
		p.record("<-", line)
		validateAgentMessage(p.t, line)
		var decoded message
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			p.t.Fatalf("kit acp wrote %q: %v", line, err)
		}
		decoded.raw = line
		if decoded.Method != "" && decoded.ID != nil && p.answer != nil {
			if result := p.answer(decoded.Method, decoded.Params); result != nil {
				p.sendJSON(map[string]any{"jsonrpc": "2.0", "id": decoded.ID, "result": result})
			}
		}
		return decoded
	case <-time.After(peerTimeout):
		p.t.Fatalf("kit acp wrote nothing for %s; stderr:\n%s", peerTimeout, p.stderr.String())
		return message{}
	}
}

// await reads until the response to id and returns it with the messages
// that came before it.
func (p *peer) await(id int) (message, []message) {
	p.t.Helper()
	want := fmt.Sprint(id)
	var before []message
	for {
		received := p.next()
		if received.Method == "" && string(received.ID) == want {
			return received, before
		}
		before = append(before, received)
	}
}

// call sends a request, waits for its response, and fails the test when the
// response is an error.
func (p *peer) call(method string, params any) json.RawMessage {
	p.t.Helper()
	response, _ := p.await(p.request(method, params))
	if response.Error != nil {
		p.t.Fatalf("%s failed: %v; stderr:\n%s", method, response.Error, p.stderr.String())
	}
	return response.Result
}

// callError sends a request and returns the error it is answered with.
func (p *peer) callError(method string, params any) *rpcError {
	p.t.Helper()
	response, _ := p.await(p.request(method, params))
	if response.Error == nil {
		p.t.Fatalf("%s succeeded with %s, want an error", method, response.Result)
	}
	return response.Error
}

// silent checks that kit acp writes nothing for a while.
func (p *peer) silent() {
	p.t.Helper()
	select {
	case line := <-p.lines:
		p.t.Fatalf("kit acp wrote %q, want nothing", line)
	case <-time.After(150 * time.Millisecond):
	}
}

// interactionExchange keeps the prompt, the requests kit acp makes, the
// client's answers, and the prompt's response.
func interactionExchange(line string) bool {
	return strings.Contains(line, `"method":"session/prompt"`) ||
		strings.Contains(line, `"method":"session/request_permission"`) ||
		strings.Contains(line, `"method":"elicitation/create"`) ||
		strings.HasPrefix(line, "-> ") && strings.Contains(line, `"result":`) ||
		strings.Contains(line, `"stopReason"`) ||
		strings.Contains(line, `"status":"completed"`)
}

func (p *peer) initialize(capabilities map[string]any) {
	p.t.Helper()
	p.call("initialize", map[string]any{
		"protocolVersion": 1, "clientCapabilities": capabilities,
		"clientInfo": map[string]any{"name": "test-client", "version": "1.0.0"},
	})
}

// newSession opens a session in the peer's cwd and returns its ID.
func (p *peer) newSession() string {
	p.t.Helper()
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(p.call("session/new", map[string]any{"cwd": p.cwd, "mcpServers": []any{}}), &result); err != nil {
		p.t.Fatal(err)
	}
	// The session's commands and usage follow its response.
	p.until(func(received message) bool { return updateKind(received) == "available_commands_update" })
	return result.SessionID
}

// until reads messages until one matches.
func (p *peer) until(match func(message) bool) message {
	p.t.Helper()
	for {
		received := p.next()
		if match(received) {
			return received
		}
	}
}

// prompt sends text and returns the response and the updates before it.
func (p *peer) prompt(sessionID, text string) (message, []message) {
	p.t.Helper()
	return p.await(p.request("session/prompt", map[string]any{
		"sessionId": sessionID, "prompt": []any{map[string]any{"type": "text", "text": text}},
	}))
}

func updateKind(received message) string {
	if received.Method != "session/update" {
		return ""
	}
	var params struct {
		Update struct {
			Kind string `json:"sessionUpdate"`
		} `json:"update"`
	}
	_ = json.Unmarshal(received.Params, &params)
	return params.Update.Kind
}

func stopReason(t *testing.T, response message) string {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("prompt failed: %v", response.Error)
	}
	var result promptResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result.StopReason
}

var (
	identityPattern = regexp.MustCompile(`\b([a-z]+)_[0-9a-f]{32}\b`)
	callPattern     = regexp.MustCompile(`\bcall_[0-9]+\b`)
	timePattern     = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T[0-9:.]+(Z|[+-]\d{2}:\d{2})"`)
	compactPattern  = regexp.MustCompile(`acp_compact_[0-9a-f]+`)
	// Context use depends on the size of Kit's system prompt.
	usedPattern = regexp.MustCompile(`"used":\d+`)
)

// normalized returns the transcript with generated identities, times, and
// the working directory replaced by stable placeholders.
func (p *peer) normalized() string {
	p.mu.Lock()
	text := strings.Join(p.transcript, "\n") + "\n"
	p.mu.Unlock()
	if p.cwd != "" {
		if resolved, err := filepath.EvalSymlinks(p.cwd); err == nil && resolved != p.cwd {
			text = strings.ReplaceAll(text, resolved, "<cwd>")
		}
		text = strings.ReplaceAll(text, p.cwd, "<cwd>")
	}
	text = numbered(text, identityPattern, func(match []string) string { return match[1] })
	text = numbered(text, callPattern, func([]string) string { return "call" })
	text = timePattern.ReplaceAllString(text, `"<time>"`)
	text = usedPattern.ReplaceAllString(text, `"used":"<tokens>"`)
	return compactPattern.ReplaceAllString(text, "<compaction>")
}

func numbered(text string, pattern *regexp.Regexp, kind func([]string) string) string {
	seen := map[string]string{}
	counts := map[string]int{}
	return pattern.ReplaceAllStringFunc(text, func(found string) string {
		if placeholder, ok := seen[found]; ok {
			return placeholder
		}
		name := kind(pattern.FindStringSubmatch(found))
		counts[name]++
		placeholder := fmt.Sprintf("<%s-%d>", name, counts[name])
		seen[found] = placeholder
		return placeholder
	})
}

// golden compares the normalized transcript with testdata/NAME.golden.
// keep, when given, chooses the lines compared, for exchanges whose other
// lines the server may order either way.
func (p *peer) golden(name string, keep ...func(string) bool) {
	p.t.Helper()
	path := filepath.Join("testdata", name+".golden")
	actual := p.normalized()
	if len(keep) > 0 {
		var kept []string
		for _, line := range strings.Split(strings.TrimSuffix(actual, "\n"), "\n") {
			if keep[0](line) {
				kept = append(kept, line)
			}
		}
		actual = strings.Join(kept, "\n") + "\n"
	}
	if os.Getenv("KIT_UPDATE_ACP_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			p.t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		p.t.Fatalf("read golden transcript (KIT_UPDATE_ACP_GOLDEN=1 writes it): %v", err)
	}
	if string(expected) != actual {
		p.t.Errorf("transcript differs from %s (KIT_UPDATE_ACP_GOLDEN=1 rewrites it)\n--- want\n%s\n--- got\n%s\n--- stderr\n%s", path, expected, actual, p.stderr.String())
	}
}

var (
	schemaOnce   sync.Once
	agentSchema  *jsonschema.Schema
	schemaFailed error
)

// validateAgentMessage checks a message kit acp wrote against the ACP v1
// schema's agent envelope.
func validateAgentMessage(t *testing.T, line string) {
	t.Helper()
	schemaOnce.Do(func() {
		file, err := os.Open(filepath.Join("testdata", "acp-v1-schema.json"))
		if err != nil {
			schemaFailed = err
			return
		}
		defer file.Close()
		document, err := jsonschema.UnmarshalJSON(file)
		if err != nil {
			schemaFailed = err
			return
		}
		compiler := jsonschema.NewCompiler()
		const location = "https://agentclientprotocol.com/schema/v1.json"
		if err := compiler.AddResource(location, document); err != nil {
			schemaFailed = err
			return
		}
		agentSchema, schemaFailed = compiler.Compile(location + "#/anyOf/0")
	})
	if schemaFailed != nil {
		t.Fatalf("load ACP schema: %v", schemaFailed)
	}
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("kit acp wrote a message that is not one line: %q", line)
	}
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(line))
	if err != nil {
		t.Fatalf("kit acp wrote invalid JSON %q: %v", line, err)
	}
	if err := agentSchema.Validate(instance); err != nil {
		t.Fatalf("kit acp wrote a message the ACP schema rejects:\n%s\n%v", line, err)
	}
}

type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func newTestSessionID() string {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	return fmt.Sprintf("session_%x", token)
}

var errUnavailable = errors.New("connection refused")
