package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

var internalIdentityPattern = regexp.MustCompile(`\b(?:subagent|task|turn|message|mailbox|subreceipt)_[[:alnum:]]+\b`)

// RedactInternalIdentities removes runtime and persistence identities from text
// crossing into parent model context. Native diagnostics retain the full text.
func RedactInternalIdentities(text string) string {
	return internalIdentityPattern.ReplaceAllString(text, "<internal>")
}

// ToolService constructs model-facing asynchronous delegation tools.
type ToolService struct {
	Supervisor           *Supervisor
	Owners               OwnerResolver
	ResolveConfiguration func(context.Context, string, string) (string, string, error)
}

// Tool creates one owner-bound tool over an immutable definition catalog.
func (s *ToolService) Tool(ownerSessionID string, catalog Catalog) (droids.AnyTool, error) {
	if s == nil || s.Supervisor == nil || s.Owners == nil || s.ResolveConfiguration == nil {
		return nil, errors.New("subagent tool service is not initialized")
	}
	copied, err := NewCatalog(catalog.Definitions()...)
	if err != nil {
		return nil, err
	}
	return droids.NewTool(droids.Tool[toolArguments]{
		Name:        "subagent",
		Description: "Work with durable named child-agent sessions. Address subagents only by configured agent name. Messages steer a running subagent at its next model boundary or start a new turn when it is idle. Wait blocks until the named subagent has no active or queued work. Actions: list_agents, start, message, inspect, wait, cancel, dismiss.",
		Parameters:  modelToolParameters(),
		Mode:        droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, arguments toolArguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return s.execute(ctx, ownerSessionID, copied, arguments), nil
		},
	})
}

func modelToolParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"list_agents", "start", "message", "inspect", "wait", "cancel", "dismiss"}},
			"agent":   map[string]any{"type": "string", "description": "Configured agent name."},
			"message": map[string]any{"type": "string", "description": "Initial instructions or a live steering message."},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

type toolArguments struct {
	Action  string `json:"action"`
	Agent   string `json:"agent,omitempty"`
	Message string `json:"message,omitempty"`
}

type agentMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
	Source      Source `json:"source"`
}

type modelConversation struct {
	AgentName         string            `json:"agent"`
	Model             string            `json:"model"`
	State             ConversationState `json:"state"`
	QueuedMessages    int               `json:"queuedMessages,omitempty"`
	LastResultSummary string            `json:"lastResultSummary,omitempty"`
}

type toolResponse struct {
	Action       string             `json:"action"`
	Agents       []agentMetadata    `json:"agents,omitempty"`
	Conversation *modelConversation `json:"conversation,omitempty"`
	Dismissed    bool               `json:"dismissed,omitempty"`
	Warning      string             `json:"warning,omitempty"`
	Error        string             `json:"error,omitempty"`
}

func projectModelConversation(conversation Conversation) modelConversation {
	return modelConversation{
		AgentName: conversation.Agent.Name, Model: conversation.Model, State: conversation.State,
		QueuedMessages: conversation.QueuedTasks, LastResultSummary: conversation.LastResultSummary,
	}
}

func (s *ToolService) execute(ctx context.Context, ownerSessionID string, catalog Catalog, arguments toolArguments) droids.ToolResult {
	response := toolResponse{Action: arguments.Action}
	var err error
	switch arguments.Action {
	case "list_agents":
		for _, definition := range catalog.Definitions() {
			response.Agents = append(response.Agents, agentMetadata{
				Name: definition.Name, Description: definition.Description,
				Model: definition.Model, Source: definition.Source,
			})
		}
	case "start":
		var conversation Conversation
		conversation, response.Warning, err = s.startModel(ctx, ownerSessionID, catalog, arguments.Agent, arguments.Message)
		if err == nil {
			projected := projectModelConversation(conversation)
			response.Conversation = &projected
		}
	case "message":
		var conversation Conversation
		conversation, err = s.messageModel(ctx, ownerSessionID, arguments.Agent, arguments.Message)
		if err == nil {
			projected := projectModelConversation(conversation)
			response.Conversation = &projected
		}
	case "inspect":
		err = s.inspect(ctx, ownerSessionID, arguments.Agent, &response)
	case "wait":
		err = s.wait(ctx, ownerSessionID, arguments.Agent, &response)
	case "cancel":
		err = s.cancel(ctx, ownerSessionID, arguments.Agent, &response)
	case "dismiss":
		err = s.dismiss(ctx, ownerSessionID, arguments.Agent, &response)
	default:
		err = fmt.Errorf("%w: unsupported subagent action %q", ErrInvalidInput, arguments.Action)
	}
	if err != nil {
		response.Error = RedactInternalIdentities(err.Error())
	}
	raw, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: marshalErr.Error()}}, IsError: true}
	}
	result := droids.ToolText(string(raw))
	result.IsError = err != nil
	if details, detailsErr := droids.EncodeDetails(response); detailsErr == nil {
		result.Details = details
	}
	return result
}

