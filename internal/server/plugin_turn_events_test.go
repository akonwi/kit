package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/protocol"
)

func TestPluginTurnEventsFlowFromDaemonModelLifecycle(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture requires python3")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "turn-events")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifestVersion":1,"id":"turn-events","transport":{"type":"stdio","command":` + mustJSON(t, python) + `,"args":["plugin.py"]}}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `import json, sys
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":{"protocolVersion":1}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":"ready-register","method":"kit/commands/register","params":{"id":"ready","description":"Ready marker"}}), flush=True)
    elif method == "shutdown":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":None}), flush=True)
        break
    elif method in ("kit/events/agent.turn.started", "kit/events/agent.turn.completed"):
        with open("events.jsonl", "a") as output:
            output.write(json.dumps(message, separators=(",", ":")) + "\n")
`
	if err := os.WriteFile(filepath.Join(root, "plugin.py"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: &daemonEchoProviders{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown blocked")
		}
	}()
	client := NewClient(paths)
	defer client.http.CloseIdleConnections()
	eventually(t, func() bool { _, _, err := client.Probe(t.Context()); return err == nil })
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
		return err == nil && len(snapshot.PluginCommands) == 1 && snapshot.PluginCommands[0].ID == "turn-events.ready"
	})
	result, err := client.RunPrompt(t.Context(), created.ID, "one prompt")
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	eventually(t, func() bool {
		events = readJSONLines(t, filepath.Join(root, "events.jsonl"))
		return len(events) == 2
	})
	if events[0]["method"] != "kit/events/agent.turn.started" || events[1]["method"] != "kit/events/agent.turn.completed" {
		t.Fatalf("event methods = %#v", events)
	}
	started := events[0]["params"].(map[string]any)
	completed := events[1]["params"].(map[string]any)
	turn := completed["turn"].(map[string]any)
	messages := turn["messages"].([]any)
	if started["sessionId"] != created.ID || started["turnId"] != result.TurnID || completed["sessionId"] != created.ID || turn["id"] != result.TurnID || len(messages) != 2 {
		t.Fatalf("turn events = %#v", events)
	}
	if messages[0].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("public messages = %#v", messages)
	}

	reconnected := NewClient(paths)
	defer reconnected.http.CloseIdleConnections()
	if _, err := reconnected.GetSessionSnapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := readJSONLines(t, filepath.Join(root, "events.jsonl")); len(got) != 2 {
		t.Fatalf("client reconnect replayed turn events: %#v", got)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not settle")
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var result []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		result = append(result, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
