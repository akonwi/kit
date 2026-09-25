package markdown

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
)

func TestParseRichCommonMarkAndGFMBlocks(t *testing.T) {
	t.Parallel()

	document := Parse(`# Heading

Paragraph with **bold**, *italic*, ~~gone~~, and ` + "`code`" + `.

> quoted [docs](https://example.com/docs)

- first
- [x] done
  1. nested

---

` + "```go title=sample\nfmt.Println(\"hello\")\n```" + `

| Name | Value |
| :--- | ---: |
| one | 1 |`)

	if len(document.Blocks) != 9 {
		t.Fatalf("block count = %d, want 9: %#v", len(document.Blocks), document.Blocks)
	}
	if heading := document.Blocks[0]; heading.Kind != BlockHeading || heading.HeadingLevel != 1 || joinedText(heading.Runs) != "Heading" {
		t.Fatalf("heading = %#v", heading)
	}
	paragraph := document.Blocks[1]
	if paragraph.Kind != BlockParagraph || !hasRun(paragraph.Runs, Run{Text: "bold", Bold: true}) ||
		!hasRun(paragraph.Runs, Run{Text: "italic", Italic: true}) ||
		!hasRun(paragraph.Runs, Run{Text: "gone", Strikethrough: true}) ||
		!hasRun(paragraph.Runs, Run{Text: "code", Code: true}) {
		t.Fatalf("paragraph runs = %#v", paragraph.Runs)
	}
	quote := document.Blocks[2]
	if quote.Kind != BlockQuote || countContainers(quote, ContainerQuote) != 1 || !hasLinkedText(quote.Runs, "docs", "https://example.com/docs") {
		t.Fatalf("quote = %#v", quote)
	}
	if item, marker := document.Blocks[3], lastListContainer(document.Blocks[3]); item.Kind != BlockParagraph || marker.ListItemID == 0 || marker.Marker != MarkerBullet {
		t.Fatalf("first list item = %#v", item)
	}
	if item, marker := document.Blocks[4], lastListContainer(document.Blocks[4]); marker.Marker != MarkerBullet || !marker.Task || !marker.Checked || joinedText(item.Runs) != "done" {
		t.Fatalf("task item = %#v", item)
	}
	if item, markers := document.Blocks[5], listContainers(document.Blocks[5]); len(markers) != 2 || markers[1].Marker != MarkerOrdered || markers[1].Number != 1 {
		t.Fatalf("nested ordered item = %#v", item)
	}
	if document.Blocks[6].Kind != BlockRule {
		t.Fatalf("rule = %#v", document.Blocks[6])
	}
	code := document.Blocks[7]
	if code.Kind != BlockCode || code.Language != "go" || code.Code != "fmt.Println(\"hello\")" {
		t.Fatalf("code = %#v", code)
	}
	table := document.Blocks[8]
	if table.Kind != BlockTable || len(table.Rows) != 2 || !table.Rows[0].Header || table.Rows[1].Header ||
		!reflect.DeepEqual(table.Alignments, []Alignment{AlignLeft, AlignRight}) || joinedText(table.Rows[1].Cells[0].Runs) != "one" {
		t.Fatalf("table = %#v", table)
	}
}

func TestParseOrderedListAlignsMultiDigitMarkers(t *testing.T) {
	t.Parallel()

	document := Parse("9. nine\n10. ten\n11. eleven")
	if len(document.Blocks) != 3 {
		t.Fatalf("blocks = %#v", document.Blocks)
	}
	for index, block := range document.Blocks {
		marker := lastListContainer(block)
		if marker.Marker != MarkerOrdered || marker.Number != 9+index || marker.MarkerWidth != 3 {
			t.Fatalf("item %d = %#v", index, block)
		}
	}
}

func TestParseKeepsOrderedContainerPathsAndIndependentTaskState(t *testing.T) {
	t.Parallel()

	document := Parse("- > quoted\n- ```go\n  code\n  ```\n- - nested\n\n1. [x] separate")
	if len(document.Blocks) != 4 {
		t.Fatalf("blocks = %#v", document.Blocks)
	}
	first := document.Blocks[0]
	if first.Kind != BlockQuote || len(first.Containers) != 2 ||
		first.Containers[0].Kind != ContainerListItem || first.Containers[1].Kind != ContainerQuote {
		t.Fatalf("list-then-quote path = %#v", first)
	}
	if code, marker := document.Blocks[1], lastListContainer(document.Blocks[1]); code.Kind != BlockCode || marker.Marker != MarkerBullet {
		t.Fatalf("code list item = %#v", code)
	}
	nested := listContainers(document.Blocks[2])
	if len(nested) != 2 || nested[0].Marker != MarkerBullet || nested[1].Marker != MarkerBullet ||
		nested[0].ListGroupID != nested[1].ListGroupID {
		t.Fatalf("nested list path = %#v", document.Blocks[2])
	}
	separate := lastListContainer(document.Blocks[3])
	if separate.Marker != MarkerOrdered || !separate.Task || !separate.Checked || separate.MarkerWidth != 4 ||
		separate.ListGroupID == nested[0].ListGroupID {
		t.Fatalf("ordered task = %#v", document.Blocks[3])
	}
}

