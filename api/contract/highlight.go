package contract

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// MaxHighlightCodeBlocks bounds the blocks of one highlightCode request.
	MaxHighlightCodeBlocks = 128
	// MaxHighlightCodeBlockBytes bounds the source of one block.
	MaxHighlightCodeBlockBytes = 512 << 10
	// MaxHighlightCodeTotalBytes bounds the source of all blocks of one request.
	MaxHighlightCodeTotalBytes = 768 << 10
	// MaxHighlightLanguageBytes bounds a language label or name.
	MaxHighlightLanguageBytes = 64
	// MaxHighlightRoles bounds the role table of one Highlight.
	MaxHighlightRoles = 256
	// MaxHighlightRoleBytes bounds one role name.
	MaxHighlightRoleBytes = 64
)

// Highlight is the syntax coloring of one text: the semantic roles of its
// ranges, independent of any theme (ADR 0062).
//
// Lines holds one entry per line of the text, split at "\n" with a final
// "\n" ending the last line rather than starting another. Each entry is a
// flat array of triples: start and length in UTF-16 code units within the
// line, and an index into Roles. Triples within a line are ordered and do not
// overlap; unstyled ranges are omitted. Roles names semantic roles such as
// "keyword", "string", and "comment"; a client draws a role it has no color
// for as plain text, so the server may add roles.
type Highlight struct {
	// Language is the language highlighted, after alias and path detection.
	Language string   `json:"language"`
	Lines    [][]int  `json:"lines"`
	Roles    []string `json:"roles"`
}

// TextLines splits text into the lines a Highlight of it describes.
func TextLines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// ValidateText checks the highlight against the text it colors.
func (h Highlight) ValidateText(text string) error {
	return h.ValidateLines(TextLines(text))
}

// ValidateLines checks the highlight against the lines it colors.
func (h Highlight) ValidateLines(lines []string) error {
	if !validRendererText(h.Language, MaxHighlightLanguageBytes) || h.Lines == nil || h.Roles == nil || len(h.Roles) > MaxHighlightRoles {
		return fmt.Errorf("highlight is invalid")
	}
	roles := make(map[string]bool, len(h.Roles))
	for _, role := range h.Roles {
		if !validRendererText(role, MaxHighlightRoleBytes) || roles[role] {
			return fmt.Errorf("highlight roles are invalid")
		}
		roles[role] = true
	}
	if len(h.Lines) != len(lines) {
		return fmt.Errorf("highlight has %d lines for %d lines of text", len(h.Lines), len(lines))
	}
	for i, triples := range h.Lines {
		if triples == nil || len(triples)%3 != 0 {
			return fmt.Errorf("highlight line %d is invalid", i+1)
		}
		width := utf16Width(lines[i])
		end := 0
		for j := 0; j < len(triples); j += 3 {
			start, length, role := triples[j], triples[j+1], triples[j+2]
			if start < end || length <= 0 || length > width-start || role < 0 || role >= len(h.Roles) {
				return fmt.Errorf("highlight line %d range is invalid", i+1)
			}
			end = start + length
		}
	}
	return nil
}

func utf16Width(text string) int {
	width := 0
	for _, r := range text {
		width += utf16.RuneLen(r)
	}
	return width
}

// HighlightCodeBlock is one fenced code block to highlight.
type HighlightCodeBlock struct {
	// Language is the block's language label, such as a code fence's info
	// word; the server resolves aliases. An empty label is never highlighted.
	Language string `json:"language"`
	Source   string `json:"source"`
}

// HighlightCodeInput requests the highlighting of a bounded batch of code
// blocks, such as the fenced code of a completed message.
type HighlightCodeInput struct {
	Blocks []HighlightCodeBlock `json:"blocks"`
}

// HighlightCodeLimit names the bound a highlightCode request exceeds.
type HighlightCodeLimit string

const (
	HighlightCodeLimitBlocks        HighlightCodeLimit = "blocks"
	HighlightCodeLimitBlockBytes    HighlightCodeLimit = "block_bytes"
	HighlightCodeLimitTotalBytes    HighlightCodeLimit = "total_bytes"
	HighlightCodeLimitLanguageBytes HighlightCodeLimit = "language_bytes"
)

