package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/githubpr"
	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/vcs"
)

func pluginHostFactory(paths apphome.Paths, cache *githubpr.Cache, logger *slog.Logger) session.PluginHostFactory {
	return func(ctx context.Context, input session.PluginSession, changed func()) session.PluginHost {
		var name *string
		if input.Name != "" {
			value := input.Name
			name = &value
		}
		host := &sessionPluginHost{vcs: vcs.NewObserver(ctx, input.CWD, cache), logger: logger}
		host.Host = plugin.NewHost(ctx, plugin.HostConfig{Home: paths, Session: plugin.SessionContext{ID: input.ID, Name: name}, CWD: input.CWD, ReservedIDs: protocol.ReservedCommandDomains(), ReservedSubagents: input.SubagentNames, Changed: changed, Project: host.projectContext, ProjectChanged: host.vcs.Updates(), Toast: host.publishToast, Failure: host.reportFailure, Interaction: host.requestInteraction})
		return host
	}
}

// sessionPluginHost projects private plugin ownership into a session-owned port.
type sessionPluginHost struct {
	vcs                 *vcs.Observer
	logger              *slog.Logger
	interactionMu       sync.RWMutex
	interactionObserver func(context.Context, session.PluginInteractionInput, func() bool) (session.PluginInteractionResult, error)
	*plugin.Host
	toastMu       sync.RWMutex
	toastObserver func(context.Context, session.PluginToast) error
}

func pluginCommandInstance(owner plugin.InstanceID) string {
	return owner.HostID + ":" + strconv.FormatUint(owner.Generation, 10)
}

func pluginCommandSelection(command plugin.Command) string {
	return pluginCommandInstance(command.Owner) + ":" + strconv.FormatUint(command.Registration, 10)
}

func (h *sessionPluginHost) Commands() []session.PluginCommand {
	commands := h.Host.Commands()
	result := make([]session.PluginCommand, 0, len(commands))
	for _, command := range commands {
		result = append(result, session.PluginCommand{ID: command.ID, LocalID: command.LocalID, PluginID: command.Owner.PluginID, Instance: pluginCommandSelection(command), Description: command.Description, ArgName: command.ArgName, Category: command.Category})
	}
	return result
}

func (h *sessionPluginHost) ExecuteCommand(ctx context.Context, instance, id, args string) error {
	for _, command := range h.Host.Commands() {
		if command.ID != id || pluginCommandSelection(command) != instance {
			continue
		}
		err := h.Host.ExecuteCommand(ctx, command, args)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, plugin.ErrCommandUnavailable) {
			return session.ErrPluginCommandUnavailable
		}
		return errors.Join(session.ErrPluginCommandFailed, err)
	}
	return session.ErrPluginCommandUnavailable
}

func (h *sessionPluginHost) SetToastObserver(observer func(context.Context, session.PluginToast) error) {
	h.toastMu.Lock()
	defer h.toastMu.Unlock()
	h.toastObserver = observer
}
func (h *sessionPluginHost) publishToast(ctx context.Context, toast plugin.Toast) error {
	h.toastMu.RLock()
	observer := h.toastObserver
	h.toastMu.RUnlock()
	if observer == nil {
		return nil
	}
	return observer(ctx, session.PluginToast{PluginID: toast.Owner.PluginID, Instance: pluginCommandInstance(toast.Owner), Title: toast.Title, Subtitle: toast.Subtitle, Variant: toast.Variant, Persistent: toast.Persistent})
}

func (h *sessionPluginHost) reportFailure(failure plugin.FailureEvent) {
	attributes := []any{
		"session_id", failure.Owner.SessionID,
		"plugin_id", failure.Owner.PluginID,
		"instance", pluginCommandInstance(failure.Owner),
		"generation", failure.Owner.Generation,
		"phase", failure.Phase,
		"error", failure.Message,
	}
	if failure.Stderr != "" {
		attributes = append(attributes, "stderr", failure.Stderr)
	}
	if h.logger != nil {
		h.logger.Error("plugin failed", attributes...)
	}
	_ = h.publishToast(context.Background(), plugin.Toast{
		Owner:      failure.Owner,
		Title:      "Plugin failed",
		Subtitle:   pluginFailureSubtitle(failure.Message),
		Variant:    "error",
		Persistent: true,
	})
}

func pluginFailureSubtitle(message string) string {
	const suffix = " See the server log for details."
	for len(message) > 4096-len(suffix) {
		_, size := utf8.DecodeLastRuneInString(message)
		message = message[:len(message)-size]
	}
	return message + suffix
}

