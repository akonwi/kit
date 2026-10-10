package toolpresentation

import (
	"encoding/json"
	"strings"
)

// Block kinds of a tool call's output.
const (
	// BlockCode is text highlighted in the language of Path, or plain without
	// one.
	BlockCode = "code"
	// BlockCommand is a shell command, highlighted as bash after a prompt.
	// Its Path is empty.
	BlockCommand = "command"
	// BlockGap is one blank row.
	BlockGap = "gap"
	// BlockRemoved is text taken out, highlighted in the language of Path.
	BlockRemoved = "removed"
	// BlockAdded is text put in, highlighted in the language of Path.
	BlockAdded = "added"
	// BlockSeparator separates the edits of one call.
	BlockSeparator = "separator"
	// BlockHunk is a unified diff's hunk header.
	BlockHunk = "hunk"
	// BlockHeader is a unified diff's file header.
	BlockHeader = "header"
)

// Output is how a tool call's result reads in its output well: blocks in
// order, and a note about what could not be shown.
type Output struct {
	Blocks []Block
	// Note explains a fallback, such as content the server did not keep.
	Note string
}

// Block is one run of output shown the same way. Removed and added lines
// carry their diff marker in Marker rather than in Text. Path names the file
// whose language highlights Text; the client detects it.
type Block struct {
	Kind   string
	Path   string
	Text   string
	Marker string
}

// Unavailable notes that a call's arguments were cut, so the output shows the
// result instead of the content.
const Unavailable = "content not available"

// ShowOutput describes the output of call with result. A running call's output
// is plain until it finishes.
func ShowOutput(call Call, result Result, running bool) Output {
	arguments, argumentsComplete := decodeArguments(call)
	path, _ := arguments["path"].(string)
	switch strings.ToLower(call.Name) {
	case "read":
		if !result.Succeeded {
			break
		}
		if details := detailsPath(result.Details); details != "" {
			path = details
		}
		return Output{Blocks: []Block{code(path, result.Text)}}
	case "write":
		if !result.Succeeded {
			break
		}
		content, ok := arguments["content"].(string)
		if !argumentsComplete || !ok {
			return Output{Blocks: []Block{code("", result.Text)}, Note: Unavailable}
		}
		return Output{Blocks: []Block{code(path, content)}}
	case "edit":
		if !result.Succeeded {
			break
		}
		edits, ok := editPairs(arguments)
		if !argumentsComplete || !ok {
			return Output{Blocks: []Block{code("", result.Text)}, Note: Unavailable}
		}
		return Output{Blocks: editBlocks(path, edits)}
	case "bash":
		command, _ := arguments["command"].(string)
		command = strings.TrimSpace(command)
		blocks := []Block{}
		if command != "" {
			blocks = append(blocks, Block{Kind: BlockCommand, Text: command}, Block{Kind: BlockGap})
		}
		switch {
		case running:
			blocks = append(blocks, code("", result.Text))
		case unifiedDiff(result.Text):
			blocks = append(blocks, diffBlocks(result.Text)...)
		default:
			blocks = append(blocks, code(dumpedFile(command), result.Text))
		}
		return Output{Blocks: blocks}
	}
	return Output{Blocks: []Block{code("", result.Text)}}
}

func code(path, text string) Block {
	return Block{Kind: BlockCode, Path: path, Text: text}
}

// decodeArguments returns the call's arguments and whether they are complete.
func decodeArguments(call Call) (map[string]any, bool) {
	arguments := map[string]any{}
	if call.ArgumentsTruncated || strings.TrimSpace(call.Arguments) == "" {
		return arguments, false
	}
	if json.Unmarshal([]byte(call.Arguments), &arguments) != nil {
		return map[string]any{}, false
	}
	return arguments, true
}

func detailsPath(details string) string {
	var decoded struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(details), &decoded) != nil {
		return ""
	}
	return decoded.Path
}

type edit struct{ old, new string }

// editPairs returns the call's edits, in either the list or the single form.
func editPairs(arguments map[string]any) ([]edit, bool) {
	pair := func(value map[string]any) (edit, bool) {
		old, oldOK := value["oldText"].(string)
		replacement, newOK := value["newText"].(string)
		return edit{old: old, new: replacement}, oldOK && newOK
	}
	if listed, ok := arguments["edits"].([]any); ok && len(listed) > 0 {
		edits := make([]edit, 0, len(listed))
		for _, item := range listed {
			value, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok := pair(value)
			if !ok {
				return nil, false
			}
			edits = append(edits, current)
		}
		return edits, true
	}
	single, ok := pair(arguments)
	if !ok {
		return nil, false
	}
	return []edit{single}, true
}

