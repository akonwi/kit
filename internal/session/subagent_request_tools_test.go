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

func TestModelDrivenSiblingRequestAndExplicitReply(t *testing.T) {
	for _, reply := range []bool{true, false} {
		name := "unanswered"
		if reply {
			name = "replied"
		}
		t.Run(name, func(t *testing.T) { runModelDrivenSiblingRequest(t, reply, false) })
	}
}

func TestModelDrivenSiblingInitializesUnstartedRecipient(t *testing.T) {
	runModelDrivenSiblingRequest(t, true, true)
}

func runModelDrivenSiblingRequest(t *testing.T, reply, unstarted bool) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner := "session_abcdefabcdefabcdefabcdefabcdefab"
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: owner, ScratchpadOwnerID: owner, CWD: root, Persistent: true,
		ModelProvider: "test", ModelID: "echo", ThinkingLevel: "off",
	}); err != nil {
		t.Fatal(err)
	}
	providers := &siblingScriptProviders{reply: reply, unstarted: unstarted, events: make(chan siblingScriptEvent, 12)}
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	bundles, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: "core", Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := session.NewChildRuntimeFactory(providers, bundles, filepath.Join(root, "droids"), nil)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	siblings := &subagent.SiblingToolService{Repository: store, Supervisor: supervisor}
	factory.SiblingTools = siblings
	if unstarted {
		paths := apphome.FromHome(filepath.Join(root, "home"))
		if err := paths.Ensure(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.Agents, "recipient.md"), []byte("---\nname: recipient\ndescription: exchanges requests\nmodel: test/echo\n---\nFollow the script.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		loader, err := subagent.NewFilesystemLoader(paths)
		if err != nil {
			t.Fatal(err)
		}
		parentTools := &subagent.ToolService{
			Owners: store, Definitions: loader,
			ResolveConfiguration: func(_ context.Context, selector, thinking string) (string, string, error) {
				model, err := providers.Resolve(selector)
				return model.Provider + "/" + model.ID, thinking, err
			},
		}
		siblings.ResolveRecipient = parentTools.ResolveConfiguredRecipient
	}
	conversations := make(map[string]subagent.Conversation)
	for _, name := range []string{"sender", "recipient"} {
		if unstarted && name == "recipient" {
			continue
		}
		conversation, _, err := store.Admit(t.Context(), subagent.Admission{
			OwnerSessionID: owner,
			Definition: subagent.Definition{
				Name: name, Description: "exchanges requests", Instructions: "Follow the script.",
				Source: subagent.Source{Kind: subagent.SourceUser, Path: filepath.Join(root, name+".md")},
			},
			CWD: root, Model: "test/echo", Message: "initial parent work", Now: time.Now(),
		}, subagent.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		conversations[name] = conversation
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	var receipt string
	var sawReplyContext, sawSendResult, sawReplyResult bool
	for {
		select {
		case event := <-providers.events:
			if event.problem != "" {
				t.Fatal(event.problem)
			}
			if event.phase == "recipient-inbox" {
				receipt = event.receipt
			}
			if event.phase == "sender-reply" {
				sawReplyContext = true
			}
			if event.phase == "sender-send-result" {
				sawSendResult = true
			}
			if event.phase == "recipient-3" || unstarted && event.phase == "recipient-2" {
				sawReplyResult = true
			}
		case <-time.After(5 * time.Millisecond):
		}
		if receipt != "" {
			request, err := store.InspectSubagentRequest(t.Context(), owner, conversations["sender"].ID, receipt)
			if err != nil {
				t.Fatal(err)
			}
			if unstarted && conversations["recipient"].ID == "" {
				conversations["recipient"], err = supervisor.Conversation(t.Context(), request.RecipientConversationID)
				if err != nil || conversations["recipient"].Agent.Source.Kind != subagent.SourceUser || conversations["recipient"].Model != "test/echo" {
					t.Fatalf("initialized configured recipient = %#v, %v", conversations["recipient"], err)
				}
			}
			tasks, err := supervisor.ListTasks(t.Context(), conversations["recipient"].ID)
			if err != nil {
				t.Fatal(err)
			}
			finishedInbox := false
			for _, task := range tasks {
				if task.RequestID == receipt && task.Origin == subagent.TaskOriginRequest && task.State == subagent.TaskCompleted {
					finishedInbox = true
				}
			}
			senderTasks, err := supervisor.ListTasks(t.Context(), conversations["sender"].ID)
			if err != nil {
				t.Fatal(err)
			}
			finishedSender := false
			finishedReply := false
			for _, task := range senderTasks {
				if task.Origin == subagent.TaskOriginParent && task.State == subagent.TaskCompleted {
					finishedSender = true
				}
				if task.RequestID == receipt && task.Origin == subagent.TaskOriginReply && task.State == subagent.TaskCompleted {
					finishedReply = true
				}
			}
			if finishedInbox && finishedSender && sawSendResult &&
				(!reply || request.State == subagent.RequestReplied && sawReplyContext && sawReplyResult && finishedReply) {
				if reply && request.Reply != "the explicit answer" || !reply && request.State != subagent.RequestOpen {
					t.Fatalf("request after recipient inbox = %#v", request)
				}
				wantSenderTasks := 1
				if reply {
					wantSenderTasks = 2
				}
				wantRecipientTasks := 2
				if unstarted {
					wantRecipientTasks = 1
				}
				if len(senderTasks) != wantSenderTasks || len(tasks) != wantRecipientTasks || !reply && sawReplyContext {
					t.Fatalf("sibling task counts: sender=%#v recipient=%#v reply context=%v", senderTasks, tasks, sawReplyContext)
				}
				break
			}
		}
		select {
		case <-deadline:
			t.Fatalf("request did not settle: receipt=%q reply context=%v", receipt, sawReplyContext)
		default:
		}
	}
	mailbox, err := store.PendingMailbox(t.Context(), owner, 8)
	wantMailbox := 2
	if unstarted {
		wantMailbox = 1
	}
	if err != nil || len(mailbox) != wantMailbox {
		t.Fatalf("parent completion mailbox (only original tasks) = %#v, %v", mailbox, err)
	}
	for _, item := range mailbox {
		if item.TaskID == "" || item.ConversationID != conversations["sender"].ID && item.ConversationID != conversations["recipient"].ID {
			t.Fatalf("unexpected parent completion = %#v", item)
		}
	}
}

type siblingScriptEvent struct {
	phase, receipt, problem string
}

type siblingScriptProviders struct {
	mu        sync.Mutex
	calls     map[string]int
	reply     bool
	unstarted bool
	events    chan siblingScriptEvent
}

func (p *siblingScriptProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *siblingScriptProviders) Resolve(id string) (droids.Model, error) {
	if id != "test/echo" && id != "echo" {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), p.model())
}
func (p *siblingScriptProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "test/echo" || id == "echo"
}
func (*siblingScriptProviders) RefreshModels(context.Context) error { return nil }
func (*siblingScriptProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (*siblingScriptProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}
func (p *siblingScriptProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	name := ""
	for _, candidate := range []string{"sender", "recipient"} {
		if strings.Contains(request.SystemPrompt, "You are the "+candidate+" subagent.") {
			name = candidate
		}
	}
	p.mu.Lock()
	if p.calls == nil {
		p.calls = make(map[string]int)
	}
	p.calls[name]++
	call := p.calls[name]
	p.mu.Unlock()
	event := siblingScriptEvent{phase: fmt.Sprintf("%s-%d", name, call)}
	tools := make(map[string]bool)
	for _, tool := range request.Tools {
		tools[tool.Name] = true
	}
	if !tools["subagent_send"] || !tools["subagent_reply"] || tools["subagent"] || name == "" {
		event.problem = fmt.Sprintf("incorrect child tools for %q: %#v", name, tools)
	}
	var result droids.AssistantMessage
	result.Provider, result.Model, result.StopReason = "test", "echo", droids.StopReasonStop
	result.Content = []droids.AssistantContent{droids.TextContent{Text: event.phase}}
	switch {
	case name == "sender" && call == 1:
		result.StopReason = droids.StopReasonToolUse
		result.Content = []droids.AssistantContent{droids.ToolCall{ID: "call_send", Name: "subagent_send", Arguments: []byte(`{"agent":"recipient","message":"please answer explicitly"}`)}}
	case name == "sender" && call == 2:
		for _, message := range request.Messages {
			if tool, ok := message.(droids.ToolResultMessage); ok && tool.ToolName == "subagent_send" && !tool.IsError {
				for _, content := range tool.Content {
					if text, ok := content.(droids.TextContent); ok && strings.Contains(text.Text, `"receipt":"subrequest_`) {
						event.phase = "sender-send-result"
					}
				}
			}
		}
		if event.phase != "sender-send-result" {
			event.problem = "sender did not receive successful send tool result with receipt"
		}
	case name == "sender" && call == 3:
		for _, message := range request.Messages {
			if boundary, ok := message.(droids.ContextMessage); ok && boundary.Kind == "subagent_inbox" {
				for _, content := range boundary.Content {
					if text, ok := content.(droids.TextInput); ok && strings.Contains(text.Text, "the explicit answer") {
						event.phase = "sender-reply"
					}
				}
			}
		}
		if event.phase != "sender-reply" {
			event.problem = "sender did not receive explicit reply inbox context"
		}
	case name == "recipient" && (call == 2 && !p.unstarted || call == 1 && p.unstarted):
		for _, message := range request.Messages {
			if boundary, ok := message.(droids.ContextMessage); ok && boundary.Kind == "subagent_inbox" {
				var details struct {
					Receipt string `json:"receipt"`
				}
				if json.Unmarshal(boundary.Details, &details) == nil && strings.Contains(details.Receipt, "subrequest_") {
					for _, content := range boundary.Content {
						if text, ok := content.(droids.TextInput); ok && strings.Contains(text.Text, "please answer explicitly") {
							event.phase, event.receipt = "recipient-inbox", details.Receipt
						}
					}
				}
			}
		}
		if event.phase != "recipient-inbox" {
			event.problem = "recipient did not receive request inbox context and receipt"
		} else if p.reply {
			arguments, _ := json.Marshal(map[string]string{"receipt": event.receipt, "message": "the explicit answer"})
			result.StopReason = droids.StopReasonToolUse
			result.Content = []droids.AssistantContent{droids.ToolCall{ID: "call_reply", Name: "subagent_reply", Arguments: arguments}}
		}
	case name == "recipient" && (call == 3 && !p.unstarted || call == 2 && p.unstarted):
		if !p.reply {
			event.problem = "recipient unexpectedly reacted again without replying"
		} else {
			var accepted bool
			for _, message := range request.Messages {
				if tool, ok := message.(droids.ToolResultMessage); ok && tool.ToolName == "subagent_reply" && !tool.IsError {
					accepted = true
				}
			}
			if !accepted {
				event.problem = "recipient did not receive successful reply tool result"
			}
		}
	case call > 3:
		event.problem = fmt.Sprintf("unexpected extra %s model call %d", name, call)
	}
	p.events <- event
	return &authorityStream{message: result}
}
