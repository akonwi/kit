package highlight

import (
	"context"
	"testing"
)

func TestNativeTreeSitterHighlightsEmbeddedLanguages(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()

	jsx := engine.highlight(context.Background(), Request{Language: "javascript", Source: "const node = <section>hello</section>;"})
	if jsx.Err != nil {
		t.Fatal(jsx.Err)
	}
	assertToken(t, jsx, "const", Keyword)
	assertToken(t, jsx, "section", Tag)

	html := engine.highlight(context.Background(), Request{Language: "html", Source: "<script>const value = 42;</script><style>.x { margin: 2rem }</style>"})
	if html.Err != nil {
		t.Fatal(html.Err)
	}
	assertToken(t, html, "script", Tag)
	assertToken(t, html, "const", Keyword)
	assertToken(t, html, "2", Number)

	markdown := engine.highlight(context.Background(), Request{Language: "markdown", Source: "```go\npackage p\n```\n"})
	if markdown.Err != nil || markdown.Fallback {
		t.Fatalf("Markdown behavior = %+v", markdown)
	}
	assertToken(t, markdown, "package", Keyword)
}

func roleAt(result Result, offset int) Role {
	for _, span := range result.Spans {
		if span.Start <= offset && offset < span.End {
			return span.Role
		}
	}
	return Text
}
