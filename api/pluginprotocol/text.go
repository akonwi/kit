package pluginprotocol

import (
	"fmt"
	"strings"
	"unicode"
)

// Text kinds name the character rules Kit applies to text members plugins
// send. The schema expresses each as a pattern built only from explicit
// character classes, generated from the same Unicode tables Kit uses, so
// ECMA-262 and RE2 validators read them the same way. Shorthand classes such
// as \s differ between engines and are not used. A validator whose $ also
// matches before a final newline, as Python's does, accepts slightly more.
//
// Kit also rejects format characters outside the Basic Multilingual Plane
// and measures lengths in UTF-8 bytes, which a pattern and maxLength cannot
// express exactly. Text a schema accepts can therefore still be rejected;
// text a schema rejects is always rejected.
const (
	// TextLine is single-line text: no control or format characters.
	TextLine = "line"
	// TextLineNonBlank is single-line text with a character other than
	// white space.
	TextLineNonBlank = "line-nonblank"
	// TextMultiline also allows newline and tab.
	TextMultiline = "multiline"
	// TextMultilineNonBlank is multiline text with a character other than
	// white space.
	TextMultilineNonBlank = "multiline-nonblank"
	// TextMessage is message text: no NUL, and a character other than white
	// space.
	TextMessage = "message"
)

// textPattern returns the pattern of a text kind.
func textPattern(kind string) (string, error) {
	line := `\x00-\x1f\x7f-\x9f` + formatCharacters()
	multiline := `\x00-\x08\x0b-\x1f\x7f-\x9f` + formatCharacters()
	nonBlank := func(excluded string) string {
		return `^[^` + excluded + `]*[^` + whiteSpace() + excluded + `][^` + excluded + `]*$`
	}
	switch kind {
	case TextLine:
		return `^[^` + line + `]*$`, nil
	case TextLineNonBlank:
		return nonBlank(line), nil
	case TextMultiline:
		return `^[^` + multiline + `]*$`, nil
	case TextMultilineNonBlank:
		return nonBlank(multiline), nil
	case TextMessage:
		return nonBlank(`\x00`), nil
	}
	return "", fmt.Errorf("unknown text kind %q", kind)
}

// mustTextPattern returns the pattern of a text kind this package defines.
func mustTextPattern(kind string) string {
	pattern, err := textPattern(kind)
	if err != nil {
		panic(err)
	}
	return pattern
}

// formatCharacters lists Unicode format characters (category Cf) in the
// Basic Multilingual Plane for a pattern's class.
func formatCharacters() string { return classOf(unicode.Cf) }

// whiteSpace lists the characters unicode.IsSpace and strings.TrimSpace
// treat as white space, for a pattern's class.
func whiteSpace() string { return classOf(unicode.White_Space) }

// classOf lists the Basic Multilingual Plane characters of table as the
// contents of a pattern's character class: ASCII as \x escapes and other
// characters literally, with consecutive runs as ranges. None of the
// characters is special inside a class.
func classOf(table *unicode.RangeTable) string {
	var runes []rune
	for _, span := range table.R16 {
		for r := rune(span.Lo); r <= rune(span.Hi); r += rune(span.Stride) {
			runes = append(runes, r)
		}
	}
	write := func(class *strings.Builder, r rune) {
		if r < 0x80 {
			fmt.Fprintf(class, `\x%02x`, r)
			return
		}
		class.WriteRune(r)
	}
	var class strings.Builder
	for start := 0; start < len(runes); {
		end := start
		for end+1 < len(runes) && runes[end+1] == runes[end]+1 {
			end++
		}
		write(&class, runes[start])
		if end > start {
			if end > start+1 {
				class.WriteByte('-')
			}
			write(&class, runes[end])
		}
		start = end + 1
	}
	return class.String()
}

// urlPattern is the shape of an address plugins may ask clients to open: an
// absolute http or https URL with a host and no credentials, white space, or
// control characters. Kit canonicalizes and checks it further.
func urlPattern() string {
	excluded := whiteSpace() + `\x00-\x1f\x7f-\x9f`
	return `^[Hh][Tt][Tt][Pp][Ss]?://[^/?#@` + excluded + `]+(?:[/?#][^` + excluded + `]*)?$`
}

// escapeInvisible replaces control, format, and white-space characters
// beyond ASCII in encoded JSON, which occur only inside patterns, with \u
// escapes so the committed schema stays readable. The escapes decode to the
// same characters.
func escapeInvisible(encoded []byte) []byte {
	var out strings.Builder
	for _, r := range string(encoded) {
		if r > 0x7f && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r)) {
			fmt.Fprintf(&out, `\u%04x`, r)
			continue
		}
		out.WriteRune(r)
	}
	return []byte(out.String())
}
