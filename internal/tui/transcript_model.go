package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
)

type transcriptItemKind string

const (
	transcriptItemUser      transcriptItemKind = "user"
	transcriptItemAssistant transcriptItemKind = "assistant"
	transcriptItemBash      transcriptItemKind = "bash"
)

type transcriptToolCall struct {
	ID                 string
	Name               string
	Arguments          json.RawMessage
	ArgumentsTruncated bool
}

type turnTranscriptItem struct {
	Kind        transcriptItemKind
	ID          string
	TurnID      string
	Message     protocol.TranscriptMessage
	ToolResults map[string]protocol.TranscriptMessage
	Aborted     bool
	Pending     bool
}

type transcriptDisplayKind string

const (
	transcriptDisplaySingle         transcriptDisplayKind = "single"
	transcriptDisplayAssistantProse transcriptDisplayKind = "assistant-prose"
	transcriptDisplayTurnWork       transcriptDisplayKind = "turn-work"
)

type transcriptDisplayItem struct {
	Kind   transcriptDisplayKind
	ID     string
	TurnID string
	Item   *turnTranscriptItem
	Items  []turnTranscriptItem
}

func buildTurnTranscriptItems(messages []protocol.TranscriptMessage) []turnTranscriptItem {
	resultsByTurn := make(map[string]map[string]protocol.TranscriptMessage)
	abortedTurns := make(map[string]bool)
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallID != "" {
			results := resultsByTurn[message.TurnID]
			if results == nil {
				results = make(map[string]protocol.TranscriptMessage)
				resultsByTurn[message.TurnID] = results
			}
			results[message.ToolCallID] = message
		}
		if message.Role == "assistant" && message.StopReason == "aborted" {
			abortedTurns[message.TurnID] = true
		}
	}

	items := make([]turnTranscriptItem, 0, len(messages))
	for _, message := range messages {
		var kind transcriptItemKind
		switch message.Role {
		case "user":
			kind = transcriptItemUser
		case "assistant":
			kind = transcriptItemAssistant
		case "bash":
			kind = transcriptItemBash
		default:
			continue
		}
		items = append(items, turnTranscriptItem{
			Kind: kind, ID: message.ID, TurnID: message.TurnID, Message: message,
			ToolResults: resultsByTurn[message.TurnID], Aborted: abortedTurns[message.TurnID],
		})
	}
	return items
}

func assistantToolCalls(message protocol.TranscriptMessage) []transcriptToolCall {
	calls := make([]transcriptToolCall, 0)
	for _, block := range message.Content {
		if block.Kind != protocol.TranscriptContentToolCall {
			continue
		}
		calls = append(calls, transcriptToolCall{
			ID: block.ToolCallID, Name: block.ToolName,
			Arguments:          append(json.RawMessage(nil), block.Arguments...),
			ArgumentsTruncated: block.ArgumentsTruncated,
		})
	}
	return calls
}

func assistantProse(message protocol.TranscriptMessage) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Kind == protocol.TranscriptContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	if (message.StopReason == "error" || message.StopReason == "aborted") && message.ErrorMessage != "" {
		return message.ErrorMessage
	}
	return ""
}

func assistantThinking(message protocol.TranscriptMessage) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Kind == protocol.TranscriptContentThinking && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func assistantHasProse(item turnTranscriptItem) bool {
	return strings.TrimSpace(assistantProse(item.Message)) != ""
}

