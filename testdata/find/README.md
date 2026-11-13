# Find in a file

`cases.json` holds the cases every client's find matcher agrees on: the Go
matcher in `internal/textfind` (which the terminal client uses), the Swift
matcher in KitShared's KitClient, and the web client's. Each matcher's tests
read this file.

## Notation

A case's `marked` text is the text searched, with its expected matches
between `⟦` and `⟧`. A `‸` marks an anchor, a position in the text that is
not part of it. Remove the markers to get the text; the positions of the
markers give the expected ranges in whatever units the reading language
uses (UTF-16 code units in Swift and TypeScript, bytes in Go). None of the
cases' texts contain the marker characters themselves.

A case's fields:

- `query`: what was typed.
- `matchCase`, `wholeWord`: the two options, false when absent.
- `limit`: how many matches to count, the default limit when absent.
- `capped`: whether more matches exist past `limit`, false when absent.
- `current`: with an anchor, the index of the match that becomes current.

`labels` gives the count each client shows for a result: `current` is the
one-based index of the current match, `total` the matches counted, `capped`
whether there are more, and `searchedLines` the lines a cut-short file
arrived with (0 when the whole file arrived).

## Rules

- **Text, not regular expressions.** The query is matched as written.
- **Code points.** Text and query are compared one Unicode code point at a
  time. A match can start or end inside a grapheme cluster, such as before a
  combining mark or between the parts of an emoji sequence.
- **No normalization.** A composed `é` (U+00E9) and `e` followed by U+0301
  are different text.
- **Case.** Without Match Case, two code points are equal when Unicode's
  simple case folding (`CaseFolding.txt` statuses C and S) maps them to the
  same code point. So `ς`, `σ` and `Σ` are equal, `ß` and `ẞ` are equal but
  `ß` doesn't match `ss`, and `İ` and `ı` equal only themselves. A match is
  always as many code points long as the query.
- **Lines.** A query that contains a line feed or carriage return matches
  nothing, so no match spans a line break.
- **Whole Word.** A word character is a letter (general category L), a mark
  (M), a decimal digit (Nd), or `_`. Ideographs (the Ideographic property)
  and kana (U+3040–U+30FF, U+31F0–U+31FF, U+FF66–U+FF9F) are each a word of
  their own. Two adjacent code points join when both are word characters
  and neither is an ideograph or kana. With Whole Word on, a match is kept
  unless its first code point joins the one before it or its last code
  point joins the one after it. The start and end of the text join nothing.
- **Order and overlap.** Matching scans left to right. A kept match resumes
  the scan at its end, so matches never overlap; a rejected candidate
  resumes it one code point later.
- **Limit.** Counting stops at the limit, 10,000 by default. A result is
  capped when another match exists past the limit.
- **Current match.** The match that becomes current is the first one that
  starts at or after the anchor, or the first match when none does. A
  client anchors at the start of the current match while the query is
  being edited, and at the top of what's on screen when find opens.
- **Lines searched.** Clients search the text they have. A file that
  arrived cut short (5,000 lines or about 1 MB) is searched as far as it
  goes, and the count says how many lines that was.
