package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInitializeNegotiatesVersionOneAndAdvertisesKitsCapabilities(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.request("initialize", map[string]any{
		"protocolVersion": 2, "clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true}},
		"clientInfo": map[string]any{"name": "test-client", "version": "1.0.0"},
	})
	p.next()
	p.golden("initialize")
}

func TestMalformedInputIsAnsweredAndTheStreamRecovers(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.send(`{"jsonrpc":"2.0","id":1,"method":"session/list","params":{}}`)
	p.next()
	p.send(`this is not json`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":2,"method":"initialize"`)
	p.next()
	p.send(`[{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":1}}]`)
	p.next()
	p.send(`42`)
	p.next()
	p.send(`{"jsonrpc":"1.0","id":4,"method":"initialize","params":{"protocolVersion":1}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":{"nested":true},"method":"initialize"}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":5,"method":7}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":6}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","method":"_kit/unknown","params":{}}`)
	p.send(`{"jsonrpc":"2.0","method":"session/cancel","params":"not an object"}`)
	p.send(`{"jsonrpc":"1.0","method":"session/cancel","params":{}}`)
	p.send(`{"jsonrpc":"2.0","method":7}`)
	p.send(`{"jsonrpc":"2.0","method":"session/cancel","result":{}}`)
	p.silent()
	p.send(`{"jsonrpc":"2.0","id":7,"method":"initialize","params":{}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":"nine","method":"_kit/extension","params":{}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":10,"method":"session/new","params":{"cwd":"relative/path","mcpServers":[]}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"sessionId":"session_missing","prompt":[]}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":12,"method":"session/list","params":{"cursor":"not-a-cursor"}}`)
	p.next()
	p.send(`{"jsonrpc":"2.0","id":13,"result":{}}`)
	p.silent()
	p.golden("malformed-input")
}

func TestAnOverlongLineIsAnsweredAndSkipped(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	long := `{"jsonrpc":"2.0","id":99,"method":"session/list","params":{"cwd":"` + strings.Repeat("x", maxLineBytes) + `"}}`
	if _, err := p.stdin.Write([]byte(long + "\n")); err != nil {
		t.Fatal(err)
	}
	response := p.next()
	if response.Error == nil || response.Error.Code != codeParseError || string(response.ID) != "null" {
		t.Fatalf("response = %s, want a parse error for no request", response.raw)
	}
	var listed listSessionsResult
	if err := json.Unmarshal(p.call("session/list", map[string]any{"cwd": p.cwd}), &listed); err != nil {
		t.Fatal(err)
	}
}

func TestStdinClosingEndsTheConnectionCleanly(t *testing.T) {
	t.Parallel()
	p := newPeer(t, t.TempDir())
	p.initialize(map[string]any{})
	if err := p.close(); err != nil {
		t.Fatalf("Serve() = %v, want nil after stdin closes", err)
	}
}

func TestAnUnreachableServerIsReportedPerRequest(t *testing.T) {
	t.Parallel()
	attempts := 0
	p := newPeer(t, t.TempDir(), withConnect(func(context.Context) (Backend, error) {
		attempts++
		if attempts == 1 {
			return nil, errUnavailable
		}
		return connectTestServer(context.Background())
	}))
	p.initialize(map[string]any{})
	failure := p.callError("session/list", map[string]any{})
	if failure.Code != codeInternalError || failure.Message != "Kit server is unavailable: connection refused" {
		t.Fatalf("session/list error = %+v", failure)
	}
	p.call("session/list", map[string]any{})
}

func TestRequestIDsAreComparedExactly(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		a, b string
		same bool
	}{
		{`7`, `7.0`, true},
		{`7`, `7e0`, true},
		{`7`, `"7"`, false},
		{`9007199254740993`, `9007199254740992`, false},
		{`"a"`, `"a"`, true},
		{`null`, `null`, true},
	} {
		if same := idKey(json.RawMessage(test.a)) == idKey(json.RawMessage(test.b)); same != test.same {
			t.Errorf("idKey(%s) == idKey(%s) is %v, want %v", test.a, test.b, same, test.same)
		}
	}
}