func groupTranscriptDisplayItems(items []turnTranscriptItem) []transcriptDisplayItem {
	result := make([]transcriptDisplayItem, 0, len(items))
	appendProse := func(item turnTranscriptItem) {
		if item.Pending || !assistantHasProse(item) {
			return
		}
		copy := item
		result = append(result, transcriptDisplayItem{
			Kind: transcriptDisplayAssistantProse, ID: "assistant-prose:" + item.ID,
			TurnID: item.TurnID, Item: &copy,
		})
	}
	for index := 0; index < len(items); {
		item := items[index]
		if item.Kind == transcriptItemUser || item.Kind == transcriptItemBash {
			copy := item
			result = append(result, transcriptDisplayItem{
				Kind: transcriptDisplaySingle, ID: "single:" + item.ID,
				TurnID: item.TurnID, Item: &copy,
			})
			index++
			continue
		}
		calls := assistantToolCalls(item.Message)
		if len(calls) == 0 {
			appendProse(item)
			index++
			continue
		}

		// Completed prose remains in the transcript immediately before the tool
		// batch it introduced. A later assistant message with prose starts a new
		// batch instead of being folded into earlier work from the same turn.
		appendProse(item)
		work := []turnTranscriptItem{item}
		next := index + 1
		for next < len(items) && items[next].TurnID == item.TurnID && items[next].Kind == transcriptItemAssistant {
			candidate := items[next]
			if len(assistantToolCalls(candidate.Message)) == 0 || assistantHasProse(candidate) {
				break
			}
			work = append(work, candidate)
			next++
		}
		result = append(result, transcriptDisplayItem{
			Kind: transcriptDisplayTurnWork, ID: "turn-work:" + item.TurnID + ":" + item.ID,
			TurnID: item.TurnID, Items: work,
		})
		index = next
	}
	return result
}

func displayItemToolCalls(item transcriptDisplayItem) []transcriptToolCall {
	var calls []transcriptToolCall
	for _, entry := range item.Items {
		calls = append(calls, assistantToolCalls(entry.Message)...)
	}
	return calls
}

type activitySection struct {
	ID       string
	TurnID   string
	Thinking string
	Prose    string
	Calls    []transcriptToolCall
	Aborted  bool
}

func buildActivitySections(source transcriptDisplayItem) []activitySection {
	sections := make([]activitySection, 0, len(source.Items))
	for _, item := range source.Items {
		if item.Kind != transcriptItemAssistant {
			continue
		}
		calls := assistantToolCalls(item.Message)
		thinking := assistantThinking(item.Message)
		sections = append(sections, activitySection{
			ID:       "activity-section:" + item.TurnID + ":" + item.ID,
			TurnID:   item.TurnID,
			Thinking: thinking,
			Calls:    calls, Aborted: item.Aborted,
		})
	}
	return sections
}

type activityListItemKind int

const (
	activityListThinking activityListItemKind = iota
	activityListProse
	activityListTool
)

type activityListItem struct {
	ID      string
	Kind    activityListItemKind
	Section activitySection
	Call    transcriptToolCall
	Key     activityToolKey
}

func buildActivityListItems(source transcriptDisplayItem) []activityListItem {
	sections := buildActivitySections(source)
	items := make([]activityListItem, 0)
	for _, section := range sections {
		if strings.TrimSpace(section.Thinking) != "" {
			items = append(items, activityListItem{ID: section.ID + ":thinking", Kind: activityListThinking, Section: section})
		}
		hasProse := strings.TrimSpace(section.Prose) != ""
		if hasProse {
			items = append(items, activityListItem{ID: section.ID + ":prose", Kind: activityListProse, Section: section})
		}
		for _, call := range section.Calls {
			key := activityToolKey{TurnID: section.TurnID, ToolCallID: call.ID}
			items = append(items, activityListItem{
				ID:   "activity-tool:" + section.TurnID + ":" + call.ID,
				Kind: activityListTool, Section: section, Call: call, Key: key,
			})
		}
	}
	return items
}

func activityToolListIndex(source transcriptDisplayItem, key activityToolKey) int {
	for index, item := range buildActivityListItems(source) {
		if item.Kind == activityListTool && item.Key == key {
			return index
		}
	}
	return -1
}

type activityToolState int

const (
	activityToolPending activityToolState = iota
	activityToolRunning
	activityToolSucceeded
	activityToolFailed
	activityToolAborted
)

