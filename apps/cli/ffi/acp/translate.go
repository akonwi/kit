package acp

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/apps/cli/ffi/sessionbridge"
	"github.com/akonwi/kit/apps/cli/ffi/toolpresentation"
)

// outputInterval is how often a running tool's output is sent at most.
const outputInterval = 100 * time.Millisecond

// translator turns one session's Kit events into ACP session updates. It is
// not safe for concurrent use; its session serializes access.
type translator struct {
	sessionID string
	cwd       string
	// compaction and notices record the client's capabilities.
	compaction bool
	notices    bool
	// servers names the session's MCP servers, for tool titles.
	servers []string

	// userTurns holds the turns whose user message the client has: those it
	// was sent, and those this connection prompted.
	userTurns map[string]bool
	// messages holds the assistant messages being sent; completed holds
	// those sent in full.
	messages  map[string]*messageState
	completed map[string]bool
	// tools holds the tool calls announced and not finished; finished holds
	// those the client has been sent a final status for.
	tools         map[string]*toolState
	finished      map[string]bool
	plugins       map[string]bool
	contextTokens int
	contextWindow int
	cost          *float64
}

type messageState struct {
	text     strings.Builder
	thinking strings.Builder
}

type toolState struct {
	// announced reports that the client has been sent the tool call.
	announced          bool
	name               string
	arguments          string
	argumentsTruncated bool
	output             strings.Builder
	// unsent reports output not yet sent to the client.
	unsent   bool
	lastSent time.Time
	timer    *time.Timer
	done     bool
}

func newTranslator(sessionID string) *translator {
	return &translator{
		sessionID: sessionID, userTurns: map[string]bool{},
		messages: map[string]*messageState{}, completed: map[string]bool{},
		tools: map[string]*toolState{}, finished: map[string]bool{}, plugins: map[string]bool{},
	}
}

// translation is what one event produces: updates to send, and a running
// tool whose output should be sent after a delay.
type translation struct {
	updates  []any
	flushIn  time.Duration
	flushFor string
}

