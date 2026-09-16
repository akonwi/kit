package highlight

import (
	"errors"
	"unicode/utf8"
)

var errInvalidSpans = errors.New("invalid semantic highlight spans")

// Validate returns result unchanged when its source and spans are safe for byte
// slicing, or a sanitized plain fallback otherwise.
func Validate(result Result) Result {
	if result.Source != Sanitize(result.Source) {
		return Plain(Request{Language: result.Language, Source: result.Source}, errInvalidSpans)
	}
	previousEnd := 0
	for _, span := range result.Spans {
		if span.Start < previousEnd || span.Start < 0 || span.End <= span.Start || span.End > len(result.Source) ||
			!utf8.RuneStart(result.Source[span.Start]) || (span.End < len(result.Source) && !utf8.RuneStart(result.Source[span.End])) || !knownRole(span.Role) {
			return Plain(Request{Language: result.Language, Source: result.Source}, errInvalidSpans)
		}
		previousEnd = span.End
	}
	return result
}

func knownRole(role Role) bool {
	switch role {
	case Text, Heading, Bold, Italic, Link, List, Quote, CodeInline, CodeBlock, Strikethrough,
		Comment, String, Escape, Number, Keyword, KeywordType, Function, Operator,
		Variable, Member, Builtin, Type, Punctuation, Tag, TagAttribute, TagDelimiter, Attribute, Label:
		return true
	default:
		return false
	}
}