func resolveActivityToolState(state transcriptMessage, exists, sourceAborted bool) activityToolState {
	if exists && !state.Pending && state.ToolStatus != "Not run" {
		if state.IsError {
			return activityToolFailed
		}
		return activityToolSucceeded
	}
	if sourceAborted || exists && (state.Aborted || state.ToolStatus == "Not run") {
		return activityToolAborted
	}
	if !exists || state.ToolStatus == "Planned" {
		return activityToolPending
	}
	return activityToolRunning
}

type activityToolKey struct {
	TurnID     string
	ToolCallID string
}

func activityToolKeys(presentation transcriptPresentation, sourceID string) []activityToolKey {
	source, ok := transcriptActivitySource(presentation.Items, sourceID)
	if !ok {
		return nil
	}
	calls := displayItemToolCalls(source)
	keys := make([]activityToolKey, 0, len(calls))
	for _, call := range calls {
		keys = append(keys, activityToolKey{TurnID: source.TurnID, ToolCallID: call.ID})
	}
	return keys
}

func moveActivityToolCursor(keys []activityToolKey, current activityToolKey, delta int) activityToolKey {
	if len(keys) == 0 {
		return activityToolKey{}
	}
	index := 0
	for candidateIndex, key := range keys {
		if key == current {
			index = candidateIndex
			break
		}
	}
	index = max(0, min(len(keys)-1, index+delta))
	return keys[index]
}

type transcriptToolStateKey struct {
	TurnID     string
	ToolCallID string
}

type transcriptPresentation struct {
	Items      []transcriptDisplayItem
	ToolStates map[transcriptToolStateKey]transcriptMessage
}

func presentTranscript(messages []transcriptMessage) transcriptPresentation {
	structured := make([]protocol.TranscriptMessage, 0, len(messages))
	toolStates := make(map[transcriptToolStateKey]transcriptMessage)
	pendingMessages := make(map[string]bool)
	currentTurnID := ""
	for index, message := range messages {
		turnID := message.TurnID
		if message.Role == "bash" {
			turnID = "bash:" + message.ID
		} else if turnID == "" {
			if message.Role == "user" || currentTurnID == "" {
				turnID = "local-turn:" + strconv.Itoa(index)
			} else {
				turnID = currentTurnID
			}
		}
		if message.Role != "bash" {
			currentTurnID = turnID
		}
		messageID := message.ID
		if messageID == "" {
			messageID = "local-message:" + strconv.Itoa(index)
		}
		role := message.Role
		content := make([]protocol.TranscriptContent, 0, 2+len(message.ToolCalls))
		// Pending assistant prose is retained for an inline work window once a
		// tool call arrives. A prose-only pending message is still omitted below.
		if message.Text != "" {
			content = append(content, protocol.TranscriptContent{Kind: protocol.TranscriptContentText, Text: message.Text})
		}
		if message.Thinking != "" {
			content = append(content, protocol.TranscriptContent{Kind: protocol.TranscriptContentThinking, Text: message.Thinking})
		}
		for _, call := range message.ToolCalls {
			content = append(content, protocol.TranscriptContent{
				Kind: protocol.TranscriptContentToolCall, ToolCallID: call.ID, ToolName: call.Name,
				Arguments: string(call.Arguments), ArgumentsTruncated: call.ArgumentsTruncated,
			})
		}
		if message.Role == "assistant" && message.Pending && len(message.ToolCalls) == 0 {
			continue
		}
		pendingMessages[messageID] = message.Pending
		projected := protocol.TranscriptMessage{
			ID: messageID, TurnID: turnID, Sequence: int64(index), Role: role, Content: content,
			Bash:       message.Bash,
			ToolCallID: message.ToolCallID, ToolName: message.ToolName, Details: message.ToolDetails,
			IsError: message.IsError,
		}
		if role == "error" {
			projected.Role = "assistant"
			projected.StopReason = "error"
			projected.ErrorMessage = message.Text
			projected.IsError = true
		}
		if message.Aborted {
			projected.StopReason = "aborted"
			projected.ErrorMessage = message.Text
			projected.IsError = true
		}
		if role == "tool" {
			if len(message.ToolContent) > 0 {
				projected.Content = append([]protocol.TranscriptContent(nil), message.ToolContent...)
			}
			toolStates[transcriptToolStateKey{TurnID: turnID, ToolCallID: message.ToolCallID}] = message
		}
		structured = append(structured, projected)
	}
	items := buildTurnTranscriptItems(structured)
	for index := range items {
		items[index].Pending = pendingMessages[items[index].ID]
	}
	return transcriptPresentation{
		Items: groupTranscriptDisplayItems(items), ToolStates: toolStates,
	}
}