func (h *sessionPluginHost) SetInteractionObserver(observer func(context.Context, session.PluginInteractionInput, func() bool) (session.PluginInteractionResult, error)) {
	h.interactionMu.Lock()
	defer h.interactionMu.Unlock()
	h.interactionObserver = observer
}
func (h *sessionPluginHost) requestInteraction(ctx context.Context, input plugin.InteractionRequest) (plugin.InteractionResponse, error) {
	h.interactionMu.RLock()
	observer := h.interactionObserver
	h.interactionMu.RUnlock()
	if observer == nil {
		return plugin.InteractionResponse{}, plugin.ErrInteractivityUnavailable
	}
	request := session.PluginInteractionInput{Owner: session.PluginInteractionOwner{PluginID: input.Owner.PluginID, Instance: pluginCommandInstance(input.Owner)}, Kind: session.InteractionKind(input.Kind), Title: input.Title, Detail: input.Message, ConfirmLabel: input.ConfirmLabel, CancelLabel: input.CancelLabel, DefaultValue: input.DefaultValue, Placeholder: input.Placeholder, InitialValue: input.InitialValue, Filterable: input.Filterable}
	for _, option := range input.Options {
		request.Options = append(request.Options, session.PluginInteractionOption{Label: option.Label, Detail: option.Description})
	}
	result, err := observer(ctx, request, func() bool { return h.Host.InteractionOwnerActive(input.Owner) })
	if err != nil {
		code := int64(-32603)
		message := "Plugin interaction failed"
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return plugin.InteractionResponse{}, err
		case errors.Is(err, session.ErrInteractionCapacity):
			code = -32005
			message = "Session interaction limit exceeded"
		case errors.Is(err, session.ErrClosed):
			code = -32002
			message = "Plugin interaction owner is unavailable"
		case errors.Is(err, session.ErrInvalidInput):
			code = -32602
			message = "Invalid plugin interaction"
		}
		return plugin.InteractionResponse{}, &plugin.RPCError{Code: code, Message: message}
	}
	return plugin.InteractionResponse{Cancelled: result.Cancelled, Confirmed: result.Confirmed, Text: result.Text, OptionIndex: result.OptionIndex}, nil
}

func (h *sessionPluginHost) Footer() session.PluginFooter {
	source := h.Host.Footer()
	result := session.PluginFooter{LocationHidden: source.LocationHidden}
	for _, item := range source.Items {
		target := session.PluginFooterItem{ID: item.ID, PluginID: item.Owner.PluginID, Instance: pluginCommandInstance(item.Owner)}
		for _, segment := range item.Content {
			target.Content = append(target.Content, session.PluginFooterSegment{Text: segment.Text, Style: session.PluginFooterStyle(segment.Style)})
		}
		result.Items = append(result.Items, target)
	}
	return result
}

func (h *sessionPluginHost) Subagents() []session.PluginSubagent {
	definitions := h.Host.Subagents()
	result := make([]session.PluginSubagent, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, session.PluginSubagent{
			ID: definition.ID, PluginID: definition.Owner.PluginID, Instance: pluginCommandInstance(definition.Owner), Registration: definition.Registration,
			Description: definition.Description, Instructions: definition.Instructions, Model: definition.Model, SourcePath: definition.SourcePath,
		})
	}
	return result
}

func pluginToolInstance(tool plugin.Tool) string {
	return pluginCommandInstance(tool.Owner) + ":" + strconv.FormatUint(tool.Registration, 10)
}

func (h *sessionPluginHost) Tools() []session.PluginTool {
	var result []session.PluginTool
	for _, tool := range h.Host.Tools() {
		result = append(result, session.PluginTool{ID: tool.ID, Instance: pluginToolInstance(tool), ModelName: tool.ModelName, Description: tool.Description, ExecutionMode: tool.ExecutionMode, InputSchema: tool.InputSchema, PromptSnippet: tool.PromptSnippet, PromptGuidelines: tool.PromptGuidelines})
	}
	return result
}

func (h *sessionPluginHost) ExecuteTool(ctx context.Context, selection session.PluginTool, callID string, input json.RawMessage) (session.PluginToolResult, error) {
	for _, tool := range h.Host.Tools() {
		if tool.ID != selection.ID || pluginToolInstance(tool) != selection.Instance {
			continue
		}
		source, err := h.Host.ExecuteTool(ctx, tool, callID, input)
		if err != nil {
			return session.PluginToolResult{}, err
		}
		result := session.PluginToolResult{Details: source.Details, Terminate: source.Terminate}
		for _, part := range source.Content {
			result.Content = append(result.Content, session.PluginToolContent{Type: part.Type, Text: part.Text, Data: part.Data, MIMEType: part.MIMEType})
		}
		return result, nil
	}
	return session.PluginToolResult{}, plugin.ErrToolUnavailable
}

func (h *sessionPluginHost) InterceptTool(ctx context.Context, identity, callID, name string, input json.RawMessage) (session.PluginInterceptionDecision, error) {
	decision, err := h.Host.InterceptTool(ctx, identity, callID, name, input)
	return session.PluginInterceptionDecision{Reject: decision.Reject, Message: decision.Message}, err
}

func (h *sessionPluginHost) TurnStarted(turnID string) bool { return h.Host.TurnStarted(turnID) }

func (h *sessionPluginHost) TurnCompleted(turn session.PluginTurn) {
	projected := plugin.PublicTurn{ID: turn.ID, Messages: make([]plugin.PublicMessage, 0, len(turn.Messages))}
	for _, message := range turn.Messages {
		item := plugin.PublicMessage{Role: message.Role, Content: make([]plugin.PublicTextContent, 0, len(message.Content))}
		for _, text := range message.Content {
			item.Content = append(item.Content, plugin.PublicTextContent{Type: "text", Text: text})
		}
		projected.Messages = append(projected.Messages, item)
	}
	h.Host.TurnCompleted(projected, turn.OmitReason)
}
