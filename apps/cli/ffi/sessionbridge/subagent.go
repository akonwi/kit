package sessionbridge

import (
	"context"
	"strings"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// SubagentPage is one page of a subagent conversation's live events,
// projected as session events so the transcript's live model applies them as
// it does the attached session's. ResyncRequired reports that the events after
// the requested sequence are gone; the page is then empty.
type SubagentPage struct {
	StreamID       string
	LastSequence   int64
	ResyncRequired bool
	Events         []Event
}

// subagentKinds maps the subagent event vocabulary onto session event kinds.
var subagentKinds = map[string]string{
	"turn.started":           "turn.started",
	"turn.settled":           "turn.completed",
	"message.text.delta":     "assistant.text.delta",
	"message.thinking.delta": "assistant.thinking.delta",
	"message.completed":      "assistant.completed",
	"tool.planned":           "tool.planned",
	"tool.started":           "tool.started",
	"tool.output.delta":      "tool.output.delta",
	"tool.completed":         "tool.completed",
}

// SubagentEvents reads a conversation's live events after the given sequence
// of the given stream. An empty stream reads the retained events from the
// start.
func SubagentEvents(ctx context.Context, session *kit.Session, conversationID, streamID string, after int64) (SubagentPage, error) {
	page, err := session.SubagentEvents(ctx, conversationID, streamID, after)
	if err != nil {
		return SubagentPage{}, err
	}
	return subagentPage(page), nil
}

func subagentPage(page protocol.SubagentLiveEventPage) SubagentPage {
	result := SubagentPage{StreamID: page.StreamID, LastSequence: page.LastSequence, ResyncRequired: page.ResyncRequired}
	result.Events = make([]Event, 0, len(page.Events))
	for _, event := range page.Events {
		kind, known := subagentKinds[event.Kind]
		if !known {
			continue
		}
		projected := Event{
			StreamID: page.StreamID, Sequence: event.Sequence, TurnID: event.TurnID, Kind: kind,
			MessageID: event.MessageID, ContentIndex: event.ContentIndex, Delta: event.Delta,
			ToolCallID: event.ToolCallID, ToolName: event.ToolName, IsError: event.IsError,
		}
		if event.Kind == "tool.output.delta" || event.Kind == "tool.completed" {
			projected.Content = []Content{{Kind: "text", Text: event.Text}}
		}
		result.Events = append(result.Events, projected)
	}
	return result
}

// ConfigureSubagent sets a conversation's model or thinking level, leaving
// an empty one unchanged, and returns the updated conversation.
func ConfigureSubagent(ctx context.Context, session *kit.Session, conversationID string, generation uint64, model, thinking string) (protocol.SubagentConversation, error) {
	input := protocol.ConfigureSubagentInput{Generation: generation}
	if model = strings.TrimSpace(model); model != "" {
		input.Model = &model
	}
	if thinking = strings.TrimSpace(thinking); thinking != "" {
		level := protocol.ThinkingLevel(thinking)
		input.ThinkingLevel = &level
	}
	result, err := session.ConfigureSubagent(ctx, conversationID, input)
	if err != nil {
		return protocol.SubagentConversation{}, err
	}
	return result.Conversation, nil
}
