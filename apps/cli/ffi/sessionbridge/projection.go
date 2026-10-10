package sessionbridge

import protocol "github.com/akonwi/kit/api/contract"

// Event is the client-owned flat projection of one canonical session event.
// It keeps Ard rendering code independent of protocol union representation.
type Event struct {
	StreamID     string
	Sequence     int64
	SessionID    string
	TurnID       string
	Kind         string
	MessageID    string
	ContentIndex int
	Text         string
	Thinking     string
	Delta        string
	ToolCallID   string
	ToolName     string
	// Arguments is a planned or started call's JSON arguments, unless
	// ArgumentsTruncated reports that the server omitted them.
	Arguments          string
	ArgumentsTruncated bool
	Content            []Content
	ContentTruncated   bool
	// Details is a completed call's raw JSON details.
	Details        string
	DetailsOmitted bool
	IsError        bool
	ContextTokens  int
	ContextWindow  int
	ErrorMessage   string
	Status         string
	Scratchpad     *protocol.Scratchpad
	// CWD is the session's new working directory after session.cwd.changed.
	CWD           string
	Interaction   *protocol.InteractionRequest
	InteractionID string
	// PluginID identifies the plugin that submitted a plugin message.
	PluginID string
}

// Content is the client-owned flat projection of transcript content.
type Content struct {
	Kind       string
	Text       string
	ToolCallID string
	ToolName   string
	// Arguments is a tool call's JSON arguments, unless ArgumentsTruncated
	// reports that the server omitted them.
	Arguments          string
	ArgumentsTruncated bool
	Filename           string
	AttachmentID       string
}

// Message is the client-owned projection of one persisted transcript message.
type Message struct {
	ID     string
	TurnID string
	// Sequence orders persisted messages within the session; it is zero for
	// messages that have not been persisted.
	Sequence     int
	Role         string
	Content      []Content
	ErrorMessage string
	ToolCallID   string
	ToolName     string
	// Details is a tool result's raw JSON details.
	Details    string
	IsError    bool
	StopReason string
	// Bash is set for a persisted bash run, whose Role is then "bash".
	Bash *Bash
	// PluginID identifies the plugin that submitted a message whose Role is
	// "plugin".
	PluginID string
}

// Events projects canonical event unions for the Ard client.
func Events(source []protocol.SessionEvent) []Event {
	result := make([]Event, 0, len(source))
	for _, event := range source {
		projected := Event{StreamID: event.StreamID, Sequence: event.Sequence, SessionID: event.SessionID, TurnID: event.TurnID, Kind: string(event.Kind())}
		switch payload := event.Payload.(type) {
		case protocol.UserMessageAddedEvent:
			projected.Text = payload.Text
		case protocol.PluginMessageAddedEvent:
			projected.Text, projected.PluginID = payload.Text, payload.PluginID
		case protocol.AssistantStartedEvent:
			projected.MessageID, projected.Text, projected.Thinking = payload.MessageID, payload.Text, payload.Thinking
		case protocol.AssistantTextDeltaEvent:
			projected.MessageID, projected.ContentIndex, projected.Delta = payload.MessageID, payload.ContentIndex, payload.Delta
		case protocol.ThinkingDeltaEvent:
			projected.MessageID, projected.ContentIndex, projected.Delta = payload.MessageID, payload.ContentIndex, payload.Delta
		case protocol.AssistantCompletedEvent:
			projected.MessageID, projected.Text, projected.Thinking = payload.MessageID, payload.Text, payload.Thinking
		case protocol.ToolPlannedEvent:
			projected.MessageID, projected.ContentIndex, projected.ToolCallID, projected.ToolName = payload.MessageID, payload.ContentIndex, payload.ToolCallID, payload.ToolName
			projected.Arguments, projected.ArgumentsTruncated = payload.Arguments, payload.ArgumentsTruncated
		case protocol.ToolStartedEvent:
			projected.ToolCallID, projected.ToolName = payload.ToolCallID, payload.ToolName
			projected.Arguments, projected.ArgumentsTruncated = payload.Arguments, payload.ArgumentsTruncated
		case protocol.ToolOutputDeltaEvent:
			projected.ToolCallID, projected.ToolName, projected.Content, projected.IsError = payload.ToolCallID, payload.ToolName, content(payload.Content), payload.IsError
		case protocol.ToolCompletedEvent:
			projected.ToolCallID, projected.ToolName, projected.Content, projected.IsError = payload.ToolCallID, payload.ToolName, content(payload.Content), payload.IsError
			projected.ContentTruncated, projected.DetailsOmitted = payload.ContentTruncated, payload.DetailsOmitted
			projected.Details = string(payload.Details)
		case protocol.ContextChangedEvent:
			projected.ContextTokens, projected.ContextWindow = payload.ContextTokens, payload.ContextWindow
		case protocol.ScratchpadChangedEvent:
			projected.Scratchpad = payload.Scratchpad
		case protocol.SessionCWDChangedEvent:
			if payload.Workspace != nil {
				projected.CWD = payload.Workspace.CWD
			}
		case protocol.InteractionRequestedEvent:
			projected.Interaction = payload.Interaction
			if payload.Interaction != nil {
				projected.InteractionID = payload.Interaction.ID
			}
		case protocol.InteractionResolvedEvent:
			projected.InteractionID = payload.InteractionID
		case protocol.TurnCompletedEvent:
			projected.Status, projected.ErrorMessage = string(payload.Status), payload.ErrorMessage
		}
		result = append(result, projected)
	}
	return result
}

// Messages projects canonical persisted transcript content for the Ard client.
func Messages(source []protocol.TranscriptMessage) []Message {
	result := make([]Message, 0, len(source))
	for _, message := range source {
		projected := Message{
			ID: message.ID, TurnID: message.TurnID, Sequence: int(message.Sequence), Role: message.Role, Content: content(message.Content),
			ErrorMessage: message.ErrorMessage, ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			IsError: message.IsError, StopReason: message.StopReason,
		}
		if message.Role == "tool" {
			projected.Details = string(message.Details)
		}
		if message.Role == "context" {
			if bash, ok := bashBoundary(message.BoundaryID, message.BoundaryKind, message.Content, message.Details); ok {
				projected.Role, projected.Bash = "bash", &bash
			} else if message.BoundaryKind == protocol.PluginMessageBoundaryKind {
				projected.Role, projected.PluginID = "plugin", message.BoundarySource
			}
		}
		result = append(result, projected)
	}
	return result
}

func content(source []protocol.TranscriptContent) []Content {
	result := make([]Content, 0, len(source))
	for _, block := range source {
		projected := Content{Kind: string(block.Kind())}
		switch payload := block.Payload.(type) {
		case protocol.TextContent:
			projected.Text = payload.Text
		case protocol.ThinkingContent:
			projected.Text = payload.Text
		case protocol.ToolCallContent:
			projected.ToolCallID, projected.ToolName = payload.ToolCallID, payload.ToolName
			projected.Arguments, projected.ArgumentsTruncated = payload.Arguments, payload.ArgumentsTruncated
		case protocol.ImageContent:
			projected.Filename, projected.AttachmentID = payload.Filename, payload.AttachmentID
		case protocol.FileContent:
			projected.Filename, projected.AttachmentID = payload.Filename, payload.AttachmentID
		case protocol.PromptCommandContent:
			// A prompt command's message reads as the invocation the user typed.
			projected.Text = payload.InvocationText()
		}
		result = append(result, projected)
	}
	return result
}