func (s *ToolService) start(ctx context.Context, ownerSessionID string, catalog Catalog, agentName, message string) (Conversation, Task, string, error) {
	definition, ok := catalog.Lookup(strings.TrimSpace(agentName))
	if !ok {
		return Conversation{}, Task{}, "", fmt.Errorf("agent %q: %w", agentName, ErrNotFound)
	}
	owner, err := s.Owners.SubagentOwner(ctx, ownerSessionID)
	if err != nil {
		return Conversation{}, Task{}, "", err
	}
	if !owner.Persistent {
		return Conversation{}, Task{}, "", ErrTemporaryUnavailable
	}
	if existing, existingErr := s.Supervisor.ConversationByAgent(ctx, ownerSessionID, definition.Name); existingErr == nil {
		conversation, task, err := s.Supervisor.Admit(ctx, Admission{
			OwnerSessionID: ownerSessionID, ConversationID: existing.ID, ExpectedGeneration: existing.Generation,
			Definition: existing.Agent, CWD: existing.CWD, Model: existing.Model, ThinkingLevel: existing.ThinkingLevel,
			Message: message, Now: time.Now().UTC(),
		})
		return conversation, task, "", err
	} else if !errors.Is(existingErr, ErrNotFound) {
		return Conversation{}, Task{}, "", existingErr
	}
	model, thinking, warning, err := resolveChildConfiguration(ctx, definition.Name, definition.Model, owner.Model, owner.ThinkingLevel, s.ResolveConfiguration)
	if err != nil {
		return Conversation{}, Task{}, "", err
	}
	conversation, task, err := s.Supervisor.Admit(ctx, Admission{
		OwnerSessionID: ownerSessionID, Definition: definition, CWD: owner.CWD,
		Model: model, ThinkingLevel: thinking, Message: message, Now: time.Now().UTC(),
	})
	return conversation, task, warning, err
}

func (s *ToolService) startModel(ctx context.Context, ownerSessionID string, catalog Catalog, agentName, message string) (Conversation, string, error) {
	definition, ok := catalog.Lookup(strings.TrimSpace(agentName))
	if !ok {
		return Conversation{}, "", fmt.Errorf("agent %q: %w", agentName, ErrNotFound)
	}
	if existing, err := s.Supervisor.ConversationByAgent(ctx, ownerSessionID, definition.Name); err == nil {
		conversation, sendErr := s.sendToConversation(ctx, ownerSessionID, existing, message)
		return conversation, "", sendErr
	} else if !errors.Is(err, ErrNotFound) {
		return Conversation{}, "", err
	}
	conversation, _, warning, err := s.start(ctx, ownerSessionID, catalog, definition.Name, message)
	return conversation, warning, err
}

func (s *ToolService) messageModel(ctx context.Context, ownerSessionID, agentName, message string) (Conversation, error) {
	conversation, err := s.resolveModelConversation(ctx, ownerSessionID, agentName)
	if err != nil {
		return Conversation{}, err
	}
	return s.sendToConversation(ctx, ownerSessionID, conversation, message)
}

func (s *ToolService) sendToConversation(ctx context.Context, ownerSessionID string, conversation Conversation, message string) (Conversation, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Conversation{}, fmt.Errorf("%w: message is required", ErrInvalidInput)
	}
	if conversation.ActiveTaskID != "" {
		steered, err := s.Supervisor.Steer(ctx, conversation.ID, message)
		if err == nil {
			return steered, nil
		}
		if !errors.Is(err, ErrConflict) {
			return Conversation{}, err
		}
		conversation, err = s.Supervisor.Conversation(ctx, conversation.ID)
		if err != nil {
			return Conversation{}, err
		}
	}
	admitted, _, err := s.admitFollowUp(ctx, ownerSessionID, conversation, message)
	return admitted, err
}

