// Package markdown parses Markdown into renderer-neutral semantic blocks.
package markdown

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const (
	maxMarkdownSourceBytes     = 256 << 10
	maxMarkdownSourceLines     = 10_000
	maxMarkdownContainerDepth  = 64
	maxMarkdownContainerLayers = 4_096
	maxMarkdownInlineDepth     = 64
	maxMarkdownInlineNodes     = 10_000
	maxMarkdownStructuralNodes = 20_000
)

// BlockKind identifies a Markdown block's semantic role.
type BlockKind uint8

const (
	BlockParagraph BlockKind = iota
	BlockHeading
	BlockCode
	BlockQuote
	BlockRule
	BlockTable
)

// MarkerKind identifies a list item's marker without choosing a renderer glyph.
type MarkerKind uint8

const (
	MarkerNone MarkerKind = iota
	MarkerBullet
	MarkerOrdered
)

// ContainerKind identifies one ordered block-container layer.
type ContainerKind uint8

const (
	ContainerQuote ContainerKind = iota
	ContainerListItem
)

// Container preserves the outer-to-inner order of quote and list-item layers.
type Container struct {
	Kind        ContainerKind
	QuoteID     int
	ListGroupID int
	ListID      int
	ListItemID  int
	Loose       bool
	Marker      MarkerKind
	Number      int
	Task        bool
	Checked     bool
	MarkerWidth int
}

// Alignment is a Markdown table column's requested alignment.
type Alignment uint8

