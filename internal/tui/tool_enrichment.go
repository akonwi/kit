package tui

import (
	"encoding/json"
	"strconv"
	"strings"
)

type activityEnrichmentKind int

const (
	activityEnrichmentFile activityEnrichmentKind = iota
	activityEnrichmentEdits
)

type activityEdit struct {
	OldText string
	NewText string
}

type activityEnrichment struct {
	Kind      activityEnrichmentKind
	Path      string
	Content   string
	LineCount int
	Notice    string
	Edits     []activityEdit
}

func detectActivityEnrichment(call transcriptToolCall, state transcriptMessage, exists bool) (activityEnrichment, bool) {
	if !exists || state.Pending || state.IsError || state.Aborted || state.ToolStatus == "Not run" || call.ArgumentsTruncated {
		return activityEnrichment{}, false
	}
	arguments := toolArguments(call)
	path, ok := arguments["path"].(string)
	if !ok || path == "" {
		return activityEnrichment{}, false
	}
	switch strings.ToLower(call.Name) {
	case "read":
		if state.Text == "" {
			return activityEnrichment{}, false
		}
		content, lines, notice := activityReadContent(state)
		return activityEnrichment{
			Kind: activityEnrichmentFile, Path: path, Content: content,
			LineCount: lines, Notice: notice,
		}, true
	case "write":
		content, ok := arguments["content"].(string)
		if !ok {
			return activityEnrichment{}, false
		}
		return activityEnrichment{
			Kind: activityEnrichmentFile, Path: path, Content: content,
			LineCount: len(splitActivityLines(content)),
		}, true
	case "edit":
		edits := activityEdits(arguments)
		if len(edits) == 0 {
			return activityEnrichment{}, false
		}
		return activityEnrichment{Kind: activityEnrichmentEdits, Path: path, Edits: edits}, true
	default:
		return activityEnrichment{}, false
	}
}

func activityReadContent(state transcriptMessage) (string, int, string) {
	content := state.Text
	lines := len(splitActivityLines(content))
	if len(state.ToolDetails) == 0 {
		return content, lines, ""
	}
	var details struct {
		Lines     int  `json:"lines"`
		Truncated bool `json:"truncated"`
	}
	if json.Unmarshal(state.ToolDetails, &details) != nil {
		return content, lines, ""
	}
	if details.Lines > 0 {
		lines = details.Lines
	}
	if details.Truncated && strings.HasSuffix(content, "\n[truncated]") {
		content = strings.TrimSuffix(content, "\n[truncated]")
		return content, lines, "[truncated]"
	}
	return content, lines, ""
}

func activityEdits(arguments map[string]any) []activityEdit {
	if raw, ok := arguments["edits"].([]any); ok {
		edits := make([]activityEdit, 0, len(raw))
		for _, candidate := range raw {
			object, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			oldText, oldOK := object["oldText"].(string)
			newText, newOK := object["newText"].(string)
			if oldOK && newOK {
				edits = append(edits, activityEdit{OldText: oldText, NewText: newText})
			}
		}
		return edits
	}
	oldText, oldOK := arguments["oldText"].(string)
	newText, newOK := arguments["newText"].(string)
	if oldOK && newOK {
		return []activityEdit{{OldText: oldText, NewText: newText}}
	}
	return nil
}

type activityDiffLineKind int

const (
	activityDiffContext activityDiffLineKind = iota
	activityDiffDelete
	activityDiffAdd
)

type activityDiffLine struct {
	Kind activityDiffLineKind
	Text string
}

const (
	maxActivityDiffInputLines = 400
	maxActivityDiffCells      = 500_000
)