func resolveChildConfiguration(
	ctx context.Context,
	agentName, requested, activeModel, activeThinking string,
	resolve func(context.Context, string, string) (string, string, error),
) (string, string, string, error) {
	selector := requested
	if selector == "" {
		selector = activeModel
	}
	if resolve == nil {
		return selector, activeThinking, "", nil
	}
	model, thinking, err := resolve(ctx, selector, activeThinking)
	if err == nil {
		return model, thinking, "", nil
	}
	if requested == "" {
		return "", "", "", fmt.Errorf("resolve active child model %q: %w", activeModel, err)
	}
	fallbackModel, fallbackThinking, fallbackErr := resolve(ctx, activeModel, activeThinking)
	if fallbackErr != nil {
		return "", "", "", fmt.Errorf("resolve requested child model %q and active fallback %q: %w", requested, activeModel, errors.Join(err, fallbackErr))
	}
	warning := fmt.Sprintf("Subagent %q requested unavailable model %q; using active model %q.", agentName, requested, fallbackModel)
	return fallbackModel, fallbackThinking, warning, nil
}

func (s *ToolService) admitFollowUp(ctx context.Context, ownerSessionID string, conversation Conversation, message string) (Conversation, Task, error) {
	var retryOf TaskID
	if conversation.State == ConversationInterrupted {
		tasks, err := s.Supervisor.ListTasks(ctx, conversation.ID)
		if err != nil {
			return Conversation{}, Task{}, err
		}
		for index := len(tasks) - 1; index >= 0; index-- {
			if tasks[index].State == TaskInterrupted {
				retryOf = tasks[index].ID
				break
			}
		}
	}
	return s.Supervisor.Admit(ctx, Admission{
		OwnerSessionID: ownerSessionID, ConversationID: conversation.ID, ExpectedGeneration: conversation.Generation,
		Definition: conversation.Agent, CWD: conversation.CWD,
		Model: conversation.Model, ThinkingLevel: conversation.ThinkingLevel,
		Message: message, RetryOf: retryOf, Now: time.Now().UTC(),
	})
}

func (s *ToolService) message(ctx context.Context, ownerSessionID, agentName string, conversationID ConversationID, message string) (Conversation, Task, error) {
	conversation, err := s.resolveConversation(ctx, ownerSessionID, agentName, string(conversationID))
	if err != nil {
		return Conversation{}, Task{}, err
	}
	return s.admitFollowUp(ctx, ownerSessionID, conversation, message)
}

func (s *ToolService) inspect(ctx context.Context, ownerSessionID, agentName string, response *toolResponse) error {
	conversation, err := s.resolveModelConversation(ctx, ownerSessionID, agentName)
	if err != nil {
		return err
	}
	projected := projectModelConversation(conversation)
	response.Conversation = &projected
	return nil
}

func (s *ToolService) wait(ctx context.Context, ownerSessionID, agentName string, response *toolResponse) error {
	conversation, err := s.resolveModelConversation(ctx, ownerSessionID, agentName)
	if err != nil {
		return err
	}
	conversation, err = s.Supervisor.WaitConversation(ctx, conversation.ID)
	if err != nil {
		return err
	}
	projected := projectModelConversation(conversation)
	response.Conversation = &projected
	return nil
}

func (s *ToolService) cancel(ctx context.Context, ownerSessionID, agentName string, response *toolResponse) error {
	conversation, err := s.resolveModelConversation(ctx, ownerSessionID, agentName)
	if err != nil {
		return err
	}
	tasks, err := s.Supervisor.ListTasks(ctx, conversation.ID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.State != TaskQueued && task.State != TaskRunning {
			continue
		}
		if _, cancelErr := s.Supervisor.Cancel(ctx, task.ID, task.CancellationGeneration, "canceled by parent"); cancelErr != nil && !errors.Is(cancelErr, ErrConflict) && !errors.Is(cancelErr, ErrNotCancelable) {
			return cancelErr
		}
	}
	conversation, err = s.Supervisor.Conversation(ctx, conversation.ID)
	if err != nil {
		return err
	}
	projected := projectModelConversation(conversation)
	response.Conversation = &projected
	return nil
}

func (s *ToolService) dismiss(ctx context.Context, ownerSessionID, agentName string, response *toolResponse) error {
	conversation, err := s.resolveModelConversation(ctx, ownerSessionID, agentName)
	if err != nil {
		return err
	}
	_, err = s.Supervisor.Dismiss(ctx, conversation.ID, conversation.Generation, "dismissed by parent")
	if err == nil {
		response.Dismissed = true
	}
	return err
}

