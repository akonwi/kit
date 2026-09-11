package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

const (
	defaultWaitTimeout = 5 * time.Second
	maxWaitTimeout     = 30 * time.Second
)

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
		Description: "Start and supervise asynchronous child-agent work. Start work, keep working independently, and inspect or wait only when needed. Actions: list_agents, start, message, inspect, wait, cancel, dismiss.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":         map[string]any{"type": "string", "enum": []string{"list_agents", "start", "message", "inspect", "wait", "cancel", "dismiss"}},
				"agent":          map[string]any{"type": "string", "description": "Configured agent name."},
				"message":        map[string]any{"type": "string", "description": "Task or follow-up instructions."},
				"conversationId": map[string]any{"type": "string", "description": "Stable child conversation identity."},
				"taskId":         map[string]any{"type": "string", "description": "Stable submitted task identity."},
				"timeoutMs":      map[string]any{"type": "integer", "minimum": 1, "maximum": maxWaitTimeout.Milliseconds()},
			},
			"required":             []string{"action"},
			"additionalProperties": false,
		},
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, arguments toolArguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return s.execute(ctx, ownerSessionID, copied, arguments), nil
		},
	})
}

type toolArguments struct {
	Action         string `json:"action"`
	Agent          string `json:"agent,omitempty"`
	Message        string `json:"message,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	TaskID         string `json:"taskId,omitempty"`
	TimeoutMS      int64  `json:"timeoutMs,omitempty"`
}

type agentMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
	Source      Source `json:"source"`
}

type modelConversation struct {
	ID                  ConversationID    `json:"id"`
	AgentName           string            `json:"agent"`
	Model               string            `json:"model"`
	State               ConversationState `json:"state"`
	Generation          uint64            `json:"generation"`
	ActiveTaskID        TaskID            `json:"activeTaskId,omitempty"`
	QueuedTasks         int               `json:"queuedTasks"`
	LastCompletedTaskID TaskID            `json:"lastCompletedTaskId,omitempty"`
	LastResultSummary   string            `json:"lastResultSummary,omitempty"`
}

type modelTask struct {
	ID                     TaskID         `json:"id"`
	ConversationID         ConversationID `json:"conversationId"`
	Sequence               uint64         `json:"sequence"`
	State                  TaskState      `json:"state"`
	ChildTurnID            string         `json:"childTurnId,omitempty"`
	CancellationGeneration uint64         `json:"cancellationGeneration"`
	RetryOf                TaskID         `json:"retryOf,omitempty"`
	ResultSummary          string         `json:"resultSummary,omitempty"`
	Error                  string         `json:"error,omitempty"`
}

type toolResponse struct {
	Action       string             `json:"action"`
	Agents       []agentMetadata    `json:"agents,omitempty"`
	Conversation *modelConversation `json:"conversation,omitempty"`
	Task         *modelTask         `json:"task,omitempty"`
	Tasks        []modelTask        `json:"tasks,omitempty"`
	TimedOut     bool               `json:"timedOut,omitempty"`
	Dismissed    bool               `json:"dismissed,omitempty"`
	Warning      string             `json:"warning,omitempty"`
	Error        string             `json:"error,omitempty"`
}

func projectModelConversation(conversation Conversation) modelConversation {
	return modelConversation{
		ID: conversation.ID, AgentName: conversation.Agent.Name, Model: conversation.Model,
		State: conversation.State, Generation: conversation.Generation,
		ActiveTaskID: conversation.ActiveTaskID, QueuedTasks: conversation.QueuedTasks,
		LastCompletedTaskID: conversation.LastCompletedTaskID, LastResultSummary: conversation.LastResultSummary,
	}
}

func projectModelTask(task Task) modelTask {
	return modelTask{
		ID: task.ID, ConversationID: task.ConversationID, Sequence: task.Sequence, State: task.State,
		ChildTurnID: task.ChildTurnID, CancellationGeneration: task.CancellationGeneration,
		RetryOf: task.RetryOf, ResultSummary: task.ResultSummary, Error: task.Error,
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
		var task Task
		conversation, task, response.Warning, err = s.start(ctx, ownerSessionID, catalog, arguments.Agent, arguments.Message)
		projectedConversation, projectedTask := projectModelConversation(conversation), projectModelTask(task)
		response.Conversation, response.Task = &projectedConversation, &projectedTask
	case "message":
		var conversation Conversation
		var task Task
		conversation, task, err = s.message(ctx, ownerSessionID, arguments)
		projectedConversation, projectedTask := projectModelConversation(conversation), projectModelTask(task)
		response.Conversation, response.Task = &projectedConversation, &projectedTask
	case "inspect":
		err = s.inspect(ctx, ownerSessionID, arguments, &response)
	case "wait":
		err = s.wait(ctx, ownerSessionID, arguments, &response)
	case "cancel":
		err = s.cancel(ctx, ownerSessionID, arguments, &response)
	case "dismiss":
		err = s.dismiss(ctx, ownerSessionID, arguments, &response)
	default:
		err = fmt.Errorf("%w: unsupported subagent action %q", ErrInvalidInput, arguments.Action)
	}
	if err != nil {
		response.Error = err.Error()
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

func (s *ToolService) message(ctx context.Context, ownerSessionID string, arguments toolArguments) (Conversation, Task, error) {
	conversation, err := s.resolveConversation(ctx, ownerSessionID, arguments.Agent, arguments.ConversationID)
	if err != nil {
		return Conversation{}, Task{}, err
	}
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
		Message: arguments.Message, RetryOf: retryOf, Now: time.Now().UTC(),
	})
}

func (s *ToolService) inspect(ctx context.Context, ownerSessionID string, arguments toolArguments, response *toolResponse) error {
	if arguments.TaskID != "" {
		task, err := s.Supervisor.Task(ctx, TaskID(arguments.TaskID))
		if err != nil {
			return err
		}
		if task.OwnerSessionID != ownerSessionID {
			return ErrNotFound
		}
		projectedTask := projectModelTask(task)
		response.Task = &projectedTask
		conversation, err := s.Supervisor.Conversation(ctx, task.ConversationID)
		if err == nil {
			projectedConversation := projectModelConversation(conversation)
			response.Conversation = &projectedConversation
		}
		return err
	}
	conversation, err := s.resolveConversation(ctx, ownerSessionID, arguments.Agent, arguments.ConversationID)
	if err != nil {
		return err
	}
	projectedConversation := projectModelConversation(conversation)
	response.Conversation = &projectedConversation
	tasks, err := s.Supervisor.ListTasks(ctx, conversation.ID)
	if len(tasks) > 20 {
		tasks = tasks[len(tasks)-20:]
	}
	response.Tasks = make([]modelTask, 0, len(tasks))
	for _, task := range tasks {
		response.Tasks = append(response.Tasks, projectModelTask(task))
	}
	return err
}

func (s *ToolService) wait(ctx context.Context, ownerSessionID string, arguments toolArguments, response *toolResponse) error {
	if arguments.TaskID == "" {
		return fmt.Errorf("%w: taskId is required", ErrInvalidInput)
	}
	timeout := defaultWaitTimeout
	if arguments.TimeoutMS > 0 {
		timeout = time.Duration(arguments.TimeoutMS) * time.Millisecond
	}
	if timeout > maxWaitTimeout {
		return fmt.Errorf("%w: timeout exceeds %s", ErrInvalidInput, maxWaitTimeout)
	}
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	task, err := s.Supervisor.WaitTask(waitContext, TaskID(arguments.TaskID))
	if errors.Is(err, context.DeadlineExceeded) {
		task, err = s.Supervisor.Task(ctx, TaskID(arguments.TaskID))
		response.TimedOut = true
	}
	if err != nil {
		return err
	}
	if task.OwnerSessionID != ownerSessionID {
		return ErrNotFound
	}
	projectedTask := projectModelTask(task)
	response.Task = &projectedTask
	return nil
}

func (s *ToolService) cancel(ctx context.Context, ownerSessionID string, arguments toolArguments, response *toolResponse) error {
	if arguments.TaskID == "" {
		return fmt.Errorf("%w: taskId is required", ErrInvalidInput)
	}
	task, err := s.Supervisor.Task(ctx, TaskID(arguments.TaskID))
	if err != nil {
		return err
	}
	if task.OwnerSessionID != ownerSessionID {
		return ErrNotFound
	}
	task, err = s.Supervisor.Cancel(ctx, task.ID, task.CancellationGeneration, "canceled by parent")
	projectedTask := projectModelTask(task)
	response.Task = &projectedTask
	return err
}

func (s *ToolService) dismiss(ctx context.Context, ownerSessionID string, arguments toolArguments, response *toolResponse) error {
	conversation, err := s.resolveConversation(ctx, ownerSessionID, arguments.Agent, arguments.ConversationID)
	if err != nil {
		return err
	}
	_, err = s.Supervisor.Dismiss(ctx, conversation.ID, conversation.Generation, "dismissed by parent")
	if err == nil {
		response.Dismissed = true
	}
	return err
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
	return s.message(ctx, owner, toolArguments{Agent: agent, ConversationID: string(conversationID), Message: message})
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
	response := toolResponse{}
	return s.dismiss(ctx, owner, toolArguments{Agent: agent, ConversationID: string(conversationID)}, &response)
}

var _ ParentToolFactory = (*ToolService)(nil)
