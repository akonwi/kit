// Package pluginmessage derives the collapsed presentation of a
// plugin-submitted session message, as the vaxis client does.
package pluginmessage

import (
	"strings"

	kitmarkdown "github.com/akonwi/kit/internal/markdown"
)

// Summary is the first visible line of a plugin message as plain text,
// without Markdown syntax. Blocks are parsed one at a time, separated by blank
// lines, until one has text: the summary rarely needs more than the first, and
// rows rebuild it on every render.
func Summary(text string) string {
	for rest := text; rest != ""; {
		var block string
		block, rest = nextBlock(rest)
		if line := blockSummary(block); line != "" {
			return line
		}
	}
	return firstNonBlankLine(text)
}

func blockSummary(source string) string {
	for _, block := range kitmarkdown.Parse(source).Blocks {
		plain := block.Code
		if len(block.Runs) > 0 {
			plain = plainText(block.Runs)
		} else if len(block.Rows) > 0 {
			cells := make([]string, 0, len(block.Rows[0].Cells))
			for _, cell := range block.Rows[0].Cells {
				cells = append(cells, plainText(cell.Runs))
			}
			plain = strings.Join(cells, " · ")
		}
		if line := firstNonBlankLine(plain); line != "" {
			return line
		}
	}
	return ""
}

func plainText(runs []kitmarkdown.Run) string {
	var text strings.Builder
	for _, run := range runs {
		text.WriteString(kitmarkdown.VisibleText(run))
	}
	return text.String()
}

// nextBlock splits text after its first run of non-blank lines.
func nextBlock(text string) (block, rest string) {
	start, offset := -1, 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		blank := strings.TrimSpace(line) == ""
		if start < 0 && !blank {
			start = offset
		} else if start >= 0 && blank {
			return text[start:offset], text[offset:]
		}
		offset += len(line)
	}
	if start < 0 {
		return "", ""
	}
	return text[start:], ""
}

func firstNonBlankLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