func (s *ToolService) resolveModelConversation(ctx context.Context, ownerSessionID, agentName string) (Conversation, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return Conversation{}, fmt.Errorf("%w: agent is required", ErrInvalidInput)
	}
	conversation, err := s.Supervisor.ConversationByAgent(ctx, ownerSessionID, agentName)
	if err != nil {
		return Conversation{}, err
	}
	if conversation.OwnerSessionID != ownerSessionID || conversation.DismissedAt != nil {
		return Conversation{}, ErrNotFound
	}
	return conversation, nil
}

func (s *ToolService) resolveConversation(ctx context.Context, ownerSessionID, agentName, conversationID string) (Conversation, error) {
	if conversationID == "" && agentName == "" {
		return Conversation{}, fmt.Errorf("%w: agent or conversationId is required", ErrInvalidInput)
	}
	var conversation Conversation
	var err error
	if conversationID != "" {
		conversation, err = s.Supervisor.Conversation(ctx, ConversationID(conversationID))
	} else {
		conversation, err = s.Supervisor.ConversationByAgent(ctx, ownerSessionID, strings.TrimSpace(agentName))
	}
	if err != nil {
		return Conversation{}, err
	}
	if conversation.OwnerSessionID != ownerSessionID || conversation.DismissedAt != nil {
		return Conversation{}, ErrNotFound
	}
	return conversation, nil
}

// InspectResult is the bounded authoritative result of a direct inspect operation.
type InspectResult struct {
	Conversation *Conversation
	Task         *Task
	Tasks        []Task
}

// Start queues work from a non-model client using the same admission path as the tool.
func (s *ToolService) Start(ctx context.Context, owner string, catalog Catalog, agent, message string) (Conversation, Task, string, error) {
	return s.start(ctx, owner, catalog, agent, message)
}

// Message queues a follow-up in an existing conversation.
func (s *ToolService) Message(ctx context.Context, owner, agent string, conversationID ConversationID, message string) (Conversation, Task, error) {
	return s.message(ctx, owner, agent, conversationID, message)
}

// Inspect returns current state selected by agent, conversation, or task.
func (s *ToolService) Inspect(ctx context.Context, owner, agent string, conversationID ConversationID, taskID TaskID) (InspectResult, error) {
	if taskID != "" {
		task, err := s.Supervisor.Task(ctx, taskID)
		if err != nil || task.OwnerSessionID != owner {
			if err == nil {
				err = ErrNotFound
			}
			return InspectResult{}, err
		}
		conversation, err := s.Supervisor.Conversation(ctx, task.ConversationID)
		if err != nil {
			return InspectResult{}, err
		}
		return InspectResult{Conversation: &conversation, Task: &task}, nil
	}
	conversation, err := s.resolveConversation(ctx, owner, agent, string(conversationID))
	if err != nil {
		return InspectResult{}, err
	}
	tasks, err := s.Supervisor.ListTasks(ctx, conversation.ID)
	if len(tasks) > 20 {
		tasks = tasks[len(tasks)-20:]
	}
	return InspectResult{Conversation: &conversation, Tasks: tasks}, err
}

// Wait waits for a bounded interval and returns current state on timeout.
func (s *ToolService) Wait(ctx context.Context, owner string, taskID TaskID, timeout time.Duration) (Task, bool, error) {
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	task, err := s.Supervisor.WaitTask(waitContext, taskID)
	timedOut := errors.Is(err, context.DeadlineExceeded)
	if timedOut {
		task, err = s.Supervisor.Task(ctx, taskID)
	}
	if err == nil && task.OwnerSessionID != owner {
		err = ErrNotFound
	}
	return task, timedOut, err
}

// Cancel generation-safely requests cancellation of a task owned by the parent.
func (s *ToolService) Cancel(ctx context.Context, owner string, taskID TaskID) (Task, error) {
	task, err := s.Supervisor.Task(ctx, taskID)
	if err != nil || task.OwnerSessionID != owner {
		if err == nil {
			err = ErrNotFound
		}
		return Task{}, err
	}
	return s.Supervisor.Cancel(ctx, task.ID, task.CancellationGeneration, "canceled by client")
}

// Dismiss destructively resets one active conversation.
func (s *ToolService) Dismiss(ctx context.Context, owner, agent string, conversationID ConversationID) error {
	conversation, err := s.resolveConversation(ctx, owner, agent, string(conversationID))
	if err != nil {
		return err
	}
	_, err = s.Supervisor.Dismiss(ctx, conversation.ID, conversation.Generation, "dismissed by client")
	return err
}

var _ ParentToolFactory = (*ToolService)(nil)
