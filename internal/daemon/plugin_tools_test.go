package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
)

type pluginToolProviders struct {
	daemonEchoProviders
	toolMu             sync.Mutex
	ready, execute     bool
	received, guidance string
}

func (p *pluginToolProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (p *pluginToolProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.toolMu.Lock()
	defer p.toolMu.Unlock()
	p.ready = false
	for _, tool := range request.Tools {
		if tool.Name == "tool-demo__echo" {
			p.ready = true
		}
	}
	p.guidance = request.SystemPrompt
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok && result.ToolName == "tool-demo__echo" {
			for _, part := range result.Content {
				if text, ok := part.(droids.TextContent); ok {
					p.received = text.Text
				}
			}
		}
	}
	response := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
	if p.execute {
		p.execute = false
		response.StopReason = droids.StopReasonToolUse
		response.Content = []droids.AssistantContent{droids.ToolCall{ID: "plugin_call", Name: "tool-demo__echo", Arguments: json.RawMessage(`{"text":"exact plugin result"}`)}}
	}
	return &daemonEchoStream{final: response}
}
func TestPluginToolFixtureReachesModelAndDurableTranscript(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python fixture requires python3")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "tool-demo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.json", "plugin.py"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", "tool-demo", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	providers := &pluginToolProviders{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
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
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := client.Probe(t.Context()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	// Background registration must become visible on a later request without reload.
	for {
		if _, err := client.RunPrompt(t.Context(), created.ID, "probe tool catalog"); err != nil {
			t.Fatal(err)
		}
		providers.toolMu.Lock()
		ready := providers.ready
		providers.toolMu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin tool never reached model")
		}
		time.Sleep(10 * time.Millisecond)
	}
	providers.toolMu.Lock()
	providers.execute = true
	providers.toolMu.Unlock()
	if _, err := client.RunPrompt(t.Context(), created.ID, "use the plugin echo tool"); err != nil {
		t.Fatal(err)
	}
	providers.toolMu.Lock()
	received, guidance := providers.received, providers.guidance
	providers.toolMu.Unlock()
	if received != "exact plugin result" || !strings.Contains(guidance, "This demonstration tool does not modify files.") {
		t.Fatalf("tool integration received=%q guidance=%q", received, guidance)
	}
	snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot.Messages)
	if !strings.Contains(string(data), "exact plugin result") || !strings.Contains(string(data), "tool-demo__echo") {
		t.Fatalf("tool result not durable: %s", data)
	}
	if err := client.DeleteSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
}