func (t *translator) event(event protocol.SessionEvent, now time.Time) translation {
	switch payload := event.Payload.(type) {
	case protocol.UserMessageAddedEvent:
		if t.userTurns[event.TurnID] {
			return translation{}
		}
		t.userTurns[event.TurnID] = true
		return t.updates(contentChunk{SessionUpdate: "user_message_chunk", Content: textBlock(payload.Text)})
	case protocol.PluginMessageAddedEvent:
		if t.userTurns[event.TurnID] {
			return translation{}
		}
		t.userTurns[event.TurnID] = true
		return t.updates(contentChunk{SessionUpdate: "user_message_chunk", Content: textBlock(payload.Text)})
	case protocol.AssistantStartedEvent:
		if t.completed[payload.MessageID] {
			return translation{}
		}
		return t.updates(t.assistantText(payload.MessageID, payload.Text, payload.Thinking)...)
	case protocol.AssistantTextDeltaEvent:
		if t.completed[payload.MessageID] {
			return translation{}
		}
		message := t.message(payload.MessageID)
		message.text.WriteString(payload.Delta)
		return t.updates(contentChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(payload.Delta), MessageID: payload.MessageID})
	case protocol.ThinkingDeltaEvent:
		if t.completed[payload.MessageID] {
			return translation{}
		}
		t.message(payload.MessageID).thinking.WriteString(payload.Delta)
		return t.updates(contentChunk{SessionUpdate: "agent_thought_chunk", Content: textBlock(payload.Delta)})
	case protocol.AssistantCompletedEvent:
		return t.updates(t.completeMessage(payload.MessageID, payload.Text, payload.Thinking)...)
	case protocol.ToolPlannedEvent:
		if t.finished[payload.ToolCallID] || t.tools[payload.ToolCallID] != nil {
			return translation{}
		}
		return t.updates(t.toolPlanned(payload.ToolCallID, payload.ToolName, payload.Arguments, payload.ArgumentsTruncated))
	case protocol.ToolStartedEvent:
		if t.finished[payload.ToolCallID] {
			return translation{}
		}
		tool := t.tool(payload.ToolCallID, payload.ToolName)
		if !tool.announced {
			announced := t.toolPlanned(payload.ToolCallID, payload.ToolName, payload.Arguments, payload.ArgumentsTruncated)
			announced.Status = toolStatusInProgress
			return t.updates(announced)
		}
		update := toolCallUpdate{SessionUpdate: "tool_call_update", ToolCallID: payload.ToolCallID, Status: toolStatusInProgress}
		if payload.Arguments != tool.arguments && payload.Arguments != "" {
			tool.arguments, tool.argumentsTruncated = payload.Arguments, payload.ArgumentsTruncated
			update.Title = t.toolTitle(tool, toolpresentation.Result{})
			update.RawInput = rawJSON(tool.arguments)
			update.Locations = t.toolLocations(tool)
		}
		return t.updates(update)
	case protocol.ToolOutputDeltaEvent:
		if t.finished[payload.ToolCallID] {
			return translation{}
		}
		tool := t.tool(payload.ToolCallID, payload.ToolName)
		if !tool.announced {
			announced := t.toolPlanned(payload.ToolCallID, payload.ToolName, "", true)
			announced.Status = toolStatusInProgress
			tool.output.WriteString(resultText(payload.Content))
			announced.Content = []toolCallContent{textContent(t.presentOutput(tool.name, tool.output.String()))}
			tool.lastSent = now
			return t.updates(announced)
		}
		tool.output.WriteString(resultText(payload.Content))
		tool.unsent = true
		if wait := outputInterval - now.Sub(tool.lastSent); wait > 0 {
			return translation{flushIn: wait, flushFor: payload.ToolCallID}
		}
		update, _ := t.flushOutput(payload.ToolCallID, now)
		return t.updates(update)
	case protocol.ToolCompletedEvent:
		if t.finished[payload.ToolCallID] {
			return translation{}
		}
		var updates []any
		if tool := t.tool(payload.ToolCallID, payload.ToolName); !tool.announced {
			updates = append(updates, t.toolPlanned(payload.ToolCallID, payload.ToolName, "", true))
		}
		return t.updates(append(updates, t.toolCompleted(payload))...)
	case protocol.CompactionStartedEvent:
		if !t.compaction {
			return translation{}
		}
		return t.updates(compactionUpdate{SessionUpdate: "compaction_update", CompactionID: payload.CompactionID, Status: compactionStatusRunning})
	case protocol.CompactionCompletedEvent:
		if !t.compaction {
			return translation{}
		}
		update := compactionUpdate{SessionUpdate: "compaction_update", CompactionID: payload.CompactionID, Status: compactionStatusComplete}
		if payload.CompactionFailureReason != "" {
			update.Status, update.Error = compactionStatusFailed, compactionFailureText(payload.CompactionFailureReason)
		}
		return t.updates(update)
	case protocol.ProviderRetryScheduledEvent:
		if !t.notices || payload.ProviderRetry == nil {
			return translation{}
		}
		notice := noticeUpdate{
			SessionUpdate: "notice", Severity: "warning",
			Title: fmt.Sprintf("The model provider failed; retrying (attempt %d)", payload.ProviderRetry.Count),
		}
		if retryAt, err := time.Parse(time.RFC3339Nano, payload.ProviderRetry.RetryAt); err == nil {
			notice.Description = fmt.Sprintf("Kit retries in %s.", max(retryAt.Sub(now).Round(time.Second), time.Second))
		}
		return t.updates(notice)
	case protocol.ContextChangedEvent:
		t.contextTokens, t.contextWindow = payload.ContextTokens, payload.ContextWindow
		return t.updates(t.usage()...)
	case protocol.UsageChangedEvent:
		if payload.Usage != nil {
			total := payload.Usage.Cost.Total
			t.cost = &total
		}
		return t.updates(t.usage()...)
	case protocol.SessionNameChangedEvent:
		return t.updates(sessionInfoUpdate{SessionUpdate: "session_info_update", Title: payload.SessionName})
	case protocol.SessionCWDChangedEvent:
		if payload.Workspace != nil && payload.Workspace.CWD != "" {
			t.cwd = payload.Workspace.CWD
		}
	}
	return translation{}
}

func (t *translator) updates(updates ...any) translation {
	return translation{updates: updates}
}

