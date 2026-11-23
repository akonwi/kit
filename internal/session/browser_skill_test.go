package session_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
)

// browserClientTools are the tools the Kit app's browser plugin connects.
func browserClientTools() []session.PluginTool {
	schema := json.RawMessage(`{"type":"object"}`)
	var tools []session.PluginTool
	for _, id := range []string{"tabs", "navigate", "snapshot", "act", "screenshot"} {
		tools = append(tools, session.PluginTool{
			ID: id, Instance: "client:2:2", ModelName: "browser__" + id, Description: "Browser " + id,
			ExecutionMode: "sequential", InputSchema: schema, Client: true,
		})
	}
	return tools
}

func skillLoader(t *testing.T) skills.Loader {
	t.Helper()
	loader, err := skills.NewFilesystemLoader(apphome.FromHome(filepath.Join(t.TempDir(), "kit-home")))
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

const browserCatalogEntry = "Connected client tools make these skills available too, through activate_skill:\n<available_skills>\n  <skill>\n    <name>kit-browser</name>"

func TestTheBrowserSkillFollowsTheBrowserToolsConnectingAndLeaving(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: "system", SkillLoader: skillLoader(t)})
	if err != nil {
		t.Fatal(err)
	}
	host := &mockPluginHost{}
	var clientToolsChanged func()
	providers := &scratchpadToolProviders{}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
		session.WithPluginHostFactory(func(_ context.Context, input session.PluginSession, _ func()) session.PluginHost {
			clientToolsChanged = input.ClientToolsChanged
			return host
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	connect := func(tools []session.PluginTool) {
		host.mu.Lock()
		host.tools = tools
		host.mu.Unlock()
		clientToolsChanged()
	}
	// The provider collects every activation result in the history, so each
	// call's result follows the earlier ones.
	var earlier string
	activate := func() (prompt, result string) {
		t.Helper()
		providers.prepare(skills.ActivateToolName, map[string]string{"name": skills.KitBrowserName})
		if _, err := manager.RunPrompt(t.Context(), record.ID, "use the browser"); err != nil {
			t.Fatal(err)
		}
		providers.mu.Lock()
		defer providers.mu.Unlock()
		all := providers.resultText
		result, earlier = strings.TrimPrefix(all, earlier), all
		return providers.systemPrompts[0], result
	}
	const unavailable = `Skill "kit-browser" isn't available now: it needs the tools browser__act, browser__navigate, browser__screenshot, browser__snapshot, browser__tabs, which a connected Kit app provides, and they aren't connected.`

	prompt, result := activate()
	if strings.Contains(prompt, "<name>kit-browser</name>") || result != unavailable {
		t.Fatalf("before the browser connected: result %q, prompt:\n%s", result, prompt)
	}

	connect(browserClientTools())
	prompt, result = activate()
	if !strings.Contains(prompt, "\n\n"+browserCatalogEntry) || !strings.HasPrefix(result, "---\nname: kit-browser\n") {
		t.Fatalf("while the browser is connected: result %q, prompt:\n%s", result, prompt)
	}

	connect(nil)
	prompt, result = activate()
	if strings.Contains(prompt, "<name>kit-browser</name>") || result != unavailable {
		t.Fatalf("after the browser left: result %q, prompt:\n%s", result, prompt)
	}

	connect(browserClientTools())
	prompt, result = activate()
	if !strings.Contains(prompt, "\n\n"+browserCatalogEntry) || !strings.HasPrefix(result, "---\nname: kit-browser\n") {
		t.Fatalf("after the browser reconnected: result %q, prompt:\n%s", result, prompt)
	}

	// A reload builds a new offering, which the connected tools carry over to.
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	prompt, result = activate()
	if !strings.Contains(prompt, "\n\n"+browserCatalogEntry) || !strings.HasPrefix(result, "---\nname: kit-browser\n") {
		t.Fatalf("after a reload: result %q, prompt:\n%s", result, prompt)
	}
}

func TestTheBrowserSkillReachesSubagentsWithTheBrowserTools(t *testing.T) {
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
	host.mu.Lock()
	host.tools = browserClientTools()
	host.mu.Unlock()
	clientToolsChanged()

	loaded, err := skillLoader(t).Load(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	offering := loaded.Registry.NewOffering()
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
	detach, err := feed.Attach(child, offering)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	childPrompt := func() string {
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
		return providers.requests[len(providers.requests)-1].SystemPrompt
	}

	if prompt := childPrompt(); !strings.Contains(prompt, "\n\n"+browserCatalogEntry) {
		t.Fatalf("child prompt with the browser connected:\n%s", prompt)
	}
	host.mu.Lock()
	host.tools = nil
	host.mu.Unlock()
	clientToolsChanged()
	if prompt := childPrompt(); strings.Contains(prompt, "<name>kit-browser</name>") {
		t.Fatalf("child prompt after the browser left:\n%s", prompt)
	}
	host.mu.Lock()
	host.tools = browserClientTools()
	host.mu.Unlock()
	clientToolsChanged()
	if prompt := childPrompt(); !strings.Contains(prompt, "\n\n"+browserCatalogEntry) {
		t.Fatalf("child prompt after the browser reconnected:\n%s", prompt)
	}
}