// EnumValues lists the highlightCode bounds.
func (HighlightCodeLimit) EnumValues() []string {
	return []string{"blocks", "block_bytes", "total_bytes", "language_bytes"}
}

// HighlightCodeLimitDetails are the details of a highlightCode request
// rejected as limit_exceeded.
type HighlightCodeLimitDetails struct {
	Limit HighlightCodeLimit `json:"limit"`
}

// HighlightCodeLimitError reports the bound a highlightCode request exceeds.
type HighlightCodeLimitError struct {
	Limit HighlightCodeLimit
}

func (e *HighlightCodeLimitError) Error() string {
	return fmt.Sprintf("highlight request exceeds the %s limit", e.Limit)
}

// Validate checks the request's bounds, returning a HighlightCodeLimitError
// for a request over them.
func (in HighlightCodeInput) Validate() error {
	if len(in.Blocks) > MaxHighlightCodeBlocks {
		return &HighlightCodeLimitError{Limit: HighlightCodeLimitBlocks}
	}
	total := 0
	for _, block := range in.Blocks {
		if len(block.Language) > MaxHighlightLanguageBytes {
			return &HighlightCodeLimitError{Limit: HighlightCodeLimitLanguageBytes}
		}
		if len(block.Source) > MaxHighlightCodeBlockBytes {
			return &HighlightCodeLimitError{Limit: HighlightCodeLimitBlockBytes}
		}
		total += len(block.Source)
		if total > MaxHighlightCodeTotalBytes {
			return &HighlightCodeLimitError{Limit: HighlightCodeLimitTotalBytes}
		}
		if block.Language != "" && !validRendererText(block.Language, MaxHighlightLanguageBytes) || !utf8.ValidString(block.Source) {
			return fmt.Errorf("highlight block is invalid")
		}
	}
	return nil
}

// HighlightedCode is the result for one requested block. Highlight is absent
// when the server cannot color the block: its language is unsupported, it
// exceeds the highlighter's bounds, or the highlighter is saturated or times
// out.
type HighlightedCode struct {
	Highlight *Highlight `json:"highlight,omitempty"`
}

// HighlightCodeResult holds one result per requested block, in order.
type HighlightCodeResult struct {
	Blocks []HighlightedCode `json:"blocks"`
}

// Validate checks the result against the request it answers.
func (r HighlightCodeResult) Validate(input HighlightCodeInput) error {
	if r.Blocks == nil || len(r.Blocks) != len(input.Blocks) {
		return fmt.Errorf("highlight result has %d blocks for %d requested", len(r.Blocks), len(input.Blocks))
	}
	for i, block := range r.Blocks {
		if block.Highlight != nil {
			if err := block.Highlight.ValidateText(input.Blocks[i].Source); err != nil {
				return fmt.Errorf("highlight block %d: %w", i, err)
			}
		}
	}
	return nil
}

// FileDiffHighlight colors the lines of a file diff page. Old covers the
// page's lines that have an old line number (context and deletions), and New
// those with a new line number (context and additions), each in page order.
// Each side was highlighted from its whole file, so constructs that span
// lines are colored correctly. A side is absent when it could not be
// highlighted.
type FileDiffHighlight struct {
	Old *Highlight `json:"old,omitempty"`
	New *Highlight `json:"new,omitempty"`
}

// DiffSideLines returns the contents of the page's lines on each side, in
// page order: the lines a FileDiffHighlight side describes.
func (p FileDiffPage) DiffSideLines() (old, new []string) {
	old, new = []string{}, []string{}
	for _, hunk := range p.Hunks {
		for _, line := range hunk.Lines {
			if line.OldLine != nil {
				old = append(old, line.Content)
			}
			if line.NewLine != nil {
				new = append(new, line.Content)
			}
		}
	}
	return old, new
}

// Validate checks the highlight against the page it colors.
func (h FileDiffHighlight) Validate(page FileDiffPage) error {
	if h.Old == nil && h.New == nil {
		return fmt.Errorf("file diff highlight has no side")
	}
	old, new := page.DiffSideLines()
	if h.Old != nil {
		if err := h.Old.ValidateLines(old); err != nil {
			return fmt.Errorf("old side: %w", err)
		}
	}
	if h.New != nil {
		if err := h.New.ValidateLines(new); err != nil {
			return fmt.Errorf("new side: %w", err)
		}
	}
	return nil
}