func TestParseDistinguishesQuoteListContainerOrder(t *testing.T) {
	t.Parallel()

	listQuote := Parse("- > item").Blocks[0]
	quoteList := Parse("> - item").Blocks[0]
	if reflect.DeepEqual(containerKinds(listQuote), containerKinds(quoteList)) ||
		!reflect.DeepEqual(containerKinds(listQuote), []ContainerKind{ContainerListItem, ContainerQuote}) ||
		!reflect.DeepEqual(containerKinds(quoteList), []ContainerKind{ContainerQuote, ContainerListItem}) {
		t.Fatalf("container paths = list/quote %#v, quote/list %#v", listQuote.Containers, quoteList.Containers)
	}
}

func TestParsePreservesLooseListParagraphBoundaries(t *testing.T) {
	t.Parallel()

	document := Parse("- first\n\n  second paragraph\n\n- third")
	if len(document.Blocks) != 3 {
		t.Fatalf("blocks = %#v", document.Blocks)
	}
	first := lastListContainer(document.Blocks[0])
	continuation := lastListContainer(document.Blocks[1])
	third := lastListContainer(document.Blocks[2])
	if !first.Loose || !continuation.Loose || !third.Loose ||
		first.Marker != MarkerBullet || continuation.Marker != MarkerNone || third.Marker != MarkerBullet ||
		first.ListItemID != continuation.ListItemID || third.ListItemID == first.ListItemID {
		t.Fatalf("loose list containers = %#v", document.Blocks)
	}
}

func TestParsePreservesFencedCodeTrailingLinesAndNormalizesLineEndings(t *testing.T) {
	t.Parallel()

	document := Parse("```go\r\nx\r\n\r\n\r\n```\r\n")
	if len(document.Blocks) != 1 || document.Blocks[0].Code != "x\n\n" {
		t.Fatalf("code block = %#v", document.Blocks)
	}
}

func TestParsePreservesSoftBreaksAndNormalizesCodeSpanBreaks(t *testing.T) {
	t.Parallel()

	document := Parse("line one\nline two with `a\nb`")
	if got := joinedText(document.Blocks[0].Runs); got != "line one\nline two with a b" {
		t.Fatalf("paragraph text = %q", got)
	}
	if !hasRun(document.Blocks[0].Runs, Run{Text: "a b", Code: true}) {
		t.Fatalf("code span = %#v", document.Blocks[0].Runs)
	}
}

func TestParseCopiesRawHTMLAndImageText(t *testing.T) {
	t.Parallel()

	document := Parse("before <kbd>Enter</kbd> ![diagram](https://example.com/a.png)\n\n<section>raw</section>")
	if len(document.Blocks) != 2 {
		t.Fatalf("blocks = %#v", document.Blocks)
	}
	if got := joinedText(document.Blocks[0].Runs); got != "before <kbd>Enter</kbd> diagram" {
		t.Fatalf("inline HTML/image text = %q", got)
	}
	if !hasRun(document.Blocks[0].Runs, Run{Text: "diagram", Link: "https://example.com/a.png", Image: true}) {
		t.Fatalf("image run = %#v", document.Blocks[0].Runs)
	}
	if got := joinedText(document.Blocks[1].Runs); got != "<section>raw</section>" {
		t.Fatalf("HTML block = %q", got)
	}
}

func TestParseAutolinkKeepsLiteralDestination(t *testing.T) {
	t.Parallel()

	document := Parse("See https://example.com and <hello@example.com>.")
	if !hasLinkedText(document.Blocks[0].Runs, "https://example.com", "https://example.com") ||
		!hasLinkedText(document.Blocks[0].Runs, "hello@example.com", "mailto:hello@example.com") {
		t.Fatalf("autolink runs = %#v", document.Blocks[0].Runs)
	}
}

func TestParsePreservesDestinationOnlyLinksAndImages(t *testing.T) {
	t.Parallel()

	document := Parse("[](https://example.com) ![](https://example.com/image.png)")
	if len(document.Blocks) != 1 || len(document.Blocks[0].Runs) != 3 {
		t.Fatalf("destination-only document = %#v", document)
	}
	if run := document.Blocks[0].Runs[0]; run.Text != "" || run.Link != "https://example.com" || run.Image {
		t.Fatalf("empty link = %#v", run)
	}
	if run := document.Blocks[0].Runs[2]; run.Text != "" || run.Link != "https://example.com/image.png" || !run.Image {
		t.Fatalf("empty image = %#v", run)
	}
}