func buildActivityDiff(oldText, newText string) ([]activityDiffLine, bool) {
	oldLines := splitActivityDiffLines(oldText)
	newLines := splitActivityDiffLines(newText)
	if len(oldLines) > maxActivityDiffInputLines || len(newLines) > maxActivityDiffInputLines {
		return nil, false
	}
	columns := len(newLines) + 1
	lcs := make([]int, (len(oldLines)+1)*columns)
	for oldIndex := len(oldLines) - 1; oldIndex >= 0; oldIndex-- {
		for newIndex := len(newLines) - 1; newIndex >= 0; newIndex-- {
			index := oldIndex*columns + newIndex
			if oldLines[oldIndex] == newLines[newIndex] {
				lcs[index] = lcs[(oldIndex+1)*columns+newIndex+1] + 1
			} else {
				lcs[index] = max(lcs[(oldIndex+1)*columns+newIndex], lcs[oldIndex*columns+newIndex+1])
			}
		}
	}
	lines := make([]activityDiffLine, 0, len(oldLines)+len(newLines))
	oldIndex, newIndex := 0, 0
	for oldIndex < len(oldLines) && newIndex < len(newLines) {
		switch {
		case oldLines[oldIndex] == newLines[newIndex]:
			lines = append(lines, activityDiffLine{Kind: activityDiffContext, Text: oldLines[oldIndex]})
			oldIndex++
			newIndex++
		case lcs[(oldIndex+1)*columns+newIndex] >= lcs[oldIndex*columns+newIndex+1]:
			lines = append(lines, activityDiffLine{Kind: activityDiffDelete, Text: oldLines[oldIndex]})
			oldIndex++
		default:
			lines = append(lines, activityDiffLine{Kind: activityDiffAdd, Text: newLines[newIndex]})
			newIndex++
		}
	}
	for ; oldIndex < len(oldLines); oldIndex++ {
		lines = append(lines, activityDiffLine{Kind: activityDiffDelete, Text: oldLines[oldIndex]})
	}
	for ; newIndex < len(newLines); newIndex++ {
		lines = append(lines, activityDiffLine{Kind: activityDiffAdd, Text: newLines[newIndex]})
	}
	oldEndings := activityLineEndings(oldText)
	newEndings := activityLineEndings(newText)
	if line, oldEnding, newEnding, changed := firstActivityLineEndingChange(oldEndings, newEndings); changed && activityLinesEqual(oldLines, newLines) {
		lines = append(lines,
			activityDiffLine{Kind: activityDiffDelete, Text: "\\ line " + strconv.Itoa(line) + " ending: " + oldEnding},
			activityDiffLine{Kind: activityDiffAdd, Text: "\\ line " + strconv.Itoa(line) + " ending: " + newEnding},
		)
	}
	return lines, true
}

func activityDiffCellCost(edit activityEdit) (int, bool) {
	oldLines := splitActivityDiffLines(edit.OldText)
	newLines := splitActivityDiffLines(edit.NewText)
	if len(oldLines) > maxActivityDiffInputLines || len(newLines) > maxActivityDiffInputLines {
		return 0, false
	}
	return (len(oldLines) + 1) * (len(newLines) + 1), true
}

func activityLinesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func activityLineEndings(value string) []string {
	if value == "" {
		return nil
	}
	endings := make([]string, 0, strings.Count(value, "\n")+1)
	lineStart := 0
	for index := 0; index < len(value); index++ {
		if value[index] != '\n' {
			continue
		}
		ending := "LF"
		if index > lineStart && value[index-1] == '\r' {
			ending = "CRLF"
		}
		endings = append(endings, ending)
		lineStart = index + 1
	}
	if lineStart < len(value) {
		endings = append(endings, "none")
	}
	return endings
}

func firstActivityLineEndingChange(oldEndings, newEndings []string) (int, string, string, bool) {
	count := max(len(oldEndings), len(newEndings))
	for index := range count {
		oldEnding := "absent"
		if index < len(oldEndings) {
			oldEnding = oldEndings[index]
		}
		newEnding := "absent"
		if index < len(newEndings) {
			newEnding = newEndings[index]
		}
		if oldEnding != newEnding {
			return index + 1, oldEnding, newEnding, true
		}
	}
	return 0, "", "", false
}

func splitActivityLines(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func splitActivityDiffLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := splitActivityLines(value)
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