func (t *translator) message(id string) *messageState {
	message := t.messages[id]
	if message == nil {
		message = &messageState{}
		t.messages[id] = message
	}
	return message
}

// completeMessage sends what a completed message adds to what was sent and
// records it as sent in full.
func (t *translator) completeMessage(id, text, thinking string) []any {
	if t.completed[id] {
		return nil
	}
	updates := t.assistantText(id, text, thinking)
	delete(t.messages, id)
	t.completed[id] = true
	return updates
}

// assistantText sends the part of a message's text and thinking that its
// deltas have not, as when a provider does not stream.
func (t *translator) assistantText(id, text, thinking string) []any {
	message := t.message(id)
	var updates []any
	if rest, ok := strings.CutPrefix(thinking, message.thinking.String()); ok && rest != "" {
		message.thinking.WriteString(rest)
		updates = append(updates, contentChunk{SessionUpdate: "agent_thought_chunk", Content: textBlock(rest)})
	}
	if rest, ok := strings.CutPrefix(text, message.text.String()); ok && rest != "" {
		message.text.WriteString(rest)
		updates = append(updates, contentChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(rest), MessageID: id})
	}
	return updates
}

func (t *translator) usage() []any {
	if t.contextWindow <= 0 {
		return nil
	}
	update := usageUpdate{SessionUpdate: "usage_update", Used: t.contextTokens, Size: t.contextWindow}
	if t.cost != nil {
		update.Cost = &usageCost{Amount: math.Round(*t.cost*1e6) / 1e6, Currency: "USD"}
	}
	return []any{update}
}

func (t *translator) tool(id, name string) *toolState {
	tool := t.tools[id]
	if tool == nil {
		tool = &toolState{name: name}
		t.tools[id] = tool
	}
	return tool
}

func (t *translator) toolPlanned(id, name, arguments string, truncated bool) toolCall {
	tool := t.tool(id, name)
	tool.announced = true
	if arguments != "" || !truncated {
		tool.arguments, tool.argumentsTruncated = arguments, truncated
	}
	return toolCall{
		SessionUpdate: "tool_call", ToolCallID: id, Title: t.toolTitle(tool, toolpresentation.Result{}),
		Name: name, Kind: toolKind(name), Status: toolStatusPending,
		Locations: t.toolLocations(tool), RawInput: rawJSON(tool.arguments),
	}
}

// flushOutput returns an update carrying a running tool's output so far.
func (t *translator) flushOutput(id string, now time.Time) (toolCallUpdate, bool) {
	tool := t.tools[id]
	if tool == nil || tool.done || !tool.unsent {
		return toolCallUpdate{}, false
	}
	tool.unsent, tool.lastSent = false, now
	return toolCallUpdate{
		SessionUpdate: "tool_call_update", ToolCallID: id,
		Content: []toolCallContent{textContent(t.presentOutput(tool.name, tool.output.String()))},
	}, true
}

func (t *translator) toolCompleted(payload protocol.ToolCompletedEvent) toolCallUpdate {
	tool := t.tool(payload.ToolCallID, payload.ToolName)
	tool.done = true
	if tool.timer != nil {
		tool.timer.Stop()
	}
	delete(t.tools, payload.ToolCallID)
	t.finished[payload.ToolCallID] = true
	text := resultText(payload.Content)
	status := toolStatusCompleted
	if payload.IsError {
		status = toolStatusFailed
	}
	update := toolCallUpdate{
		SessionUpdate: "tool_call_update", ToolCallID: payload.ToolCallID, Status: status,
		Title:     t.toolTitle(tool, toolpresentation.Result{Succeeded: !payload.IsError, Text: text, Details: string(payload.Details)}),
		RawOutput: rawJSON(string(payload.Details)),
	}
	if !payload.IsError {
		update.Content = t.toolDiffs(tool)
	}
	if text != "" {
		update.Content = append(update.Content, textContent(t.presentOutput(tool.name, text)))
	}
	if update.Content == nil {
		update.Content = []toolCallContent{}
	}
	return update
}

// endTurn stops sending output for tools the turn left running and treats
// its messages as complete.
func (t *translator) endTurn() {
	for id, tool := range t.tools {
		if tool.timer != nil {
			tool.timer.Stop()
		}
		tool.done = true
		delete(t.tools, id)
	}
	for id := range t.messages {
		t.completed[id] = true
	}
	t.messages = map[string]*messageState{}
}

