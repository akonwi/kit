package highlight

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNativeTreeSitterCapturesRepresentativeLanguages(t *testing.T) {
	tests := []struct {
		language string
		source   string
		text     string
		role     Role
	}{
		{"ard", "fn greet(name: Str) Str { return \"hi\" }\n", "greet", Function},
		{"go", "package main\nfunc greet(name string) string { return `hi ` + name }\n", "func", Keyword},
		{"javascript", "const view = <section className=\"hero\">hello</section>;\n", "section", Tag},
		{"typescript", "interface User { name: string }\n", "interface", Keyword},
		{"tsx", "const view = <section>hello</section>;\n", "section", Tag},
		{"python", "def greet(name: str):\n    # hello\n    return f\"hi {name}\"\n", "# hello", Comment},
		{"rust", "fn main() { let answer: u32 = 42; println!(\"{answer}\"); }\n", "fn", Keyword},
		{"bash", "#!/usr/bin/env bash\nname=world\necho \"hello $name\"\n", "#!/usr/bin/env bash", Comment},
		{"html", "<article class=\"post\">hello</article>\n", "article", Tag},
		{"css", ".post { color: red; margin: 2rem; }\n", "2", Number},
		{"json", "{\"enabled\": true, \"count\": 2}\n", "\"enabled\"", String},
		{"yaml", "enabled: true\n# note\n", "# note", Comment},
		{"toml", "enabled = true\ncount = 2\n", "2", Number},
		{"sql", "SELECT name FROM users WHERE id = 2;\n", "SELECT", Keyword},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			var engine treeSitterEngine
			defer engine.close()
			result := engine.highlight(context.Background(), Request{Language: test.language, Source: test.source})
			if result.Err != nil || result.Fallback {
				t.Fatalf("highlight failed: fallback=%t err=%v", result.Fallback, result.Err)
			}
			assertToken(t, result, test.text, test.role)
		})
	}
}

func TestNativeTreeSitterDisablesUnsupportedPropertyPredicates(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	source := "function f(console) { console.log('x') }"
	result := engine.highlight(context.Background(), Request{Language: "javascript", Source: source})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if offset := strings.LastIndex(source, "console"); roleAt(result, offset) != Variable {
		t.Fatalf("shadowed console role = %q, want variable", roleAt(result, offset))
	}
}

func TestNativeTreeSitterExactArdSpansAndMalformedRecovery(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	source := "// note\nfn greet(name: Str) Str {\n  let ready = true\n  \"hi\"\n}\n"
	result := engine.highlight(context.Background(), Request{Language: "ard", Source: source})
	want := []Span{
		{0, 7, Comment}, {8, 10, Keyword}, {11, 16, Function}, {16, 17, Punctuation},
		{17, 21, Variable}, {21, 22, Punctuation}, {23, 26, Type}, {26, 27, Punctuation},
		{28, 31, Type}, {32, 33, Punctuation}, {36, 39, Keyword}, {39, 45, Variable},
		{46, 47, Operator}, {48, 52, Builtin}, {55, 59, String}, {60, 61, Punctuation},
	}
	if result.Err != nil || !reflect.DeepEqual(result.Spans, want) {
		t.Fatalf("Ard spans = %+v, want %+v, err %v", result.Spans, want, result.Err)
	}

	malformed := "fn broken(name: Str { let ready = false\n// tail\n"
	result = engine.highlight(context.Background(), Request{Language: "ard", Source: malformed})
	if result.Err != nil || result.Source != malformed {
		t.Fatalf("malformed Ard result = %+v", result)
	}
	assertToken(t, result, "Str", Type)
	assertToken(t, result, "false", Builtin)
	assertToken(t, result, "// tail", Comment)
}

func TestNativeTreeSitterDetectsArdFiles(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	result := engine.highlight(context.Background(), Request{Path: "examples/main.ard", Source: "fn main() { let ready = true }\n"})
	if result.Err != nil || result.Fallback || result.Language != "ard" {
		t.Fatalf("Ard highlight = %+v", result)
	}
	assertToken(t, result, "fn", Keyword)
	assertToken(t, result, "main", Function)
	assertToken(t, result, "true", Builtin)
}

func TestNativeTreeSitterEvaluatesQueryPredicates(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	result := engine.highlight(context.Background(), Request{Language: "go", Source: "package p\nfunc f() { append(nil, 1); custom() }\n"})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	assertToken(t, result, "append", Builtin)
	assertToken(t, result, "custom", Function)
}

func TestNativeTreeSitterExactGoSpansAndMultilineRecovery(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	exact := engine.highlight(context.Background(), Request{Language: "go", Source: "package p\nfunc f() int { return 42 }\n"})
	want := []Span{{0, 7, Keyword}, {10, 14, Keyword}, {15, 16, Function}, {19, 22, Type}, {25, 31, Keyword}, {32, 34, Number}}
	if exact.Err != nil || !reflect.DeepEqual(exact.Spans, want) {
		t.Fatalf("exact spans = %+v, want %+v, err %v", exact.Spans, want, exact.Err)
	}

	source := "package p\n/* first\nsecond */\nfunc broken( { return \"open\n"
	result := engine.highlight(context.Background(), Request{Language: "go", Source: source})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	assertToken(t, result, "/* first\nsecond */", Comment)
	if result.Source != source {
		t.Fatalf("source changed: %q", result.Source)
	}
}

