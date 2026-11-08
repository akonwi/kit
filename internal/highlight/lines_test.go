package highlight

import (
	"context"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func sanitizedSpan(t *testing.T, delivered, covered string, role Role) Span {
	t.Helper()
	source := Sanitize(delivered)
	start := strings.Index(source, covered)
	if start < 0 {
		t.Fatalf("%q is not in sanitized %q", covered, source)
	}
	return Span{Start: start, End: start + len(covered), Role: role}
}

func TestLinesMapSanitizedSpansToDeliveredText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		delivered string
		spans     func(t *testing.T, delivered string) []Span
		want      [][]LineSpan
	}{
		{
			name:      "tab expands to four spaces",
			delivered: "\tx\t:= \"a\tb\"",
			spans: func(t *testing.T, delivered string) []Span {
				return []Span{sanitizedSpan(t, delivered, "x", Variable), sanitizedSpan(t, delivered, "\"a    b\"", String)}
			},
			want: [][]LineSpan{{{Start: 1, Length: 1, Role: Variable}, {Start: 6, Length: 5, Role: String}}},
		},
		{
			name:      "span edge inside a tab expansion is dropped",
			delivered: "a\tb c",
			spans: func(t *testing.T, delivered string) []Span {
				return []Span{{Start: 3, End: 6, Role: Comment}, sanitizedSpan(t, delivered, "c", Keyword)}
			},
			want: [][]LineSpan{{{Start: 4, Length: 1, Role: Keyword}}},
		},
		{
			name:      "CRLF line endings are not styled",
			delivered: "a // c\r\nb\r\n",
			spans: func(t *testing.T, delivered string) []Span {
				return []Span{sanitizedSpan(t, delivered, "// c\nb", Comment)}
			},
			want: [][]LineSpan{{{Start: 2, Length: 4, Role: Comment}}, {{Start: 0, Length: 1, Role: Comment}}},
		},
		{
			name:      "lone carriage return and controls map back to one character",
			delivered: "a\rb\x1bc\u200bd",
			spans: func(t *testing.T, delivered string) []Span {
				source := Sanitize(delivered)
				b := strings.Index(source, "b")
				d := strings.Index(source, "d")
				return []Span{{Start: b, End: d, Role: String}, {Start: d, End: d + 1, Role: Number}}
			},
			want: [][]LineSpan{{{Start: 2, Length: 4, Role: String}, {Start: 6, Length: 1, Role: Number}}},
		},
		{
			name:      "non-ASCII counts UTF-16 code units",
			delivered: "é = \"日本\" // 😀 ok\n😀x",
			spans: func(t *testing.T, delivered string) []Span {
				return []Span{sanitizedSpan(t, delivered, "\"日本\"", String), sanitizedSpan(t, delivered, "// 😀 ok", Comment), sanitizedSpan(t, delivered, "x", Variable)}
			},
			want: [][]LineSpan{{{Start: 4, Length: 4, Role: String}, {Start: 9, Length: 8, Role: Comment}}, {{Start: 2, Length: 1, Role: Variable}}},
		},
		{
			name:      "span covering only a line ending styles nothing",
			delivered: "a\r\nb",
			spans: func(t *testing.T, delivered string) []Span {
				return []Span{{Start: 1, End: 2, Role: Text}}
			},
			want: [][]LineSpan{nil, nil},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := Result{Source: Sanitize(test.delivered), Language: "go", Spans: test.spans(t, test.delivered)}
			got, ok := Lines(test.delivered, result)
			if !ok {
				t.Fatal("Lines() rejected a matching result")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Lines() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestLinesCountsDeliveredLines(t *testing.T) {
	t.Parallel()
	for delivered, want := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "a\r\nb\r\n": 2, "\n\n": 2} {
		got, ok := Lines(delivered, Result{Source: Sanitize(delivered)})
		if !ok || len(got) != want {
			t.Fatalf("Lines(%q) = %d lines, %v; want %d", delivered, len(got), ok, want)
		}
	}
}

func TestLinesRejectsResultsThatDoNotMatchTheDeliveredText(t *testing.T) {
	t.Parallel()
	for name, result := range map[string]Result{
		"different source": {Source: "a := 2", Spans: []Span{{Start: 0, End: 1, Role: Variable}}},
		"longer source":    {Source: "a := 1\nb"},
		"shorter source":   {Source: "a :="},
		"fallback":         {Source: "a := 1", Fallback: true},
		"overlapping":      {Source: "a := 1", Spans: []Span{{Start: 0, End: 2, Role: Variable}, {Start: 1, End: 3, Role: Operator}}},
		"out of range":     {Source: "a := 1", Spans: []Span{{Start: 5, End: 9, Role: Number}}},
	} {
		if _, ok := Lines("a := 1", result); ok {
			t.Errorf("Lines() accepted %s", name)
		}
	}
}

// referenceLines maps spans by brute force: a delivered character is styled
// when its whole non-empty sanitized image lies in a span whose edges both
// fall on image boundaries. A line break belongs to no line.
func referenceLines(delivered string, result Result) [][]LineSpan {
	type character struct {
		line, column, width int
		start, end          int
		newline             bool
	}
	var characters []character
	line, column, offset := 0, 0, 0
	for i := 0; i < len(delivered); {
		r, size := utf8.DecodeRuneInString(delivered[i:])
		image := Sanitize(delivered[i : i+size])
		if r == '\r' && strings.HasPrefix(delivered[i+size:], "\n") {
			image = ""
		}
		width := len(utf16.Encode([]rune{r}))
		characters = append(characters, character{line: line, column: column, width: width, start: offset, end: offset + len(image), newline: r == '\n'})
		offset += len(image)
		if r == '\n' {
			line, column = line+1, 0
		} else {
			column += width
		}
		i += size
	}
	boundary := map[int]bool{offset: true}
	for _, c := range characters {
		boundary[c.start] = true
	}
	lines := make([][]LineSpan, lineCount(delivered))
	for _, span := range result.Spans {
		if !boundary[span.Start] || !boundary[span.End] {
			continue
		}
		for _, c := range characters {
			if !c.newline && c.end > c.start && c.start >= span.Start && c.end <= span.End {
				lines[c.line] = append(lines[c.line], LineSpan{Start: c.column, Length: c.width, Role: span.Role})
			}
		}
	}
	return lines
}

func TestLinesMatchesReferenceMapping(t *testing.T) {
	t.Parallel()
	pieces := []string{"a", "b", " ", "\t", "\r", "\n", "\r\n", "\x1b", "é", "日", "😀", "\u200b", "\uFFFD", "\x7f"}
	roles := []Role{Keyword, String, Comment}
	random := rand.New(rand.NewSource(62))
	for iteration := 0; iteration < 5000; iteration++ {
		var builder strings.Builder
		for range random.Intn(24) {
			builder.WriteString(pieces[random.Intn(len(pieces))])
		}
		delivered := builder.String()
		source := Sanitize(delivered)
		var starts []int
		for i := range source {
			starts = append(starts, i)
		}
		starts = append(starts, len(source))
		var spans []Span
		for i := 0; i+1 < len(starts); {
			start := i + random.Intn(3)
			end := start + 1 + random.Intn(4)
			if end >= len(starts) {
				break
			}
			spans = append(spans, Span{Start: starts[start], End: starts[end], Role: roles[random.Intn(len(roles))]})
			i = end
		}
		result := Result{Source: source, Spans: spans}
		got, ok := Lines(delivered, result)
		if !ok {
			t.Fatalf("Lines(%q) rejected spans %v", delivered, spans)
		}
		if want := referenceLines(delivered, result); !equalLines(got, want) {
			t.Fatalf("Lines(%q) with spans %v\n got %v\nwant %v", delivered, spans, got, want)
		}
	}
}

// equalLines compares the role of every styled UTF-16 cell, since the
// reference styles one character at a time. Lines also must not overlap.
func equalLines(got, want [][]LineSpan) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		end := 0
		for _, segment := range got[i] {
			if segment.Start < end || segment.Length <= 0 {
				return false
			}
			end = segment.Start + segment.Length
		}
		covered := func(segments []LineSpan) map[int]Role {
			cells := map[int]Role{}
			for _, segment := range segments {
				for column := segment.Start; column < segment.Start+segment.Length; column++ {
					cells[column] = segment.Role
				}
			}
			return cells
		}
		if !reflect.DeepEqual(covered(got[i]), covered(want[i])) {
			return false
		}
	}
	return true
}

func TestLinesMapRealHighlighterOutput(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(1, 1, 0)
	t.Cleanup(scheduler.Close)
	delivered := "package main\r\n\r\nfunc main() {\r\n\tprintln(\"😀\t日本\") // é\x1b\r\n}\r\n"
	result := scheduler.Highlight(context.Background(), Request{Language: "go", Source: delivered})
	if result.Fallback {
		t.Fatalf("Highlight() fell back: %v", result.Err)
	}
	lines, ok := Lines(delivered, result)
	if !ok {
		t.Fatal("Lines() rejected the highlighter's own result")
	}
	text := func(line, start, length int) string {
		units := utf16.Encode([]rune(strings.Split(delivered, "\n")[line]))
		return string(utf16.Decode(units[start : start+length]))
	}
	var got []string
	for number, segments := range lines {
		for _, segment := range segments {
			got = append(got, string(segment.Role)+":"+text(number, segment.Start, segment.Length))
		}
	}
	want := []string{"keyword:package", "keyword:func", "function:main", "builtin:println", "string:\"😀\t日本\"", "comment:// é\x1b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("highlighted ranges = %q, want %q", got, want)
	}
}