func (t *translator) toolTitle(tool *toolState, result toolpresentation.Result) string {
	presentation := toolpresentation.Present(toolpresentation.Call{
		Name: tool.name, Arguments: tool.arguments, ArgumentsTruncated: tool.argumentsTruncated,
	}, result, t.servers)
	title := presentation.Title
	if title == "" {
		title = tool.name
	}
	if presentation.Summary != "" {
		title += ": " + presentation.Summary
	}
	return title
}

// toolKind is the ACP kind of a Kit tool.
func toolKind(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "read"
	case "write", "edit":
		return "edit"
	case "bash", "shell", "exec", "exec_command":
		return "execute"
	case "grep", "find", "glob", "ls":
		return "search"
	case "fetch", "web_fetch", "webfetch":
		return "fetch"
	}
	return "other"
}

func (t *translator) arguments(tool *toolState) map[string]any {
	var arguments map[string]any
	if tool.argumentsTruncated || json.Unmarshal([]byte(tool.arguments), &arguments) != nil {
		return nil
	}
	return arguments
}

func (t *translator) absolute(path string) string {
	if path == "" || filepath.IsAbs(path) || t.cwd == "" {
		return path
	}
	return filepath.Join(t.cwd, path)
}

func (t *translator) toolLocations(tool *toolState) []toolCallLocation {
	switch toolKind(tool.name) {
	case "read", "edit":
	default:
		return nil
	}
	arguments := t.arguments(tool)
	path, _ := arguments["path"].(string)
	if path == "" {
		return nil
	}
	location := toolCallLocation{Path: t.absolute(path)}
	if offset, ok := arguments["offset"].(float64); ok && offset >= 1 {
		line := int(offset)
		location.Line = &line
	}
	return []toolCallLocation{location}
}

// toolDiffs shows a completed edit or write as diffs.
func (t *translator) toolDiffs(tool *toolState) []toolCallContent {
	arguments := t.arguments(tool)
	path, _ := arguments["path"].(string)
	if path == "" {
		return nil
	}
	path = t.absolute(path)
	switch strings.ToLower(tool.name) {
	case "write":
		content, ok := arguments["content"].(string)
		if !ok {
			return nil
		}
		return []toolCallContent{{Type: "diff", Path: path, NewText: &content}}
	case "edit":
		edits, _ := arguments["edits"].([]any)
		if len(edits) == 0 {
			edits = []any{arguments}
		}
		var diffs []toolCallContent
		for _, raw := range edits {
			edit, _ := raw.(map[string]any)
			oldText, oldOK := edit["oldText"].(string)
			newText, newOK := edit["newText"].(string)
			if oldOK && newOK {
				diffs = append(diffs, toolCallContent{Type: "diff", Path: path, OldText: &oldText, NewText: &newText})
			}
		}
		return diffs
	}
	return nil
}

// presentOutput fences a command's output so the client shows it verbatim.
func (t *translator) presentOutput(name, text string) string {
	if toolKind(name) != "execute" {
		return text
	}
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "\n" + strings.TrimSuffix(text, "\n") + "\n" + fence
}

func textContent(text string) toolCallContent {
	block := textBlock(text)
	return toolCallContent{Type: "content", Content: &block}
}

// resultText joins a tool result's text and names what it cannot show.
func resultText(content protocol.ToolResultContent) string {
	var text strings.Builder
	for _, block := range content {
		switch payload := block.Payload.(type) {
		case protocol.TextContent:
			text.WriteString(payload.Text)
		case protocol.ImageContent:
			fmt.Fprintf(&text, "[image %s]", payload.Filename)
		case protocol.FileContent:
			fmt.Fprintf(&text, "[file %s]", payload.Filename)
		}
	}
	return text.String()
}

// rawJSON returns text as a raw JSON value, or nil when it is not JSON.
func rawJSON(text string) json.RawMessage {
	if text == "" || !json.Valid([]byte(text)) {
		return nil
	}
	return json.RawMessage(text)
}

func compactionFailureText(reason protocol.CompactionFailureReason) string {
	return sessionbridge.CompactionFailedTurnText(string(reason))
}
