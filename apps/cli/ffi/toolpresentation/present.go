// Package toolpresentation derives a tool call's title and summary for the
// transcript's tool rows, as the vaxis client does (presentToolCall in
// internal/tui/transcript_model.go).
package toolpresentation

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	middleDot = "·"
	ellipsis  = "…"

	maxArgumentSummaryLength    = 80
	maxBashCommandSummaryLength = 72
)

// Call is one tool call as the model made it.
type Call struct {
	Name string
	// Arguments is the call's JSON arguments, which may be malformed.
	Arguments string
	// ArgumentsTruncated reports that the server omitted the arguments.
	ArgumentsTruncated bool
}

// Result is what a call's result contributes to its row.
type Result struct {
	// Succeeded reports a finished call that did not fail.
	Succeeded bool
	Text      string
	// Details is the result's raw JSON details, if any.
	Details string
}

// Presentation is a tool row's title and summary.
type Presentation struct {
	Title   string
	Summary string
	// Path is the call's path argument. A narrow row shortens a summary that
	// starts with it from the start of the path.
	Path string
	// PathFirst reports that the summary leads with a path, which a narrow row
	// keeps the end of.
	PathFirst bool
}

// Present returns the title and summary of a tool call. servers names the
// session's MCP servers, each of which the model calls as one tool. A summary
// may be empty when the call has nothing to add to its title.
func Present(call Call, result Result, servers []string) Presentation {
	arguments := parseArguments(call.Arguments)
	path, _ := arguments["path"].(string)
	if server, ok := mcpServer(call.Name, servers); ok {
		return presentMCP(call, arguments, server)
	}
	presentation := present(call, arguments, path, result)
	presentation.Path = path
	presentation.PathFirst = pathFirst(call.Name)
	return presentation
}

func present(call Call, arguments map[string]any, path string, result Result) Presentation {
	summary := path
	switch strings.ToLower(call.Name) {
	case "bash", "shell", "exec", "exec_command":
		command, _ := arguments["command"].(string)
		return Presentation{Title: "Run command", Summary: summaryOrFallback(call, BashCommand(command).Text)}
	case "change_cwd":
		return Presentation{Title: "Change directory", Summary: summaryOrFallback(call, path)}
	case "read":
		if result.Succeeded {
			lines, truncated := readLines(result)
			if result.Text == "" {
				summary += " " + middleDot + " empty"
			} else if lines > 0 {
				start := integerArgument(arguments, "offset", 1)
				summary += fmt.Sprintf(":%d–%d", start, start+lines-1)
			}
			if truncated {
				summary += " " + middleDot + " truncated"
			}
		}
		return Presentation{Title: "Read file", Summary: summaryOrFallback(call, summary)}
	case "write":
		content, ok := arguments["content"].(string)
		if !ok {
			return Presentation{Title: "Write file", Summary: summaryOrFallback(call, path)}
		}
		return Presentation{Title: "Write " + lineCount(recordedLineCount(content)), Summary: summaryOrFallback(call, path)}
	case "edit":
		edits := editCount(arguments)
		title := "Edit file"
		if edits == 1 {
			title = "Edit 1 section"
		} else if edits > 1 {
			title = fmt.Sprintf("Edit %d sections", edits)
		}
		return Presentation{Title: title, Summary: summaryOrFallback(call, path)}
	case "edit_scratchpad":
		return Presentation{Title: "Update scratchpad", Summary: editLabel(editCount(arguments))}
	case "grep":
		if arguments == nil {
			return Presentation{Title: "Search", Summary: fallbackArguments(call)}
		}
		return Presentation{Title: "Search", Summary: patternScope(arguments)}
	case "find", "glob":
		if arguments == nil {
			return Presentation{Title: "Find files", Summary: fallbackArguments(call)}
		}
		return Presentation{Title: "Find files", Summary: patternScope(arguments)}
	case "ls":
		return Presentation{Title: "List directory", Summary: summaryOrFallback(call, path)}
	case "activate_skill":
		name, _ := arguments["name"].(string)
		return Presentation{Title: "Load skill", Summary: summaryOrFallback(call, name)}
	case "peer_session":
		action, _ := arguments["action"].(string)
		title := map[string]string{
			"discover": "Discover sessions", "send": "Ask session",
			"inspect": "Inspect peer query", "wait": "Wait for peer query",
		}[action]
		if title == "" {
			title = "Peer session"
		}
		summary := formatArguments(call, arguments)
		if action == "discover" && summary == "" {
			summary = "available sessions"
		}
		return Presentation{Title: title, Summary: summaryOrFallback(call, summary)}
	case "create_session":
		name, _ := arguments["name"].(string)
		cwd, _ := arguments["cwd"].(string)
		summary := strings.TrimSpace(name)
		if summary != "" && strings.TrimSpace(cwd) != "" {
			summary += " " + middleDot + " " + strings.TrimSpace(cwd)
		}
		return Presentation{Title: "Create session", Summary: summaryOrFallback(call, summary)}
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
		agent, _ := arguments["agent"].(string)
		return Presentation{Title: title, Summary: summaryOrFallback(call, strings.TrimSpace(agent))}
	case "read_scratchpad":
		return Presentation{Title: "Read scratchpad"}
	case "confirm_from_user", "input_from_user", "select_from_user", "guided_questions":
		title := map[string]string{
			"confirm_from_user": "Confirm", "input_from_user": "Ask",
			"select_from_user": "Ask", "guided_questions": "Ask questions",
		}[strings.ToLower(call.Name)]
		request, _ := arguments["title"].(string)
		return Presentation{Title: title, Summary: summaryOrFallback(call, strings.TrimSpace(request))}
	case "show_image":
		caption, _ := arguments["caption"].(string)
		summary = strings.TrimSpace(path)
		if summary == "" {
			summary = strings.TrimSpace(caption)
		}
		return Presentation{Title: "Show image", Summary: summaryOrFallback(call, summary)}
	case "inspect_image":
		return Presentation{Title: "Inspect image", Summary: summaryOrFallback(call, strings.TrimSpace(path))}
	case "subagent_inbox":
		return Presentation{Title: "Check inbox"}
	case "subagent_send":
		agent, _ := arguments["agent"].(string)
		return Presentation{Title: "Message sibling", Summary: summaryOrFallback(call, strings.TrimSpace(agent))}
	case "subagent_reply", "subagent_inspect":
		title := "Reply"
		if strings.ToLower(call.Name) == "subagent_inspect" {
			title = "Inspect request"
		}
		receipt, _ := arguments["receipt"].(string)
		return Presentation{Title: title, Summary: summaryOrFallback(call, strings.TrimSpace(receipt))}
	default:
		if plugin, tool, ok := pluginTool(call.Name); ok {
			return Presentation{Title: humanize(tool), Summary: plugin}
		}
		return Presentation{Title: humanize(call.Name), Summary: fallbackArguments(call)}
	}
}