const (
	AlignDefault Alignment = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// Run is one semantically styled inline text run.
type Run struct {
	Text          string
	Bold          bool
	Italic        bool
	Code          bool
	Strikethrough bool
	Link          string
	Image         bool
	RawHTML       bool
}

// TableCell is one table cell's inline content.
type TableCell struct {
	Runs []Run
}

// TableRow is one Markdown table row.
type TableRow struct {
	Header bool
	Cells  []TableCell
}

// Block is one renderer-neutral Markdown block.
type Block struct {
	Kind         BlockKind
	HeadingLevel int
	Language     string
	Code         string
	Containers   []Container
	Runs         []Run
	Rows         []TableRow
	Alignments   []Alignment
}

// Document is a parsed Markdown document.
type Document struct {
	Blocks            []Block
	PlainTextFallback bool
}

// Parser owns an immutable Goldmark parser configured for CommonMark and GFM.
type Parser struct {
	markdown goldmark.Markdown
}

// NewParser creates a CommonMark parser with GFM tables, task lists,
// strikethrough, and autolinks.
func NewParser() *Parser {
	return &Parser{markdown: goldmark.New(goldmark.WithExtensions(extension.GFM))}
}

var defaultParser = NewParser()

// Parse parses source with the package's shared parser.
func Parse(source string) Document {
	return defaultParser.Parse(source)
}

// Parse converts source into copied semantic data that does not retain
// Goldmark source segments.
func (p *Parser) Parse(source string) Document {
	if markdownExceedsComplexityBudget(source) {
		return plainTextDocument(source)
	}
	if p == nil {
		p = defaultParser
	}
	src := []byte(source)
	document := p.markdown.Parser().Parse(text.NewReader(src))
	walker := parserWalker{source: src, blocks: make([]Block, 0, 16)}
	walker.container(document, blockContext{})
	if walker.complexityExceeded {
		return plainTextDocument(source)
	}
	return Document{Blocks: walker.blocks}
}

type blockContext struct {
	containers []Container
	listGroup  int
}

type parserWalker struct {
	source             []byte
	blocks             []Block
	nextQuoteID        int
	nextListID         int
	nextListItem       int
	inlineNodes        int
	structuralNodes    int
	complexityExceeded bool
}

func (w *parserWalker) container(node ast.Node, context blockContext) {
	for child := node.FirstChild(); child != nil && !w.complexityExceeded; child = child.NextSibling() {
		w.block(child, context)
	}
}

func (w *parserWalker) block(node ast.Node, context blockContext) {
	if w.complexityExceeded {
		return
	}
	if !w.consumeStructuralNodes(1) {
		return
	}
	containers := context.containers
	switch typed := node.(type) {
	case *ast.Heading:
		w.emit(Block{
			Kind: BlockHeading, HeadingLevel: typed.Level,
			Containers: cloneContainers(containers), Runs: w.inlines(node),
		})
	case *ast.Paragraph, *ast.TextBlock:
		kind := BlockParagraph
		if containersHaveKind(containers, ContainerQuote) {
			kind = BlockQuote
		}
		w.emit(Block{Kind: kind, Containers: cloneContainers(containers), Runs: w.inlines(node)})
	case *ast.FencedCodeBlock:
		w.emit(Block{
			Kind: BlockCode, Language: strings.TrimSpace(string(typed.Language(w.source))),
			Code: w.rawLines(node), Containers: cloneContainers(containers),
		})
	case *ast.CodeBlock:
		w.emit(Block{Kind: BlockCode, Code: w.rawLines(node), Containers: cloneContainers(containers)})
	case *ast.Blockquote:
		w.nextQuoteID++
		next := blockContext{
			containers: append(containers, Container{Kind: ContainerQuote, QuoteID: w.nextQuoteID}),
			listGroup:  context.listGroup,
		}
		w.container(node, next)
	case *ast.List:
		w.list(typed, context)
	case *ast.ThematicBreak:
		w.emit(Block{Kind: BlockRule, Containers: cloneContainers(containers)})
	case *extast.Table:
		w.table(typed, context)
	case *ast.HTMLBlock:
		w.emit(Block{
			Kind: BlockParagraph, Containers: cloneContainers(containers),
			Runs: []Run{{Text: w.rawLines(node), RawHTML: true}},
		})
	default:
		if node.Type() == ast.TypeBlock {
			w.container(node, context)
		}
	}
}

func (w *parserWalker) consumeStructuralNodes(count int) bool {
	w.structuralNodes += count
	if w.structuralNodes > maxMarkdownStructuralNodes {
		w.complexityExceeded = true
		return false
	}
	return true
}

func (w *parserWalker) list(list *ast.List, context blockContext) {
	w.nextListID++
	listID := w.nextListID
	groupID := context.listGroup
	if groupID == 0 {
		groupID = listID
	}
	type itemSpec struct {
		node    ast.Node
		marker  MarkerKind
		number  int
		task    bool
		checked bool
	}
	items := make([]itemSpec, 0, list.ChildCount())
	number := list.Start
	if number == 0 {
		number = 1
	}
	markerWidth := 1
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if !w.consumeStructuralNodes(1) {
			return
		}
		spec := itemSpec{node: item, marker: MarkerBullet}
		if list.IsOrdered() {
			spec.marker = MarkerOrdered
			spec.number = number
			number++
		}
		spec.checked, spec.task = taskState(item)
		width := 1
		if spec.marker == MarkerOrdered {
			width = utf8.RuneCountInString(strconv.Itoa(spec.number)) + 1
			if spec.task {
				width += 2
			}
		}
		markerWidth = max(markerWidth, width)
		items = append(items, spec)
	}

	for _, item := range items {
		w.nextListItem++
		itemID := w.nextListItem
		listContainer := Container{
			Kind: ContainerListItem, ListGroupID: groupID,
			ListID: listID, ListItemID: itemID, Loose: !list.IsTight,
			Marker: item.marker, Number: item.number, Task: item.task,
			Checked: item.checked, MarkerWidth: markerWidth,
		}
		itemContext := blockContext{
			containers: append(context.containers, listContainer),
			listGroup:  groupID,
		}
		start := len(w.blocks)
		for child := item.node.FirstChild(); child != nil; child = child.NextSibling() {
			w.block(child, itemContext)
		}
		if len(w.blocks) == start {
			w.blocks = append(w.blocks, Block{
				Kind: BlockParagraph, Containers: cloneContainers(itemContext.containers),
			})
		}
		first := true
		for blockIndex := start; blockIndex < len(w.blocks); blockIndex++ {
			for containerIndex := range w.blocks[blockIndex].Containers {
				container := &w.blocks[blockIndex].Containers[containerIndex]
				if container.Kind != ContainerListItem || container.ListItemID != itemID {
					continue
				}
				if first {
					first = false
				} else {
					container.Marker = MarkerNone
					container.Number = 0
					container.Task = false
					container.Checked = false
				}
				break
			}
		}
	}
}

