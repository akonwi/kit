package sessionbridge

import protocol "github.com/akonwi/kit/api/contract"

// Event is the client-owned flat projection of one canonical session event.
// It keeps Ard rendering code independent of protocol union representation.
type Event struct {
	StreamID         string
	Sequence         int64
	SessionID        string
	TurnID           string
	Kind             string
	MessageID        string
	ContentIndex     int
	Text             string
	Thinking         string
	Delta            string
	ToolCallID       string
	ToolName         string
	Content          []Content
	ContentTruncated bool
	DetailsOmitted   bool
	IsError          bool
	ContextTokens    int
	ContextWindow    int
	ErrorMessage     string
	Status           string
	Scratchpad       *protocol.Scratchpad
	Interaction      *protocol.InteractionRequest
	InteractionID    string
}

// Content is the client-owned flat projection of transcript content.
type Content struct {
	Kind         string
	Text         string
	ToolCallID   string
	ToolName     string
	Filename     string
	AttachmentID string
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
	IsError      bool
	StopReason   string
}

// Events projects canonical event unions for the Ard client.
func Events(source []protocol.SessionEvent) []Event {
	result := make([]Event, 0, len(source))
	for _, event := range source {
		projected := Event{StreamID: event.StreamID, Sequence: event.Sequence, SessionID: event.SessionID, TurnID: event.TurnID, Kind: string(event.Kind())}
		switch payload := event.Payload.(type) {
		case protocol.UserMessageAddedEvent:
			projected.Text = payload.Text
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
		case protocol.ToolStartedEvent:
			projected.ToolCallID, projected.ToolName = payload.ToolCallID, payload.ToolName
		case protocol.ToolOutputDeltaEvent:
			projected.ToolCallID, projected.ToolName, projected.Content, projected.IsError = payload.ToolCallID, payload.ToolName, content(payload.Content), payload.IsError
		case protocol.ToolCompletedEvent:
			projected.ToolCallID, projected.ToolName, projected.Content, projected.IsError = payload.ToolCallID, payload.ToolName, content(payload.Content), payload.IsError
			projected.ContentTruncated, projected.DetailsOmitted = payload.ContentTruncated, payload.DetailsOmitted
		case protocol.ContextChangedEvent:
			projected.ContextTokens, projected.ContextWindow = payload.ContextTokens, payload.ContextWindow
		case protocol.ScratchpadChangedEvent:
			projected.Scratchpad = payload.Scratchpad
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
		result = append(result, Message{
			ID: message.ID, TurnID: message.TurnID, Sequence: int(message.Sequence), Role: message.Role, Content: content(message.Content),
			ErrorMessage: message.ErrorMessage, ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			IsError: message.IsError, StopReason: message.StopReason,
		})
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
		case protocol.ImageContent:
			projected.Filename, projected.AttachmentID = payload.Filename, payload.AttachmentID
		case protocol.FileContent:
			projected.Filename, projected.AttachmentID = payload.Filename, payload.AttachmentID
		}
		result = append(result, projected)
	}
	return result
}