// presentMCP presents a call to the tool of an MCP server, which lists,
// searches, describes, or calls the server's tools, or logs out of it. Titles
// stay short to fit the title column.
func presentMCP(call Call, arguments map[string]any, server string) Presentation {
	action, _ := arguments["action"].(string)
	query, _ := arguments["query"].(string)
	tool, _ := arguments["tool"].(string)
	switch action {
	case "list":
		return Presentation{Title: "List " + server}
	case "search":
		return Presentation{Title: "Search " + server, Summary: summaryOrFallback(call, strings.TrimSpace(query))}
	case "describe":
		return Presentation{Title: "Describe " + server, Summary: summaryOrFallback(call, strings.TrimSpace(tool))}
	case "call":
		return Presentation{Title: "Call " + server, Summary: summaryOrFallback(call, strings.TrimSpace(tool))}
	case "logout":
		return Presentation{Title: "Log out of " + server}
	default:
		return Presentation{Title: server, Summary: fallbackArguments(call)}
	}
}

// mcpServer is the MCP server among servers whose tool is name.
func mcpServer(name string, servers []string) (string, bool) {
	for _, server := range servers {
		if tool, ok := mcpToolName(server); ok && tool == name {
			return server, true
		}
	}
	return "", false
}

// mcpToolName is the name the model calls server's tool by, as Kit's MCP
// manager derives it: lowercase ASCII letters, digits, `_`, and `-`, with
// each run of other characters as one `_`, trimmed of `_` and `-`.
func mcpToolName(server string) (string, bool) {
	var name strings.Builder
	underscore := false
	for _, character := range strings.ToLower(server) {
		if character <= 127 && (character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			name.WriteRune(character)
			underscore = false
			continue
		}
		if !underscore {
			name.WriteByte('_')
			underscore = true
		}
	}
	tool := strings.Trim(name.String(), "_-")
	return tool, tool != ""
}

// pluginToolID matches a plugin tool's model-facing name, `<plugin>__<tool>`.
var pluginToolID = regexp.MustCompile(`^([a-z][a-z0-9-]{0,31})__(.+)$`)