func (w *parserWalker) table(table *extast.Table, context blockContext) {
	block := Block{
		Kind: BlockTable, Containers: cloneContainers(context.containers),
		Alignments: make([]Alignment, len(table.Alignments)),
	}
	for index, alignment := range table.Alignments {
		switch alignment {
		case extast.AlignLeft:
			block.Alignments[index] = AlignLeft
		case extast.AlignCenter:
			block.Alignments[index] = AlignCenter
		case extast.AlignRight:
			block.Alignments[index] = AlignRight
		default:
			block.Alignments[index] = AlignDefault
		}
	}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		if !w.consumeStructuralNodes(1) {
			return
		}
		_, header := row.(*extast.TableHeader)
		parsed := TableRow{Header: header, Cells: make([]TableCell, 0, row.ChildCount())}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			if !w.consumeStructuralNodes(1) {
				return
			}
			parsed.Cells = append(parsed.Cells, TableCell{Runs: w.inlines(cell)})
			if w.complexityExceeded {
				return
			}
		}
		block.Rows = append(block.Rows, parsed)
	}
	w.emit(block)
}

func (w *parserWalker) inlines(node ast.Node) []Run {
	runs := make([]Run, 0, 8)
	w.inline(node, Run{}, &runs, 0)
	if w.complexityExceeded {
		return nil
	}
	return coalesceRuns(runs)
}

func (w *parserWalker) inline(node ast.Node, style Run, runs *[]Run, depth int) {
	if depth > maxMarkdownInlineDepth {
		w.complexityExceeded = true
		return
	}
	for child := node.FirstChild(); child != nil && !w.complexityExceeded; child = child.NextSibling() {
		w.inlineNodes++
		if w.inlineNodes > maxMarkdownInlineNodes {
			w.complexityExceeded = true
			return
		}
		switch typed := child.(type) {
		case *ast.Text:
			run := style
			run.Text = string(typed.Segment.Value(w.source))
			pushRun(runs, run)
			if typed.HardLineBreak() || typed.SoftLineBreak() {
				lineBreak := style
				lineBreak.Text = "\n"
				pushRun(runs, lineBreak)
			}
		case *ast.String:
			run := style
			run.Text = string(typed.Value)
			pushRun(runs, run)
		case *ast.CodeSpan:
			run := style
			run.Code = true
			run.Text = w.codeSpanText(typed)
			pushRun(runs, run)
		case *ast.Emphasis:
			next := style
			if typed.Level >= 2 {
				next.Bold = true
			} else {
				next.Italic = true
			}
			w.inline(child, next, runs, depth+1)
		case *extast.Strikethrough:
			next := style
			next.Strikethrough = true
			w.inline(child, next, runs, depth+1)
		case *ast.Link:
			next := style
			next.Link = string(typed.Destination)
			start := len(*runs)
			w.inline(child, next, runs, depth+1)
			if len(*runs) == start {
				*runs = append(*runs, Run{Link: next.Link})
			}
		case *ast.AutoLink:
			target := string(typed.URL(w.source))
			if typed.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(target), "mailto:") {
				target = "mailto:" + target
			}
			run := style
			run.Text = string(typed.Label(w.source))
			run.Link = target
			pushRun(runs, run)
		case *ast.Image:
			next := style
			next.Link = string(typed.Destination)
			next.Image = true
			start := len(*runs)
			w.inline(child, next, runs, depth+1)
			if len(*runs) == start {
				*runs = append(*runs, Run{Link: next.Link, Image: true})
			}
		case *ast.RawHTML:
			run := style
			run.Text = string(typed.Segments.Value(w.source))
			run.RawHTML = true
			pushRun(runs, run)
		case *extast.TaskCheckBox:
			// The block-level list marker carries task state.
		default:
			w.inline(child, style, runs, depth+1)
		}
	}
}

func (w *parserWalker) rawLines(node ast.Node) string {
	var builder strings.Builder
	lines := node.Lines()
	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		builder.Write(segment.Value(w.source))
	}
	content := strings.ReplaceAll(builder.String(), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSuffix(content, "\n")
}

func cloneContainers(containers []Container) []Container {
	return append([]Container(nil), containers...)
}

