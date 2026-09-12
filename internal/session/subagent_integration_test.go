package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestConcurrentSubagentVerticalSlice(t *testing.T) {
	home := t.TempDir()
	paths := apphome.FromHome(home)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Agents, "scout.md"), []byte("---\nname: scout\ndescription: inspects repositories\nmodel: production/scout\n---\nInspect carefully and report evidence.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	providers := &subagentIntegrationProviders{childStarted: make(chan struct{}), releaseChild: make(chan struct{})}
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	childBundles, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: "core", Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	childFactory, err := session.NewChildRuntimeFactory(providers, childBundles, filepath.Join(paths.Droids, "subagents"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := subagent.NewSupervisor(store, childFactory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := subagent.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	tools := &subagent.ToolService{Supervisor: supervisor, Owners: store, ResolveConfiguration: func(_ context.Context, selector, thinking string) (string, string, error) {
		_, model, err := providers.Resolve(selector)
		if err != nil {
			return "", "", err
		}
		return model.Provider + "/" + model.ID, thinking, nil
	}}
	parentBundles, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: "core", Registry: registry, SubagentLoader: loader, SubagentToolFactory: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(store, providers, parentBundles, session.WithDroidStoreDirectory(paths.Droids))
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.SetEventSink(manager); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = supervisor.Shutdown(context.Background())
		_ = manager.Shutdown(context.Background())
		_ = store.Close()
	})
	record, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_abcdefabcdefabcdefabcdefabcdefab", CWD: t.TempDir(), Model: "test/echo",
	})
	if err != nil {
		t.Fatal(err)
	}

	parentDone := make(chan session.PromptResult, 1)
	parentErr := make(chan error, 1)
	go func() {
		result, err := manager.RunPrompt(context.Background(), record.ID, "delegate to scout and continue")
		if err != nil {
			parentErr <- err
			return
		}
		parentDone <- result
	}()
	select {
	case err := <-parentErr:
		t.Fatal(err)
	case result := <-parentDone:
		if result.Text != "parent continued" || result.Status != session.RunStatusCompleted {
			t.Fatalf("parent result = %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parent blocked on child completion")
	}
	select {
	case <-providers.childStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("child did not start")
	}
	conversations, err := supervisor.ListConversations(t.Context(), record.ID)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("conversations = %#v, %v", conversations, err)
	}
	if conversations[0].Model != "test/echo" {
		t.Fatalf("fallback child model = %q, want test/echo", conversations[0].Model)
	}
	tasks, err := supervisor.ListTasks(t.Context(), conversations[0].ID)
	if err != nil || len(tasks) != 1 || tasks[0].State != subagent.TaskRunning || tasks[0].ChildTurnID == "" {
		t.Fatalf("running tasks = %#v, %v", tasks, err)
	}
	close(providers.releaseChild)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	completed, err := supervisor.WaitTask(ctx, tasks[0].ID)
	cancel()
	if err != nil || completed.State != subagent.TaskCompleted || completed.ResultSummary != "child evidence" {
		t.Fatalf("completed child = %#v, %v", completed, err)
	}
	childStorePath := filepath.Join(paths.Droids, "subagents", string(conversations[0].ID)+".db")
	if info, err := os.Stat(childStorePath); err != nil || info.Size() == 0 {
		t.Fatalf("child transcript store %q = %#v, %v", childStorePath, info, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		pending, pendingErr := store.PendingMailbox(t.Context(), record.ID, 10)
		snapshot, snapshotErr := manager.Snapshot(t.Context(), record.ID)
		providers.mu.Lock()
		sawMailbox := providers.parentSawMailbox
		providers.mu.Unlock()
		if pendingErr == nil && snapshotErr == nil && len(pending) == 0 && snapshot.ActiveRunID == "" && sawMailbox {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("autonomous parent reaction did not settle: pending=%#v snapshot=%#v mailbox=%v errors=%v/%v", pending, snapshot, sawMailbox, pendingErr, snapshotErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	providers.mu.Lock()
	defer providers.mu.Unlock()
	if !providers.parentHadSubagentTool || providers.childHadSubagentTool || !providers.parentSawMailbox || !providers.parentSawModelFallback || !providers.childSawSteering || providers.parentSawOpaqueID {
		t.Fatalf("tool isolation/mailbox/fallback/steering/opaque IDs = parent tool:%v child tool:%v mailbox:%v fallback:%v steering:%v opaque IDs:%v", providers.parentHadSubagentTool, providers.childHadSubagentTool, providers.parentSawMailbox, providers.parentSawModelFallback, providers.childSawSteering, providers.parentSawOpaqueID)
	}
	conversation, err := supervisor.Conversation(t.Context(), conversations[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.DroidInitializedAt == nil {
		t.Fatal("child conversation was not marked initialized")
	}
	if err := os.Remove(childStorePath); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Transcript(t.Context(), conversation.ID); err == nil || !strings.Contains(err.Error(), "initialized child droid store is missing") {
		t.Fatalf("missing initialized child store error = %v", err)
	}
	if _, err := supervisor.Dismiss(t.Context(), conversation.ID, conversation.Generation, "test cleanup"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(childStorePath); !os.IsNotExist(err) {
		t.Fatalf("dismissed child store still exists: %v", err)
	}
}

type subagentIntegrationProviders struct {
	mu                     sync.Mutex
	parentCalls            int
	childStarted           chan struct{}
	releaseChild           chan struct{}
	parentHadSubagentTool  bool
	childHadSubagentTool   bool
	parentSawMailbox       bool
	parentSawModelFallback bool
	parentSawOpaqueID      bool
	childSawSteering       bool
}

func (p *subagentIntegrationProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *subagentIntegrationProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	if id != "test/echo" && id != "echo" {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), p.model(), nil
}
func (*subagentIntegrationProviders) Model(id string) (droids.Model, bool) {
	return (&subagentIntegrationProviders{}).model(), id == "test/echo" || id == "echo"
}
func (*subagentIntegrationProviders) RefreshModels(context.Context) error { return nil }
func (*subagentIntegrationProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *subagentIntegrationProviders) Stream(ctx context.Context, _ droids.Model, request droids.Request) droids.Stream {
	child := strings.Contains(request.SystemPrompt, "You are the scout subagent.")
	hasSubagentTool := false
	for _, tool := range request.Tools {
		if tool.Name == "subagent" {
			hasSubagentTool = true
		}
	}
	if child {
		p.mu.Lock()
		p.childHadSubagentTool = hasSubagentTool
		for _, message := range request.Messages {
			if user, ok := message.(droids.UserMessage); ok {
				for _, content := range user.Content {
					if text, ok := content.(droids.TextInput); ok && strings.Contains(text.Text, "change direction") {
						p.childSawSteering = true
					}
				}
			}
		}
		p.mu.Unlock()
		select {
		case <-p.childStarted:
		default:
			close(p.childStarted)
		}
		select {
		case <-p.releaseChild:
		case <-ctx.Done():
		}
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "child evidence"}},
		}}
	}
	p.mu.Lock()
	p.parentCalls++
	call := p.parentCalls
	p.parentHadSubagentTool = p.parentHadSubagentTool || hasSubagentTool
	for _, message := range request.Messages {
		if contextMessage, ok := message.(droids.ContextMessage); ok && contextMessage.Kind == "subagent_result" {
			p.parentSawMailbox = true
			raw := string(contextMessage.Details)
			if strings.Contains(raw, "taskId") || strings.Contains(raw, "conversationId") {
				p.parentSawOpaqueID = true
			}
		}
		if result, ok := message.(droids.ToolResultMessage); ok && result.ToolName == "subagent" {
			var details struct {
				Warning string `json:"warning"`
			}
			if json.Unmarshal(result.Details, &details) == nil && strings.Contains(details.Warning, "using active model") {
				p.parentSawModelFallback = true
			}
			raw := string(result.Details)
			if strings.Contains(raw, "taskId") || strings.Contains(raw, "conversationId") {
				p.parentSawOpaqueID = true
			}
		}
	}
	p.mu.Unlock()
	switch call {
	case 1:
		arguments, _ := json.Marshal(map[string]string{"action": "start", "agent": "scout", "message": "inspect the repository"})
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{ID: "call_subagent", Name: "subagent", Arguments: arguments}},
		}}
	case 2:
		select {
		case <-p.childStarted:
		case <-ctx.Done():
		}
		arguments, _ := json.Marshal(map[string]string{"action": "message", "agent": "scout", "message": "change direction"})
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{droids.ToolCall{ID: "call_steer_subagent", Name: "subagent", Arguments: arguments}},
		}}
	case 3:
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "parent continued"}},
		}}
	default:
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "parent received child"}},
		}}
	}
}
func (*subagentIntegrationProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}