func transcriptActivitySource(items []transcriptDisplayItem, sourceID string) (transcriptDisplayItem, bool) {
	for _, item := range items {
		if item.Kind == transcriptDisplayTurnWork && item.ID == sourceID {
			return item, true
		}
	}
	return transcriptDisplayItem{}, false
}

func transcriptActivityInProgress(presentation transcriptPresentation, sourceID string) bool {
	source, ok := transcriptActivitySource(presentation.Items, sourceID)
	if !ok {
		return false
	}
	aborted := false
	for _, item := range source.Items {
		aborted = aborted || item.Aborted
	}
	if aborted {
		return false
	}
	for _, call := range displayItemToolCalls(source) {
		state, exists := presentation.ToolStates[transcriptToolStateKey{TurnID: source.TurnID, ToolCallID: call.ID}]
		if !exists || state.Pending {
			return true
		}
	}
	return false
}

type bashCommandPresentation struct {
	Text         string
	Summarized   bool
	CommandCount int
}

type bashCommandPart struct {
	command   string
	separator byte
}

const (
	maxToolArgSummaryLength     = 80
	maxBashCommandSummaryLength = 72
)

func presentBashCommand(command string) bashCommandPresentation {
	trimmed := strings.TrimSpace(command)
	parts, complexSyntax := splitBashCommand(command)
	lineCount := 0
	for _, line := range strings.Split(command, "\n") {
		if strings.TrimSpace(line) != "" {
			lineCount++
		}
	}
	for _, part := range parts {
		switch bashExecutable(part.command) {
		case "for", "while", "until", "if", "case", "select", "function":
			complexSyntax = true
		}
	}
	if complexSyntax {
		if lineCount > 1 {
			return bashCommandPresentation{Text: "shell script · " + strconv.Itoa(lineCount) + " lines", Summarized: true, CommandCount: len(parts)}
		}
		return bashCommandPresentation{Text: "shell command · " + strconv.Itoa(max(1, len(parts))) + " steps", Summarized: true, CommandCount: len(parts)}
	}
	if strings.Contains(command, "\n") {
		if len(parts) > 1 {
			return bashCommandPresentation{Text: "shell script · " + strconv.Itoa(len(parts)) + " commands", Summarized: true, CommandCount: len(parts)}
		}
		return bashCommandPresentation{Text: "shell script · " + strconv.Itoa(lineCount) + " lines", Summarized: true, CommandCount: len(parts)}
	}
	normalized := strings.Join(strings.Fields(trimmed), " ")
	if len(parts) <= 1 && utf8.RuneCountInString(trimmed) <= maxBashCommandSummaryLength {
		return bashCommandPresentation{Text: trimmed, CommandCount: len(parts)}
	}
	if len(parts) <= 1 {
		return bashCommandPresentation{Text: truncateRunes(normalized, maxBashCommandSummaryLength-1) + "…", Summarized: true, CommandCount: len(parts)}
	}

	var summary strings.Builder
	summary.WriteString(bashExecutable(parts[0].command))
	for index := 1; index < len(parts); index++ {
		if parts[index-1].separator == '|' {
			summary.WriteString(" → ")
		} else {
			summary.WriteString(" · ")
		}
		summary.WriteString(bashExecutable(parts[index].command))
	}
	text := summary.String()
	if utf8.RuneCountInString(text) > maxBashCommandSummaryLength {
		text = strings.TrimRight(truncateRunes(text, maxBashCommandSummaryLength-1), " ") + "…"
	}
	return bashCommandPresentation{Text: text, Summarized: true, CommandCount: len(parts)}
}