func containersHaveKind(containers []Container, kind ContainerKind) bool {
	for _, container := range containers {
		if container.Kind == kind {
			return true
		}
	}
	return false
}

func (w *parserWalker) emit(block Block) {
	if block.Kind != BlockRule && block.Kind != BlockCode && block.Kind != BlockTable && len(block.Runs) == 0 {
		return
	}
	w.blocks = append(w.blocks, block)
}

func pushRun(runs *[]Run, run Run) {
	if run.Text != "" {
		*runs = append(*runs, run)
	}
}

func coalesceRuns(runs []Run) []Run {
	result := make([]Run, 0, len(runs))
	for start := 0; start < len(runs); {
		end := start + 1
		var text strings.Builder
		text.WriteString(runs[start].Text)
		for end < len(runs) && sameRunStyle(runs[start], runs[end]) {
			text.WriteString(runs[end].Text)
			end++
		}
		run := runs[start]
		run.Text = text.String()
		if run.Text != "" || run.Link != "" || run.Image {
			result = append(result, run)
		}
		start = end
	}
	return result
}

func sameRunStyle(left, right Run) bool {
	left.Text = ""
	right.Text = ""
	return left == right
}

func taskState(item ast.Node) (bool, bool) {
	first := item.FirstChild()
	if first == nil {
		return false, false
	}
	for child := first.FirstChild(); child != nil; child = child.NextSibling() {
		if checkbox, ok := child.(*extast.TaskCheckBox); ok {
			return checkbox.IsChecked, true
		}
	}
	return false, false
}

func plainTextDocument(source string) Document {
	return Document{
		Blocks:            []Block{{Kind: BlockParagraph, Runs: []Run{{Text: source}}}},
		PlainTextFallback: true,
	}
}

func markdownExceedsComplexityBudget(source string) bool {
	if len(source) > maxMarkdownSourceBytes {
		return true
	}
	lines := 0
	containerLayers := 0
	for start := 0; start < len(source); {
		end := strings.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		lines++
		depth := markdownContainerPrefixDepth(source[start:end])
		containerLayers += depth
		if lines > maxMarkdownSourceLines || depth > maxMarkdownContainerDepth || containerLayers > maxMarkdownContainerLayers {
			return true
		}
		if end == len(source) {
			break
		}
		start = end + 1
	}
	return false
}

func markdownContainerPrefixDepth(line string) int {
	depth := 0
	for offset := 0; offset < len(line); {
		spaces := 0
		for offset < len(line) && (line[offset] == ' ' || line[offset] == '\t') {
			if line[offset] == '\t' {
				spaces += 4
			} else {
				spaces++
			}
			offset++
		}
		if offset >= len(line) {
			return depth
		}
		consumed := 0
		switch line[offset] {
		case '>':
			consumed = 1
		case '-', '+', '*':
			if offset+1 < len(line) && (line[offset+1] == ' ' || line[offset+1] == '\t') {
				consumed = 1
			}
		default:
			end := offset
			for end < len(line) && end-offset < 10 && line[end] >= '0' && line[end] <= '9' {
				end++
			}
			if end > offset && end < len(line) && (line[end] == '.' || line[end] == ')') &&
				end+1 < len(line) && (line[end+1] == ' ' || line[end+1] == '\t') {
				consumed = end - offset + 1
			}
		}
		if consumed == 0 {
			return depth
		}
		depth += spaces/2 + 1
		if depth > maxMarkdownContainerDepth {
			return depth
		}
		offset += consumed
	}
	return depth
}

func (w *parserWalker) codeSpanText(span *ast.CodeSpan) string {
	var builder bytes.Buffer
	for child := span.FirstChild(); child != nil; child = child.NextSibling() {
		w.inlineNodes++
		if w.inlineNodes > maxMarkdownInlineNodes {
			w.complexityExceeded = true
			return ""
		}
		textNode := child.(*ast.Text)
		value := textNode.Segment.Value(w.source)
		if bytes.HasSuffix(value, []byte("\n")) {
			builder.Write(value[:len(value)-1])
			builder.WriteByte(' ')
		} else {
			builder.Write(value)
		}
	}
	return builder.String()
}