// pluginTool splits a plugin tool's name into its plugin and local tool.
func pluginTool(name string) (string, string, bool) {
	match := pluginToolID.FindStringSubmatch(name)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

func parseArguments(raw string) map[string]any {
	var arguments map[string]any
	if json.Unmarshal([]byte(raw), &arguments) != nil {
		return nil
	}
	return arguments
}

// pathFirst reports tools whose summary leads with a path.
func pathFirst(name string) bool {
	switch strings.ToLower(name) {
	case "change_cwd", "read", "write", "edit", "ls", "show_image", "inspect_image":
		return true
	default:
		return false
	}
}

// readLines returns the number of lines a read returned and whether its
// content was truncated, preferring the counts in its details.
func readLines(result Result) (int, bool) {
	lines := len(splitLines(result.Text))
	if result.Details == "" {
		return lines, false
	}
	var details struct {
		Lines     int  `json:"lines"`
		Truncated bool `json:"truncated"`
	}
	if json.Unmarshal([]byte(result.Details), &details) != nil {
		return lines, false
	}
	if details.Lines > 0 {
		lines = details.Lines
	}
	return lines, details.Truncated && strings.HasSuffix(result.Text, "\n[truncated]")
}

func splitLines(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
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

func lineCount(lines int) string {
	if lines == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", lines)
}

func editLabel(edits int) string {
	if edits == 1 {
		return "1 edit"
	}
	return fmt.Sprintf("%d edits", edits)
}

// editCount counts the edits an edit call makes, from its edits list or its
// single oldText and newText.
func editCount(arguments map[string]any) int {
	if raw, ok := arguments["edits"].([]any); ok {
		count := 0
		for _, candidate := range raw {
			object, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			_, oldOK := object["oldText"].(string)
			_, newOK := object["newText"].(string)
			if oldOK && newOK {
				count++
			}
		}
		return count
	}
	_, oldOK := arguments["oldText"].(string)
	_, newOK := arguments["newText"].(string)
	if oldOK && newOK {
		return 1
	}
	return 0
}

func integerArgument(arguments map[string]any, key string, fallback int) int {
	value, ok := arguments[key].(float64)
	if !ok || value < 1 || value != float64(int(value)) {
		return fallback
	}
	return int(value)
}

func patternScope(arguments map[string]any) string {
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

func humanize(name string) string {
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

func summaryOrFallback(call Call, summary string) string {
	if strings.TrimSpace(summary) != "" {
		return summary
	}
	return fallbackArguments(call)
}

// fallbackArguments summarizes arguments without a typed presentation: a
// known string argument, else the compacted JSON.
func fallbackArguments(call Call) string {
	if summary := formatArguments(call, parseArguments(call.Arguments)); summary != "" {
		return summary
	}
	raw := strings.Join(strings.Fields(call.Arguments), " ")
	if raw == "" {
		if call.ArgumentsTruncated {
			return "arguments truncated"
		}
		return "no arguments"
	}
	if utf8.RuneCountInString(raw) > maxArgumentSummaryLength {
		raw = truncateRunes(raw, maxArgumentSummaryLength-1) + ellipsis
	}
	if call.ArgumentsTruncated {
		raw += " " + middleDot + " truncated"
	}
	return raw
}

func argumentKeys(name string) []string {
	switch name {
	case "subagent":
		return []string{"message", "action"}
	case "peer_session":
		return []string{"message", "requestId", "sessionId"}
	case "activate_skill":
		return []string{"name"}
	case "create_session":
		return []string{"name", "cwd", "prompt"}
	default:
		return []string{"command", "path", "agent"}
	}
}

// formatArguments returns the first of the tool's known string arguments,
// on one line and shortened.
func formatArguments(call Call, arguments map[string]any) string {
	if call.ArgumentsTruncated {
		return ""
	}
	for _, key := range argumentKeys(call.Name) {
		value, ok := arguments[key].(string)
		if !ok {
			continue
		}
		summary := strings.Join(strings.Fields(value), " ")
		if utf8.RuneCountInString(summary) > maxArgumentSummaryLength {
			summary = truncateRunes(summary, maxArgumentSummaryLength-3) + "..."
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

// BashPresentation summarizes a shell command for one row.
type BashPresentation struct {
	Text         string
	Summarized   bool
	CommandCount int
}

type bashPart struct {
	command   string
	separator byte
}

// BashCommand summarizes a shell command: short single commands as written,
// pipelines and lists as their executables, and scripts by their size.
func BashCommand(command string) BashPresentation {
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
			return BashPresentation{Text: "shell script · " + strconv.Itoa(lineCount) + " lines", Summarized: true, CommandCount: len(parts)}
		}
		return BashPresentation{Text: "shell command · " + strconv.Itoa(max(1, len(parts))) + " steps", Summarized: true, CommandCount: len(parts)}
	}
	if strings.Contains(command, "\n") {
		if len(parts) > 1 {
			return BashPresentation{Text: "shell script · " + strconv.Itoa(len(parts)) + " commands", Summarized: true, CommandCount: len(parts)}
		}
		return BashPresentation{Text: "shell script · " + strconv.Itoa(lineCount) + " lines", Summarized: true, CommandCount: len(parts)}
	}
	normalized := strings.Join(strings.Fields(trimmed), " ")
	if len(parts) <= 1 && utf8.RuneCountInString(trimmed) <= maxBashCommandSummaryLength {
		return BashPresentation{Text: trimmed, CommandCount: len(parts)}
	}
	if len(parts) <= 1 {
		return BashPresentation{Text: truncateRunes(normalized, maxBashCommandSummaryLength-1) + ellipsis, Summarized: true, CommandCount: len(parts)}
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
		text = strings.TrimRight(truncateRunes(text, maxBashCommandSummaryLength-1), " ") + ellipsis
	}
	return BashPresentation{Text: text, Summarized: true, CommandCount: len(parts)}
}

func splitBashCommand(command string) ([]bashPart, bool) {
	parts := make([]bashPart, 0)
	complexSyntax := false
	start := 0
	var quote byte
	escaped := false
	nesting := 0
	push := func(end int, separator byte) {
		value := strings.TrimSpace(command[start:end])
		if value != "" {
			parts = append(parts, bashPart{command: value, separator: separator})
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