func splitBashCommand(command string) ([]bashCommandPart, bool) {
	parts := make([]bashCommandPart, 0)
	complexSyntax := false
	start := 0
	var quote byte
	escaped := false
	nesting := 0
	push := func(end int, separator byte) {
		value := strings.TrimSpace(command[start:end])
		if value != "" {
			parts = append(parts, bashCommandPart{command: value, separator: separator})
		}
	}
	for index := 0; index < len(command); index++ {
		character := command[index]
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' || character == '`' {
			quote = character
			continue
		}
		if character == '#' && (index == 0 || strings.ContainsRune(" \t\r\n;&|()", rune(command[index-1]))) {
			newline := strings.IndexByte(command[index:], '\n')
			if newline < 0 {
				push(index, 0)
				start = len(command)
				break
			}
			push(index, ';')
			index += newline
			start = index + 1
			continue
		}
		if character == '<' && (index == 0 || command[index-1] != '<') && index+1 < len(command) && command[index+1] == '<' && (index+2 >= len(command) || command[index+2] != '<') {
			complexSyntax = true
		}
		switch character {
		case '(', '{', '[':
			nesting++
			continue
		case ')', '}', ']':
			if nesting > 0 {
				nesting--
			}
			continue
		}
		if nesting > 0 {
			continue
		}
		next := byte(0)
		if index+1 < len(command) {
			next = command[index+1]
		}
		if character == '|' && next != '|' {
			push(index, '|')
			if next == '&' {
				index++
			}
			start = index + 1
			continue
		}
		separator := character == ';' || character == '\n' || (character == '&' && next == '&') ||
			(character == '&' && next != '>' && (index == 0 || command[index-1] != '>') && (index == 0 || command[index-1] != '<')) ||
			(character == '|' && next == '|')
		if separator {
			push(index, ';')
			if next == character {
				index++
			}
			start = index + 1
		}
	}
	push(len(command), 0)
	return parts, complexSyntax
}

var (
	bashWords      = regexp.MustCompile(`([^\s"'\\]+|\\.|"[^"]*"|'[^']*')+`)
	bashAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

func bashExecutable(command string) string {
	words := bashWords.FindAllString(command, -1)
	index := 0
	for index < len(words) && bashAssignment.MatchString(words[index]) {
		index++
	}
	if index >= len(words) {
		return "shell"
	}
	executable := strings.Trim(words[index], "'\"")
	if base := filepath.Base(executable); base != "." && base != "" {
		return base
	}
	return "shell"
}

func toolArguments(call transcriptToolCall) map[string]any {
	var arguments map[string]any
	if json.Unmarshal(call.Arguments, &arguments) != nil {
		return nil
	}
	return arguments
}

func subagentToolConversationID(call transcriptToolCall, state transcriptMessage, exists bool, conversations []protocol.SubagentConversation) string {
	agentName := subagentToolAgentName(call)
	if agentName == "" {
		return ""
	}
	if conversationID := subagentConversationIDForAgent(conversations, agentName); conversationID != "" {
		return conversationID
	}
	if !exists || state.ToolDetailsOmitted || len(state.ToolDetails) == 0 {
		return ""
	}
	var details struct {
		Conversation *struct {
			ID        string `json:"id"`
			Agent     string `json:"agent"`
			AgentName string `json:"agentName"`
		} `json:"conversation"`
	}
	if json.Unmarshal(state.ToolDetails, &details) != nil || details.Conversation == nil {
		return ""
	}
	resultAgent := details.Conversation.Agent
	if resultAgent == "" {
		resultAgent = details.Conversation.AgentName
	}
	conversationID := details.Conversation.ID
	if resultAgent != agentName || !strings.HasPrefix(conversationID, "subagent_") || len(conversationID) > 128 || strings.ContainsAny(conversationID, " \t\r\n") {
		return ""
	}
	return conversationID
}

func subagentToolAgentName(call transcriptToolCall) string {
	if call.Name != "subagent" {
		return ""
	}
	agent, _ := toolArguments(call)["agent"].(string)
	return strings.TrimSpace(agent)
}

type toolCallPresentation struct {
	Title   string
	Summary string
}

func presentToolCall(call transcriptToolCall, state transcriptMessage, exists bool) toolCallPresentation {
	arguments := toolArguments(call)
	path, _ := arguments["path"].(string)
	summary := path
	switch strings.ToLower(call.Name) {
	case "bash", "shell", "exec", "exec_command":
		command, _ := arguments["command"].(string)
		return toolCallPresentation{Title: "Run command", Summary: toolSummaryOrFallback(call, presentBashCommand(command).Text)}
	case "change_cwd":
		return toolCallPresentation{Title: "Change directory", Summary: toolSummaryOrFallback(call, path)}
	case "read":
		if exists && resolveActivityToolState(state, true, false) == activityToolSucceeded {
			_, lines, notice := activityReadContent(state)
			if state.Text == "" {
				summary += " " + glyphMiddleDot + " empty"
			} else if lines > 0 {
				start := toolIntegerArgument(arguments, "offset", 1)
				summary += fmt.Sprintf(":%d–%d", start, start+lines-1)
			}
			if notice != "" {
				summary += " " + glyphMiddleDot + " truncated"
			}
		}
		return toolCallPresentation{Title: "Read file", Summary: toolSummaryOrFallback(call, summary)}
	case "write":
		content, ok := arguments["content"].(string)
		if !ok {
			return toolCallPresentation{Title: "Write file", Summary: toolSummaryOrFallback(call, path)}
		}
		return toolCallPresentation{Title: "Write " + activityLineMetadata(recordedLineCount(content)), Summary: toolSummaryOrFallback(call, path)}
	case "edit":
		edits := activityEdits(arguments)
		title := "Edit file"
		if len(edits) > 0 {
			title = fmt.Sprintf("Edit %d sections", len(edits))
			if len(edits) == 1 {
				title = "Edit 1 section"
			}
		}
		return toolCallPresentation{Title: title, Summary: toolSummaryOrFallback(call, path)}
	case "edit_scratchpad":
		return toolCallPresentation{Title: "Update scratchpad", Summary: activityEditMetadata(len(activityEdits(arguments)))}
	case "grep":
		if arguments == nil {
			return toolCallPresentation{Title: "Search", Summary: fallbackToolArguments(call)}
		}
		return toolCallPresentation{Title: "Search", Summary: toolPatternScope(arguments)}
	case "find", "glob":
		if arguments == nil {
			return toolCallPresentation{Title: "Find files", Summary: fallbackToolArguments(call)}
		}
		return toolCallPresentation{Title: "Find files", Summary: toolPatternScope(arguments)}
	case "ls":
		return toolCallPresentation{Title: "List directory", Summary: toolSummaryOrFallback(call, path)}
	case "activate_skill":
		name, _ := arguments["name"].(string)
		return toolCallPresentation{Title: "Load skill", Summary: toolSummaryOrFallback(call, name)}
	case "subagent":
		action, _ := arguments["action"].(string)
		title := map[string]string{
			"start": "Start agent", "run": "Start agent", "spawn": "Start agent",
			"message": "Message agent", "send": "Message agent", "wait": "Wait for agent",
			"cancel": "Cancel agent", "dismiss": "Dismiss agent", "inspect": "Inspect agent",
		}[action]
		if title == "" {
			title = "Inspect agents"
		}
		return toolCallPresentation{Title: title, Summary: toolSummaryOrFallback(call, subagentToolAgentName(call))}
	default:
		return toolCallPresentation{Title: humanizeToolName(call.Name), Summary: fallbackToolArguments(call)}
	}
}

func recordedLineCount(value string) int {
	if value == "" {
		return 0
	}
	lines := strings.Count(value, "\n")
	if !strings.HasSuffix(value, "\n") {
		lines++
	}
	return lines
}

func toolIntegerArgument(arguments map[string]any, key string, fallback int) int {
	value, ok := arguments[key].(float64)
	if !ok || value < 1 || value != float64(int(value)) {
		return fallback
	}
	return int(value)
}

func toolSummaryPrefersTail(call transcriptToolCall) bool {
	switch strings.ToLower(call.Name) {
	case "change_cwd", "read", "write", "edit", "ls":
		return true
	default:
		return false
	}
}

func truncateToolPathSummary(call transcriptToolCall, value string, maxCells int) string {
	arguments := toolArguments(call)
	path, _ := arguments["path"].(string)
	if path == "" || !strings.HasPrefix(value, path) {
		return truncateToolPathTail(value, maxCells)
	}
	suffix := strings.TrimPrefix(value, path)
	pathBudget := maxCells - workspaceTextWidth(suffix)
	if pathBudget < 2 {
		pathBudget = 2
	}
	return truncateToolPathTail(path, pathBudget) + suffix
}

func truncateToolPathTail(value string, maxCells int) string {
	if maxCells < 2 || workspaceTextWidth(value) <= maxCells {
		return value
	}
	budget := maxCells - workspaceTextWidth(glyphEllipsis+"/")
	runes := []rune(value)
	start := len(runes)
	used := 0
	for start > 0 {
		width := workspaceTextWidth(string(runes[start-1]))
		if used+width > budget {
			break
		}
		used += width
		start--
	}
	tail := strings.TrimLeft(string(runes[start:]), "/")
	return glyphEllipsis + "/" + tail
}

func toolPatternScope(arguments map[string]any) string {
	pattern, _ := arguments["pattern"].(string)
	path, _ := arguments["path"].(string)
	if path == "" {
		path = "."
	}
	if pattern == "" {
		return path
	}
	return pattern + " in " + path
}

func humanizeToolName(name string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(name))
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
			words[index] = string(runes)
		}
	}
	if len(words) == 0 {
		return "Tool call"
	}
	return strings.Join(words, " ")
}

func toolSummaryOrFallback(call transcriptToolCall, summary string) string {
	if strings.TrimSpace(summary) != "" {
		return summary
	}
	return fallbackToolArguments(call)
}

func fallbackToolArguments(call transcriptToolCall) string {
	if summary := formatToolArguments(call, false); summary != "" {
		return summary
	}
	raw := strings.Join(strings.Fields(string(call.Arguments)), " ")
	if raw == "" {
		if call.ArgumentsTruncated {
			return "arguments truncated"
		}
		return "no arguments"
	}
	if utf8.RuneCountInString(raw) > maxToolArgSummaryLength {
		raw = truncateRunes(raw, maxToolArgSummaryLength-1) + glyphEllipsis
	}
	if call.ArgumentsTruncated {
		raw += " " + glyphMiddleDot + " truncated"
	}
	return raw
}

func toolArgumentKeys(call transcriptToolCall) []string {
	switch call.Name {
	case "subagent":
		return []string{"message", "action"}
	case "activate_skill":
		return []string{"name"}
	default:
		return []string{"command", "path", "agent"}
	}
}

func formatToolArguments(call transcriptToolCall, full bool) string {
	if call.ArgumentsTruncated {
		return ""
	}
	arguments := toolArguments(call)
	for _, key := range toolArgumentKeys(call) {
		value, ok := arguments[key].(string)
		if !ok {
			continue
		}
		summary := strings.Join(strings.Fields(value), " ")
		if !full && utf8.RuneCountInString(summary) > maxToolArgSummaryLength {
			summary = truncateRunes(summary, maxToolArgSummaryLength-3) + "..."
		}
		return summary
	}
	return ""
}

func truncateRunes(value string, length int) string {
	if length <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length])
}
