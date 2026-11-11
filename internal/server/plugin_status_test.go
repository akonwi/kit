package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/api/pluginprotocol"
	"github.com/akonwi/kit/internal/apphome"
)

// TestPluginsSeeSessionStatusAndCompaction runs a version 2 plugin in a real
// daemon. It records the status in initialize, status notifications, and
// compaction events, and asks for the status after each turn.
func TestPluginsSeeSessionStatusAndCompaction(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture requires python3")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "status")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifestVersion":1,"protocolVersion":2,"id":"status","transport":{"type":"stdio","command":` + mustJSON(t, python) + `,"args":["plugin.py"]}}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `import json, sys
def record(kind, value):
    with open("events.jsonl", "a") as output:
        output.write(json.dumps({"kind": kind, "value": value}, separators=(",", ":")) + "\n")
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        record("initialize", message["params"].get("status"))
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":{"protocolVersion":2}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":"ready","method":"kit/commands/register","params":{"id":"ready","description":"Ready marker"}}), flush=True)
    elif method == "shutdown":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":None}), flush=True)
        break
    elif method == "kit/events/agent.turn.completed":
        print(json.dumps({"jsonrpc":"2.0","id":"status","method":"kit/session/status"}), flush=True)
    elif method in ("kit/events/session.status.changed", "kit/events/compaction.started", "kit/events/compaction.completed"):
        record(method, message["params"])
    elif "method" not in message and message.get("id") == "status":
        record("request", message.get("result"))
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
	client := localTransport(paths)
	defer client.CloseIdleConnections()
	eventually(t, func() bool { _, _, err := client.Probe(t.Context(), true); return err == nil })
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
		return err == nil && len(snapshot.PluginCommands) == 1
	})

	type recorded struct {
		Kind  string          `json:"kind"`
		Value json.RawMessage `json:"value"`
	}
	read := func() []recorded {
		var result []recorded
		for _, line := range readJSONLines(t, filepath.Join(root, "events.jsonl")) {
			data, _ := json.Marshal(line)
			var item recorded
			_ = json.Unmarshal(data, &item)
			result = append(result, item)
		}
		return result
	}
	decode := func(raw json.RawMessage) pluginprotocol.SessionStatus {
		t.Helper()
		var status pluginprotocol.SessionStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			t.Fatalf("status %s: %v", raw, err)
		}
		return status
	}

	initial := decode(read()[0].Value)
	if read()[0].Kind != "initialize" || initial.SessionID != created.ID || initial.Model != "test/echo" || initial.Parent != nil || initial.Usage != (protocol.SessionUsage{}) {
		t.Fatalf("initialize status = %+v", initial)
	}

	if _, err := client.Prompt(t.Context(), created.ID, "one prompt"); err != nil {
		t.Fatal(err)
	}
	var snapshot protocol.SessionSnapshot
	var latest, requested pluginprotocol.SessionStatus
	eventually(t, func() bool {
		snapshot, err = client.GetSessionSnapshot(t.Context(), created.ID)
		if err != nil || snapshot.Usage.Input == 0 || snapshot.ActiveTurnID != "" {
			return false
		}
		latest, requested = pluginprotocol.SessionStatus{}, pluginprotocol.SessionStatus{}
		for _, item := range read() {
			switch item.Kind {
			case "kit/events/session.status.changed":
				latest = decode(item.Value)
			case "request":
				requested = decode(item.Value)
			}
		}
		return reflect.DeepEqual(latest.Usage, snapshot.Usage) && reflect.DeepEqual(requested.Usage, snapshot.Usage)
	})
	// The plugin sees what clients see. Its context is the estimate of the
	// latest model request, as clients' live context events report it; a
	// snapshot taken after the turn re-estimates it with the reply included.
	want := pluginprotocol.SessionStatus{SessionID: created.ID, Model: snapshot.Session.Model, ThinkingLevel: snapshot.Session.ThinkingLevel, Usage: snapshot.Usage, Context: latest.Context}
	if !reflect.DeepEqual(latest, want) || !reflect.DeepEqual(requested, want) {
		t.Fatalf("plugin status = %+v, requested %+v, want %+v", latest, requested, want)
	}
	if latest.Context == nil || latest.Context.Window != snapshot.ContextWindow || latest.Context.Tokens <= 0 || latest.Context.Tokens > snapshot.ContextTokens {
		t.Fatalf("plugin context = %+v, snapshot %d of %d", latest.Context, snapshot.ContextTokens, snapshot.ContextWindow)
	}

	if _, err := client.CompactSession(t.Context(), created.ID, protocol.CompactSessionInput{OperationID: "compact_status_test"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		var started, completed map[string]any
		for _, item := range read() {
			switch item.Kind {
			case "kit/events/compaction.started":
				_ = json.Unmarshal(item.Value, &started)
			case "kit/events/compaction.completed":
				_ = json.Unmarshal(item.Value, &completed)
			}
		}
		return started != nil && completed != nil && started["sessionId"] == created.ID && started["turnId"] == nil &&
			completed["compactionId"] == started["compactionId"] && completed["turnId"] == nil && completed["failureReason"] == nil
	})

	// After a client's compaction, the plugin's context is the compacted
	// context clients see.
	snapshot, err = client.GetSessionSnapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, item := range read() {
			if item.Kind == "kit/events/session.status.changed" {
				latest = decode(item.Value)
			}
		}
		return latest.Context != nil && *latest.Context == (pluginprotocol.ContextStatus{Tokens: snapshot.ContextTokens, Window: snapshot.ContextWindow})
	})

	// Switching models updates the model and the context window at once,
	// before another model request.
	if _, err := client.ConfigureSession(t.Context(), created.ID, protocol.ConfigureSessionInput{ExpectedRevision: snapshot.Session.ConfigurationRevision, Model: "test/echo-alt"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, item := range read() {
			if item.Kind == "kit/events/session.status.changed" {
				latest = decode(item.Value)
			}
		}
		return latest.Model == "test/echo-alt"
	})
	if latest.Context == nil || latest.Context.Window != 64_000 {
		t.Fatalf("status after switching models = %+v, context %+v", latest, latest.Context)
	}
}
