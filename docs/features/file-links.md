# File links

A Markdown link in a reply, or in a Markdown file, whose target is a file
path opens that file in Kit. The target can name lines, as GitHub does, so a
link opens the file at them:

| Target                             | Opens          |
| ---------------------------------- | -------------- |
| `src/server.go`                    | the file       |
| `src/server.go#L42`                | line 42        |
| `src/server.go#L42-L50`, `#L42-50` | lines 42 to 50 |

Kit's system prompt asks the model to write them in the target:
`[src/server.go:42](src/server.go#L42)`. Only the target's fragment names
lines. A line in a link's text is just text, and a colon in a target is part
of the file's name, so `src/server.go:42` names a file called
`server.go:42`. Lines that can't be, such as `#L0`, `#L50-L42`, or a line
past 999,999,999, open the whole file. Every client reads targets the same
way; `testdata/file-links` holds the cases they are tested against.

## By client

- **macOS:** a click opens the file's tab beside the conversation, or selects
  it if it is already open, and centres the lines. Code selects them in the
  editor's accent selection until the next click. A rendered Markdown file
  tints the blocks showing them and marks their margin until the next click
  in the tab; a file too large to render shows them in its source. A link to
  lines names its file and lines at the top of its context menu, as
  `server.go#L42-L50`, and offers **Copy Path and Line** beside **Copy
  Path**. Hovering a file link shows its path and lines. Paths in the working
  directory open as workspace files and others as files on the server
  ([ADR 0050](../adrs/0050-view-files-outside-the-working-directory.md)).
- **Browser client:** a link to a file in the session's folder opens the file
  viewer with the lines marked and in view. Files elsewhere stay plain text.
- **iOS:** a tap opens a file in the session's folder with the lines marked
  and in view. Files elsewhere open nothing.
- **Terminal:** file links show their labels but don't open yet; tool rows,
  annotations, and the Diff tab open File tabs at a line.

In every client but the macOS app, a Markdown file opened at lines shows its
source, where lines are marked.
