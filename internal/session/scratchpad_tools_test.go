package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

type scratchpadToolProviders struct {
	mu             sync.Mutex
	call           int
	toolName       string
	arguments      []byte
	toolNames      []string
	toolParameters map[string]map[string]any
	resultText     string
	systemPrompts  []string
}

func (p *scratchpadToolProviders) ID() string             { return "test" }
func (p *scratchpadToolProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *scratchpadToolProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}
func (p *scratchpadToolProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (*scratchpadToolProviders) RefreshModels(context.Context) error { return nil }
func (*scratchpadToolProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *scratchpadToolProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.call++
	p.systemPrompts = append(p.systemPrompts, request.SystemPrompt)
	if p.call == 1 {
		p.toolNames = p.toolNames[:0]
		p.toolParameters = make(map[string]map[string]any)
		for _, tool := range request.Tools {
			p.toolNames = append(p.toolNames, tool.Name)
			p.toolParameters[tool.Name] = tool.Parameters
		}
		if p.toolName == "" {
			return &authorityStream{message: droids.AssistantMessage{
				Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
				Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
			}}
		}
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{ID: "call_scratchpad", Name: p.toolName, Arguments: append([]byte(nil), p.arguments...)}},
		}}
	}
	for _, message := range request.Messages {
		result, ok := message.(droids.ToolResultMessage)
		if !ok || result.ToolName != p.toolName {
			continue
		}
		for _, content := range result.Content {
			if text, ok := content.(droids.TextContent); ok {
				p.resultText += text.Text
			}
		}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
	}}
}
func (*scratchpadToolProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}
func (p *scratchpadToolProviders) prepare(name string, arguments any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.call = 0
	p.toolName = name
	p.arguments, _ = json.Marshal(arguments)
	p.resultText = ""
	p.toolNames = nil
	p.toolParameters = nil
	p.systemPrompts = nil
}

func TestScratchpadToolsReadAndEditSharedStateWithoutPaths(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &scratchpadToolProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}

	providers.prepare(session.EditScratchpadToolName, map[string]any{"edits": []map[string]string{{"oldText": "", "newText": "# Shared\n"}}})
	if _, err := manager.RunPrompt(t.Context(), record.ID, "initialize the scratchpad"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	toolNames := append([]string(nil), providers.toolNames...)
	editParameters := providers.toolParameters[session.EditScratchpadToolName]
	editResult := providers.resultText
	providers.mu.Unlock()
	if !containsString(toolNames, session.ReadScratchpadToolName) || !containsString(toolNames, session.EditScratchpadToolName) {
		t.Fatalf("provider tools = %v", toolNames)
	}
	properties, _ := editParameters["properties"].(map[string]any)
	if properties["path"] != nil || properties["sessionId"] != nil || properties["ownerSessionId"] != nil || !strings.Contains(editResult, "Applied 1 edit") {
		t.Fatalf("edit tool result = %q tools=%v parameters=%v", editResult, toolNames, editParameters)
	}
	updated, err := manager.Scratchpad(t.Context(), record.ID)
	if err != nil || updated.Content != "# Shared\n" || updated.Revision != 2 {
		t.Fatalf("edited scratchpad = %+v, %v", updated, err)
	}

	providers.prepare(session.ReadScratchpadToolName, map[string]any{})
	if _, err := manager.RunPrompt(t.Context(), record.ID, "read the scratchpad"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	readResult := providers.resultText
	systemPrompts := append([]string(nil), providers.systemPrompts...)
	providers.mu.Unlock()
	if !strings.Contains(readResult, "Scratchpad revision 2") || !strings.Contains(readResult, "# Shared") {
		t.Fatalf("read tool result = %q", readResult)
	}
	for _, prompt := range systemPrompts {
		if strings.Contains(prompt, "# Shared") {
			t.Fatalf("system prompt included scratchpad content: %q", prompt)
		}
	}

	providers.prepare("", map[string]any{})
	temporary, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", CWD: root, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), temporary.ID, "check tools"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	temporaryTools := append([]string(nil), providers.toolNames...)
	providers.mu.Unlock()
	if containsString(temporaryTools, session.ReadScratchpadToolName) || containsString(temporaryTools, session.EditScratchpadToolName) {
		t.Fatalf("temporary tools = %v", temporaryTools)
	}
}
