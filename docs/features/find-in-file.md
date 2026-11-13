# Find in a file

A file tab has a find bar: type to search the file, step through the
matches, and see how many there are. It searches what the tab shows: a code
file's text, or a Markdown file's rendered text (its paragraphs, headings,
lists, quotes, callouts, tables, and code blocks; not a callout's title,
diagrams, data figures, or diff fences). A Markdown file shown as source
is searched as source.

Find matches the query as written, not as a regular expression, ignoring
case unless **Match Case** (Aa) is on. **Whole Word** (ab) skips matches
inside a longer word; each Chinese or Japanese character counts as a word
of its own. Matches never overlap and never span lines. The bar counts up
to 10,000 matches and shows more as `10,000+`. A file that arrived cut
short is searched as far as it goes, and the count says so: `3 of 12 in the
first 5,000 lines`.

The match that becomes current is the first one at or after the top of
what's on screen. Typing more of the query keeps it where it is. Stepping
past the last match wraps to the first. Every client matches the same way;
`testdata/find` holds the rules and the cases they are tested against.

## By client

- **macOS:** **Edit › Find › Find…** (⌘F) opens the bar under the file
  tab's header, with the last query selected. Return and **Find Next**
  (⌘G) go to the next match; Shift-Return and **Find Previous** (⇧⌘G) to
  the previous one. **Use Selection for Find** (⌘E) searches for the
  selected code. The commands act on the focused tile's file tab and are
  unavailable elsewhere. Matches on screen are tinted in the accent, the
  current one more strongly; code selects it, and a rendered Markdown file
  fills it and marks its block's margin. Esc or **Done** closes the bar,
  leaving the current match selected and the keyboard in the file; ⌘G then
  reopens it at the next match.
