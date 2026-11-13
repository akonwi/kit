// Package textfind finds a plain-text query in a file's text the way every
// Kit client's find bar does. testdata/find at the repository root holds
// the rules and the cases each client's matcher agrees on; this package is
// the reference for them and the terminal client's matcher.
package textfind

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DefaultLimit is how many matches Find counts when Options.Limit is unset.
const DefaultLimit = 10_000

// Options are the find bar's options.
type Options struct {
	// MatchCase compares code points exactly instead of by simple case
	// folding.
	MatchCase bool
	// WholeWord keeps only matches that don't join a word character on
	// either side.
	WholeWord bool
	// Limit is how many matches to count; zero or less means DefaultLimit.
	Limit int
}

// Range is a match's half-open byte range in the searched text.
type Range struct {
	Start int
	End   int
}

// Result is what Find found.
type Result struct {
	// Matches are in text order and never overlap.
	Matches []Range
	// Capped reports that another match exists past the limit.
	Capped bool
}

// Find returns the matches of query in text, scanning left to right. An
// empty query, or one with a line feed or carriage return, matches nothing.
func Find(text, query string, options Options) Result {
	if query == "" || strings.ContainsAny(query, "\n\r") {
		return Result{}
	}
	limit := options.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	fold := func(r rune) rune { return r }
	if !options.MatchCase {
		fold = simpleFold
	}
	needle := make([]rune, 0, utf8.RuneCountInString(query))
	for _, r := range query {
		needle = append(needle, fold(r))
	}

	var result Result
	for start := 0; start < len(text); {
		first, width := utf8.DecodeRuneInString(text[start:])
		end, ok := -1, fold(first) == needle[0]
		if ok {
			end = matchEnd(text, start+width, needle[1:], fold)
			ok = end >= 0 && (!options.WholeWord || isWholeWord(text, start, end))
		}
		if !ok {
			start += width
			continue
		}
		if len(result.Matches) == limit {
			result.Capped = true
			break
		}
		result.Matches = append(result.Matches, Range{Start: start, End: end})
		start = end
	}
	return result
}

// matchEnd returns where rest ends when text continues with it at offset,
// or -1 when it doesn't.
func matchEnd(text string, offset int, rest []rune, fold func(rune) rune) int {
	for _, want := range rest {
		if offset >= len(text) {
			return -1
		}
		r, width := utf8.DecodeRuneInString(text[offset:])
		if fold(r) != want {
			return -1
		}
		offset += width
	}
	return offset
}

// simpleFold maps r to one member of its simple case folding orbit, the
// same member for every rune in the orbit.
func simpleFold(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z':
		return r - 'a' + 'A'
	case r < utf8.RuneSelf:
		return r
	}
	least := r
	for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
		least = min(least, next)
	}
	return least
}

// isWholeWord reports whether text[start:end] joins no word character
// before or after it.
func isWholeWord(text string, start, end int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		first, _ := utf8.DecodeRuneInString(text[start:])
		if joins(before, first) {
			return false
		}
	}
	if end < len(text) {
		last, _ := utf8.DecodeLastRuneInString(text[:end])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if joins(last, after) {
			return false
		}
	}
	return true
}

// joins reports whether two adjacent code points belong to one word.
func joins(a, b rune) bool {
	return isWordCharacter(a) && isWordCharacter(b) && !isOwnWord(a) && !isOwnWord(b)
}

func isWordCharacter(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsMark(r) || unicode.Is(unicode.Nd, r)
}

// isOwnWord reports whether r is an ideograph or kana, each a word of its own.
func isOwnWord(r rune) bool {
	return unicode.Is(unicode.Ideographic, r) ||
		(r >= 0x3040 && r <= 0x30FF) || (r >= 0x31F0 && r <= 0x31FF) || (r >= 0xFF66 && r <= 0xFF9F)
}

// Following returns the index of the first match that starts at or after
// offset, the first match when none does, or -1 when there are none.
func Following(matches []Range, offset int) int {
	if len(matches) == 0 {
		return -1
	}
	index := sort.Search(len(matches), func(i int) bool { return matches[i].Start >= offset })
	if index == len(matches) {
		return 0
	}
	return index
}

// Label is the count a find bar shows: current is the one-based index of
// the current match, total the matches counted, capped whether more exist,
// and searchedLines the lines a cut-short file arrived with, or 0 when the
// whole file arrived.
func Label(current, total int, capped bool, searchedLines int) string {
	label := "No matches"
	if total > 0 {
		label = grouped(current) + " of " + grouped(total)
		if capped {
			label += "+"
		}
	}
	if searchedLines > 0 {
		label += " in the first " + grouped(searchedLines) + " lines"
	}
	return label
}

// grouped writes n with commas between groups of three digits.
func grouped(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}
