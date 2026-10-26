package highlight

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// A parse cancelled while it balances its tree left the parser aborting the
// process on its next parse, as closing a long Markdown file's tab while it
// was highlighted did. A parser whose parse stopped is replaced.
func TestNativeTreeSitterRecoversFromAParseCancelledWhileBalancing(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	grammar, err := engine.grammar("go")
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("package main\n" + strings.Repeat("var a = 1\n", 20_000))
	if err := engine.parser.SetLanguage(grammar.spec.language); err != nil {
		t.Fatal(err)
	}
	// Every byte is read before the tree is balanced, so this stops the
	// parse while it balances.
	stopped := engine.parser.ParseWithOptions(func(offset int, _ sitter.Point) []byte {
		if offset >= len(source) {
			return nil
		}
		return source[offset:]
	}, nil, &sitter.ParseOptions{ProgressCallback: func(state sitter.ParseState) bool {
		return state.CurrentByteOffset >= uint32(len(source))
	}})
	if stopped != nil {
		stopped.Close()
		t.Fatal("the parse was not stopped while balancing")
	}
	engine.replaceParser()

	result := engine.highlight(context.Background(), Request{Language: "go", Path: "main.go", Source: "package main\n"})
	if result.Err != nil || len(result.Spans) == 0 {
		t.Fatalf("highlight after a stopped parse = %v with %d spans", result.Err, len(result.Spans))
	}
}
