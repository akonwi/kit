package session_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/session"
)

func TestClientPluginToolsFollowTheOwnerIntoAttachedSubagents(t *testing.T) {
	host := &mockPluginHost{}
	providers := &authorityProviders{}
	var clientToolsChanged func()
	manager := pluginTestManagerWithProviders(t, providers, func(_ context.Context, input session.PluginSession, _ func()) session.PluginHost {
		clientToolsChanged = input.ClientToolsChanged
		return host
	})
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	schema := json.RawMessage(`{"type":"object"}`)
	host.mu.Lock()
	host.tools = []session.PluginTool{
		{ID: "demo.echo", Instance: "process:1:1", ModelName: "demo__echo", Description: "Echo", ExecutionMode: "sequential", InputSchema: schema},
		{ID: "browser.snapshot", Instance: "client:2:2", ModelName: "browser__snapshot", Description: "Outline a tab", ExecutionMode: "sequential", InputSchema: schema, PromptSnippet: "Use refs from the outline.", Client: true},
	}
	host.mu.Unlock()
	clientToolsChanged()

	feed, err := manager.ClientToolFeed(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := providers.Resolve("test/echo")
	if err != nil {
		t.Fatal(err)
	}
	child, err := droids.Spawn(t.Context(), "subagent_0123456789abcdef0123456789abcdef", droids.Config{Store: droids.NewMemoryStore(), Model: model, SystemPrompt: "child"})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	detach, err := feed.Attach(child)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	childRequest := func() droids.Request {
		t.Helper()
		handle, err := child.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "look"}}}, droids.PromptOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := handle.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		providers.mu.Lock()
		defer providers.mu.Unlock()
		return providers.requests[len(providers.requests)-1]
	}
	names := func(request droids.Request) string {
		var result []string
		for _, tool := range request.Tools {
			result = append(result, tool.Name)
		}
		return strings.Join(result, ",")
	}

	request := childRequest()
	if got := names(request); got != "browser__snapshot" {
		t.Fatalf("child tools = %q, want only the client-hosted tool", got)
	}
	if !strings.Contains(request.SystemPrompt, "Tool browser__snapshot:\nUse refs from the outline.") {
		t.Fatalf("child prompt omits the client tool snippet:\n%s", request.SystemPrompt)
	}

	host.mu.Lock()
	host.tools = host.tools[:1]
	host.mu.Unlock()
	clientToolsChanged()
	if got := names(childRequest()); got != "" {
		t.Fatalf("child tools after the client left = %q", got)
	}
}
