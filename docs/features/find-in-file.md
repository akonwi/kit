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
- **Browser:** the magnifier in a file's header, or ⌘F (Ctrl+F off Apple
  devices), opens the bar under the header, where it stays while the file
  scrolls; ⌘F again in the bar opens the browser's own find. Return and
  ⌘G go to the next match; Shift-Return and ⇧⌘G to the previous one. ⌘E
  searches for the text selected in the file. A file that arrived cut short
  says how far it was searched under the field. Matches are tinted in the
  accent, the current one filled; in a rendered Markdown file a bar in the
  margin marks its block. A diff fence shows as code in the browser, so it
  is searched there. Esc or the close button closes the bar, leaving the
  keyboard in the file and, with a mouse or trackpad, the current match
  selected; ⌘G then reopens it at the next match. A browser without the
  CSS Custom Highlight API counts and scrolls to matches without tinting
  them.
- **Terminal:** Ctrl+F on a File tab, or **find** in the command palette,
  opens a row under the tab's header and gives it the keyboard; on a File
  tab, Ctrl+F no longer moves the composer's cursor right. Return or ↓ go
  to the next match; Shift-Return or ↑ to the previous one. Alt+C and
  Alt+W toggle **Aa** and **ab**, which can also be clicked, as can the
  arrows and ×. (In a macOS terminal, Alt shortcuts need Option set to act
  as Meta.) A narrow pane drops the label and arrows and counts as `3/12`.
  Matches are tinted, the current one filled with the accent; in the
  source it becomes the cursor line, scrolling sideways into view if need
  be, and in a rendered Markdown file a bar in the gutter marks its block.
  Tabs show as spaces in the terminal and are searched as shown. Esc
  closes the row, keeping the query for next time, and gives the keyboard
  back to the composer.