func TestParseFallsBackBeforePathologicalContainerNesting(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		strings.Repeat(">", maxMarkdownContainerDepth+1) + " deep",
		strings.Repeat("- ", maxMarkdownContainerDepth+1) + "deep",
		strings.Repeat(strings.Repeat("> ", maxMarkdownContainerDepth)+"deep\n", maxMarkdownContainerLayers/maxMarkdownContainerDepth+1),
	} {
		document := Parse(source)
		if !document.PlainTextFallback || len(document.Blocks) != 1 || joinedText(document.Blocks[0].Runs) != source {
			t.Fatalf("complexity fallback = %#v", document)
		}
	}
}

func TestParseFallsBackBeforePathologicalTableStructure(t *testing.T) {
	t.Parallel()

	columns := maxMarkdownStructuralNodes + 1
	source := strings.Repeat("| ", columns) + "|\n" + strings.Repeat("| --- ", columns) + "|\n"
	document := Parse(source)
	if !document.PlainTextFallback || len(document.Blocks) != 1 || joinedText(document.Blocks[0].Runs) != source {
		t.Fatalf("table structural fallback = %t with %d blocks", document.PlainTextFallback, len(document.Blocks))
	}
}

func TestParseFallsBackBeforePathologicalInlineComplexity(t *testing.T) {
	t.Parallel()

	manyNodes := strings.Repeat("[x](https://e.co) ", maxMarkdownInlineNodes/2+1)
	if document := Parse(manyNodes); !document.PlainTextFallback || joinedText(document.Blocks[0].Runs) != manyNodes {
		t.Fatalf("inline node fallback = %t with %d blocks", document.PlainTextFallback, len(document.Blocks))
	}

	root := ast.NewTextBlock()
	var parent ast.Node = root
	for range maxMarkdownInlineDepth + 1 {
		emphasis := ast.NewEmphasis(1)
		parent.AppendChild(parent, emphasis)
		parent = emphasis
	}
	parent.AppendChild(parent, ast.NewString([]byte("x")))
	walker := parserWalker{}
	var runs []Run
	walker.inline(root, Run{}, &runs, 0)
	if !walker.complexityExceeded {
		t.Fatal("inline depth budget was not enforced")
	}
}

func TestParseCoalescesManyTextFragmentsOnce(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("line\n", 9_999)
	document := Parse(source)
	if document.PlainTextFallback || len(document.Blocks) != 1 || len(document.Blocks[0].Runs) != 1 ||
		joinedText(document.Blocks[0].Runs) != strings.TrimSuffix(source, "\n") {
		t.Fatalf("coalesced line document = fallback %t, blocks %d", document.PlainTextFallback, len(document.Blocks))
	}
}

func TestParseEmptyInput(t *testing.T) {
	t.Parallel()
	if document := Parse(""); len(document.Blocks) != 0 {
		t.Fatalf("empty blocks = %#v", document.Blocks)
	}
}

func BenchmarkParseGrowingStreamingResponse(b *testing.B) {
	full := strings.Repeat("Paragraph with **bold text** and `code`.\n\n", 256)
	b.ResetTimer()
	for b.Loop() {
		for end := 256; end <= len(full); end += 256 {
			_ = Parse(full[:end])
		}
	}
}

func BenchmarkParseStreamingResponse(b *testing.B) {
	source := `## Result

The implementation keeps **semantic runs** and [safe links](https://example.com).

` + "```go\nfunc main() { fmt.Println(\"hello\") }\n```\n" + `
- one
- two
- three
`
	for b.Loop() {
		_ = Parse(source)
	}
}

func countContainers(block Block, kind ContainerKind) int {
	count := 0
	for _, container := range block.Containers {
		if container.Kind == kind {
			count++
		}
	}
	return count
}

func containerKinds(block Block) []ContainerKind {
	kinds := make([]ContainerKind, len(block.Containers))
	for index, container := range block.Containers {
		kinds[index] = container.Kind
	}
	return kinds
}

func listContainers(block Block) []Container {
	var result []Container
	for _, container := range block.Containers {
		if container.Kind == ContainerListItem {
			result = append(result, container)
		}
	}
	return result
}

func lastListContainer(block Block) Container {
	containers := listContainers(block)
	if len(containers) == 0 {
		return Container{}
	}
	return containers[len(containers)-1]
}

func joinedText(runs []Run) string {
	result := ""
	for _, run := range runs {
		result += run.Text
	}
	return result
}

func hasLinkedText(runs []Run, text, link string) bool {
	for _, run := range runs {
		if run.Text == text && run.Link == link {
			return true
		}
	}
	return false
}

func hasRun(runs []Run, expected Run) bool {
	for _, run := range runs {
		if reflect.DeepEqual(run, expected) {
			return true
		}
	}
	return false
}