func TestUnsupportedAndOversizedInputFallsBackToSanitizedText(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	unsupported := engine.highlight(context.Background(), Request{Language: "unknown", Source: "const x = 1\u202e"})
	if !unsupported.Fallback || unsupported.Source != "const x = 1�" || unsupported.Err == nil {
		t.Fatalf("unsupported result = %+v", unsupported)
	}
	oversized := engine.highlight(context.Background(), Request{Language: "go", Source: strings.Repeat("x", MaxSourceBytes+1)})
	if !oversized.Fallback || oversized.Err == nil {
		t.Fatalf("oversized result = %+v", oversized)
	}
}

func TestNativeTreeSitterRepeatedGrammarLifecycle(t *testing.T) {
	fixtures := []Request{
		{Language: "ard", Source: "fn main() {}\n"},
		{Language: "go", Source: "package p\n"},
		{Language: "tsx", Source: "const x = <div />\n"},
		{Language: "markdown", Source: "```go\npackage p\n```\n"},
		{Language: "html", Source: "<script>const x = 1</script>"},
		{Language: "sql", Source: "SELECT 1;"},
	}
	var engine treeSitterEngine
	for index := range 100 {
		result := engine.highlight(context.Background(), fixtures[index%len(fixtures)])
		if result.Err != nil {
			engine.close()
			t.Fatalf("iteration %d: %v", index, result.Err)
		}
	}
	engine.close()
}

func TestSchedulerCancellationAndCache(t *testing.T) {
	scheduler := NewScheduler(1, 1, 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := scheduler.Highlight(ctx, Request{Language: "go", Source: "package p"})
	if !result.Fallback || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("canceled result = %+v", result)
	}
	request := Request{Language: "go", Source: "package p\n", Revision: "one"}
	first := scheduler.Highlight(context.Background(), request)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cached := scheduler.Highlight(ctx, request)
	if cached.Err != nil || cached.Fallback || len(cached.Spans) == 0 {
		t.Fatalf("cached result = %+v", cached)
	}
	if _, ok := scheduler.cache.get(cacheKeyFor(request)); !ok {
		t.Fatal("successful result was not cached")
	}
	scheduler.Close()
	closed := scheduler.Highlight(context.Background(), request)
	if !closed.Fallback || !errors.Is(closed.Err, errSchedulerClosed) {
		t.Fatalf("closed scheduler result = %+v", closed)
	}
}

func TestActiveParseCancellationFallsBackPromptly(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Result, 1)
	go func() {
		result <- engine.highlight(ctx, Request{Language: "go", Source: benchmarkGoSource(100 << 10)})
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case got := <-result:
		if !got.Fallback || got.Err == nil {
			t.Fatalf("active cancellation result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("active native parse ignored cancellation")
	}
}

func TestActiveQueryDeadlineFallsBackPromptly(t *testing.T) {
	var engine treeSitterEngine
	defer engine.close()
	grammar, err := engine.grammar("go")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.parser.SetLanguage(grammar.spec.language); err != nil {
		t.Fatal(err)
	}
	source := []byte(benchmarkGoSource(400 << 10))
	tree := engine.parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("fixture parse stopped")
	}
	defer tree.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = collectQueryCaptures(ctx, grammar.highlights, tree.RootNode(), source, 0, 0, &highlightBudget{})
	if err == nil {
		t.Fatal("query deadline returned a partial successful result")
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("query cancellation took %s", elapsed)
	}
}

func TestCaptureSweepHonorsPriorityAndCancellation(t *testing.T) {
	captures := []captureSpan{
		{Span: Span{Start: 0, End: 10, Role: Comment}, priority: 1},
		{Span: Span{Start: 2, End: 5, Role: String}, priority: 2},
	}
	got, err := flattenCaptures(context.Background(), 10, captures)
	want := []Span{{0, 2, Comment}, {2, 5, String}, {5, 10, Comment}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("capture sweep = %+v, want %+v, err %v", got, want, err)
	}
	deep, err := flattenCaptures(context.Background(), 10, []captureSpan{
		{Span: Span{Start: 3, End: 4, Role: Number}, depth: 0, priority: 100},
		{Span: Span{Start: 0, End: 10, Role: String}, depth: 1, priority: 1},
	})
	if err != nil || !reflect.DeepEqual(deep, []Span{{0, 10, String}}) {
		t.Fatalf("injection precedence = %+v, want deeper span, err %v", deep, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := flattenCaptures(ctx, 10, captures); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture sweep error = %v", err)
	}
}

func TestValidateRejectsUnsafeBackendSpans(t *testing.T) {
	for _, result := range []Result{
		{Source: "é", Spans: []Span{{Start: 1, End: 2, Role: Keyword}}},
		{Source: "safe", Spans: []Span{{Start: -1, End: 2, Role: Keyword}}},
		{Source: "safe", Spans: []Span{{Start: 0, End: 2, Role: Role("unknown")}}},
	} {
		got := Validate(result)
		if !got.Fallback || !errors.Is(got.Err, errInvalidSpans) {
			t.Fatalf("invalid span result = %+v", got)
		}
	}
}

func assertToken(t *testing.T, result Result, text string, role Role) {
	t.Helper()
	start := strings.Index(result.Source, text)
	if start < 0 {
		t.Fatalf("%q absent from source", text)
	}
	end := start + len(text)
	for _, span := range result.Spans {
		if span.Start <= start && span.End >= end && span.Role == role {
			return
		}
	}
	t.Fatalf("token %q role %q not found in spans: %+v", text, role, result.Spans)
}
