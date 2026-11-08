package highlight

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// LineSpan assigns Role to Length UTF-16 code units of one line of delivered
// text, starting Start code units into the line.
type LineSpan struct {
	Start  int
	Length int
	Role   Role
}

// Supported reports whether language, or the language detected from path,
// has a grammar, so a caller can skip the scheduler for text it cannot color.
func Supported(language, path string) bool {
	_, ok := nativeGrammars[DetectLanguage(language, path)]
	return ok
}

// Lines maps result's spans, which index the sanitized Result.Source, onto
// delivered, the text that was sanitized to produce it. It returns one entry
// per line of delivered, split at "\n" with a final "\n" ending the last line
// rather than starting another, each holding that line's styled ranges in
// UTF-16 code units, ordered and non-overlapping.
//
// A span edge that falls inside the expansion of one delivered character,
// such as within the spaces a tab became, cannot be mapped, so that span is
// dropped. The "\r" of a "\r\n" line ending is never styled. Lines reports
// false for a fallback result, invalid UTF-8, or a result whose source is not
// the sanitized delivered text.
func Lines(delivered string, result Result) ([][]LineSpan, bool) {
	if result.Fallback || result.Err != nil || !utf8.ValidString(delivered) {
		return nil, false
	}
	m := lineMapper{delivered: delivered, sanitized: result.Source}
	lines := make([][]LineSpan, lineCount(delivered))
	previousEnd := 0
	var pending []pendingSpan
	for _, span := range result.Spans {
		if span.Start < previousEnd || span.End <= span.Start || span.End > len(result.Source) {
			return nil, false
		}
		previousEnd = span.End
		pending = pending[:0]
		mapped, ok := m.mapSpan(span, &pending)
		if !ok {
			return nil, false
		}
		if !mapped {
			continue
		}
		for _, segment := range pending {
			if segment.line < len(lines) {
				lines[segment.line] = append(lines[segment.line], segment.span)
			}
		}
	}
	if !m.finish() {
		return nil, false
	}
	return lines, true
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

type pendingSpan struct {
	line int
	span LineSpan
}

// lineMapper walks delivered text once, in step with its sanitized image.
// Each delivered character has an image in the sanitized text: itself, four
// spaces for a tab, U+FFFD for a control character, or nothing for the "\r"
// of "\r\n". The mapper checks every image against the sanitized source, so
// a mismatched result is rejected rather than mis-mapped.
type lineMapper struct {
	delivered string
	sanitized string
	// next is the byte offset of the next delivered character, and offset
	// the sanitized offset of its image.
	next   int
	offset int
	line   int
	column int
	failed bool
}

const replacement = "\uFFFD"

// peek returns the next delivered character, its encoded size, its
// sanitized image, and its width in UTF-16 code units.
func (m *lineMapper) peek() (rune, int, string, int) {
	r, size := utf8.DecodeRuneInString(m.delivered[m.next:])
	width := 1
	if r > 0xFFFF {
		width = 2
	}
	switch {
	case r == '\n':
		return r, size, "\n", width
	case r == '\r' && strings.HasPrefix(m.delivered[m.next+size:], "\n"):
		return r, size, "", width
	case r == '\t':
		return r, size, "    ", width
	case r == '\r' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		return r, size, replacement, width
	default:
		return r, size, m.delivered[m.next : m.next+size], width
	}
}

// advance consumes the next delivered character, checking its image.
func (m *lineMapper) advance() (rune, string, bool) {
	r, size, image, width := m.peek()
	if !strings.HasPrefix(m.sanitized[m.offset:], image) {
		m.failed = true
		return r, image, false
	}
	m.next += size
	m.offset += len(image)
	if r == '\n' {
		m.line++
		m.column = 0
	} else {
		m.column += width
	}
	return r, image, true
}

func (m *lineMapper) done() bool { return m.next >= len(m.delivered) }

// mapSpan appends span's per-line segments to segments. It reports whether
// the span could be mapped, and false for ok when the walk found the result
// does not match the delivered text.
func (m *lineMapper) mapSpan(span Span, segments *[]pendingSpan) (mapped bool, ok bool) {
	for m.offset < span.Start && !m.done() {
		if _, _, ok := m.advance(); !ok {
			return false, false
		}
	}
	if m.offset != span.Start {
		// The start falls inside one character's image, or past the end.
		return false, !m.failed && m.offset > span.Start
	}
	// A span starting where "\r\n" begins styles the line ending, not "\r".
	for !m.done() {
		if _, _, image, _ := m.peek(); image != "" {
			break
		}
		if _, _, ok := m.advance(); !ok {
			return false, false
		}
	}
	line, start, end := m.line, m.column, m.column
	for m.offset < span.End && !m.done() {
		r, image, ok := m.advance()
		if !ok {
			return false, false
		}
		switch {
		case r == '\n':
			if end > start {
				*segments = append(*segments, pendingSpan{line: line, span: LineSpan{Start: start, Length: end - start, Role: span.Role}})
			}
			line, start, end = m.line, 0, 0
		case image != "":
			end = m.column
		}
	}
	if m.offset != span.End {
		*segments = (*segments)[:0]
		return false, !m.failed && m.offset > span.End
	}
	if end > start {
		*segments = append(*segments, pendingSpan{line: line, span: LineSpan{Start: start, Length: end - start, Role: span.Role}})
	}
	return true, true
}

// finish walks the rest of the text, so a result whose source differs from
// the sanitized delivered text after its last span is rejected too.
func (m *lineMapper) finish() bool {
	for !m.done() {
		if _, _, ok := m.advance(); !ok {
			return false
		}
	}
	return !m.failed && m.offset == len(m.sanitized)
}