func editBlocks(path string, edits []edit) []Block {
	blocks := []Block{}
	for index, current := range edits {
		if index > 0 {
			blocks = append(blocks, Block{Kind: BlockSeparator, Text: "⋯"})
		}
		if current.old != "" {
			blocks = append(blocks, Block{Kind: BlockRemoved, Path: path, Text: current.old, Marker: "-"})
		}
		if current.new != "" {
			blocks = append(blocks, Block{Kind: BlockAdded, Path: path, Text: current.new, Marker: "+"})
		}
	}
	return blocks
}

// dumpedFile is the one file a command only prints, such as `cat main.go` or
// `sed -n '1,40p' main.go`, or empty for any other command.
func dumpedFile(command string) string {
	words, ok := shellWords(command)
	if !ok || len(words) < 2 {
		return ""
	}
	files := []string{}
	switch words[0] {
	case "cat":
		for _, word := range words[1:] {
			if strings.HasPrefix(word, "-") {
				return ""
			}
			files = append(files, word)
		}
	case "head", "tail":
		for index := 1; index < len(words); index++ {
			word := words[index]
			switch {
			case word == "-n" || word == "-c":
				index++
			case strings.HasPrefix(word, "-"):
			default:
				files = append(files, word)
			}
		}
	case "sed":
		if len(words) != 4 || words[1] != "-n" {
			return ""
		}
		files = append(files, words[3])
	default:
		return ""
	}
	if len(files) != 1 {
		return ""
	}
	return files[0]
}

// shellWords splits a simple command into words, honoring single and double
// quotes. It reports false for an unterminated quote, or for anything that
// makes the command more than one plain program run: pipes, redirects, lists,
// expansions, globs, comments, escapes, and line breaks.
func shellWords(command string) ([]string, bool) {
	words := []string{}
	var word strings.Builder
	inWord := false
	quote := rune(0)
	for _, character := range command {
		switch {
		case character == '\n':
			return nil, false
		case quote == '\'':
			if character == quote {
				quote = 0
			} else {
				word.WriteRune(character)
			}
		case quote == '"':
			switch character {
			case '"':
				quote = 0
			case '$', '`', '\\':
				return nil, false
			default:
				word.WriteRune(character)
			}
		case strings.ContainsRune("|&;<>()$`{}*?[]#\\", character):
			return nil, false
		case character == '\'' || character == '"':
			quote = character
			inWord = true
		case character == ' ' || character == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(character)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, true
}

// unifiedDiff reports whether output reads as a unified diff: a file header
// and at least one hunk.
func unifiedDiff(output string) bool {
	header, hunk := false, false
	previous := ""
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header = true
		case strings.HasPrefix(line, "+++ ") && strings.HasPrefix(previous, "--- "):
			header = true
		case strings.HasPrefix(line, "@@ ") && header:
			hunk = true
		}
		previous = line
	}
	return header && hunk
}

// diffBlocks splits a unified diff into runs of lines of one kind. Context
// lines keep a blank marker so their text aligns with changed lines.
func diffBlocks(output string) []Block {
	blocks := []Block{}
	add := func(kind, marker, line string) {
		if last := len(blocks) - 1; last >= 0 && blocks[last].Kind == kind && blocks[last].Marker == marker {
			blocks[last].Text += "\n" + line
			return
		}
		blocks = append(blocks, Block{Kind: kind, Text: line, Marker: marker})
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	inHunk := false
	for index, line := range lines {
		next := ""
		if index+1 < len(lines) {
			next = lines[index+1]
		}
		fileHeader := strings.HasPrefix(line, "diff --git ") ||
			strings.HasPrefix(line, "--- ") && strings.HasPrefix(next, "+++ ")
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			add(BlockHunk, "", line)
		case fileHeader || !inHunk:
			inHunk = false
			add(BlockHeader, "", line)
		case strings.HasPrefix(line, "-"):
			add(BlockRemoved, "-", line[1:])
		case strings.HasPrefix(line, "+"):
			add(BlockAdded, "+", line[1:])
		default:
			add(BlockCode, " ", strings.TrimPrefix(line, " "))
		}
	}
	return blocks
}
