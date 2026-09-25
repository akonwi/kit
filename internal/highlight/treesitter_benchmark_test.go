package highlight

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func BenchmarkNativeTreeSitter(b *testing.B) {
	b.Run("cold_parser_and_query_10KiB", func(b *testing.B) {
		source := benchmarkGoSource(10 << 10)
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for range b.N {
			var engine treeSitterEngine
			result := engine.highlight(context.Background(), Request{Language: "go", Source: source})
			engine.close()
			if result.Err != nil {
				b.Fatal(result.Err)
			}
		}
	})
	for _, size := range []int{10 << 10, 100 << 10, (1 << 20) - 4096} {
		b.Run(fmt.Sprintf("warm_%dKiB", size>>10), func(b *testing.B) {
			source := benchmarkGoSource(size)
			var engine treeSitterEngine
			defer engine.close()
			expectBoundedFallback := size > MaxSourceBytes
			result := engine.highlight(context.Background(), Request{Language: "go", Source: source})
			if result.Err != nil && !expectBoundedFallback {
				b.Fatal(result.Err)
			}
			if result.Fallback {
				b.ReportMetric(1, "fallback/op")
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(source)))
			b.ResetTimer()
			for range b.N {
				result = engine.highlight(context.Background(), Request{Language: "go", Source: source})
				if result.Err != nil && !expectBoundedFallback {
					b.Fatal(result.Err)
				}
			}
		})
	}
	b.Run("malformed_multiline", func(b *testing.B) {
		source := strings.Repeat("func broken( { /* unterminated\nconst x = `open\n", 200)
		var engine treeSitterEngine
		defer engine.close()
		b.ReportAllocs()
		b.SetBytes(int64(len(source)))
		for range b.N {
			if result := engine.highlight(context.Background(), Request{Language: "go", Source: source}); result.Err != nil {
				b.Fatal(result.Err)
			}
		}
	})
	for _, scenario := range []struct{ language, name, source string }{
		{"javascript", "jsx", "export const App = () => <main><strong>hello</strong></main>;\n"},
		{"markdown", "markdown_injection", "```go\npackage p\n```\n"},
		{"html", "html_injection", "<style>.x { color: red }</style><script>const x = 1</script>\n"},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			var engine treeSitterEngine
			defer engine.close()
			request := Request{Language: scenario.language, Source: scenario.source}
			b.ReportAllocs()
			for range b.N {
				_ = engine.highlight(context.Background(), request)
			}
		})
	}
}

func BenchmarkNativeTreeSitterRetainedHeap(b *testing.B) {
	source := benchmarkGoSource(100 << 10)
	var engine treeSitterEngine
	defer engine.close()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range 4 {
		if result := engine.highlight(context.Background(), Request{Language: "go", Source: source}); result.Err != nil {
			b.Fatal(result.Err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	b.ReportMetric(float64(retained), "retained-B")
}

func benchmarkGoSource(size int) string {
	const header = "package benchmark\n\n"
	const line = "func generated() int { // representative line\n    return 42\n}\n"
	var source strings.Builder
	source.Grow(size)
	source.WriteString(header)
	for source.Len()+len(line) <= size {
		source.WriteString(line)
	}
	source.WriteString(strings.Repeat(" ", size-source.Len()))
	return source.String()
}
