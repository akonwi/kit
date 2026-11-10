package acp

import (
	"context"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
)

// summaryBoundaryKind is the context record a compaction leaves.
const summaryBoundaryKind = "summary"

// transcript returns a session's whole transcript, oldest first, starting
// from the newest page a snapshot carries.
func transcript(ctx context.Context, handle SessionHandle, snapshot protocol.SessionSnapshot) ([]protocol.TranscriptMessage, error) {
	pages := [][]protocol.TranscriptMessage{snapshot.Messages}
	cursor, more := snapshot.PreviousMessageCursor, snapshot.HasMoreMessages
	for more && cursor != "" {
		page, err := handle.TranscriptPage(ctx, cursor)
		if err != nil {
			return nil, projectBackendError(err)
		}
		pages = append(pages, page.Messages)
		cursor, more = page.PreviousMessageCursor, page.HasMoreMessages
	}
	var messages []protocol.TranscriptMessage
	for index := len(pages) - 1; index >= 0; index-- {
		messages = append(messages, pages[index]...)
	}
	return messages, nil
}

// reconcile translates transcript messages into the updates the client has
// not been sent, as a client would have received them live: each tool call
// is announced and then completed with its result. It replays a transcript
// when a session is loaded, and catches up a turn whose stream was lost.
func (t *translator) reconcile(messages []protocol.TranscriptMessage) []any {
	var updates []any
	for _, message := range messages {
		switch message.Role {
		case "user":
			key := message.TurnID
			if key == "" {
				key = message.ID
			}
			if t.userTurns[key] {
				continue
			}
			t.userTurns[key] = true
			for _, block := range message.Content {
				if text := userBlockText(block); text != "" {
					updates = append(updates, contentChunk{SessionUpdate: "user_message_chunk", Content: textBlock(text), MessageID: message.ID})
				}
			}
		case "assistant":
			var text, thinking strings.Builder
			for _, block := range message.Content {
				switch payload := block.Payload.(type) {
				case protocol.ThinkingContent:
					thinking.WriteString(payload.Text)
				case protocol.TextContent:
					text.WriteString(payload.Text)
				}
			}
			updates = append(updates, t.completeMessage(message.ID, text.String(), thinking.String())...)
			for _, block := range message.Content {
				if call, ok := block.Payload.(protocol.ToolCallContent); ok && !t.finished[call.ToolCallID] && t.tools[call.ToolCallID] == nil {
					updates = append(updates, t.toolPlanned(call.ToolCallID, call.ToolName, call.Arguments, call.ArgumentsTruncated))
				}
			}
		case "tool":
			if t.finished[message.ToolCallID] {
				continue
			}
			if tool := t.tool(message.ToolCallID, message.ToolName); !tool.announced {
				updates = append(updates, t.toolPlanned(message.ToolCallID, message.ToolName, "", true))
			}
			updates = append(updates, t.toolCompleted(protocol.ToolCompletedEvent{
				ToolCallID: message.ToolCallID, ToolName: message.ToolName,
				Content: protocol.ToolResultContent(message.Content), Details: message.Details, IsError: message.IsError,
			}))
		case "context":
			if t.plugins[message.ID] {
				continue
			}
			t.plugins[message.ID] = true
			switch message.BoundaryKind {
			case protocol.PluginMessageBoundaryKind:
				if text := message.TextContent(); text != "" {
					updates = append(updates, contentChunk{SessionUpdate: "user_message_chunk", Content: textBlock(text), MessageID: message.ID})
				}
			case summaryBoundaryKind:
				if t.compaction {
					update := compactionUpdate{SessionUpdate: "compaction_update", CompactionID: "replay_" + message.ID, Status: compactionStatusComplete}
					if text := message.TextContent(); text != "" {
						update.Summary = []contentBlock{textBlock(text)}
					}
					updates = append(updates, update)
				}
			}
		}
	}
	return updates
}

func userBlockText(block protocol.TranscriptContent) string {
	switch payload := block.Payload.(type) {
	case protocol.TextContent:
		return payload.Text
	case protocol.PromptCommandContent:
		return payload.InvocationText()
	case protocol.ImageContent:
		return "[image " + payload.Filename + "]"
	case protocol.FileContent:
		return "[file " + payload.Filename + "]"
	}
	return ""
}
